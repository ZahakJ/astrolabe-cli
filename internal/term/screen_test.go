package term_test

import (
	"bytes"
	"math/rand"
	"strings"
	"testing"

	"github.com/ZahakJ/astrolabe-cli/internal/term"
	"github.com/ZahakJ/astrolabe-cli/internal/term/termtest"
	"github.com/ZahakJ/astrolabe-cli/internal/text"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
)

var tc = term.Encoder{Profile: term.ProfileTrueColor, StyledUnderline: true, Hyperlinks: true}

func newPair(w, h int) (*term.Screen, *termtest.VT) {
	vt := termtest.New(w, h)
	vt.Write([]byte(term.SeqAltScreenOn + term.SeqAutowrapOff))
	return term.NewScreen(vt, w, h, tc), vt
}

// assertMatches compares the virtual terminal grid with the screen buffer.
func assertMatches(t *testing.T, s *term.Screen, vt *termtest.VT) {
	t.Helper()
	w, h := s.Size()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sc, vc := s.Cell(x, y), vt.Cell(x, y)
			if sc.Text != vc.Text || sc.Width != vc.Width || sc.Style != vc.Style {
				t.Fatalf("cell (%d,%d): screen %+v, terminal %+v\nterminal:\n%s", x, y, sc, vc, vt.String())
			}
		}
	}
}

func TestScreenBasicFlush(t *testing.T) {
	s, vt := newPair(20, 4)
	gold := theme.Style{FG: theme.Hex("#c9a227"), Attrs: theme.Bold}
	s.PutString(0, 0, "Hello, astrolabe", theme.Style{})
	s.PutString(2, 2, "✦ gold", gold)
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := vt.String(); got != "Hello, astrolabe\n\n  ✦ gold" {
		t.Errorf("screen = %q", got)
	}
	if vt.Style(4, 2) != gold {
		t.Errorf("style = %+v", vt.Style(4, 2))
	}
	assertMatches(t, s, vt)
	if ok, frames := vt.SyncBalanced(); !ok || frames != 1 {
		t.Errorf("sync marks: balanced=%v frames=%d", ok, frames)
	}
}

func TestScreenFlushIsMinimal(t *testing.T) {
	var buf bytes.Buffer
	s := term.NewScreen(&buf, 30, 5, tc)
	s.PutString(0, 0, "unchanged line", theme.Style{})
	s.PutString(0, 3, "will change", theme.Style{})
	s.Flush()
	buf.Reset()
	if err := s.Flush(); err != nil || buf.Len() != 0 {
		t.Fatalf("idle flush wrote %q", buf.String())
	}
	s.PutString(5, 3, "CHANGE", theme.Style{})
	s.Flush()
	out := buf.String()
	if strings.Contains(out, "unchanged") || !strings.Contains(out, "CHANGE") {
		t.Errorf("diff output = %q", out)
	}
	if !strings.HasPrefix(out, term.SeqSyncBegin) || !strings.HasSuffix(out, term.SeqSyncEnd) {
		t.Errorf("frame not wrapped in synchronized update: %q", out)
	}
	// Same text drawn again is not re-sent.
	buf.Reset()
	s.PutString(5, 3, "CHANGE", theme.Style{})
	s.Flush()
	if buf.Len() != 0 {
		t.Errorf("redundant redraw: %q", buf.String())
	}
}

type countingWriter struct {
	writes int
	bytes.Buffer
}

func (c *countingWriter) Write(p []byte) (int, error) { c.writes++; return c.Buffer.Write(p) }

func TestScreenSingleWrite(t *testing.T) {
	var cw countingWriter
	s := term.NewScreen(&cw, 80, 24, tc)
	for y := 0; y < 24; y++ {
		s.PutString(0, y, strings.Repeat("x", 80), theme.Style{FG: theme.RGB(uint8(y), 0, 0)})
	}
	s.SetCursor(3, 3)
	s.ShowCursor(true)
	s.Flush()
	if cw.writes != 1 {
		t.Errorf("Flush made %d writes", cw.writes)
	}
}

func TestScreenWideAndZeroWidth(t *testing.T) {
	s, vt := newPair(12, 3)
	s.PutString(0, 0, "日本語abc", theme.Style{})
	s.PutString(0, 1, "café ẍ", theme.Style{})
	s.PutString(0, 2, "👍🏽ok", theme.Style{})
	s.Flush()
	assertMatches(t, s, vt)
	if c := s.Cell(1, 0); c.Width != 0 || c.Text != "" {
		t.Errorf("continuation cell = %+v", c)
	}
	if got := vt.Row(1); !strings.HasPrefix(got, "café ẍ") {
		t.Errorf("combining row = %q", got)
	}
	if x := s.PutString(0, 0, "ab", theme.Style{}); x != 2 {
		t.Errorf("PutString returned %d", x)
	}
	// Overwriting the second half of 本 (cells 2-3) blanks its first half.
	s.PutString(3, 0, "Z", theme.Style{})
	if got := s.Cell(2, 0); got.Text != " " || got.Width != 1 {
		t.Errorf("broken wide lead = %+v", got)
	}
	s.Flush()
	assertMatches(t, s, vt)
	if got := strings.TrimRight(vt.Row(0), " "); got != "ab Z語abc" {
		t.Errorf("row 0 = %q", got)
	}
}

func TestScreenClipping(t *testing.T) {
	s, vt := newPair(6, 2)
	if x := s.PutString(3, 0, "abcdef", theme.Style{}); x != 6 {
		t.Errorf("clipped end = %d", x)
	}
	s.PutString(4, 1, "日本", theme.Style{})   // 日 fits in 4-5, 本 does not
	s.PutString(-2, 1, "xyz", theme.Style{}) // starts off-screen
	s.PutString(0, 5, "nothing", theme.Style{})
	s.PutStringClip(0, 0, "日本", theme.Style{}, 3) // 本 straddles the clip
	s.Flush()
	assertMatches(t, s, vt)
	if got := vt.Row(0); got != "日 abc" {
		t.Errorf("row 0 = %q", got)
	}
	if got := vt.Row(1); got != "z   日" {
		t.Errorf("row 1 = %q", got)
	}
	// A wide grapheme at the last column becomes a blank.
	s.PutString(5, 0, "語", theme.Style{})
	if s.Cell(5, 0).Text != " " {
		t.Errorf("wide at edge = %+v", s.Cell(5, 0))
	}
}

func TestScreenBoxFillRestyle(t *testing.T) {
	s, vt := newPair(10, 5)
	border := theme.Style{FG: theme.Hex("#3d3427")}
	fill := theme.Style{BG: theme.Hex("#1f1b14")}
	s.Box(term.Rect{X: 1, Y: 0, W: 8, H: 4}, theme.UnicodeGlyphs, border, fill)
	s.PutString(2, 1, "hi", fill)
	s.Restyle(term.Rect{X: 2, Y: 1, W: 2, H: 1}, func(st theme.Style) theme.Style { return st.With(theme.Reverse) })
	s.Flush()
	assertMatches(t, s, vt)
	want := []string{" ╭──────╮", " │hi    │", " │      │", " ╰──────╯", ""}
	for y, w := range want {
		if got := strings.TrimRight(vt.Row(y), " "); got != w {
			t.Errorf("row %d = %q, want %q", y, got, w)
		}
	}
	if !vt.Style(2, 1).Attrs.Has(theme.Reverse) || vt.Style(4, 1).Attrs.Has(theme.Reverse) {
		t.Error("Restyle")
	}
	s.Box(term.Rect{X: 0, Y: 0, W: 10, H: 5}, theme.ASCIIGlyphs, border, fill)
	s.Flush()
	if got := vt.Row(0); got != "+--------+" {
		t.Errorf("ascii box = %q", got)
	}
}

func TestScreenCursor(t *testing.T) {
	s, vt := newPair(10, 3)
	s.PutString(0, 0, "text", theme.Style{})
	s.SetCursor(4, 1)
	s.ShowCursor(true)
	s.SetCursorShape(term.CursorBar)
	s.Flush()
	if x, y := vt.Cursor(); x != 4 || y != 1 || !vt.CursorVisible() || vt.CursorShape() != 6 {
		t.Errorf("cursor (%d,%d) visible=%v shape=%d", x, y, vt.CursorVisible(), vt.CursorShape())
	}
	s.SetCursorShape(term.CursorBlock)
	s.SetCursor(1, 2)
	s.Flush()
	if x, y := vt.Cursor(); x != 1 || y != 2 || vt.CursorShape() != 2 {
		t.Errorf("cursor (%d,%d) shape=%d", x, y, vt.CursorShape())
	}
	// Drawing elsewhere leaves the cursor where it was set.
	s.PutString(5, 0, "more", theme.Style{})
	s.Flush()
	if x, y := vt.Cursor(); x != 1 || y != 2 || !vt.CursorVisible() {
		t.Errorf("cursor moved by drawing: (%d,%d)", x, y)
	}
	s.ShowCursor(false)
	s.Flush()
	if vt.CursorVisible() {
		t.Error("cursor still visible")
	}
}

func TestScreenHyperlinks(t *testing.T) {
	s, vt := newPair(30, 2)
	link := theme.Style{FG: theme.Hex("#c9a227"), Link: "https://example.org"}
	s.PutString(0, 0, "see ", theme.Style{})
	s.PutString(4, 0, "example", link)
	s.PutString(11, 0, " here", theme.Style{})
	s.Flush()
	assertMatches(t, s, vt)
	if vt.Style(5, 0).Link != "https://example.org" || vt.Style(12, 0).Link != "" {
		t.Errorf("links: %q / %q", vt.Style(5, 0).Link, vt.Style(12, 0).Link)
	}
	// Without hyperlink support no OSC 8 is emitted.
	var buf bytes.Buffer
	s2 := term.NewScreen(&buf, 30, 1, term.Encoder{Profile: term.ProfileTrueColor})
	s2.PutString(0, 0, "x", link)
	s2.Flush()
	if strings.Contains(buf.String(), "\x1b]8") {
		t.Error("OSC 8 emitted without support")
	}
}

func TestScreenInvalidateAndResize(t *testing.T) {
	s, vt := newPair(10, 3)
	s.PutString(0, 0, "keep", theme.Style{})
	s.Flush()
	vt.Write([]byte("\x1b[2;1Hgarbage from elsewhere")) // something scribbled
	s.Invalidate()
	s.Flush()
	assertMatches(t, s, vt)
	s.Resize(12, 4)
	vt.Resize(12, 4)
	s.Clear(theme.Style{BG: theme.Hex("#16130e")})
	s.PutString(0, 3, "resized", theme.Style{})
	s.Flush()
	assertMatches(t, s, vt)
}

func TestScreen256And16(t *testing.T) {
	for _, enc := range []term.Encoder{{Profile: term.Profile256}, {Profile: term.Profile16}, {Profile: term.ProfileNone}} {
		vt := termtest.New(10, 1)
		s := term.NewScreen(vt, 10, 1, enc)
		st := theme.Style{FG: theme.Hex("#c9a227"), BG: theme.Hex("#16130e"), Attrs: theme.Italic}
		s.PutString(0, 0, "x", st)
		s.Flush()
		want := term.Downsample(st, enc.Profile)
		if got := vt.Style(0, 0); got != want {
			t.Errorf("%v: style %+v, want %+v", enc.Profile, got, want)
		}
	}
}

// TestScreenRandomFrames draws random content over many frames and checks
// the terminal always matches the buffer — the diff must never leave stale
// cells, whatever mix of wide, narrow and styled cells changes.
func TestScreenRandomFrames(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	words := []string{"a", "bc", "日本", "語", "é", "x́", "✦", "──", "👍", "  ", "ﻻ", "مر", "q"}
	styles := []theme.Style{
		{}, {FG: theme.Hex("#c9a227")}, {BG: theme.Hex("#1f1b14")}, {Attrs: theme.Bold | theme.Italic},
		{FG: theme.Yellow, Attrs: theme.Reverse}, {Link: "https://example.org", Attrs: theme.Underline},
	}
	s, vt := newPair(23, 7)
	for frame := 0; frame < 300; frame++ {
		n := rng.Intn(12)
		for i := 0; i < n; i++ {
			x, y := rng.Intn(25)-1, rng.Intn(7)
			var b strings.Builder
			for k := rng.Intn(5); k >= 0; k-- {
				b.WriteString(words[rng.Intn(len(words))])
			}
			s.PutString(x, y, b.String(), styles[rng.Intn(len(styles))])
		}
		if rng.Intn(20) == 0 {
			s.Fill(term.Rect{X: rng.Intn(20), Y: rng.Intn(6), W: rng.Intn(8), H: rng.Intn(3)}, " ", styles[rng.Intn(len(styles))])
		}
		s.SetCursor(rng.Intn(23), rng.Intn(7))
		s.ShowCursor(rng.Intn(2) == 0)
		s.Flush()
		assertMatches(t, s, vt)
		if x, y, vis := s.Cursor(); vis {
			if vx, vy := vt.Cursor(); vx != x || vy != y || !vt.CursorVisible() {
				t.Fatalf("frame %d: cursor (%d,%d) vs terminal (%d,%d)", frame, x, y, vx, vy)
			}
		} else if vt.CursorVisible() {
			t.Fatalf("frame %d: cursor should be hidden", frame)
		}
	}
	if ok, _ := vt.SyncBalanced(); !ok {
		t.Error("unbalanced synchronized updates")
	}
}

func TestPutSpans(t *testing.T) {
	s, vt := newPair(20, 1)
	spans := []text.Span[theme.Style]{{Text: "bold ", Style: theme.Style{Attrs: theme.Bold}}, {Text: "plain text here", Style: theme.Style{}}}
	if x := s.PutSpans(1, 0, spans, 12); x != 12 {
		t.Errorf("PutSpans end = %d", x)
	}
	s.Flush()
	if got := vt.Row(0); got != " bold plain         " {
		t.Errorf("row = %q", got)
	}
}

// shownByRunTerminal returns row y of vt as a run-reversing terminal
// (kitty) displays it: the text of every right-to-left run reversed in
// place. text.ReverseRTLRuns is its own inverse and is checked against a
// model of kitty in package text.
func shownByRunTerminal(vt *termtest.VT, w, y int) []text.RunCell {
	row := make([]text.RunCell, w)
	for x := 0; x < w; x++ {
		c := vt.Cell(x, y)
		row[x].Text = c.Text
		if c.Width != 1 {
			row[x].Text = ""
		}
		if c.Style.Attrs&theme.Bold != 0 {
			row[x].Face |= 1
		}
		if c.Style.Attrs&theme.Italic != 0 {
			row[x].Face |= 2
		}
	}
	text.ReverseRTLRuns(row)
	return row
}

func assertRunsMatch(t *testing.T, s *term.Screen, vt *termtest.VT) {
	t.Helper()
	w, h := s.Size()
	for y := 0; y < h; y++ {
		shown := shownByRunTerminal(vt, w, y)
		for x := 0; x < w; x++ {
			sc, vc := s.Cell(x, y), vt.Cell(x, y)
			want := sc.Text
			if sc.Width != 1 {
				want = ""
			}
			styleOK := sc.Style == vc.Style || sc.Width == 0 // a continuation shows its lead's style
			if shown[x].Text != want || !styleOK || sc.Width != vc.Width {
				t.Fatalf("cell (%d,%d): screen %+v, terminal shows %q with %+v\nterminal:\n%s", x, y, sc, shown[x].Text, vc, vt.String())
			}
		}
	}
}

func TestScreenBidiRuns(t *testing.T) {
	enc := tc
	enc.BidiRuns = true
	vt := termtest.New(24, 3)
	vt.Write([]byte(term.SeqAltScreenOn + term.SeqAutowrapOff))
	s := term.NewScreen(vt, 24, 3, enc)
	word := text.Visual("مرحبا بالعالم", text.RTL) // visual order, shaped
	s.PutString(1, 0, word, theme.Style{})
	s.PutString(0, 1, "x "+text.Visual("كتاب", text.RTL)+" 日本", theme.Style{})
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	// The terminal is sent each word in logical order.
	if got := vt.String(); !strings.Contains(got, text.Shape("بالعالم")+" "+text.Shape("مرحبا")) {
		t.Errorf("emitted %q", got)
	}
	assertRunsMatch(t, s, vt)
	// A colour change in mid-word stays on its visual cell.
	red := theme.Style{FG: theme.Hex("#cc3333")}
	s.Restyle(term.Rect{X: 2, Y: 0, W: 2, H: 1}, func(theme.Style) theme.Style { return red })
	s.Flush()
	assertRunsMatch(t, s, vt)
	if vt.Style(2, 0) != red || vt.Style(4, 0) == red {
		t.Error("restyled cells moved")
	}
	// Overwriting part of a word changes the whole run's emitted text.
	s.PutString(3, 0, "…", theme.Style{})
	s.Flush()
	assertRunsMatch(t, s, vt)
}

func TestScreenBidiRunsRandom(t *testing.T) {
	enc := tc
	enc.BidiRuns = true
	const w, h = 30, 4
	vt := termtest.New(w, h)
	vt.Write([]byte(term.SeqAltScreenOn + term.SeqAutowrapOff))
	s := term.NewScreen(vt, w, h, enc)
	words := []string{"مرحبا", "كتاب", "السلام", "مَرْحَبًا", "שלום", "١٢٣", "hello", "(x)", "#", "…", "日本", " "}
	styles := []theme.Style{{}, {Attrs: theme.Bold}, {Attrs: theme.Italic}, {FG: theme.Hex("#c9a227")}, {BG: theme.Hex("#202020")}}
	rng := rand.New(rand.NewSource(3))
	for frame := 0; frame < 300; frame++ {
		for k := rng.Intn(4); k >= 0; k-- {
			wd := words[rng.Intn(len(words))]
			s.PutString(rng.Intn(w), rng.Intn(h), text.Visual(wd, text.BaseDirection(wd)), styles[rng.Intn(len(styles))])
		}
		if rng.Intn(4) == 0 {
			s.Restyle(term.Rect{X: rng.Intn(w), Y: rng.Intn(h), W: rng.Intn(5) + 1, H: 1}, func(theme.Style) theme.Style {
				return styles[rng.Intn(len(styles))]
			})
		}
		if err := s.Flush(); err != nil {
			t.Fatal(err)
		}
		assertRunsMatch(t, s, vt)
	}
}

func TestAppendSpansBidiRuns(t *testing.T) {
	enc := term.Encoder{BidiRuns: true}
	bold := theme.Style{Attrs: theme.Bold}
	vis := text.Visual("كتاب جميل", text.RTL) // "ﻞﻴﻤﺟ ﺏﺎﺘﻛ"
	r := []rune(vis)
	spans := []text.Span[theme.Style]{{Text: string(r[:4]), Style: bold}, {Text: string(r[4:])}}
	got := string(enc.AppendSpans(nil, spans))
	want := "\x1b[0;1m" + text.Shape("جميل") + "\x1b[0m " + text.Shape("كتاب")
	if got != want {
		t.Errorf("AppendSpans = %q, want %q", got, want)
	}
	// Without BidiRuns: unchanged visual order.
	if got := string(term.Encoder{}.AppendSpans(nil, spans)); !strings.Contains(got, string(r[:4])) {
		t.Errorf("plain AppendSpans = %q", got)
	}
}

func TestRunsLine(t *testing.T) {
	sh := text.Shape
	vis := func(s string) string { return text.Visual(s, text.BaseDirection(s)) }
	tests := []struct{ in, want string }{
		{"plain", "plain"},
		{vis("مرحبا بالعالم"), sh("بالعالم") + " " + sh("مرحبا")},
		// Colour in mid-word: the escape stays before the same cell.
		{"\x1b[0;38;2;1;2;3m" + string([]rune(vis("كتاب"))[:2]) + "\x1b[0m" + string([]rune(vis("كتاب"))[2:]) + ".",
			"\x1b[0;38;2;1;2;3m" + string([]rune(sh("كتاب"))[:2]) + "\x1b[0m" + string([]rune(sh("كتاب"))[2:]) + "."},
		// Bold ends a run; 38;2;1;… is a colour, not bold.
		{"\x1b[1m" + string([]rune(vis("كتاب"))[:2]) + "\x1b[22m" + string([]rune(vis("كتاب"))[2:]),
			"\x1b[1m" + string([]rune(vis("كتاب"))[1]) + string([]rune(vis("كتاب"))[0]) + "\x1b[22m" + string([]rune(vis("كتاب"))[3]) + string([]rune(vis("كتاب"))[2])},
		// OSC 8 links and harakat.
		{"\x1b]8;;file:///x\x1b\\" + vis("مَرْحَبًا") + "\x1b]8;;\x1b\\ 12", "\x1b]8;;file:///x\x1b\\" + sh("مَرْحَبًا") + "\x1b]8;;\x1b\\ 12"},
	}
	for _, tt := range tests {
		if got := term.RunsLine(tt.in); got != tt.want {
			t.Errorf("RunsLine(%q)\n = %q\nwant %q", tt.in, got, tt.want)
		}
	}
}
