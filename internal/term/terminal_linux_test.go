//go:build linux

package term

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ZahakJ/astrolabe-cli/internal/term/termtest"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
	"golang.org/x/sys/unix"
)

// openPTY creates a pseudo-terminal pair and returns the master and the
// slave's path.
func openPTY(t *testing.T) (*os.File, string) {
	t.Helper()
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Skipf("unlockpt: %v", err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Skipf("ptsname: %v", err)
	}
	unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{Row: 10, Col: 40})
	master := os.NewFile(uintptr(fd), "ptmx")
	t.Cleanup(func() { master.Close() })
	return master, fmt.Sprintf("/dev/pts/%d", n)
}

type ptyHarness struct {
	t      *testing.T
	master *os.File
	vt     *termtest.VT
	term   *Terminal
	slave  string
}

func newHarness(t *testing.T, o OpenOptions) *ptyHarness {
	master, slave := openPTY(t)
	vt := termtest.New(40, 10)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				vt.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	o.TTYPath = slave
	o.Color = "truecolor"
	if o.EscTimeout == 0 {
		// Wider than the default so a slow shared CI runner cannot
		// stretch the pause inside a split sequence past it.
		o.EscTimeout = testEscTimeout
	}
	tm, err := Open(o)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { tm.Close() })
	return &ptyHarness{t: t, master: master, vt: vt, term: tm, slave: slave}
}

func (h *ptyHarness) send(s string) {
	h.t.Helper()
	if _, err := h.master.WriteString(s); err != nil {
		h.t.Fatal(err)
	}
}

func (h *ptyHarness) next() Event {
	h.t.Helper()
	select {
	case ev := <-h.term.Events():
		return ev
	case <-time.After(waitLimit):
		h.t.Fatal("timed out waiting for an event")
	}
	return nil
}

// Timing: deadlines are generous (a loaded CI runner can stall a goroutine
// for a long time) and every wait polls, so a fast machine never waits them
// out.
const (
	waitLimit      = 10 * time.Second
	testEscTimeout = 150 * time.Millisecond
)

// eventually polls cond for up to waitLimit.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitLimit)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func termiosOf(t *testing.T, path string) *unix.Termios {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	tio, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	return tio
}

func TestTerminalSession(t *testing.T) {
	h := newHarness(t, OpenOptions{Mouse: true})
	tm := h.term
	if w, ht := tm.Size(); w != 40 || ht != 10 {
		t.Errorf("size = %dx%d", w, ht)
	}
	eventually(t, "alternate screen", func() bool { return h.vt.AltScreen() })
	if !h.vt.Mode(2004) || !h.vt.Mode(1006) || !h.vt.Mode(1000) || h.vt.Mode(7) {
		t.Errorf("modes: paste=%v sgr-mouse=%v mouse=%v autowrap=%v", h.vt.Mode(2004), h.vt.Mode(1006), h.vt.Mode(1000), h.vt.Mode(7))
	}
	if tio := termiosOf(t, h.slave); tio.Lflag&unix.ICANON != 0 || tio.Lflag&unix.ECHO != 0 {
		t.Error("not in raw mode")
	}

	// Drawing.
	sc := tm.Screen()
	sc.PutString(1, 1, "astrolabe ✦ 日本", theme.Style{FG: theme.Hex("#c9a227")})
	sc.SetCursor(3, 4)
	sc.ShowCursor(true)
	sc.SetCursorShape(CursorBar)
	if err := tm.Flush(); err != nil {
		t.Fatal(err)
	}
	eventually(t, "drawn text", func() bool { return strings.HasPrefix(h.vt.Row(1), " astrolabe ✦ 日本") })
	eventually(t, "cursor", func() bool { x, y := h.vt.Cursor(); return x == 3 && y == 4 && h.vt.CursorShape() == 6 })

	// Keys, including a sequence split across writes.
	h.send("j")
	if ev := h.next(); !reflect.DeepEqual(ev, KeyEvent{Key: KeyRune, Rune: 'j'}) {
		t.Errorf("j = %#v", ev)
	}
	h.send("\x1b[1;")
	time.Sleep(5 * time.Millisecond)
	h.send("5A")
	if ev := h.next(); !reflect.DeepEqual(ev, KeyEvent{Key: KeyUp, Mod: ModCtrl}) {
		t.Errorf("split ctrl-up = %#v", ev)
	}
	// Lone ESC resolves after the timeout; ESC+x in one write is Alt-x.
	start := time.Now()
	h.send("\x1b")
	if ev := h.next(); !reflect.DeepEqual(ev, KeyEvent{Key: KeyEscape}) {
		t.Errorf("esc = %#v", ev)
	}
	if el := time.Since(start); el < testEscTimeout {
		t.Errorf("esc resolved after %v, before the timeout", el)
	}
	h.send("\x1bx")
	if ev := h.next(); !reflect.DeepEqual(ev, KeyEvent{Key: KeyRune, Rune: 'x', Mod: ModAlt}) {
		t.Errorf("alt-x = %#v", ev)
	}
	// Ctrl-C and Ctrl-Z arrive as keys in raw mode (no signals).
	h.send("\x03\x1a")
	if ev := h.next(); !reflect.DeepEqual(ev, KeyEvent{Key: KeyRune, Rune: 'c', Mod: ModCtrl}) {
		t.Errorf("ctrl-c = %#v", ev)
	}
	if ev := h.next(); !reflect.DeepEqual(ev, KeyEvent{Key: KeyRune, Rune: 'z', Mod: ModCtrl}) {
		t.Errorf("ctrl-z = %#v", ev)
	}
	// Paste split across writes.
	h.send("\x1b[200~hello ")
	time.Sleep(testEscTimeout + 50*time.Millisecond) // longer than the escape timeout
	h.send("world\x1b[201~")
	if ev := h.next(); !reflect.DeepEqual(ev, PasteEvent{Text: "hello world"}) {
		t.Errorf("paste = %#v", ev)
	}
	// Mouse.
	h.send("\x1b[<64;5;3M")
	if ev := h.next(); !reflect.DeepEqual(ev, MouseEvent{X: 4, Y: 2, Button: MouseWheelUp}) {
		t.Errorf("wheel = %#v", ev)
	}

	// Resize via SIGWINCH.
	unix.IoctlSetWinsize(int(h.master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 12, Col: 50})
	syscall.Kill(os.Getpid(), syscall.SIGWINCH)
	if ev := h.next(); !reflect.DeepEqual(ev, ResizeEvent{Width: 50, Height: 12}) {
		t.Errorf("resize = %#v", ev)
	}

	// Mouse off at runtime.
	tm.SetMouse(false)
	eventually(t, "mouse off", func() bool { return !h.vt.Mode(1000) })

	// OSC 52.
	tm.Copy("[[Note]]")
	eventually(t, "clipboard", func() bool { return h.vt.Clipboard() == "[[Note]]" })

	// Close restores everything.
	tm.Close()
	eventually(t, "main screen", func() bool { return !h.vt.AltScreen() })
	if h.vt.Mode(2004) || !h.vt.Mode(7) || !h.vt.CursorVisible() {
		t.Errorf("after close: paste=%v autowrap=%v cursor=%v", h.vt.Mode(2004), h.vt.Mode(7), h.vt.CursorVisible())
	}
	if tio := termiosOf(t, h.slave); tio.Lflag&unix.ICANON == 0 {
		t.Error("cooked mode not restored")
	}
	if _, ok := <-tm.Events(); ok {
		t.Error("events channel not closed")
	}
	tm.Close() // idempotent
}

func TestTerminalRunExternal(t *testing.T) {
	h := newHarness(t, OpenOptions{})
	out := filepath.Join(t.TempDir(), "got")
	// The child reads one line from the terminal: the reader must be paused
	// so it does not steal the keystrokes.
	cmd := exec.Command("/bin/sh", "-c", `read line; printf '%s' "$line" > "$1"`, "sh", out)
	done := make(chan error, 1)
	go func() { done <- h.term.RunExternal(cmd) }()
	eventually(t, "main screen for the child", func() bool { return !h.vt.AltScreen() })
	time.Sleep(20 * time.Millisecond)
	h.send("typed in child\r")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunExternal: %v", err)
		}
	case <-time.After(waitLimit):
		t.Fatal("child did not finish")
	}
	b, _ := os.ReadFile(out)
	if string(b) != "typed in child" {
		t.Errorf("child read %q", b)
	}
	eventually(t, "alternate screen again", func() bool { return h.vt.AltScreen() })
	if tio := termiosOf(t, h.slave); tio.Lflag&unix.ICANON != 0 {
		t.Error("raw mode not re-entered")
	}
	// A resize event is queued and input flows again.
	if _, ok := h.next().(ResizeEvent); !ok {
		t.Error("expected a ResizeEvent after the external program")
	}
	h.send("k")
	if ev := h.next(); !reflect.DeepEqual(ev, KeyEvent{Key: KeyRune, Rune: 'k'}) {
		t.Errorf("after child: %#v", ev)
	}
	// The next Flush redraws everything.
	h.term.Screen().PutString(0, 0, "back", theme.Style{})
	h.term.Flush()
	eventually(t, "redraw", func() bool { return strings.HasPrefix(h.vt.Row(0), "back") })
}

func TestTerminalRecoverPanic(t *testing.T) {
	h := newHarness(t, OpenOptions{})
	eventually(t, "alt screen", func() bool { return h.vt.AltScreen() })
	func() {
		defer func() {
			if r := recover(); r != "boom" {
				t.Errorf("re-panic value = %v", r)
			}
		}()
		defer h.term.RecoverPanic()
		panic("boom")
	}()
	eventually(t, "restored after panic", func() bool { return !h.vt.AltScreen() })
	if tio := termiosOf(t, h.slave); tio.Lflag&unix.ICANON == 0 {
		t.Error("cooked mode not restored after panic")
	}
}

func TestTerminalPauseWhileAppBusy(t *testing.T) {
	// The application is not reading events (it is busy starting an
	// external program); input that fills the channel must not deadlock
	// the pause.
	h := newHarness(t, OpenOptions{})
	for i := 0; i < 300; i++ {
		h.send("x")
	}
	time.Sleep(50 * time.Millisecond)
	done := make(chan error, 1)
	go func() { done <- h.term.RunExternal(exec.Command("/bin/true")) }()
	select {
	case <-done:
	case <-time.After(waitLimit):
		t.Fatal("RunExternal deadlocked with a full event channel")
	}
}

// TestTerminalSignalHook re-runs this test binary as a child that opens a
// pty session with an OnSignal hook and is then sent SIGHUP (the terminal
// window closing): the hook must run before the process dies of the
// signal, and the terminal must be restored.
func TestTerminalSignalHook(t *testing.T) {
	if out := os.Getenv("ASTROLABE_TERM_SIGNAL_CHILD"); out != "" {
		signalChild(t, out)
		return
	}
	master, slave := openPTY(t)
	go func() { // drain the pty so the child never blocks writing to it
		buf := make([]byte, 4096)
		for {
			if _, err := master.Read(buf); err != nil {
				return
			}
		}
	}()
	out := filepath.Join(t.TempDir(), "hook")
	cmd := exec.Command(os.Args[0], "-test.run=^TestTerminalSignalHook$")
	cmd.Env = append(os.Environ(), "ASTROLABE_TERM_SIGNAL_CHILD="+out, "ASTROLABE_TERM_SIGNAL_TTY="+slave)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ready := out + ".ready"
	eventually(t, "child session", func() bool { _, err := os.Stat(ready); return err == nil })
	cmd.Process.Signal(syscall.SIGHUP)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-time.After(waitLimit):
		cmd.Process.Kill()
		t.Fatal("child did not exit on SIGHUP")
	}
	b, _ := os.ReadFile(out)
	if string(b) != "hangup" {
		t.Errorf("hook wrote %q", b)
	}
	ee, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("child exit: %v", err)
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGHUP {
		t.Errorf("child did not die of SIGHUP: %v", err)
	}
	if tio := termiosOf(t, slave); tio.Lflag&unix.ICANON == 0 {
		t.Error("cooked mode not restored after the signal")
	}
}

func signalChild(t *testing.T, out string) {
	tm, err := Open(OpenOptions{TTYPath: os.Getenv("ASTROLABE_TERM_SIGNAL_TTY"), OnSignal: func(sig os.Signal) {
		os.WriteFile(out, []byte(sig.String()), 0o644)
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer tm.Close()
	os.WriteFile(out+".ready", nil, 0o644)
	for range tm.Events() {
	}
	time.Sleep(waitLimit) // the signal ends the process before this
}
