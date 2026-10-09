package tui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZahakJ/astrolabe-cli/internal/cli"
	"github.com/ZahakJ/astrolabe-cli/internal/render"
	"github.com/ZahakJ/astrolabe-cli/internal/term"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
	"github.com/ZahakJ/astrolabe-cli/internal/vault"
)

// processStart approximates the process start time (package initialisation
// runs before main), for the cold-start measurement printed with
// ASTROLABE_DEBUG=1.
var processStart = time.Now()

// resolver connects internal/render to the vault (render does not import
// vault).
type resolver struct {
	v    *vault.Vault
	from string // vault-relative path of the note being rendered ("" = root)
	// lenient reports links that cannot be resolved yet as resolved while
	// the vault scan is still running (the TUI's first paint).
	lenient bool
}

// Resolve implements render.Resolver.
func (r *resolver) Resolve(q render.LinkQuery) (string, bool) {
	if r.v == nil {
		return "", false
	}
	kind := vault.KindWikilink
	switch {
	case q.Embed:
		kind = vault.KindEmbed
	case !q.Wiki:
		kind = vault.KindMarkdown
	}
	t, ok := r.v.ResolveLink(r.from, vault.Link{
		Kind: kind, Target: q.Target, Heading: q.Heading, Block: q.Block,
		Wiki: q.Wiki, Embed: q.Embed,
	})
	if !ok {
		// While the scan runs, a link to a note not indexed yet is not
		// known to be broken: show it neutral (path unknown) rather than
		// flashing it red; the page is re-rendered when the scan ends.
		if r.lenient && !r.v.Scanned() && !q.Embed {
			return "", true
		}
		return "", false
	}
	return t.Path, true
}

// embedLimit caps how much of an embedded note is parsed for its preview.
const embedLimit = 32 << 10

// Embed implements render.Resolver.
func (r *resolver) Embed(p string) (title, source string, ok bool) {
	if r.v == nil {
		return "", "", false
	}
	b, _, err := r.v.ReadNote(p)
	if err != nil || binaryContent(b) {
		return "", "", false
	}
	if len(b) > embedLimit {
		cut := embedLimit
		for cut < len(b) && b[cut] != '\n' {
			cut++
		}
		b = b[:cut]
	}
	title = noteName(p)
	if n, ok := r.v.Note(p); ok && n.Title != "" {
		title = n.Title
	}
	return title, string(b), true
}

// Run is the cli.Hooks.RunTUI entry point: it opens /dev/tty, shows what the
// request asks for (DESIGN.md §4.1) and runs the event loop until the user
// quits. The terminal is restored on every exit path, including panics and
// termination signals (handled by internal/term).
func Run(ctx context.Context, req cli.TUIRequest) (cli.TUIResult, error) {
	if req.Vault == nil {
		return cli.TUIResult{}, fmt.Errorf("no vault")
	}
	topts := req.Display.Term
	topts.TTY, topts.File = nil, nil
	mouse := req.Display.Mouse
	// A termination signal (the terminal window closed: SIGHUP; SIGTERM)
	// asks the event loop to save a dirty editor buffer before internal/term
	// restores the terminal and re-raises the signal. If the loop has
	// already ended, finish has done the same.
	sigReq := make(chan chan struct{})
	runDone := make(chan struct{})
	onSignal := func(os.Signal) {
		done := make(chan struct{})
		select {
		case sigReq <- done:
			select {
			case <-done:
			case <-runDone:
			}
		case <-runDone:
		}
	}
	t, err := term.Open(term.OpenOptions{Options: topts, Mouse: mouse, OnSignal: onSignal})
	if err != nil {
		return cli.TUIResult{}, fmt.Errorf("cannot open the terminal: %w", err)
	}
	var (
		res        cli.TUIResult
		runErr     error
		firstFrame time.Duration
	)
	func() {
		defer close(runDone)
		defer t.Close()
		defer t.RecoverPanic()
		caps := t.Caps
		// Glyphs follow the terminal's own locale (req.Display was
		// detected on stdout, which may be a pipe).
		gl := theme.GlyphsFor(!caps.UTF8 || topts.ASCII)
		cwd, _ := os.Getwd()
		home := req.Home
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		configFile := ""
		if req.Config != nil {
			configFile = req.Config.File
		}
		if configFile == "" {
			configFile = vault.ConfigFile(topts.Getenv)
		}
		a := newApp(config{
			Vault:        req.Vault,
			Screen:       t.Screen(),
			Host:         t,
			Profile:      caps.Profile,
			Theme:        req.Display.Theme,
			Glyphs:       gl,
			Ground:       req.Display.Ground,
			Bidi:         caps.Bidi,
			Measure:      req.Display.Measure,
			Mouse:        mouse,
			External:     req.Display.Editor == "external",
			ConfigFile:   configFile,
			RecentFile:   req.RecentFile,
			RecoveredDir: recoveredDir(req.RecentFile),
			Cwd:          cwd,
			Home:         home,
			Debug:        os.Getenv("ASTROLABE_DEBUG") == "1",
		})
		a.sigReq = sigReq
		if w, h := t.Size(); w > 0 && h > 0 {
			a.resize(w, h)
		}
		a.start(req)
		if req.Mode != cli.ModePick {
			a.announceRecovered()
		}
		a.draw()
		if err := t.Flush(); err != nil {
			runErr = err
			return
		}
		firstFrame = time.Since(processStart)
		a.loop(ctx, t.Events(), t.Flush)
		a.finish()
		res, runErr = a.result, a.err
	}()
	if os.Getenv("ASTROLABE_DEBUG") == "1" {
		fmt.Fprintf(os.Stderr, "astrolabe: first frame %.1f ms after start\n", float64(firstFrame.Microseconds())/1000)
	}
	return res, runErr
}

// start opens what the request asks for.
func (a *app) start(req cli.TUIRequest) {
	switch req.Mode {
	case cli.ModePick:
		a.view = viewPick
		a.pushOverlay(newPicker(a, req.Query))
		return
	case cli.ModeStdin:
		d := &docView{stdin: true, name: "stdin", focus: -1}
		d.setSource(req.Source)
		a.show(d, -1, -1)
		return
	case cli.ModeEdit:
		if req.Path != "" {
			a.firstEdit = req.Path
			line := req.Line
			if line <= 0 {
				line = -1
			}
			a.openEditorOn(req.Path, line)
			if a.view == viewEditor {
				if req.Line <= 0 {
					a.startWriting() // astrolabe new -o: a fresh note
				}
				return
			}
		}
	}
	if req.Path == "" {
		a.view = viewHome
		return
	}
	if !a.openNote(req.Path, req.Line) {
		a.view = viewHome
	}
	a.back = nil
}

// loop runs until the user quits: it waits for input, background results,
// the end of the scan or a timer, handles everything that is ready, then
// draws one frame (so bursts of events, resize storms included, cost one
// redraw).
func (a *app) loop(ctx context.Context, events <-chan term.Event, flush func() error) {
	scanDone := a.v.Done()
	if a.v.Scanned() {
		scanDone = nil
		a.scanFinished()
	}
	var timer *time.Timer
	poll := time.NewTicker(250 * time.Millisecond)
	defer poll.Stop()
	for !a.quit {
		var timerC <-chan time.Time
		if d := a.deadline(); !d.IsZero() {
			wait := time.Until(d)
			if wait < 0 {
				wait = 0
			}
			if timer == nil {
				timer = time.NewTimer(wait)
			} else {
				timer.Reset(wait)
			}
			timerC = timer.C
		}
		var pollC <-chan time.Time
		if !a.scanDone {
			pollC = poll.C // refresh counts while the scan runs
		}
		select {
		case <-ctx.Done():
			a.quit = true
		case ev, ok := <-events:
			if !ok {
				a.quit = true
				break
			}
			a.handle(ev)
		case fn := <-a.async:
			fn()
		case done := <-a.sigReq:
			a.emergencySave()
			a.quit = true
			close(done)
		case <-scanDone:
			scanDone = nil
			a.scanFinished()
		case <-timerC:
		case <-pollC:
		}
		if timer != nil && timerC != nil && !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		a.tick(a.now())
		// Drain whatever else is already waiting before drawing.
	drain:
		for i := 0; i < 1024 && !a.quit; i++ {
			select {
			case ev, ok := <-events:
				if !ok {
					a.quit = true
					break drain
				}
				a.handle(ev)
			case fn := <-a.async:
				fn()
			default:
				break drain
			}
		}
		if a.quit {
			break
		}
		a.draw()
		if err := flush(); err != nil {
			a.err = err
			return
		}
	}
	// Cancel background searches and wait for them, so no goroutine
	// outlives the run.
	for _, o := range a.overlays {
		if vs, ok := o.(*vaultSearch); ok {
			vs.stop()
		}
	}
	for a.jobs > 0 {
		(<-a.async)()
	}
}

// finish saves the recent-notes state.
func (a *app) finish() {
	a.overlays = nil // no close hooks: the run is over
	if a.edit != nil && a.edit.ed.Dirty() {
		if a.discard {
			// Quit was confirmed with unsaved edits: they are dropped.
			a.edit = nil
		} else {
			// Any other way out (the terminal went away, a signal, a
			// cancelled context) keeps them.
			a.emergencySave()
		}
	}
	a.remember()
	if a.recent != nil {
		_ = a.recent.Save()
	}
}

// recoveredDir puts recovered buffers next to the recent-notes file
// ($XDG_STATE_HOME/astrolabe/recovered).
func recoveredDir(recentFile string) string {
	if recentFile == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(recentFile), "recovered")
}

// emergencySave keeps a dirty editor buffer when astrolabe is about to end
// without the user's say (SIGHUP, SIGTERM, the terminal gone): it writes
// the note if the file is unchanged on disk since it was read, otherwise
// (or if that write fails) it writes the buffer to RecoveredDir, which the
// next start announces. Nothing is drawn: the terminal may be gone.
func (a *app) emergencySave() {
	es := a.edit
	if es == nil || !es.ed.Dirty() {
		return
	}
	data := es.ed.Text()
	if es.kept != nil && bytes.Equal(es.kept, data) {
		return // already saved or recovered as it is
	}
	tok, err := a.v.WriteNote(es.doc.path, data, &es.tok)
	if err == nil {
		es.tok = tok
		es.doc.tok = tok
		es.ed.MarkSaved()
		es.kept = data
		return
	}
	if a.cfg.RecoveredDir == "" {
		return
	}
	if _, err := vault.WriteRecovered(a.cfg.RecoveredDir, es.doc.path, data, a.now()); err == nil {
		es.kept = data // do not write it twice
	}
}

// announceRecovered shows, once, that an earlier run left recovered
// buffers.
func (a *app) announceRecovered() {
	if a.cfg.RecoveredDir == "" {
		return
	}
	fs := vault.UnseenRecovered(a.cfg.RecoveredDir)
	if len(fs) == 0 {
		return
	}
	more := ""
	if len(fs) > 1 {
		more = sprintf(" (+%d more)", len(fs)-1)
	}
	p := fs[0]
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home+string(filepath.Separator)) {
		p = "~" + p[len(home):]
	}
	a.msg = message{text: "unsaved edits were recovered to " + p + more, err: true, until: a.now().Add(3 * messageTime)}
}
