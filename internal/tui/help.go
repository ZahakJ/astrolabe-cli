package tui

import (
	"github.com/ZahakJ/folio/internal/term"
	"github.com/ZahakJ/folio/internal/text"
	"github.com/ZahakJ/folio/internal/theme"
)

const (
	boldAttr   = theme.Bold
	strikeAttr = theme.Strike
)

// helpLine is one line of the help overlay.
type helpLine struct {
	head bool   // a section heading
	keys string // key column
	desc string
}

// editorHelp summarises the built-in editor (its keys are Vim's and are
// dispatched by internal/editor, not by the reader keymap).
var editorHelp = [][2]string{
	{"i a o O I A", "insert; Esc back to Normal"},
	{"Esc (Normal)", "back to the reader, saving if changed"},
	{"h j k l w b e 0 ^ $", "motions; j k by display line"},
	{"gg G { } % f t ; ,", "more motions, with counts"},
	{"d c y > < + motion", "operators; iw aw ip ap i\" i( text objects"},
	{"dd cc yy D C x p P J", "line edits, put, join"},
	{"u Ctrl-r .", "undo, redo, repeat"},
	{"/ ? n N *", "search;  :s/a/b/g  :%s/a/b/g"},
	{"v V", "visual, visual line"},
	{":w :q :q! :wq :x ZZ", "save and leave;  Ctrl-s saves"},
	{"Enter Tab Shift-Tab", "continue lists, indent, table cells"},
	{"[[ Ctrl-t", "link completer, timestamp"},
}

// help is the scrollable keymap overlay, generated from the dispatch table.
type help struct {
	lines []helpLine
	top   int
	keyW  int
}

func newHelp(a *app) *help {
	h := &help{}
	ascii := a.gl.Name == "ascii"
	for _, sec := range sections {
		h.lines = append(h.lines, helpLine{head: true, desc: "Reader " + a.gl.Dot + " " + sec})
		if sec == secLeader {
			h.lines[len(h.lines)-1].desc = sec
		}
		for i := range keymap {
			b := &keymap[i]
			if b.section != sec {
				continue
			}
			h.lines = append(h.lines, helpLine{keys: bindingKeys(b, ascii), desc: b.desc})
		}
		h.lines = append(h.lines, helpLine{})
	}
	h.lines = append(h.lines, helpLine{head: true, desc: "Editor (a Vim subset)"})
	for _, e := range editorHelp {
		h.lines = append(h.lines, helpLine{keys: e[0], desc: e[1]})
	}
	for _, l := range h.lines {
		h.keyW = max(h.keyW, text.Width(l.keys))
	}
	h.keyW = min(h.keyW, 24)
	return h
}

func (h *help) key(a *app, k term.KeyEvent) {
	page := max(1, a.h-6)
	switch k.String() {
	case "j", "down", "ctrl+e", "ctrl+n":
		h.top++
	case "k", "up", "ctrl+y", "ctrl+p":
		h.top--
	case "ctrl+d", "space", "pgdown", "ctrl+f":
		h.top += page / 2
	case "ctrl+u", "pgup", "ctrl+b":
		h.top -= page / 2
	case "g", "home":
		h.top = 0
	case "G", "end":
		h.top = len(h.lines)
	case "q", "?":
		a.closeOverlay()
	}
}

func (h *help) paste(a *app, s string) {}

func (h *help) mouse(a *app, m term.MouseEvent) {
	switch m.Button {
	case term.MouseWheelDown:
		h.top += 3
	case term.MouseWheelUp:
		h.top -= 3
	}
}

func (h *help) draw(a *app) {
	s := a.scr
	r := panelRect(a.w, a.h, 80, 24)
	in := a.drawPanel(r, "Keys")
	rows := in.H - 1
	h.top = max(0, min(h.top, len(h.lines)-rows))
	keyW := min(h.keyW, in.W/2)
	for i := 0; i < rows; i++ {
		li := h.top + i
		if li >= len(h.lines) {
			break
		}
		l := h.lines[li]
		y := in.Y + i
		if l.head {
			s.PutStringClip(in.X, y, l.desc, a.st.pAccent.With(boldAttr), in.X+in.W)
			continue
		}
		s.PutStringClip(in.X, y, text.Truncate(l.keys, keyW, a.gl.Ellipsis), a.st.panel.With(boldAttr), in.X+keyW)
		s.PutStringClip(in.X+keyW+2, y, text.Truncate(l.desc, in.W-keyW-2, a.gl.Ellipsis), a.st.pMuted, in.X+in.W)
	}
	// Footer: position and how to leave.
	foot := "j k scroll " + a.gl.Dot + " Esc closes"
	if len(h.lines) > rows {
		pct := 100
		if len(h.lines)-rows > 0 {
			pct = h.top * 100 / (len(h.lines) - rows)
		}
		foot = itoa(pct) + "%  " + a.gl.Dot + "  " + foot
	}
	s.PutStringClip(in.X+in.W-text.Width(foot), in.Y+in.H-1, foot, a.st.pFaint, in.X+in.W)
}

// --- leader palette ----------------------------------------------------------------

// palette lists the Space bindings after a pause on Space (DESIGN.md §4.3).
// Keys typed while it shows go to the dispatcher, which completes the
// sequence and closes it.
type palette struct{}

func newPalette() *palette { return &palette{} }

func (p *palette) key(a *app, k term.KeyEvent) {
	a.closeOverlayNoHook(p)
	a.keys.pending = []string{"space"}
	a.dispatch(k)
}

func (p *palette) closed(a *app) { a.keys.reset() }

func (p *palette) paste(a *app, s string) {}

func (p *palette) draw(a *app) {
	s := a.scr
	bs := leaderBindings()
	ascii := a.gl.Name == "ascii"
	// Two columns of "key  word".
	colW := 0
	for _, b := range bs {
		colW = max(colW, text.Width(keyName(lastKey(b.keys[0]), ascii))+2+text.Width(b.word))
	}
	colW += 4
	cols := 2
	if a.w < colW*2+8 {
		cols = 1
	}
	rows := (len(bs) + cols - 1) / cols
	r := panelRect(a.w, a.h, colW*cols+4, rows+3)
	// The palette sits low, near where the eye is after pressing Space.
	r.Y = max(0, a.h-1-r.H-1)
	in := a.drawPanel(r, "Space")
	for i, b := range bs {
		c, row := i/rows, i%rows
		if row >= in.H {
			continue
		}
		x := in.X + c*colW
		y := in.Y + row
		x = s.PutStringClip(x, y, keyName(lastKey(b.keys[0]), ascii), a.st.pAccent.With(boldAttr), in.X+in.W)
		s.PutStringClip(x+2, y, b.word, a.st.panel, in.X+in.W)
	}
}

func lastKey(seq string) string {
	for i := len(seq) - 1; i >= 0; i-- {
		if seq[i] == ' ' {
			return seq[i+1:]
		}
	}
	return seq
}

// closePalette removes the leader palette if it is showing.
func (a *app) closePalette() {
	for i := len(a.overlays) - 1; i >= 0; i-- {
		if _, ok := a.overlays[i].(*palette); ok {
			a.overlays = append(a.overlays[:i], a.overlays[i+1:]...)
		}
	}
}
