package term

import (
	"testing"

	"github.com/ZahakJ/folio/internal/text"

	"github.com/ZahakJ/folio/internal/theme"
)

func TestAppendSGR(t *testing.T) {
	gold := theme.Hex("#c9a227")
	ground := theme.Hex("#16130e")
	tests := []struct {
		name string
		enc  Encoder
		st   theme.Style
		want string
	}{
		{"reset", Encoder{Profile: ProfileTrueColor}, theme.Style{}, "\x1b[0m"},
		{"truecolor fg bg", Encoder{Profile: ProfileTrueColor}, theme.Style{FG: gold, BG: ground}, "\x1b[0;38;2;201;162;39;48;2;22;19;14m"},
		{"attrs", Encoder{Profile: ProfileTrueColor}, theme.Style{Attrs: theme.Bold | theme.Italic | theme.Strike | theme.Reverse | theme.Faint}, "\x1b[0;1;2;3;7;9m"},
		{"underline", Encoder{Profile: ProfileTrueColor}, theme.Style{Attrs: theme.Underline}, "\x1b[0;4m"},
		{"curly supported", Encoder{Profile: ProfileTrueColor, StyledUnderline: true}, theme.Style{Attrs: theme.CurlyUnderline}, "\x1b[0;4:3m"},
		{"dotted supported", Encoder{Profile: ProfileTrueColor, StyledUnderline: true}, theme.Style{Attrs: theme.DottedUnderline}, "\x1b[0;4:4m"},
		{"curly degraded", Encoder{Profile: ProfileTrueColor}, theme.Style{Attrs: theme.CurlyUnderline}, "\x1b[0;4m"},
		{"256 downsample", Encoder{Profile: Profile256}, theme.Style{FG: gold, BG: ground}, "\x1b[0;38;5;178;48;5;233m"},
		{"16 semantic indexed", Encoder{Profile: Profile16}, theme.Style{FG: theme.Yellow, Attrs: theme.Bold}, "\x1b[0;1;33m"},
		{"16 bright", Encoder{Profile: Profile16}, theme.Style{FG: theme.BrightBlack}, "\x1b[0;90m"},
		{"16 indexed bg kept", Encoder{Profile: Profile16}, theme.Style{BG: theme.Red}, "\x1b[0;41m"},
		{"16 rgb bg dropped", Encoder{Profile: Profile16}, theme.Style{FG: gold, BG: ground}, "\x1b[0;33m"},
		{"16 ivory text is default", Encoder{Profile: Profile16}, theme.Style{FG: theme.Hex("#eae2d0")}, "\x1b[0m"},
		{"none drops colour", Encoder{Profile: ProfileNone}, theme.Style{FG: gold, BG: ground, Attrs: theme.Bold}, "\x1b[0;1m"},
		{"indexed in truecolor", Encoder{Profile: ProfileTrueColor}, theme.Style{FG: theme.ANSI(200), BG: theme.BrightWhite}, "\x1b[0;38;5;200;107m"},
	}
	for _, tt := range tests {
		if got := tt.enc.SGR(tt.st); got != tt.want {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestNearest256(t *testing.T) {
	tests := []struct {
		r, g, b uint8
		want    uint8
	}{
		{0, 0, 0, 16},
		{255, 255, 255, 231},
		{255, 0, 0, 196},
		{0, 255, 0, 46},
		{0, 0, 255, 21},
		{128, 128, 128, 244}, // grey ramp beats cube 102 (135,135,135)? 244 = 128
		{8, 8, 8, 232},
		{238, 238, 238, 255},
		{95, 135, 175, 67},
		{0x16, 0x13, 0x0e, 233}, // iron-gall ground → near-black grey
		{0xc9, 0xa2, 0x27, 178}, // gold leaf → 215,175,0
	}
	for _, tt := range tests {
		if got := Nearest256(tt.r, tt.g, tt.b); got != tt.want {
			r, g, b := theme.XtermPalette(got)
			t.Errorf("Nearest256(%d,%d,%d) = %d (%d,%d,%d), want %d", tt.r, tt.g, tt.b, got, r, g, b, tt.want)
		}
	}
	// Exhaustive sanity: every palette colour maps to itself (or an
	// identical colour).
	for i := 16; i < 256; i++ {
		r, g, b := theme.XtermPalette(uint8(i))
		got := Nearest256(r, g, b)
		gr, gg, gb := theme.XtermPalette(got)
		if gr != r || gg != g || gb != b {
			t.Errorf("palette %d (%d,%d,%d) → %d (%d,%d,%d)", i, r, g, b, got, gr, gg, gb)
		}
	}
}

func TestNearest256NeverWorseThanCube(t *testing.T) {
	// The chosen entry is at least as close as the per-channel cube pick.
	for r := 0; r < 256; r += 17 {
		for g := 0; g < 256; g += 17 {
			for b := 0; b < 256; b += 17 {
				got := Nearest256(uint8(r), uint8(g), uint8(b))
				gr, gg, gb := theme.XtermPalette(got)
				d := colorDist(uint8(r), uint8(g), uint8(b), gr, gg, gb)
				for i := 16; i < 256; i++ {
					pr, pg, pb := theme.XtermPalette(uint8(i))
					if colorDist(uint8(r), uint8(g), uint8(b), pr, pg, pb) < d-1e-9 {
						t.Fatalf("(%d,%d,%d): picked %d but %d is closer", r, g, b, got, i)
					}
				}
			}
		}
	}
}

func TestNearest16(t *testing.T) {
	tests := []struct {
		hex  string
		want theme.Color
	}{
		{"#c9a227", theme.Yellow},
		{"#d06a52", theme.Red},
		{"#97ad72", theme.Green},
		{"#8fa8c2", theme.Blue},
		{"#86b3ab", theme.Cyan},
		{"#a993c4", theme.Magenta},
		{"#eae2d0", theme.Default}, // ivory body ink stays the terminal's ink
		{"#16130e", theme.Default},
		{"#746b59", theme.BrightBlack},
	}
	for _, tt := range tests {
		r, g, b := theme.Hex(tt.hex).Components()
		if got := Nearest16(r, g, b); got != tt.want {
			t.Errorf("Nearest16(%s) = %v, want %v", tt.hex, got, tt.want)
		}
	}
}

func TestThemeFor(t *testing.T) {
	th := theme.IronGall
	if ThemeFor(th, ProfileTrueColor).Ground != th.Ground || ThemeFor(th, Profile256).Ground != th.Ground {
		t.Error("truecolor/256 keep the authored theme")
	}
	if b := ThemeFor(th, Profile16); b.Accent != theme.Yellow || !b.Ground.IsDefault() {
		t.Error("16 uses Basic16")
	}
	if m := ThemeFor(th, ProfileNone); !m.Accent.IsDefault() {
		t.Error("none uses Mono")
	}
}

func TestStyledAndLinks(t *testing.T) {
	e := Encoder{Profile: ProfileTrueColor, Hyperlinks: true}
	st := theme.Style{FG: theme.Yellow, Link: "https://example.org/a"}
	got := e.Styled("site", st)
	want := "\x1b]8;;https://example.org/a\x1b\\\x1b[0;33msite\x1b[0m\x1b]8;;\x1b\\"
	if got != want {
		t.Errorf("Styled = %q, want %q", got, want)
	}
	if got := (Encoder{}).Styled("plain", theme.Style{FG: theme.Yellow}); got != "plain" {
		t.Errorf("no-colour Styled = %q", got)
	}
	if got := (Encoder{Profile: ProfileTrueColor}).Styled("x", theme.Style{Link: "u"}); got != "x" {
		t.Errorf("link without hyperlinks = %q", got)
	}
	if got := string(e.AppendLink(nil, "bad\x1b\\url")); got != "\x1b]8;;bad\\url\x1b\\" {
		t.Errorf("control bytes not stripped: %q", got)
	}
	if OSC52("hi") != "\x1b]52;c;aGk=\x07" {
		t.Error("OSC52")
	}
}

func TestAppendSpans(t *testing.T) {
	bold := theme.Style{Attrs: theme.Bold}
	spans := []text.Span[theme.Style]{
		{Text: "plain "}, {Text: "bold", Style: bold}, {Text: " still", Style: bold},
		{Text: " link", Style: theme.Style{Link: "https://example.org"}}, {Text: " end"},
	}
	e := Encoder{Profile: ProfileTrueColor, Hyperlinks: true}
	got := string(e.AppendSpans(nil, spans))
	want := "plain \x1b[0;1mbold still\x1b]8;;https://example.org\x1b\\\x1b[0m link\x1b]8;;\x1b\\ end"
	if got != want {
		t.Errorf("AppendSpans = %q\nwant          %q", got, want)
	}
	if got := string((Encoder{}).AppendSpans(nil, []text.Span[theme.Style]{{Text: "a", Style: theme.Style{FG: theme.Yellow}}})); got != "a" {
		t.Errorf("no-colour spans = %q", got)
	}
	if got := string(e.AppendSpans(nil, spans[1:2])); got != "\x1b[0;1mbold\x1b[0m" {
		t.Errorf("trailing reset = %q", got)
	}
}
