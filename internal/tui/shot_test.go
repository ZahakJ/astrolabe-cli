package tui

import (
	"flag"
	"fmt"
	"html"
	"os"
	"strings"
	"testing"

	"github.com/ZahakJ/astrolabe-cli/internal/term/termtest"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
)

// Screenshots for looking at the TUI by eye (not run by default):
//
//	tmux capture-pane -p -e > shot.ansi
//	go test ./internal/tui -run TestShot -capture shot.ansi -svg shot.svg -cols 120 -rows 36
//	rsvg-convert shot.svg -o shot.png
var (
	shotCapture = flag.String("capture", "", "TestShot: tmux capture-pane -e output to convert")
	shotSVG     = flag.String("svg", "", "TestShot: SVG output path")
	shotCols    = flag.Int("cols", 120, "TestShot: terminal width")
	shotRows    = flag.Int("rows", 36, "TestShot: terminal height")
	shotLight   = flag.Bool("light", false, "TestShot: default colours of a light terminal")
)

func TestShot(t *testing.T) {
	if *shotCapture == "" || *shotSVG == "" {
		t.Skip("screenshot helper: pass -capture and -svg")
	}
	b, err := os.ReadFile(*shotCapture)
	if err != nil {
		t.Fatal(err)
	}
	vt := termtest.New(*shotCols, *shotRows)
	s := strings.TrimRight(string(b), "\n")
	s = strings.ReplaceAll(s, "\n", "\r\n")
	vt.Write([]byte(s))
	if err := os.WriteFile(*shotSVG, []byte(vtSVG(vt, !*shotLight)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// vtSVG draws a virtual terminal's grid as SVG: one text element per cell.
func vtSVG(vt *termtest.VT, dark bool) string {
	const cw, ch = 8.4, 17.0
	defFG, defBG := "#d4d4d4", "#1c1c1c"
	if !dark {
		defFG, defBG = "#202020", "#f8f8f4"
	}
	hex := func(c theme.Color, def string) string {
		if c.IsDefault() {
			return def
		}
		r, g, b := c.Components()
		return fmt.Sprintf("#%02x%02x%02x", r, g, b)
	}
	w, h := vt.Size()
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%.0f">`+"\n", float64(w)*cw, float64(h)*ch)
	fmt.Fprintf(&b, `<rect width="100%%" height="100%%" fill="%s"/>`+"\n", defBG)
	b.WriteString(`<g font-family="DejaVu Sans Mono, Noto Sans Arabic, monospace" font-size="14">` + "\n")
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := vt.Cell(x, y)
			if c.Width == 0 && c.Text == "" {
				continue
			}
			st := c.Style
			fg, bg := hex(st.FG, defFG), hex(st.BG, "")
			if st.Attrs&theme.Reverse != 0 {
				fg, bg = hex(st.BG, defBG), hex(st.FG, defFG)
			}
			px, py := float64(x)*cw, float64(y)*ch
			cells := max(1, c.Width)
			if bg != "" {
				fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`, px, py, cw*float64(cells)+0.5, ch+0.5, bg)
			}
			if strings.TrimSpace(c.Text) == "" {
				continue
			}
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
			fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" fill="%s"%s>%s</text>`, px, py+ch-4, fg, attrs, html.EscapeString(c.Text))
		}
		b.WriteString("\n")
	}
	b.WriteString("</g></svg>\n")
	return b.String()
}
