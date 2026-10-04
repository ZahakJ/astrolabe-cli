package render

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/folio/internal/md"
	"github.com/ZahakJ/folio/internal/term"
	"github.com/ZahakJ/folio/internal/theme"
)

// The show flags print a rendering of a note for looking at by eye:
//
//	go test ./internal/render -run TestShow -show ../../examples/vault/Typography.md \
//	    -width 80 -color truecolor -ascii=false -bidi=true
var (
	showFile  = flag.String("show", "", "render this Markdown file to stdout (TestShow)")
	showWidth = flag.Int("width", 80, "pane width for -show")
	showColor = flag.String("color", "truecolor", "colour profile for -show: truecolor|256|16|none|plain")
	showASCII = flag.Bool("ascii", false, "ASCII glyphs for -show")
	showBidi  = flag.Bool("bidi", true, "bidi display transform for -show")
	showTheme = flag.String("theme", "iron-gall", "theme for -show")
	showFolds = flag.String("folds", "", "comma-separated fold ids to toggle for -show")
)

// fakeResolver resolves the notes of the example vault used by the tests.
type fakeResolver struct{ notes map[string]string }

func (f fakeResolver) Resolve(q LinkQuery) (string, bool) {
	t := strings.TrimSuffix(q.Target, ".md")
	for name := range f.notes {
		if strings.EqualFold(strings.TrimSuffix(name, ".md"), t) {
			return name, true
		}
	}
	if isImageName(t) && !strings.Contains(strings.ToLower(t), "missing") {
		return t, true
	}
	return "", false
}

func (f fakeResolver) Embed(path string) (string, string, bool) {
	src, ok := f.notes[path]
	return strings.TrimSuffix(path, ".md"), src, ok
}

const astrolabeNote = `# Astrolabe

An astrolabe is an inclined plane of brass on which the sky is drawn in
stereographic projection.

## The rete

The rete carries pointers for the brightest stars.

- Aldebaran
- Vega
- Regulus

## Plates

Each plate serves one latitude.

Line ten of the embed.
Line eleven.
Line twelve.
Line thirteen, which should be cut.
`

func testResolver() Resolver {
	return fakeResolver{notes: map[string]string{
		"Astrolabe.md":    astrolabeNote,
		"Reading list.md": "# Reading list\n",
		"Target.md":       "# Target\n\ntarget body\n\n## Section\n\nmore\n",
	}}
}

func TestShow(t *testing.T) {
	if *showFile == "" {
		t.Skip("use -show FILE to print a rendering")
	}
	src, err := os.ReadFile(*showFile)
	if err != nil {
		t.Fatal(err)
	}
	th, ok := theme.Lookup(*showTheme)
	if !ok {
		t.Fatalf("unknown theme %q", *showTheme)
	}
	folds := map[string]bool{}
	if *showFolds != "" {
		for _, id := range strings.Split(*showFolds, ",") {
			folds[id] = !folds[id]
		}
	}
	opt := Options{Width: *showWidth, Glyphs: theme.GlyphsFor(*showASCII), Bidi: *showBidi,
		Resolver: testResolver(), Today: time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC), Backlinks: 3,
		Title: strings.TrimSuffix(filepath.Base(*showFile), ".md"), Folds: folds}
	if *showColor == "plain" {
		opt.Theme = th
		fmt.Print(Plain(string(src), opt))
		return
	}
	prof, _, err := term.ParseProfile(*showColor)
	if err != nil {
		t.Fatal(err)
	}
	opt.Theme = term.ThemeFor(th, prof)
	opt.PaintGround = prof >= term.Profile256
	page := Render(md.Parse(string(src)), opt)
	if *showSVG != "" {
		if err := writeSVG(*showSVG, page, prof, opt.Theme.Dark || prof < term.Profile256); err != nil {
			t.Fatal(err)
		}
		return
	}
	fmt.Print(page.ANSI(term.Encoder{Profile: prof, StyledUnderline: true}))
}
