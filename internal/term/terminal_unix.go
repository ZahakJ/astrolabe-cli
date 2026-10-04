//go:build linux || darwin

package term

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	xterm "golang.org/x/term"
)

// Terminal is an interactive terminal session on /dev/tty: raw mode, the
// alternate screen, an input reader producing Events, signal handling
// (resize, suspend/resume, termination) and a Screen to draw on.
//
// Typical use:
//
//	t, err := term.Open(term.OpenOptions{Mouse: true})
//	if err != nil { … }
//	defer t.Close()
//	defer t.RecoverPanic()
//	for ev := range t.Events() { …draw into t.Screen()…; t.Flush() }
//
// Screen, Flush and the mode-changing methods must be used from one
// goroutine (the application's event loop). Events may be read from any.
type Terminal struct {
	// Caps are the detected capabilities.
	Caps Caps

	tty    *os.File
	fd     int
	screen *Screen
	opts   OpenOptions

	mu       sync.Mutex // guards terminal modes and output
	saved    *xterm.State
	active   bool // raw mode and alternate screen entered
	mouse    bool
	closed   bool
	redraw   atomic.Bool // invalidate the screen at the next Flush
	child    atomic.Bool // an external program owns the terminal
	stopping atomic.Bool

	events  chan Event
	ctl     chan ctlMsg
	wakeR   int
	wakeW   int
	ctlMu   sync.Mutex // serialises controlReader
	paused  bool
	readerD chan struct{}

	sigCh   chan os.Signal
	sigDone chan struct{}
	contCh  chan struct{}
	suspend atomic.Bool

	closeOnce sync.Once
}

type ctlKind int

const (
	ctlPause ctlKind = iota
	ctlResume
	ctlStop
)

type ctlMsg struct {
	kind ctlKind
	ack  chan struct{}
}

// Open opens the terminal device, detects capabilities, enters raw mode and
// the alternate screen, enables bracketed paste (and mouse reporting if
// asked) and starts the input reader. Always Close the Terminal; defer
// RecoverPanic as well so a panic restores the terminal before crashing.
func Open(o OpenOptions) (*Terminal, error) {
	path := o.TTYPath
	if path == "" {
		path = "/dev/tty"
	}
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	tty := os.NewFile(uintptr(fd), path)
	if !xterm.IsTerminal(fd) {
		tty.Close()
		return nil, fmt.Errorf("%s is not a terminal", path)
	}
	var p [2]int
	if err := unix.Pipe(p[:]); err != nil {
		tty.Close()
		return nil, err
	}
	unix.CloseOnExec(p[0])
	unix.CloseOnExec(p[1])
	if o.EscTimeout <= 0 {
		o.EscTimeout = DefaultEscTimeout
	}
	det := o.Options
	det.File = tty
	yes := true
	det.TTY = &yes
	t := &Terminal{
		Caps:    Detect(det),
		tty:     tty,
		fd:      fd,
		opts:    o,
		mouse:   o.Mouse,
		events:  make(chan Event, 256),
		ctl:     make(chan ctlMsg),
		wakeR:   p[0],
		wakeW:   p[1],
		readerD: make(chan struct{}),
		sigCh:   make(chan os.Signal, 8),
		sigDone: make(chan struct{}),
		contCh:  make(chan struct{}, 1),
	}
	t.screen = NewScreen(tty, t.Caps.Width, t.Caps.Height, t.Caps.Encoder())
	t.mu.Lock()
	err = t.enterLocked()
	t.mu.Unlock()
	if err != nil {
		tty.Close()
		unix.Close(p[0])
		unix.Close(p[1])
		return nil, err
	}
	// SIGTSTP is deliberately not caught: once Go installs a handler for
	// it, signal.Reset does not restore the default stop action, and
	// Suspend relies on that action. In raw mode Ctrl-Z arrives as a key
	// (the application calls Suspend); an external "kill -TSTP" stops the
	// process directly and SIGCONT then re-asserts the TUI's modes.
	signal.Notify(t.sigCh, unix.SIGWINCH, unix.SIGCONT,
		unix.SIGINT, unix.SIGTERM, unix.SIGHUP, unix.SIGQUIT)
	go t.signalLoop()
	go t.readLoop()
	return t, nil
}

// Screen returns the screen to draw on. Its size follows the terminal once
// the application handles ResizeEvent by calling Screen().Resize.
func (t *Terminal) Screen() *Screen { return t.screen }

// Events returns the input event channel. It is closed when the Terminal
// is closed or input fails (after an ErrorEvent).
func (t *Terminal) Events() <-chan Event { return t.events }

// Size queries the current terminal size.
func (t *Terminal) Size() (w, h int) {
	w, h, err := xterm.GetSize(t.fd)
	if err != nil || w <= 0 || h <= 0 {
		return t.screen.Size()
	}
	return w, h
}

// Flush draws the screen's changes on the terminal (see Screen.Flush).
// After a resume or an external program it redraws everything.
func (t *Terminal) Flush() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active {
		return nil
	}
	if t.redraw.Swap(false) {
		t.screen.Invalidate()
	}
	return t.screen.Flush()
}

// Write writes raw bytes to the terminal (for sequences the Screen does not
// produce). Prefer Screen drawing.
func (t *Terminal) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.tty.Write(p)
}

// SetMouse turns mouse reporting on or off (mouse=off keeps the terminal's
// own text selection working).
func (t *Terminal) SetMouse(on bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.mouse == on {
		return
	}
	t.mouse = on
	if t.active {
		if on {
			t.tty.WriteString(SeqMouseOn)
		} else {
			t.tty.WriteString(SeqMouseOff)
		}
	}
}

// Copy puts text on the system clipboard with OSC 52. It returns
// ErrClipboardUnsupported when the terminal is not believed to support OSC 52; the
// sequence is still sent, since the guess may be wrong and it is harmless.
func (t *Terminal) Copy(text string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, err := t.tty.WriteString(OSC52(text)); err != nil {
		return err
	}
	if !t.Caps.Clipboard {
		return ErrClipboardUnsupported
	}
	return nil
}

// enterLocked enters raw mode and the alternate screen. t.mu must be held.
func (t *Terminal) enterLocked() error {
	if t.active || t.closed {
		return nil
	}
	st, err := xterm.MakeRaw(t.fd)
	if err != nil {
		return fmt.Errorf("raw mode: %w", err)
	}
	if t.saved == nil {
		t.saved = st
	}
	seq := SeqAltScreenOn + SeqAutowrapOff + SeqBracketedPasteOn + SeqCursorHide
	if t.mouse {
		seq += SeqMouseOn
	}
	if t.opts.KittyKeyboard {
		seq += SeqKittyKeysOn
	}
	if t.opts.FocusEvents {
		seq += "\x1b[?1004h"
	}
	t.tty.WriteString(seq)
	t.active = true
	t.redraw.Store(true)
	return nil
}

// leaveLocked restores the terminal: modes off, main screen, cooked mode.
// t.mu must be held.
func (t *Terminal) leaveLocked() {
	if !t.active {
		return
	}
	seq := ""
	if t.opts.FocusEvents {
		seq += "\x1b[?1004l"
	}
	if t.opts.KittyKeyboard {
		seq += SeqKittyKeysOff
	}
	if t.mouse {
		seq += SeqMouseOff
	}
	seq += SeqBracketedPasteOff + SeqAutowrapOn + CursorDefault.seq() + Reset + SeqCursorShow + SeqAltScreenOff
	t.tty.WriteString(seq)
	if t.saved != nil {
		xterm.Restore(t.fd, t.saved)
	}
	t.active = false
}

// Restore puts the terminal back in its original state without closing the
// session (Close does this too). Safe to call more than once.
func (t *Terminal) Restore() {
	t.mu.Lock()
	t.leaveLocked()
	t.mu.Unlock()
}

// RecoverPanic restores the terminal if the calling goroutine is panicking,
// then re-panics so the stack trace prints on a sane terminal. Use it as
// `defer t.RecoverPanic()` in every goroutine that draws.
func (t *Terminal) RecoverPanic() {
	if r := recover(); r != nil {
		t.Close()
		panic(r)
	}
}

// Close stops input, restores the terminal and releases the device. It is
// safe to call more than once.
func (t *Terminal) Close() error {
	t.closeOnce.Do(func() {
		t.stopping.Store(true)
		signal.Stop(t.sigCh)
		close(t.sigDone)
		t.controlReader(ctlStop)
		<-t.readerD
		t.mu.Lock()
		t.leaveLocked()
		t.closed = true
		t.mu.Unlock()
		t.tty.Close()
		unix.Close(t.wakeR)
		unix.Close(t.wakeW)
	})
	return nil
}

// controlReader sends a control message to the reader goroutine and waits
// for it to be handled. A running reader is woken through the pipe; a
// paused reader is already waiting on the channel.
func (t *Terminal) controlReader(k ctlKind) {
	t.ctlMu.Lock()
	defer t.ctlMu.Unlock()
	select {
	case <-t.readerD:
		return // reader already gone
	default:
	}
	ack := make(chan struct{})
	if !t.paused {
		unix.Write(t.wakeW, []byte{1})
	}
	select {
	case t.ctl <- ctlMsg{kind: k, ack: ack}:
		<-ack
	case <-t.readerD:
		return
	}
	switch k {
	case ctlPause:
		t.paused = true
	case ctlResume:
		t.paused = false
	}
}

// Suspend restores the terminal and stops the process group with SIGTSTP
// (what Ctrl-Z does in a cooked terminal); when the shell resumes it, the
// terminal is re-entered and the next Flush redraws everything. A
// ResizeEvent is queued since the size may have changed meanwhile.
func (t *Terminal) Suspend() error {
	t.controlReader(ctlPause)
	t.mu.Lock()
	t.leaveLocked()
	t.mu.Unlock()

	t.suspend.Store(true)
	// Drain a stale continue notification.
	select {
	case <-t.contCh:
	default:
	}
	err := unix.Kill(0, unix.SIGTSTP)
	// If the process group is orphaned SIGTSTP is discarded and we were
	// never stopped; either way carry on once continued or shortly after.
	select {
	case <-t.contCh:
	case <-time.After(50 * time.Millisecond):
	}
	t.suspend.Store(false)

	t.mu.Lock()
	eerr := t.enterLocked()
	t.mu.Unlock()
	t.controlReader(ctlResume)
	t.queueResize()
	if err != nil {
		return err
	}
	return eerr
}

// RunExternal runs cmd (typically EditorCommand) with the terminal restored
// to cooked mode on the main screen, then re-enters the TUI; the next Flush
// redraws everything. The input reader is paused meanwhile so the program
// gets every keystroke. Unset Stdin, Stdout and Stderr are connected to the
// terminal device (so it works when folio's own stdout is a pipe).
func (t *Terminal) RunExternal(cmd *exec.Cmd) error {
	t.controlReader(ctlPause)
	t.mu.Lock()
	t.leaveLocked()
	t.mu.Unlock()
	if cmd.Stdin == nil {
		cmd.Stdin = t.tty
	}
	if cmd.Stdout == nil {
		cmd.Stdout = t.tty
	}
	if cmd.Stderr == nil {
		cmd.Stderr = t.tty
	}
	t.child.Store(true)
	err := cmd.Run()
	t.child.Store(false)
	t.mu.Lock()
	eerr := t.enterLocked()
	t.mu.Unlock()
	t.controlReader(ctlResume)
	t.queueResize()
	if err != nil {
		return err
	}
	return eerr
}

func (t *Terminal) queueResize() {
	w, h := t.Size()
	t.sendNonBlocking(ResizeEvent{Width: w, Height: h})
}

func (t *Terminal) sendNonBlocking(ev Event) {
	select {
	case t.events <- ev:
	default:
	}
}

// signalLoop handles resize, job control and termination signals.
func (t *Terminal) signalLoop() {
	for {
		select {
		case <-t.sigDone:
			return
		case sig := <-t.sigCh:
			switch sig {
			case unix.SIGWINCH:
				w, h := t.Size()
				select {
				case t.events <- ResizeEvent{Width: w, Height: h}:
				case <-t.sigDone:
					return
				}
			case unix.SIGCONT:
				if t.suspend.Load() {
					select {
					case t.contCh <- struct{}{}:
					default:
					}
					continue
				}
				// Stopped by someone else (SIGSTOP): the shell may have
				// reset the terminal; re-assert our modes.
				t.mu.Lock()
				if t.active && t.saved != nil {
					xterm.MakeRaw(t.fd)
					t.tty.WriteString(SeqAltScreenOn + SeqAutowrapOff + SeqBracketedPasteOn)
					if t.mouse {
						t.tty.WriteString(SeqMouseOn)
					}
					t.redraw.Store(true)
				}
				t.mu.Unlock()
				t.queueResize()
			case unix.SIGINT, unix.SIGQUIT:
				if t.child.Load() {
					continue // meant for the external program
				}
				t.die(sig)
			case unix.SIGTERM, unix.SIGHUP:
				t.die(sig)
			}
		}
	}
}

// die runs the OnSignal hook (bounded by SignalHookTimeout), restores the
// terminal and re-raises sig with its default action.
func (t *Terminal) die(sig os.Signal) {
	if hook := t.opts.OnSignal; hook != nil {
		done := make(chan struct{})
		go func() {
			defer close(done)
			defer func() { _ = recover() }() // a failing hook must not stop the exit
			hook(sig)
		}()
		select {
		case <-done:
		case <-time.After(SignalHookTimeout):
		}
	}
	t.mu.Lock()
	t.leaveLocked()
	t.mu.Unlock()
	signal.Reset(sig)
	if s, ok := sig.(syscall.Signal); ok {
		unix.Kill(os.Getpid(), s)
	}
	time.Sleep(100 * time.Millisecond)
	os.Exit(128 + int(sig.(syscall.Signal)))
}

// readLoop reads the terminal, decodes events and resolves lone ESC after
// the escape timeout.
func (t *Terminal) readLoop() {
	defer close(t.readerD)
	defer close(t.events)
	defer func() {
		if r := recover(); r != nil {
			t.Restore()
			panic(r)
		}
	}()
	buf := make([]byte, 4096)
	var pending []byte
	for {
		timeout := time.Duration(-1)
		if len(pending) > 0 {
			timeout = t.opts.EscTimeout
		}
		ttyReady, wake, err := waitReadable(t.fd, t.wakeR, timeout)
		if err != nil {
			t.sendFinal(ErrorEvent{Err: err})
			return
		}
		if wake {
			var b [1]byte
			unix.Read(t.wakeR, b[:])
			if !t.handleCtl(<-t.ctl) {
				return
			}
			continue
		}
		if !ttyReady {
			// Escape timeout: resolve whatever is pending.
			var ok bool
			if pending, ok = t.decodeAll(pending, true); !ok {
				return
			}
			continue
		}
		n, err := unix.Read(t.fd, buf)
		if err == unix.EINTR || err == unix.EAGAIN {
			continue
		}
		if err != nil || n == 0 {
			if err == nil {
				err = errors.New("terminal closed")
			}
			t.sendFinal(ErrorEvent{Err: err})
			return
		}
		pending = append(pending, buf[:n]...)
		var ok bool
		if pending, ok = t.decodeAll(pending, false); !ok {
			return
		}
	}
}

// decodeAll emits every complete event in buf and returns the remainder.
// ok is false when the reader must stop.
func (t *Terminal) decodeAll(buf []byte, final bool) ([]byte, bool) {
	for len(buf) > 0 {
		ev, n := Decode(buf, final)
		if n == 0 {
			break
		}
		buf = buf[n:]
		if ev != nil && !t.send(ev) {
			return nil, false
		}
	}
	if len(buf) == 0 {
		return buf[:0], true
	}
	return append([]byte(nil), buf...), true
}

// send delivers ev, serving control messages while the channel is full so a
// pause request cannot deadlock against a busy application.
func (t *Terminal) send(ev Event) bool {
	for {
		select {
		case t.events <- ev:
			return true
		case m := <-t.ctl:
			var b [1]byte
			unix.Read(t.wakeR, b[:]) // the wake byte that came with m
			if !t.handleCtl(m) {
				return false
			}
		}
	}
}

func (t *Terminal) sendFinal(ev Event) {
	if t.stopping.Load() {
		return
	}
	select {
	case t.events <- ev:
	case m := <-t.ctl:
		close(m.ack)
	}
}

// handleCtl processes a control message; while paused it blocks until
// resumed or stopped. It returns false when the reader must exit.
func (t *Terminal) handleCtl(m ctlMsg) bool {
	close(m.ack)
	switch m.kind {
	case ctlStop:
		return false
	case ctlPause:
		for {
			m2 := <-t.ctl
			close(m2.ack)
			switch m2.kind {
			case ctlStop:
				return false
			case ctlResume:
				return true
			}
		}
	}
	return true
}

// waitReadable waits until fd or wake is readable, or timeout elapses
// (negative = forever). select(2) is used rather than poll(2) because macOS
// poll does not support terminal devices.
func waitReadable(fd, wake int, timeout time.Duration) (fdReady, wakeReady bool, err error) {
	for {
		var rset unix.FdSet
		rset.Zero()
		rset.Set(fd)
		rset.Set(wake)
		var tv *unix.Timeval
		if timeout >= 0 {
			v := unix.NsecToTimeval(timeout.Nanoseconds())
			tv = &v
		}
		_, err := unix.Select(max(fd, wake)+1, &rset, nil, nil, tv)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return false, false, err
		}
		return rset.IsSet(fd), rset.IsSet(wake), nil
	}
}
