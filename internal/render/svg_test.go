package render

import (
	"flag"
	"fmt"
	"html"
	"os"
	"strings"

	"github.com/ZahakJ/folio/internal/term"
	"github.com/ZahakJ/folio/internal/text"
	"github.com/ZahakJ/folio/internal/theme"
)

var showSVG = flag.String("svg", "", "with -show: also write the rendering as an SVG picture to this path")

// pageSVG draws a page as an SVG terminal screenshot: one text element per
// grapheme at its cell position, so misalignment is visible. Colours are
// taken after downsampling for prof; indexed colours use the xterm palette
// and the default ink/ground of a dark terminal.
func pageSVG(p *Page, prof term.Profile, dark bool) string {
	const cw, ch = 8.4, 17.0
	defFG, defBG := "#d4d4d4", "#1c1c1c"
	if !dark {
		defFG, defBG = "#202020", "#f8f8f4"
	}
	hex := func(c theme.Color, def string) string {
		switch {
		case c.IsDefault():
			return def
		default:
			r, g, b := c.Components()
			return fmt.Sprintf("#%02x%02x%02x", r, g, b)
		}
	}
	var b strings.Builder
	w := float64(p.Width) * cw
	h := float64(len(p.Lines)+1) * ch
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%.0f">`+"\n", w, h)
	fmt.Fprintf(&b, `<rect width="100%%" height="100%%" fill="%s"/>`+"\n", defBG)
	b.WriteString(`<g font-family="DejaVu Sans Mono, Noto Sans Arabic, Noto Sans CJK SC, monospace" font-size="14">` + "\n")
	for y, l := range p.Lines {
		x := 0
		for _, sp := range l.Spans {
			st := term.Downsample(sp.Style, prof)
			fg, bg := hex(st.FG, defFG), hex(st.BG, "")
			if st.Attrs&theme.Reverse != 0 {
				fg, bg = hex(st.BG, defBG), hex(st.FG, defFG)
			}
			for _, g := range text.Graphemes(sp.Text) {
				px := float64(x) * cw
				py := float64(y) * ch
				if bg != "" {
					fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`, px, py, cw*float64(g.Width)+0.5, ch+0.5, bg)
				}
				if strings.TrimSpace(g.Text) != "" {
					attrs := ""
					if st.Attrs&theme.Bold != 0 {
						attrs += ` font-weight="bold"`
					}
					if st.Attrs&theme.Italic != 0 {
						attrs += ` font-style="italic"`
					}
					if st.Attrs&theme.Faint != 0 {
						attrs += ` opacity="0.55"`
					}
					var deco []string
					if st.Attrs.AnyUnderline() {
						deco = append(deco, "underline")
					}
					if st.Attrs&theme.Strike != 0 {
						deco = append(deco, "line-through")
					}
					if len(deco) > 0 {
						attrs += ` text-decoration="` + strings.Join(deco, " ") + `"`
					}
					fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" fill="%s"%s>%s</text>`, px, py+ch-4, fg, attrs, html.EscapeString(g.Text))
				}
				x += g.Width
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("</g></svg>\n")
	return b.String()
}

func writeSVG(path string, p *Page, prof term.Profile, dark bool) error {
	return os.WriteFile(path, []byte(pageSVG(p, prof, dark)), 0o644)
}
