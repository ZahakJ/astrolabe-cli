package tui

import (
	"fmt"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/ZahakJ/folio/internal/cli"
	"github.com/ZahakJ/folio/internal/term"
	"github.com/ZahakJ/folio/internal/text"
	"github.com/ZahakJ/folio/internal/theme"
	"github.com/ZahakJ/folio/internal/vault"
)

// Host is what the application needs from the real terminal beyond drawing
// on its Screen. *term.Terminal implements it; tests use a fake.
type Host interface {
	// Copy puts text on the clipboard (OSC 52).
	Copy(text string) error
	// RunExternal runs a program on the restored terminal ($EDITOR).
	RunExternal(cmd *exec.Cmd) error
	// Suspend stops the process as Ctrl-Z does in a shell.
	Suspend() error
	// SetMouse turns mouse reporting on or off.
	SetMouse(on bool)
}

// config is everything newApp needs; Run fills it from the CLI request and
// the opened terminal, tests fill it directly.
type config struct {
	Vault   *vault.Vault
	Screen  *term.Screen
	Host    Host
	Profile term.Profile
	// Theme is the authored (24-bit) theme; ThemeFor is applied here.
	Theme      theme.Theme
	Glyphs     theme.Glyphs
	Ground     bool
	Bidi       bool
	Measure    int
	Mouse      bool
	External   bool   // config editor = external: i/e/a open $EDITOR
	ConfigFile string // where Space T persists the theme ("" = don't)
	RecentFile string // recent-notes state file ("" = don't remember)
	// RecoveredDir receives unsaved editor buffers that could not be
	// written to their note when folio is terminated ("" = nowhere).
	RecoveredDir string
	Cwd          string // where :export writes by default
	Now          func() time.Time
	// EditorCommand builds the $EDITOR command (nil = term.EditorCommand).
	EditorCommand func(file string, line int) *exec.Cmd
	// Debug shows timings in the finder and search overlays (FOLIO_DEBUG=1).
	Debug bool
}

// Views: what fills the page area.
const (
	viewHome   = iota // no note open: the home screen
	viewReader        // a rendered note
	viewEditor        // the built-in editor
	viewPick          // `folio pick`: the finder alone
)

// message is a transient status-bar message (DESIGN.md §4.2: 3 s).
type message struct {
	text  string
	err   bool
	until time.Time
}

const messageTime = 3 * time.Second

// histEntry is one place in the reader's history.
type histEntry struct {
	path  string // vault-relative; "" with stdin for `folio -`
	stdin bool
	src   int // source line of the cursor (0-based)
	off   int // cursor row minus top row
}

// app is the interactive application. All methods run on the event loop
// goroutine; background work reports back through post.
type app struct {
	cfg  config
	v    *vault.Vault
	scr  *term.Screen
	host Host
	w, h int

	base    theme.Theme // authored theme
	th      theme.Theme // theme for this profile (ThemeFor, ground)
	st      styles
	gl      theme.Glyphs
	profile term.Profile
	ground  bool
	bidi    bool
	measure int
	number  bool // editor line numbers (:set nu), kept across edit sessions
	gen     int  // bumped on every change that invalidates rendered pages
	now     func() time.Time

	view     int
	doc      *docView
	stdinDoc *docView
	back     []histEntry
	fwd      []histEntry
	folds    map[string]map[string]bool // fold states per note path
	edit     *editSession
	overlays []overlay
	keys     keyState
	msg      message
	prompt   *cmdPrompt
	ctx      contextPanel
	tree     treePanel
	home     homeState
	recent   *vault.Recent

	quit   bool
	result cli.TUIResult
	err    error

	// discard is set when the user confirmed quitting with unsaved edits;
	// any other way out with a dirty buffer saves it (emergencySave).
	discard bool
	// sigReq carries termination-signal requests to the event loop: it
	// saves a dirty buffer, closes the channel it received and quits.
	sigReq chan chan struct{}

	async     chan func()
	jobs      int
	scanDone  bool
	lastLen   int // vault size at the last draw (to refresh lists while scanning)
	firstEdit string
}

func newApp(c config) *app {
	a := &app{
		cfg:     c,
		v:       c.Vault,
		scr:     c.Screen,
		host:    c.Host,
		base:    c.Theme,
		gl:      c.Glyphs,
		profile: c.Profile,
		ground:  c.Ground,
		bidi:    c.Bidi,
		measure: c.Measure,
		now:     c.Now,
		folds:   map[string]map[string]bool{},
		async:   make(chan func(), 64),
	}
	if a.now == nil {
		a.now = time.Now
	}
	if a.measure <= 0 {
		a.measure = 78
	}
	if a.gl.Name == "" {
		a.gl = theme.UnicodeGlyphs
	}
	if a.cfg.EditorCommand == nil {
		a.cfg.EditorCommand = term.EditorCommand
	}
	a.w, a.h = a.scr.Size()
	a.applyTheme()
	if c.RecentFile != "" {
		a.recent = vault.LoadRecent(c.RecentFile)
	}
	a.ctx.auto = true
	return a
}

// applyTheme derives the drawing theme from the authored one.
func (a *app) applyTheme() {
	th := term.ThemeFor(a.base, a.profile)
	if !a.ground {
		th = th.WithoutGround()
	}
	a.th = th
	a.st = makeStyles(th)
	a.gen++
	if a.edit != nil {
		a.edit.ed.SetTheme(a.th, a.gl)
	}
}

// goAsync runs work in the background and apply on the loop afterwards.
func (a *app) goAsync(work func() func()) {
	a.jobs++
	go func() {
		var apply func()
		func() {
			// A panic in background work must not kill the process with
			// the terminal still in raw mode: report it instead.
			defer func() {
				if r := recover(); r != nil {
					msg := fmt.Sprint(r)
					apply = func() { a.flashErr("internal error: %s", msg) }
				}
			}()
			apply = work()
		}()
		a.async <- func() {
			a.jobs--
			if apply != nil {
				apply()
			}
		}
	}()
}

// flash shows a transient message in the status bar.
func (a *app) flash(format string, args ...any) {
	a.msg = message{text: sprintf(format, args...), until: a.now().Add(messageTime)}
}

// flashErr shows a transient error message.
func (a *app) flashErr(format string, args ...any) {
	a.msg = message{text: sprintf(format, args...), err: true, until: a.now().Add(messageTime)}
}

// --- layout -------------------------------------------------------------------

// layout is where things go for the current size and panel states.
type layout struct {
	main              term.Rect // everything above the status bar
	page              term.Rect // the page pane
	tree, ctx         term.Rect // zero when hidden
	treeOver, ctxOver bool      // drawn over the page (narrow windows)
	status            int       // status bar row
}

const (
	minW, minH   = 40, 10
	treeWidth    = 30
	ctxWidth     = 36
	minPageWidth = 50
	ctxAutoWidth = 124
)

func (a *app) tooSmall() bool { return a.w < minW || a.h < minH }

// ctxOpen reports whether the context panel is shown: by default only at
// ≥ 124 columns, unless the user toggled it (DESIGN.md §4.2).
func (a *app) ctxOpen() bool {
	if a.view != viewReader || a.doc == nil {
		return false
	}
	if a.ctx.auto {
		return a.w >= ctxAutoWidth
	}
	return a.ctx.open
}

func (a *app) treeOpen() bool {
	return a.tree.open && (a.view == viewReader || a.view == viewHome)
}

func (a *app) layout() layout {
	var l layout
	l.status = a.h - 1
	l.main = term.Rect{X: 0, Y: 0, W: a.w, H: a.h - 1}
	l.page = l.main
	tw, cw := 0, 0
	if a.treeOpen() {
		tw = min(treeWidth, a.w/2)
	}
	if a.ctxOpen() {
		cw = min(ctxWidth, a.w/2)
	}
	if tw+cw == 0 {
		return l
	}
	// Dock the panels when the page keeps at least 50 columns; otherwise
	// they overlay the page instead of squeezing it.
	dock := a.w-tw-cw-boolInt(tw > 0)-boolInt(cw > 0) >= minPageWidth
	if tw > 0 {
		l.tree = term.Rect{X: 0, Y: 0, W: tw, H: l.main.H}
		l.treeOver = !dock
	}
	if cw > 0 {
		l.ctx = term.Rect{X: a.w - cw, Y: 0, W: cw, H: l.main.H}
		l.ctxOver = !dock
	}
	if dock {
		x := 0
		if tw > 0 {
			x = tw + 1
		}
		right := a.w
		if cw > 0 {
			right = a.w - cw - 1
		}
		l.page = term.Rect{X: x, Y: 0, W: right - x, H: l.main.H}
	}
	return l
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// --- drawing ------------------------------------------------------------------

// draw paints one complete frame into the screen buffer (Flush sends the
// difference).
func (a *app) draw() {
	s := a.scr
	s.Clear(a.st.base)
	s.ShowCursor(false)
	s.SetCursorShape(term.CursorDefault)
	if a.tooSmall() {
		msg := "window too small"
		if a.w < text.Width(msg) {
			msg = "too small"
		}
		x := max(0, (a.w-text.Width(msg))/2)
		s.PutStringClip(x, a.h/2, msg, a.st.muted, a.w)
		return
	}
	l := a.layout()
	switch a.view {
	case viewHome:
		a.drawHome(l.page)
	case viewReader:
		a.drawReader(l.page)
	case viewEditor:
		a.drawEditor(l.page)
	case viewPick:
	}
	if l.tree.W > 0 {
		a.drawTree(l.tree, l.treeOver)
	}
	if l.ctx.W > 0 {
		a.drawContext(l.ctx, l.ctxOver)
	}
	if a.view != viewPick {
		a.drawStatus(l.status)
	}
	for i, o := range a.overlays {
		if i == 0 {
			if _, light := o.(*palette); !light && a.view != viewPick {
				a.dimBehind()
			}
		}
		s.ShowCursor(false)
		o.draw(a)
	}
	a.lastLen = a.v.Len()
}

// --- events -------------------------------------------------------------------

// handle processes one input event.
func (a *app) handle(ev term.Event) {
	switch e := ev.(type) {
	case term.KeyEvent:
		a.handleKey(e)
	case term.PasteEvent:
		a.handlePaste(e.Text)
	case term.MouseEvent:
		a.handleMouse(e)
	case term.ResizeEvent:
		a.resize(e.Width, e.Height)
	case term.ErrorEvent:
		a.quit = true
	}
}

func (a *app) resize(w, h int) {
	if w <= 0 || h <= 0 || (w == a.w && h == a.h) {
		return
	}
	a.w, a.h = w, h
	a.scr.Resize(w, h)
}

func (a *app) handleKey(k term.KeyEvent) {
	ks := k.String()
	if ks == "ctrl+z" {
		if err := a.host.Suspend(); err != nil {
			a.flashErr("suspend: %v", err)
		}
		return
	}
	if a.tooSmall() {
		// Only the ways out work in a window too small to draw in.
		if ks == "ctrl+c" && a.view == viewPick {
			a.cancelPick()
		}
		if (ks == "q" || ks == "esc") && len(a.overlays) == 0 && a.view != viewEditor {
			if a.view == viewPick {
				a.cancelPick()
			} else {
				a.requestQuit()
			}
		}
		return
	}
	if n := len(a.overlays); n > 0 {
		top := a.overlays[n-1]
		if ks == "esc" || (ks == "ctrl+c" && a.view != viewEditor) {
			a.closeOverlay()
			return
		}
		top.key(a, k)
		return
	}
	if a.prompt != nil {
		a.promptKey(k)
		return
	}
	switch a.view {
	case viewEditor:
		a.editorKey(k)
	case viewPick:
		// The picker overlay was closed: nothing left to do.
		a.cancelPick()
	default:
		a.readerKey(k)
	}
}

func (a *app) handlePaste(s string) {
	if n := len(a.overlays); n > 0 {
		a.overlays[n-1].paste(a, s)
		return
	}
	if a.prompt != nil {
		a.prompt.in.insert(s)
		a.promptChanged()
		return
	}
	if a.view == viewEditor && a.edit != nil {
		a.editorResult(a.edit.ed.HandlePaste(term.PasteEvent{Text: s}))
	}
}

func (a *app) handleMouse(m term.MouseEvent) {
	if a.tooSmall() {
		return
	}
	if n := len(a.overlays); n > 0 {
		if mo, ok := a.overlays[n-1].(mouser); ok {
			mo.mouse(a, m)
		}
		return
	}
	if a.view == viewEditor && a.edit != nil {
		a.editorResult(a.edit.ed.HandleMouse(m))
		return
	}
	l := a.layout()
	if l.tree.W > 0 && l.tree.Contains(m.X, m.Y) {
		a.treeMouse(l.tree, m)
		return
	}
	if l.ctx.W > 0 && l.ctx.Contains(m.X, m.Y) {
		a.ctxMouse(l.ctx, m)
		return
	}
	if a.view == viewReader && l.page.Contains(m.X, m.Y) {
		a.readerMouse(l.page, m)
	}
	if a.view == viewHome && l.page.Contains(m.X, m.Y) {
		a.homeMouse(m)
	}
}

// pushOverlay opens an overlay on top of the others.
func (a *app) pushOverlay(o overlay) {
	a.keys.reset()
	a.overlays = append(a.overlays, o)
}

// closeOverlay closes the topmost overlay.
func (a *app) closeOverlay() {
	n := len(a.overlays)
	if n == 0 {
		return
	}
	o := a.overlays[n-1]
	a.overlays = a.overlays[:n-1]
	if c, ok := o.(closer); ok {
		c.closed(a)
	}
}

// closeAll closes every overlay (after an action that navigates away).
func (a *app) closeAll() {
	for len(a.overlays) > 0 {
		a.closeOverlay()
	}
}

// --- timers -------------------------------------------------------------------

// deadline returns the earliest time something needs to happen without
// input (zero = nothing scheduled).
func (a *app) deadline() time.Time {
	var d time.Time
	sooner := func(t time.Time) {
		if !t.IsZero() && (d.IsZero() || t.Before(d)) {
			d = t
		}
	}
	if a.msg.text != "" {
		sooner(a.msg.until)
	}
	sooner(a.keys.leaderAt)
	for _, o := range a.overlays {
		if t, ok := o.(ticker); ok {
			sooner(t.deadline())
		}
	}
	return d
}

// tick runs whatever is due at now.
func (a *app) tick(now time.Time) {
	if a.msg.text != "" && !now.Before(a.msg.until) {
		a.msg = message{}
	}
	if !a.keys.leaderAt.IsZero() && !now.Before(a.keys.leaderAt) {
		a.keys.leaderAt = time.Time{}
		if a.keys.leaderPending() && len(a.overlays) == 0 {
			a.pushOverlay(newPalette())
			a.keys.pending = []string{"space"} // pushOverlay reset it
		}
	}
	for _, o := range append([]overlay(nil), a.overlays...) {
		if t, ok := o.(ticker); ok {
			if d := t.deadline(); !d.IsZero() && !now.Before(d) {
				t.tick(a, now)
			}
		}
	}
}

// scanFinished is called once when the background scan completes: links
// that were not indexed yet resolve now, backlinks are known.
func (a *app) scanFinished() {
	a.scanDone = true
	a.gen++
	if a.doc != nil && !a.doc.stdin {
		if n, ok := a.v.Note(a.doc.path); ok {
			a.doc.note = n
		}
	}
	if err := a.v.ScanErr(); err != nil {
		a.flashErr("scan: %v", err)
	}
}

// --- quitting -----------------------------------------------------------------

// requestQuit leaves folio, asking first when an edit is unsaved.
func (a *app) requestQuit() {
	if a.edit != nil && a.edit.ed.Dirty() {
		a.pushOverlay(newConfirm("Unsaved changes in "+a.edit.doc.name+". Quit anyway?", func(a *app) {
			a.discard = true
			a.quit = true
		}, nil))
		return
	}
	a.quit = true
}

func (a *app) cancelPick() {
	a.err = cli.ErrCancelled
	a.quit = true
}

// remember records the current note and line in the recent file.
func (a *app) remember() {
	if a.recent == nil || a.doc == nil || a.doc.stdin || a.doc.path == "" {
		return
	}
	line := 0
	if a.view == viewEditor && a.edit != nil && a.edit.doc == a.doc {
		line = a.edit.ed.CursorLine() + 1
	} else if a.doc.page != nil {
		line = a.doc.page.SourceLine(a.doc.cur) + 1
	}
	a.recent.Touch(a.v.Abs(a.doc.path), max(line, 0))
}

// noteName returns the display name of a vault path: the file name without
// ".md".
func noteName(p string) string {
	b := path.Base(p)
	return strings.TrimSuffix(b, ".md")
}
