package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/ZahakJ/folio/internal/md"
	"github.com/ZahakJ/folio/internal/term"
	"github.com/ZahakJ/folio/internal/text"
	"github.com/ZahakJ/folio/internal/vault"
)

// overlay is a centred raised panel above the page (DESIGN.md §4.2). The app
// handles Esc (closing the topmost overlay) before passing keys on.
type overlay interface {
	key(a *app, k term.KeyEvent)
	paste(a *app, s string)
	draw(a *app)
}

// ticker is an overlay with timed work (debounced search).
type ticker interface {
	deadline() time.Time
	tick(a *app, now time.Time)
}

// closer is an overlay that wants to know when it is closed with Esc.
type closer interface{ closed(a *app) }

// mouser is an overlay that handles the mouse.
type mouser interface {
	mouse(a *app, m term.MouseEvent)
}

func errorf(format string, args ...any) error { return fmt.Errorf(format, args...) }

func parseBytes(b []byte) *md.Document { return md.ParseBytes(b) }

// --- yes / no -----------------------------------------------------------------

// confirm asks a yes/no question.
type confirm struct {
	question string
	yes, no  func(a *app)
}

func newConfirm(q string, yes, no func(a *app)) *confirm {
	return &confirm{question: q, yes: yes, no: no}
}

func (c *confirm) key(a *app, k term.KeyEvent) {
	switch k.String() {
	case "y", "Y", "enter":
		a.closeOverlayNoHook(c)
		if c.yes != nil {
			c.yes(a)
		}
	case "n", "N", "q":
		a.closeOverlayNoHook(c)
		if c.no != nil {
			c.no(a)
		}
	}
}

func (c *confirm) closed(a *app) {
	if c.no != nil {
		c.no(a)
	}
}

func (c *confirm) paste(a *app, s string) {}

func (c *confirm) draw(a *app) {
	hint := "y yes  " + a.gl.Dot + "  n no"
	w := max(text.Width(c.question), text.Width(hint)) + 6
	r := panelRect(a.w, a.h, w, 5)
	in := a.drawPanel(r, "")
	a.scr.PutStringClip(in.X, in.Y, c.question, a.st.panel, in.X+in.W)
	a.scr.PutStringClip(in.X, in.Y+2, hint, a.st.pMuted, in.X+in.W)
}

// closeOverlayNoHook removes o (normally the topmost overlay) without running
// its close hook: the overlay finished on its own terms.
func (a *app) closeOverlayNoHook(o overlay) {
	for i := len(a.overlays) - 1; i >= 0; i-- {
		if a.overlays[i] == o {
			a.overlays = append(a.overlays[:i], a.overlays[i+1:]...)
			return
		}
	}
}

// --- several choices ------------------------------------------------------------

// choice is a prompt with lettered answers (the save conflict:
// reload / overwrite / cancel). Esc picks the last choice.
type choice struct {
	title   string
	lines   []string
	options []choiceOption
}

type choiceOption struct {
	key   string
	label string
	run   func(a *app)
}

func (c *choice) key(a *app, k term.KeyEvent) {
	ks := strings.ToLower(k.String())
	for _, o := range c.options {
		if ks == o.key {
			a.closeOverlayNoHook(c)
			if o.run != nil {
				o.run(a)
			}
			return
		}
	}
}

func (c *choice) closed(a *app) {
	if n := len(c.options); n > 0 && c.options[n-1].run != nil {
		c.options[n-1].run(a)
	}
}

func (c *choice) paste(a *app, s string) {}

func (c *choice) draw(a *app) {
	var parts []string
	for _, o := range c.options {
		parts = append(parts, o.key+" "+o.label)
	}
	hint := strings.Join(parts, "  "+a.gl.Dot+"  ")
	w := text.Width(hint)
	for _, l := range c.lines {
		w = max(w, text.Width(l))
	}
	r := panelRect(a.w, a.h, w+6, len(c.lines)+4)
	in := a.drawPanel(r, c.title)
	y := in.Y
	for _, l := range c.lines {
		a.scr.PutStringClip(in.X, y, text.Truncate(l, in.W, a.gl.Ellipsis), a.st.panel, in.X+in.W)
		y++
	}
	x := in.X
	for i, o := range c.options {
		if i > 0 {
			x = a.scr.PutStringClip(x, in.Y+in.H-1, "  "+a.gl.Dot+"  ", a.st.pFaint, in.X+in.W)
		}
		x = a.scr.PutStringClip(x, in.Y+in.H-1, o.key, a.st.pAccent.With(boldAttr), in.X+in.W)
		x = a.scr.PutStringClip(x, in.Y+in.H-1, " "+o.label, a.st.pMuted, in.X+in.W)
	}
}

// --- one-line text prompt ---------------------------------------------------------

// textPrompt is a one-line input panel: capture and new note.
type textPrompt struct {
	title  string
	prompt string
	hint   func(a *app, p *textPrompt) string
	in     lineInput
	task   bool // capture: as a task
	submit func(a *app, p *textPrompt)
	tab    func(a *app, p *textPrompt)
}

func (p *textPrompt) key(a *app, k term.KeyEvent) {
	switch k.String() {
	case "enter":
		a.closeOverlayNoHook(p)
		p.submit(a, p)
		return
	case "tab":
		if p.tab != nil {
			p.tab(a, p)
		}
		return
	}
	p.in.handleKey(k)
}

func (p *textPrompt) paste(a *app, s string) { p.in.insert(s) }

func (p *textPrompt) draw(a *app) {
	r := panelRect(a.w, a.h, 64, 5)
	in := a.drawPanel(r, p.title)
	s := a.scr
	x := s.PutString(in.X, in.Y, p.prompt, a.st.pAccent)
	x++
	cx := p.in.draw(s, x, in.Y, in.X+in.W-x, a.st.panel)
	if p.hint != nil {
		s.PutStringClip(in.X, in.Y+2, p.hint(a, p), a.st.pFaint, in.X+in.W)
	}
	s.SetCursor(cx, in.Y)
	s.SetCursorShape(term.CursorBar)
	s.ShowCursor(true)
}

// openCapture opens the capture prompt: one line appended to today's daily
// note without leaving the page (DESIGN.md §4.3 Space c).
func (a *app) openCapture() {
	p := &textPrompt{title: "Capture", prompt: a.gl.Crumb}
	p.hint = func(a *app, p *textPrompt) string {
		kind := "note  " + a.gl.Dot + "  Tab makes it a task"
		if p.task {
			kind = "task " + a.gl.TaskOpen + "  " + a.gl.Dot + "  Tab makes it a note"
		}
		return "to " + a.v.DailyPath(a.now()) + " as a " + kind
	}
	p.tab = func(a *app, p *textPrompt) { p.task = !p.task }
	p.submit = func(a *app, p *textPrompt) {
		t := strings.TrimSpace(p.in.text)
		task := p.task
		for _, pre := range []string{"- [ ] ", "[ ] ", "[] "} {
			if strings.HasPrefix(t, pre) {
				t, task = strings.TrimSpace(t[len(pre):]), true
			}
		}
		if t == "" {
			a.flash("nothing captured")
			return
		}
		res, err := a.v.Capture(t, vault.CaptureOptions{Task: task, Now: a.now()})
		if err != nil {
			a.flashErr("capture failed: %v", err)
			return
		}
		a.flash("captured to %s:%d", res.Path, res.Line)
		if a.doc != nil && a.doc.path == res.Path && !a.doc.stdin {
			a.reloadDoc(a.doc)
		}
	}
	a.pushOverlay(p)
}

// openNewNote prompts for a title and opens the new note in the editor.
func (a *app) openNewNote() {
	p := &textPrompt{title: "New note", prompt: a.gl.Crumb}
	p.hint = func(a *app, p *textPrompt) string {
		t := strings.TrimSpace(p.in.text)
		if t == "" {
			return "a title; folders with /"
		}
		dir, title := splitTitle(t)
		name := vault.SafeFilename(title) + ".md"
		if dir != "" {
			name = dir + "/" + name
		}
		return "creates " + name
	}
	p.submit = func(a *app, p *textPrompt) {
		t := strings.TrimSpace(p.in.text)
		if t == "" {
			return
		}
		a.leaveEditorThen(func() { a.createAndEdit(t) })
	}
	a.pushOverlay(p)
}

func splitTitle(t string) (dir, title string) {
	if i := strings.LastIndex(t, "/"); i >= 0 {
		return strings.Trim(t[:i], "/"), strings.TrimSpace(t[i+1:])
	}
	return "", t
}

// createAndEdit creates a note titled t ("folder/Title" allowed) and opens
// it in the editor.
func (a *app) createAndEdit(t string) {
	dir, title := splitTitle(t)
	if title == "" {
		a.flashErr("a note needs a title")
		return
	}
	rel, err := a.v.CreateNote(title, vault.NewNoteOptions{Dir: dir})
	if err != nil {
		a.flashErr("cannot create %s: %v", t, err)
		return
	}
	a.gen++
	a.pushHistory()
	a.openEditorOn(rel, -1)
	a.startWriting()
	a.flash("created %s", rel)
}

// openToday opens today's daily note, creating it if needed.
func (a *app) openToday() {
	rel, created, err := a.v.EnsureDaily(a.now())
	if err != nil {
		a.flashErr("today: %v", err)
		return
	}
	if created {
		a.gen++
	}
	a.openNote(rel, 0)
	if created {
		a.flash("created %s", rel)
	}
}
