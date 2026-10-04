package theme

import (
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/rivo/uniseg"
)

func TestParseHex(t *testing.T) {
	tests := []struct {
		in      string
		want    Color
		wantErr bool
	}{
		{"#16130e", RGB(0x16, 0x13, 0x0e), false},
		{"c9a227", RGB(0xc9, 0xa2, 0x27), false},
		{"#fff", RGB(255, 255, 255), false},
		{"#000000", RGB(0, 0, 0), false},
		{"#12345", Default, true},
		{"#gggggg", Default, true},
		{"", Default, true},
	}
	for _, tt := range tests {
		got, err := ParseHex(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("ParseHex(%q) = %v, %v; want %v, err=%v", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestColorKinds(t *testing.T) {
	black := RGB(0, 0, 0)
	if black.IsDefault() || !black.IsRGB() || black.IsIndexed() {
		t.Error("RGB black must not be Default")
	}
	if !Default.IsDefault() || Default.IsRGB() || Default.IsIndexed() {
		t.Error("Default kinds wrong")
	}
	if !Yellow.IsIndexed() || Yellow.Index() != 3 || BrightBlack.Index() != 8 {
		t.Error("indexed colours wrong")
	}
	if ANSI(0) == Default {
		t.Error("ANSI(0) must differ from Default")
	}
	if s := Hex("#c9a227").String(); s != "#c9a227" {
		t.Errorf("String = %q", s)
	}
	if s := ANSI(42).String(); s != "ansi:42" {
		t.Errorf("String = %q", s)
	}
}

func TestXtermPalette(t *testing.T) {
	tests := []struct {
		i       uint8
		r, g, b uint8
	}{
		{16, 0, 0, 0}, {231, 255, 255, 255}, {196, 255, 0, 0}, {232, 8, 8, 8}, {255, 238, 238, 238},
		{1, 205, 0, 0},
	}
	for _, tt := range tests {
		r, g, b := XtermPalette(tt.i)
		if r != tt.r || g != tt.g || b != tt.b {
			t.Errorf("XtermPalette(%d) = %d,%d,%d", tt.i, r, g, b)
		}
	}
}

func TestContrastKnownValues(t *testing.T) {
	if c := Contrast(RGB(0, 0, 0), RGB(255, 255, 255)); c < 20.99 || c > 21.01 {
		t.Errorf("black/white contrast = %v", c)
	}
	if c := Contrast(Hex("#777777"), Hex("#777777")); c != 1 {
		t.Errorf("self contrast = %v", c)
	}
}

func TestStyleOver(t *testing.T) {
	base := Style{FG: Hex("#111111"), BG: Hex("#222222"), Attrs: Italic}
	top := Style{FG: Hex("#333333"), Attrs: Bold, Link: "https://example.org"}
	got := top.Over(base)
	want := Style{FG: Hex("#333333"), BG: Hex("#222222"), Attrs: Bold | Italic, Link: "https://example.org"}
	if got != want {
		t.Errorf("Over = %+v, want %+v", got, want)
	}
	if s := (Bold | Strike).String(); s != "bold+strike" {
		t.Errorf("Attr.String = %q", s)
	}
}

// contrastFloors are DESIGN.md §7's minimum ratios against the ground, plus
// the floors folio holds itself to for other ink that must stay readable.
func TestThemeContrast(t *testing.T) {
	for _, name := range Names() {
		th, ok := Lookup(name)
		if !ok {
			t.Fatalf("theme %q not registered", name)
		}
		checks := []struct {
			token string
			fg    Color
			bg    Color
			min   float64
		}{
			{"text/ground", th.Text, th.Ground, 7},
			{"muted/ground", th.Muted, th.Ground, 4.5},
			{"faint/ground", th.Faint, th.Ground, 3},
			// Beyond §7: ink that carries meaning stays readable.
			{"text/raised", th.Text, th.Raised, 7},
			{"text/cursorline", th.Text, th.CursorLine.BG, 7},
			{"muted/raised", th.Muted, th.Raised, 4.5},
			{"faint/raised", th.Faint, th.Raised, 3},
			{"heading/ground", th.Heading, th.Ground, 7},
			{"accent/ground", th.Accent, th.Ground, 4.5},
			{"link/ground", th.Link, th.Ground, 4.5},
			{"danger/ground", th.Danger, th.Ground, 4.5},
			{"ok/ground", th.Ok, th.Ground, 4.5},
			{"math/ground", th.Math, th.Ground, 4.5},
			{"text/hover", th.Text, th.Hover, 4.5},
			{"code comment/raised", th.CodeComment, th.Raised, 3},
			{"code string/raised", th.CodeString, th.Raised, 4.5},
			{"code number/raised", th.CodeNumber, th.Raised, 4.5},
			{"code keyword/raised", th.CodeKeyword, th.Raised, 4.5},
			{"selection", th.Selection.FG, th.Selection.BG, 4.5},
			{"highlight", th.Highlight.FG, th.Highlight.BG, 4.5},
			{"linkfocus", th.LinkFocus.FG, th.LinkFocus.BG, 4.5},
			{"search", th.Search.FG, th.Search.BG, 4.5},
			{"search current", th.SearchCurrent.FG, th.SearchCurrent.BG, 4.5},
			{"status bar", th.StatusBar.FG, th.StatusBar.BG, 4.5},
			{"pill read", th.PillRead.FG, th.PillRead.BG, 4.5},
			{"pill normal", th.PillNormal.FG, th.PillNormal.BG, 4.5},
			{"pill insert", th.PillInsert.FG, th.PillInsert.BG, 4.5},
			{"pill visual", th.PillVisual.FG, th.PillVisual.BG, 4.5},
		}
		for _, k := range CalloutKinds() {
			checks = append(checks, struct {
				token string
				fg    Color
				bg    Color
				min   float64
			}{"callout " + k, th.CalloutColor(k), th.Ground, 4.5})
		}
		for _, c := range checks {
			if !c.fg.IsRGB() || !c.bg.IsRGB() {
				t.Errorf("%s: %s must be RGB (fg %v, bg %v)", name, c.token, c.fg, c.bg)
				continue
			}
			if r := Contrast(c.fg, c.bg); r < c.min {
				t.Errorf("%s: %s contrast %.2f < %.1f (%v on %v)", name, c.token, r, c.min, c.fg, c.bg)
			}
		}
	}
}

func TestThemeTokensComplete(t *testing.T) {
	// Every colour field of every built-in theme must be set (non-Default).
	for _, name := range Names() {
		th, _ := Lookup(name)
		v := reflect.ValueOf(th)
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			if f.Type == reflect.TypeOf(Color(0)) && v.Field(i).Interface().(Color).IsDefault() {
				t.Errorf("%s: token %s unset", name, f.Name)
			}
		}
		cv := reflect.ValueOf(th.Callout)
		for i := 0; i < cv.NumField(); i++ {
			if cv.Field(i).Interface().(Color).IsDefault() {
				t.Errorf("%s: callout %s unset", name, cv.Type().Field(i).Name)
			}
		}
	}
}

func TestDesignAnchors(t *testing.T) {
	// The colours DESIGN.md §7 fixes by value.
	tests := []struct {
		theme, token string
		got, want    Color
	}{
		{"iron-gall", "ground", IronGall.Ground, Hex("#16130e")},
		{"iron-gall", "text", IronGall.Text, Hex("#eae2d0")},
		{"iron-gall", "accent", IronGall.Accent, Hex("#c9a227")},
		{"parchment", "ground", Parchment.Ground, Hex("#f2ebda")},
		{"parchment", "text", Parchment.Text, Hex("#33291a")},
		{"parchment", "accent", Parchment.Accent, Hex("#7a5f14")},
		{"graphite", "ground", Graphite.Ground, Hex("#0d1117")},
		{"graphite", "accent", Graphite.Accent, Hex("#c9a227")},
		{"mocha", "ground", Mocha.Ground, Hex("#1e1e2e")},
		{"mocha", "text", Mocha.Text, Hex("#cdd6f4")},
		{"mocha", "accent", Mocha.Accent, Hex("#cba6f7")},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s %s = %v, want %v", tt.theme, tt.token, tt.got, tt.want)
		}
	}
}

func TestLookupAndCycle(t *testing.T) {
	if DefaultTheme().Name != "iron-gall" || Names()[0] != "iron-gall" {
		t.Fatal("iron-gall must be the default")
	}
	if _, ok := Lookup("Parchment"); !ok {
		t.Error("lookup must be case-insensitive")
	}
	if _, ok := Lookup("nope"); ok {
		t.Error("unknown theme found")
	}
	seen := map[string]bool{}
	n := DefaultName
	for i := 0; i < len(Names()); i++ {
		seen[n] = true
		n = Next(n)
	}
	if n != DefaultName || len(seen) != 4 {
		t.Errorf("cycle broken: %v", seen)
	}
	if Next("unknown") != DefaultName {
		t.Error("Next of unknown must be the default")
	}
}

func TestBasic16AndMono(t *testing.T) {
	b := IronGall.Basic16()
	if !b.Ground.IsDefault() || !b.Raised.IsDefault() || !b.Hover.IsDefault() || !b.AccentSoft.IsDefault() {
		t.Error("16-colour variant must not paint grounds")
	}
	if b.Accent != Yellow || b.Faint != BrightBlack || b.Danger != Red || b.Ok != Green {
		t.Error("16-colour semantic mapping wrong")
	}
	if !b.Selection.Attrs.Has(Reverse) || !b.Selection.BG.IsDefault() {
		t.Error("16-colour selection must use reverse video")
	}
	// No RGB colour anywhere in the 16-colour or mono variants.
	for _, th := range []Theme{b, IronGall.Mono()} {
		walkColors(reflect.ValueOf(th), func(path string, c Color) {
			if c.IsRGB() {
				t.Errorf("%s: RGB colour %v in reduced theme", path, c)
			}
		}, "")
	}
	m := Mocha.Mono()
	if m.LinkAttrs != Underline || !m.Text.IsDefault() {
		t.Error("mono must underline links and use default ink")
	}
	g := IronGall.WithoutGround()
	if !g.Ground.IsDefault() || g.Raised.IsDefault() {
		t.Error("WithoutGround must drop only the page ground")
	}
}

func walkColors(v reflect.Value, fn func(string, Color), path string) {
	switch {
	case v.Type() == reflect.TypeOf(Color(0)):
		fn(path, v.Interface().(Color))
	case v.Kind() == reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				walkColors(v.Field(i), fn, path+"."+v.Type().Field(i).Name)
			}
		}
	}
}

func TestCalloutKind(t *testing.T) {
	tests := map[string]string{
		"note": "note", "NOTE": "note", "faq": "question", "tldr": "abstract",
		"caution": "warning", "error": "danger", "cite": "quote", "whatever": "note",
		" Tip ": "tip", "done": "success", "missing": "failure",
	}
	for in, want := range tests {
		if got := CalloutKind(in); got != want {
			t.Errorf("CalloutKind(%q) = %q, want %q", in, got, want)
		}
	}
	if IronGall.CalloutColor("caution") != IronGall.Callout.Warning {
		t.Error("CalloutColor alias")
	}
	if IronGall.CalloutColor("mystery") != IronGall.Callout.Note {
		t.Error("unknown callout must use the note hue")
	}
}

func glyphStrings(g Glyphs) map[string]string {
	out := map[string]string{}
	v := reflect.ValueOf(g)
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if !f.IsExported() || f.Name == "Name" {
			continue
		}
		switch x := v.Field(i).Interface().(type) {
		case string:
			out[f.Name] = x
		case [3]string:
			for j, s := range x {
				out[fmt.Sprintf("%s[%d]", f.Name, j)] = s
			}
		}
	}
	for _, k := range CalloutKinds() {
		out["Callout("+k+")"] = g.Callout(k)
	}
	return out
}

func TestUnicodeGlyphsSingleCell(t *testing.T) {
	for name, s := range glyphStrings(UnicodeGlyphs) {
		if s == "" {
			t.Errorf("glyph %s empty", name)
			continue
		}
		if name == "Ornament" {
			continue // a composed string of single-cell glyphs
		}
		if w := uniseg.StringWidth(s); w != 1 || uniseg.GraphemeClusterCount(s) != 1 {
			t.Errorf("glyph %s = %q has width %d", name, s, w)
		}
		for _, r := range s {
			if r >= 0xE000 && r <= 0xF8FF || r >= 0xF0000 {
				t.Errorf("glyph %s uses a private-use (Nerd Font) code point %U", name, r)
			}
			if r >= 0x1F000 {
				t.Errorf("glyph %s uses an emoji-range code point %U", name, r)
			}
		}
	}
}

func TestASCIIGlyphsAreASCII(t *testing.T) {
	for name, s := range glyphStrings(ASCIIGlyphs) {
		if s == "" {
			t.Errorf("glyph %s empty", name)
		}
		for _, r := range s {
			if r > 0x7e || r < 0x20 {
				t.Errorf("ASCII glyph %s = %q contains %U", name, s, r)
			}
		}
	}
	if g, ok := GlyphSet("ASCII"); !ok || g.Name != "ascii" {
		t.Error("GlyphSet(ascii)")
	}
	if GlyphsFor(false).Brand != "✦" || GlyphsFor(true).Brand != "*" {
		t.Error("GlyphsFor")
	}
}

// TestUnicodeGlyphsInDejaVu checks font coverage with fontconfig when DejaVu
// Sans Mono is installed (skipped otherwise, e.g. in CI).
func TestUnicodeGlyphsInDejaVu(t *testing.T) {
	if _, err := exec.LookPath("fc-list"); err != nil {
		t.Skip("fontconfig not available")
	}
	out, err := exec.Command("fc-list", ":family=DejaVu Sans Mono", "family").Output()
	if err != nil || !strings.Contains(string(out), "DejaVu Sans Mono") {
		t.Skip("DejaVu Sans Mono not installed")
	}
	seen := map[rune]bool{}
	for name, s := range glyphStrings(UnicodeGlyphs) {
		for _, r := range s {
			if r < 0x80 || seen[r] {
				continue
			}
			seen[r] = true
			out, err := exec.Command("fc-list", fmt.Sprintf(":charset=%x", r), "family").Output()
			if err != nil {
				t.Fatalf("fc-list: %v", err)
			}
			if !strings.Contains(string(out), "DejaVu Sans Mono") {
				t.Errorf("glyph %s %q (%U) missing from DejaVu Sans Mono", name, string(r), r)
			}
		}
	}
}
