package editor

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/ZahakJ/astrolabe-cli/internal/term"
	"github.com/ZahakJ/astrolabe-cli/internal/text"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
)

func newScreen(w, h int) *term.Screen { return term.NewScreen(io.Discard, w, h, term.Encoder{}) }

// rowText reads screen row y between x0 and x1 (wide cells once).
func rowText(s *term.Screen, y, x0, x1 int) string {
	var sb strings.Builder
	for x := x0; x < x1; x++ {
		sb.WriteString(s.Cell(x, y).Text)
	}
	return strings.TrimRight(sb.String(), " ")
}

func drawEditor(t *testing.T, text string, w, h int, cfg func(*Config)) (*Editor, *term.Screen) {
	t.Helper()
	c := Config{Text: []byte(text), Measure: 20, Width: w, Height: h}
	if cfg != nil {
		cfg(&c)
	}
	e := New(c)
	s := newScreen(w, h)
	return e, s
}

func TestDrawWrapAndCentre(t *testing.T) {
	e, s := drawEditor(t, "alpha beta gamma delta epsilon zeta\nnext", 40, 6, nil)
	cur := e.Draw(s, term.Rect{W: 40, H: 6})
	// page of 20 cells centred in 40: x = 10
	if got := rowText(s, 0, 0, 40); got != strings.Repeat(" ", 10)+"alpha beta gamma" {
		t.Errorf("row 0 = %q", got)
	}
	if got := rowText(s, 1, 10, 40); got != "delta epsilon zeta" {
		t.Errorf("row 1 = %q", got)
	}
	if got := rowText(s, 2, 10, 40); got != "next" {
		t.Errorf("row 2 = %q", got)
	}
	if cur.X != 10 || cur.Y != 0 || cur.Shape != term.CursorBlock || !cur.Visible {
		t.Errorf("cursor %+v", cur)
	}
	// the wrapped continuation is reached with j (display line)
	e.HandleKey(term.KeyEvent{Key: term.KeyRune, Rune: 'j'})
	if p := e.CursorPos(); p.Line != 0 || p.Col != 17 {
		t.Errorf("j on wrapped line → %+v", p)
	}
	cur = e.Draw(s, term.Rect{W: 40, H: 6})
	if cur.X != 10 || cur.Y != 1 {
		t.Errorf("cursor after j %+v", cur)
	}
	// $ then k keeps the end
	for _, k := range parseKeys("jk") {
		e.HandleKey(k)
	}
	if p := e.CursorPos(); p.Line != 0 || p.Col != 17 {
		t.Errorf("jk → %+v", p)
	}
}

func TestDrawHangingIndent(t *testing.T) {
	e, s := drawEditor(t, "- item one two three four five", 40, 4, nil)
	e.Draw(s, term.Rect{W: 40, H: 4})
	if got := rowText(s, 0, 10, 40); got != "- item one two three" {
		t.Errorf("row 0 = %q", got)
	}
	if got := rowText(s, 1, 10, 40); got != "  four five" {
		t.Errorf("row 1 = %q (want hanging indent)", got)
	}
	e2, s2 := drawEditor(t, "  - [ ] task with words that wrap around", 40, 4, nil)
	e2.Draw(s2, term.Rect{W: 40, H: 4})
	if got := rowText(s2, 1, 10, 40); !strings.HasPrefix(got, "        ") || strings.HasPrefix(got, "         ") {
		t.Errorf("task continuation %q: want 8-cell indent", got)
	}
}

func TestDrawCursorShapes(t *testing.T) {
	e, s := drawEditor(t, "abc", 40, 4, nil)
	for _, k := range parseKeys("A") {
		e.HandleKey(k)
	}
	cur := e.Draw(s, term.Rect{W: 40, H: 4})
	if cur.Shape != term.CursorBar || cur.X != 13 {
		t.Errorf("insert cursor %+v", cur)
	}
	if x, y, vis := s.Cursor(); x != 13 || y != 0 || !vis {
		t.Errorf("screen cursor %d,%d %v", x, y, vis)
	}
	for _, k := range parseKeys("<Esc>:") {
		e.HandleKey(k)
	}
	e.Draw(s, term.Rect{W: 40, H: 3})
	if _, _, vis := s.Cursor(); vis {
		t.Error("cursor visible while the command line is active")
	}
	for _, k := range parseKeys("set nu") {
		e.HandleKey(k)
	}
	c := e.DrawCommandLine(s, term.Rect{Y: 3, W: 40, H: 1})
	if got := rowText(s, 3, 0, 40); got != ":set nu" {
		t.Errorf("command line %q", got)
	}
	if c.X != 7 || c.Y != 3 || c.Shape != term.CursorBar {
		t.Errorf("command line cursor %+v", c)
	}
}

func TestDrawTabsAndControls(t *testing.T) {
	e, s := drawEditor(t, "a\tb\x01c", 40, 2, nil)
	e.Draw(s, term.Rect{W: 40, H: 2})
	if got := rowText(s, 0, 10, 40); got != "a   b^Ac" {
		t.Errorf("row %q", got)
	}
	e.SetCursor(0, 2)
	cur := e.Draw(s, term.Rect{W: 40, H: 2})
	if cur.X != 14 {
		t.Errorf("cursor after tab at x=%d, want 14", cur.X)
	}
}

func TestDrawWideChars(t *testing.T) {
	e, s := drawEditor(t, "日本語のテキスト", 40, 2, nil)
	e.SetCursor(0, len("日本"))
	cur := e.Draw(s, term.Rect{W: 40, H: 2})
	if cur.X != 14 {
		t.Errorf("cursor on third wide char at x=%d, want 14", cur.X)
	}
	if c := s.Cell(14, 0); c.Text != "語" || c.Width != 2 {
		t.Errorf("cell %+v", c)
	}
}

func TestDrawRTL(t *testing.T) {
	const salam = "سلام"
	e, s := drawEditor(t, salam, 40, 2, func(c *Config) { c.Bidi = true })
	cur := e.Draw(s, term.Rect{W: 40, H: 2})
	clusters := []string{"س", "ل", "ا", "م"}
	shaped := text.ShapeClusters(clusters)
	// right-aligned in the page (x 10..29): logical order runs right to
	// left; the lam-alef ligature takes the lam's cell and the alef none
	right := 29
	x := right
	for _, g := range shaped {
		if g == "" {
			continue
		}
		if c := s.Cell(x, 0); c.Text != g {
			t.Errorf("cell %d = %q, want %q", x, c.Text, g)
		}
		x--
	}
	if shaped[2] != "" {
		t.Errorf("expected a lam-alef ligature, got %q", shaped)
	}
	if cur.X != right {
		t.Errorf("cursor on first letter at x=%d, want %d", cur.X, right)
	}
	e.HandleKey(term.KeyEvent{Key: term.KeyRune, Rune: 'l'})
	cur = e.Draw(s, term.Rect{W: 40, H: 2})
	if cur.X != right-1 {
		t.Errorf("l moves the cursor left on screen: x=%d, want %d", cur.X, right-1)
	}
	// Insert-mode bar before a right-to-left letter sits at its right edge
	e.HandleKey(term.KeyEvent{Key: term.KeyRune, Rune: 'i'})
	cur = e.Draw(s, term.Rect{W: 40, H: 2})
	if cur.X != right || cur.Shape != term.CursorBar {
		t.Errorf("insert bar %+v, want x=%d", cur, right)
	}

	// mixed line: LTR base, Arabic run reversed in place
	e2, s2 := drawEditor(t, "abc "+salam, 40, 2, func(c *Config) { c.Bidi = true })
	e2.SetCursor(0, len("abc "))
	cur = e2.Draw(s2, term.Rect{W: 40, H: 2})
	if got := rowText(s2, 0, 10, 14); got != "abc" {
		t.Errorf("ltr part %q", got)
	}
	// "abc " then the three cells of سلام reversed: س is the rightmost
	if cur.X != 16 {
		t.Errorf("cursor on س in mixed line at x=%d, want 16", cur.X)
	}

	// bidi off: logical order untouched
	e3, s3 := drawEditor(t, salam, 40, 2, nil)
	e3.Draw(s3, term.Rect{W: 40, H: 2})
	if got := rowText(s3, 0, 10, 40); got != salam {
		t.Errorf("bidi off row %q", got)
	}
}

func TestDrawLineNumbers(t *testing.T) {
	e, s := drawEditor(t, "a\nb\nc", 40, 4, func(c *Config) { c.Number = true })
	e.Draw(s, term.Rect{W: 40, H: 4})
	row := rowText(s, 1, 0, 40)
	if !strings.Contains(row, "  2 b") {
		t.Errorf("numbered row %q", row)
	}
}

func TestDrawScrolling(t *testing.T) {
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	e, s := drawEditor(t, strings.Join(lines, "\n"), 40, 10, nil)
	e.HandleKey(term.KeyEvent{Key: term.KeyRune, Rune: 'G'})
	cur := e.Draw(s, term.Rect{W: 40, H: 10})
	if got := rowText(s, 9, 10, 40); got != "line 100" || cur.Y != 9 {
		t.Errorf("G: bottom row %q cursor %+v", got, cur)
	}
	e.HandleKey(term.KeyEvent{Key: term.KeyRune, Rune: 'g'})
	e.HandleKey(term.KeyEvent{Key: term.KeyRune, Rune: 'g'})
	for i := 0; i < 8; i++ {
		e.HandleKey(term.KeyEvent{Key: term.KeyRune, Rune: 'j'})
	}
	cur = e.Draw(s, term.Rect{W: 40, H: 10})
	// scrolloff keeps 3 rows below the cursor
	if cur.Y != 6 || rowText(s, 0, 10, 40) != "line 3" {
		t.Errorf("scrolloff: cursor %+v top %q", cur, rowText(s, 0, 10, 40))
	}
	e.HandleKey(term.KeyEvent{Key: term.KeyRune, Rune: 'd', Mod: term.ModCtrl})
	if e.CursorLine() != 13 {
		t.Errorf("ctrl-d moved to line %d, want 13", e.CursorLine())
	}
	e.HandleMouse(term.MouseEvent{Button: term.MouseWheelDown})
	e.Draw(s, term.Rect{W: 40, H: 10})
	if top := rowText(s, 0, 10, 40); top != "line 11" {
		t.Errorf("wheel: top %q", top)
	}
	// a click places the cursor
	e.HandleMouse(term.MouseEvent{X: 12, Y: 2, Button: term.MouseLeft, Action: term.MousePress})
	if p := e.CursorPos(); p.Line != 12 || p.Col != 2 {
		t.Errorf("click → %+v", p)
	}
	// drag selects
	e.HandleMouse(term.MouseEvent{X: 14, Y: 3, Button: term.MouseLeft, Action: term.MouseMotion})
	if e.Mode() != ModeVisual {
		t.Errorf("drag mode %v", e.Mode())
	}
}

func TestDrawSelectionAndSearch(t *testing.T) {
	th := theme.IronGall
	e, s := drawEditor(t, "foo bar foo", 40, 3, func(c *Config) { c.Theme = th })
	for _, k := range parseKeys("/foo<CR>") {
		e.HandleKey(k)
	}
	e.Draw(s, term.Rect{W: 40, H: 3})
	if st := s.Cell(10, 0).Style; st.BG != th.Search.BG {
		t.Errorf("first match not highlighted: %+v", st)
	}
	if st := s.Cell(18, 0).Style; st.BG != th.SearchCurrent.BG {
		t.Errorf("current match style %+v", st)
	}
	for _, k := range parseKeys(":noh<CR>0vl") {
		e.HandleKey(k)
	}
	e.Draw(s, term.Rect{W: 40, H: 3})
	if st := s.Cell(11, 0).Style; st.BG != th.Selection.BG {
		t.Errorf("selection style %+v", st)
	}
	if st := s.Cell(12, 0).Style; st.BG == th.Selection.BG {
		t.Errorf("selection too long")
	}
}

func TestDrawCompleterPopup(t *testing.T) {
	e, s := drawEditor(t, "", 60, 12, func(c *Config) {
		c.Completer = FuzzyCompleter([]Candidate{{Target: "Alpha", Label: "Alpha", Detail: "notes/Alpha.md"}})
	})
	for _, k := range parseKeys("i[[al") {
		e.HandleKey(k)
	}
	e.Draw(s, term.Rect{W: 60, H: 12})
	found := false
	for y := 0; y < 12; y++ {
		if strings.Contains(rowText(s, y, 0, 60), "Alpha") && strings.Contains(rowText(s, y, 0, 60), "notes/Alpha.md") {
			found = true
		}
	}
	if !found {
		t.Errorf("popup not drawn:\n%s", dumpScreen(s, 60, 12))
	}
}

func TestDrawStyles(t *testing.T) {
	th := theme.IronGall
	src := "# Title\n\nSome **bold** and `code` with [[Link]] #tag\n```go\nx := 1\n```\n> quote"
	e, s := drawEditor(t, src, 60, 10, func(c *Config) { c.Theme = th; c.Measure = 50 })
	e.SetCursor(2, 0)
	e.Draw(s, term.Rect{W: 60, H: 10})
	x0 := 5
	check := func(name string, x, y int, f func(theme.Style) bool) {
		t.Helper()
		if st := s.Cell(x, y).Style; !f(st) {
			t.Errorf("%s at %d,%d (%q): %+v", name, x, y, s.Cell(x, y).Text, st)
		}
	}
	check("heading marker faint", x0, 0, func(st theme.Style) bool { return st.FG == th.Faint })
	check("heading bold", x0+2, 0, func(st theme.Style) bool { return st.FG == th.Heading && st.Attrs.Has(theme.Bold) })
	check("bold text", x0+7, 2, func(st theme.Style) bool { return st.Attrs.Has(theme.Bold) })
	check("bold marker faint", x0+5, 2, func(st theme.Style) bool { return st.FG == th.Faint })
	check("code ground", x0+19, 2, func(st theme.Style) bool { return st.BG == th.Raised })
	check("link accent", x0+32, 2, func(st theme.Style) bool { return st.FG == th.Link })
	check("tag accent", x0+39, 2, func(st theme.Style) bool { return st.FG == th.Accent })
	check("code block ground", x0, 4, func(st theme.Style) bool { return st.BG == th.Raised })
	check("quote muted", x0+2, 6, func(st theme.Style) bool { return st.FG == th.Muted && st.Attrs.Has(theme.Italic) })
	check("cursor line ground", x0+50, 2, func(st theme.Style) bool { return st.BG == th.CursorLine.BG })
}

func dumpScreen(s *term.Screen, w, h int) string {
	var sb strings.Builder
	for y := 0; y < h; y++ {
		sb.WriteString(rowText(s, y, 0, w))
		sb.WriteByte('\n')
	}
	return sb.String()
}

func TestDrawTiny(t *testing.T) {
	e, s := drawEditor(t, "some text that is long enough to wrap many many times", 5, 3, nil)
	e.Draw(s, term.Rect{W: 5, H: 3})
	e.Draw(s, term.Rect{W: 0, H: 0})
	e.Draw(s, term.Rect{W: 1, H: 1})
}
