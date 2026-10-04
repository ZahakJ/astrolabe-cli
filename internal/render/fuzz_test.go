package render

import (
	"testing"

	"github.com/ZahakJ/folio/internal/md"
	"github.com/ZahakJ/folio/internal/text"
	"github.com/ZahakJ/folio/internal/theme"
)

// FuzzRender checks that any input renders without panicking and that no
// line exceeds the pane, at a narrow and a wide width, bidi on.
func FuzzRender(f *testing.F) {
	for _, src := range inputs(f) {
		f.Add(src)
	}
	f.Add("| a |\n|---|\n| \x1b |\n")
	f.Add("> [!x]-\n> - [ ] ```\n")
	f.Fuzz(func(t *testing.T, src string) {
		doc := md.Parse(src)
		for _, w := range []int{20, 97} {
			opt := Options{Width: w, Theme: theme.IronGall, Bidi: true, PaintGround: w > 50,
				Resolver: testResolver(), Today: testToday}
			p := Render(doc, opt)
			for i, l := range p.Lines {
				if got := text.SpansWidth(l.Spans); got > w || got != l.Width {
					t.Fatalf("w=%d line %d: width %d (recorded %d)", w, i, got, l.Width)
				}
			}
			for s := 0; s < doc.LineCount(); s++ {
				if i := p.LineForSource(s); len(p.Lines) > 0 && (i < 0 || i >= len(p.Lines)) {
					t.Fatalf("LineForSource(%d) = %d", s, i)
				}
			}
			_ = p.Plain()
		}
	})
}
