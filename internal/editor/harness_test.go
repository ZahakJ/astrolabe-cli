package editor

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ZahakJ/folio/internal/term"
)

// cursorMark marks the cursor position in test buffers.
const cursorMark = "█"

var namedKeys = map[string]term.KeyEvent{
	"Esc":      {Key: term.KeyEscape},
	"CR":       {Key: term.KeyEnter},
	"BS":       {Key: term.KeyBackspace},
	"Tab":      {Key: term.KeyTab},
	"S-Tab":    {Key: term.KeyTab, Mod: term.ModShift},
	"Del":      {Key: term.KeyDelete},
	"Up":       {Key: term.KeyUp},
	"Down":     {Key: term.KeyDown},
	"Left":     {Key: term.KeyLeft},
	"Right":    {Key: term.KeyRight},
	"Home":     {Key: term.KeyHome},
	"End":      {Key: term.KeyEnd},
	"PageUp":   {Key: term.KeyPageUp},
	"PageDown": {Key: term.KeyPageDown},
}

// parseKeys turns "dw<Esc><C-r>" into key events.
func parseKeys(s string) []term.KeyEvent {
	var out []term.KeyEvent
	for len(s) > 0 {
		if s[0] == '<' {
			if j := strings.IndexByte(s, '>'); j > 1 {
				name := s[1:j]
				if k, ok := namedKeys[name]; ok {
					out = append(out, k)
					s = s[j+1:]
					continue
				}
				if strings.HasPrefix(name, "C-") && utf8.RuneCountInString(name) == 3 {
					r, _ := utf8.DecodeRuneInString(name[2:])
					out = append(out, term.KeyEvent{Key: term.KeyRune, Rune: r, Mod: term.ModCtrl})
					s = s[j+1:]
					continue
				}
			}
		}
		r, n := utf8.DecodeRuneInString(s)
		out = append(out, term.KeyEvent{Key: term.KeyRune, Rune: r})
		s = s[n:]
	}
	return out
}

// splitCursor removes the cursor mark and returns the text and position.
func splitCursor(t *testing.T, s string) (string, Pos) {
	t.Helper()
	i := strings.Index(s, cursorMark)
	if i < 0 {
		return s, Pos{}
	}
	before := s[:i]
	line := strings.Count(before, "\n")
	col := len(before) - strings.LastIndexByte(before, '\n') - 1
	return before + s[i+len(cursorMark):], Pos{line, col}
}

func withCursor(text string, p Pos) string {
	lines := strings.Split(text, "\n")
	if p.Line < len(lines) {
		l := lines[p.Line]
		if p.Col <= len(l) {
			lines[p.Line] = l[:p.Col] + cursorMark + l[p.Col:]
		}
	}
	return strings.Join(lines, "\n")
}

type harness struct {
	t    *testing.T
	ed   *Editor
	res  []Result
	clip []string
}

func newHarness(t *testing.T, in string) *harness {
	t.Helper()
	text, p := splitCursor(t, in)
	h := &harness{t: t}
	h.ed = New(Config{
		Text:      []byte(text),
		Width:     80,
		Height:    24,
		Now:       func() time.Time { return time.Date(2026, 10, 4, 9, 30, 0, 0, time.UTC) },
		Clipboard: func(s string) error { h.clip = append(h.clip, s); return nil },
		Completer: FuzzyCompleter([]Candidate{
			{Target: "Note", Label: "Note", Detail: "Note.md"},
			{Target: "Notebook", Label: "Notebook", Detail: "Notebook.md"},
			{Target: "projects/Plan", Label: "Plan", Detail: "projects/Plan.md"},
		}),
	})
	h.ed.SetCursor(p.Line, p.Col)
	return h
}

func (h *harness) keys(s string) *harness {
	for _, k := range parseKeys(s) {
		h.res = append(h.res, h.ed.HandleKey(k))
	}
	return h
}

func (h *harness) last() Result {
	if len(h.res) == 0 {
		return Result{}
	}
	return h.res[len(h.res)-1]
}

// state renders the buffer (LF-joined, without BOM) with the cursor mark.
func (h *harness) state() string {
	return withCursor(strings.Join(h.ed.buf.lines, "\n"), h.ed.cur)
}

func (h *harness) text() string { return strings.Join(h.ed.buf.lines, "\n") }

func pasteEvent(s string) term.PasteEvent { return term.PasteEvent{Text: s} }
