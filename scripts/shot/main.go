// Command shot turns a tmux pane capture (`tmux capture-pane -p -e`) into an
// SVG screenshot drawn the way a terminal draws it: every cell's background
// is a rectangle and every grapheme is its own absolutely positioned <text>
// element at its cell's x, so a browser cannot re-flow, kern, re-shape or
// bidi-reorder anything. Box-drawing hairlines and the partial-block bars
// astrolabe uses (─ │ ┼ ╭ ▎ …) are drawn as vector shapes so they join cleanly
// regardless of the viewer's font. The result sits in a rounded window frame
// with a subtle title bar, and only uses system font stacks, so it renders
// the same on GitHub (where SVGs may not load web fonts) as locally.
//
// It is normally driven by scripts/shot.sh:
//
//	go run ./scripts/shot -in pane.ansi -cols 110 -rows 32 -title astrolabe -svg out.svg
//
// Colours: truecolour SGR is used as given; 256-colour indexes 16–255 use
// the xterm cube and grey ramp; the 16 ANSI colours and the default
// foreground/background use a neutral palette (-light for a light one), as
// a typical terminal would.
package main

import (
	"flag"
	"fmt"
	"html"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/ZahakJ/astrolabe-cli/internal/term/termtest"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
)

// Cell geometry in SVG user units. 0.6 em is the advance of DejaVu Sans
// Mono, Menlo, SF Mono and (nearly) Consolas at this size.
const (
	fontSize = 14.0
	cellW    = 8.4
	cellH    = 18.0
	baseline = 13.6 // text baseline offset from the cell's top
	padX     = 14.0
	padY     = 10.0
	barH     = 30.0 // title bar height
	radius   = 9.0
)

const fontStack = `'SF Mono', ui-monospace, Menlo, Consolas, 'DejaVu Sans Mono', 'Liberation Mono', 'Noto Sans Mono', monospace`

// palette is the default foreground/background and the 16 ANSI colours of
// the imaginary terminal the capture is shown in.
type palette struct {
	fg, bg string
	ansi   [16]string
}

var darkPalette = palette{
	fg: "#d6d3cc", bg: "#1b1b1d",
	ansi: [16]string{
		"#2b2b2f", "#d4706b", "#a4b56a", "#e0b450", "#7d9dc4", "#b48ead", "#82b5ad", "#c8c5bd",
		"#6e6a63", "#e48a84", "#bccc84", "#f0cf74", "#9cb6d8", "#c9a4c3", "#9ccbc3", "#ecebe6",
	},
}

var lightPalette = palette{
	fg: "#2a2723", bg: "#f7f5f0",
	ansi: [16]string{
		"#2a2723", "#b0403a", "#5b7a2a", "#9a6e00", "#355f9a", "#8a4f86", "#2f7a73", "#d9d5cc",
		"#7c776e", "#c8554e", "#6e8f35", "#b08419", "#4a76b4", "#a2629e", "#3e938a", "#ffffff",
	},
}

type options struct {
	in, out     string
	cols, rows  int
	title       string
	light       bool
	cursor      string // "x y shape" (shape block|bar|underline); empty = none
	cursorColor string
	frame       bool
}

func main() {
	var o options
	flag.StringVar(&o.in, "in", "-", "capture-pane -e output (- for stdin)")
	flag.StringVar(&o.out, "svg", "-", "SVG output path (- for stdout)")
	flag.IntVar(&o.cols, "cols", 80, "pane width in cells")
	flag.IntVar(&o.rows, "rows", 24, "pane height in cells")
	flag.StringVar(&o.title, "title", "", "window title")
	flag.BoolVar(&o.light, "light", false, "light default colours")
	flag.StringVar(&o.cursor, "cursor", "", `draw the cursor: "X Y SHAPE" (block, bar or underline)`)
	flag.StringVar(&o.cursorColor, "cursor-color", "", "cursor colour (default: the cell's foreground)")
	flag.BoolVar(&o.frame, "frame", true, "draw the window frame and title bar")
	flag.Parse()

	var r io.Reader = os.Stdin
	if o.in != "-" {
		f, err := os.Open(o.in)
		if err != nil {
			fatal(err)
		}
		defer f.Close()
		r = f
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		fatal(err)
	}
	vt := load(raw, o.cols, o.rows)
	svg := draw(vt, o)
	if o.out == "-" {
		os.Stdout.WriteString(svg)
		return
	}
	if err := os.WriteFile(o.out, []byte(svg), 0o644); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "shot:", err)
	os.Exit(1)
}

// load applies a capture to a virtual terminal. capture-pane separates rows
// with bare LF and emits at most `rows` lines.
func load(raw []byte, cols, rows int) *termtest.VT {
	vt := termtest.New(cols, rows)
	s := strings.TrimRight(string(raw), "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > rows {
		lines = lines[len(lines)-rows:]
	}
	for i, ln := range lines {
		// Position every row explicitly: a row that fills the width must
		// not wrap into the next one, and SGR state carries across rows
		// the way capture-pane expects.
		fmt.Fprintf(vt, "\x1b[%d;1H%s", i+1, ln)
	}
	return vt
}

type cellStyle struct {
	fg, bg             string
	bold, italic       bool
	under, curly, dots bool
	strike             bool
}

type drawer struct {
	pal     palette
	body    string // dominant background
	classes map[string]string
	order   []string
}

func (d *drawer) color(c theme.Color, def string) string {
	switch {
	case c.IsDefault():
		return def
	case c.IsRGB():
		r, g, b := c.Components()
		return fmt.Sprintf("#%02x%02x%02x", r, g, b)
	default:
		i := c.Index()
		if i < 16 {
			return d.pal.ansi[i]
		}
		r, g, b := theme.XtermPalette(i)
		return fmt.Sprintf("#%02x%02x%02x", r, g, b)
	}
}

func (d *drawer) resolve(st theme.Style) cellStyle {
	fg, bg := d.color(st.FG, d.pal.fg), d.color(st.BG, d.pal.bg)
	if st.Attrs.Has(theme.Bold) && st.FG.IsIndexed() && st.FG.Index() < 8 {
		// Like most terminals, bold brightens the 8 base colours.
		fg = d.pal.ansi[st.FG.Index()+8]
	}
	if st.Attrs.Has(theme.Reverse) {
		fg, bg = bg, fg
	}
	if st.Attrs.Has(theme.Faint) {
		fg = mix(fg, bg, 0.42)
	}
	return cellStyle{
		fg: fg, bg: bg,
		bold:   st.Attrs.Has(theme.Bold),
		italic: st.Attrs.Has(theme.Italic),
		under:  st.Attrs.Has(theme.Underline),
		curly:  st.Attrs.Has(theme.CurlyUnderline),
		dots:   st.Attrs.Has(theme.DottedUnderline),
		strike: st.Attrs.Has(theme.Strike),
	}
}

// class returns a short CSS class for a text style, so each glyph element
// carries one attribute instead of three.
func (d *drawer) class(cs cellStyle) string {
	key := fmt.Sprintf("fill:%s", cs.fg)
	if cs.bold {
		key += ";font-weight:bold"
	}
	if cs.italic {
		key += ";font-style:italic"
	}
	if c, ok := d.classes[key]; ok {
		return c
	}
	c := "t" + strconv.Itoa(len(d.classes))
	d.classes[key] = c
	d.order = append(d.order, key)
	return c
}

func draw(vt *termtest.VT, o options) string {
	pal := darkPalette
	if o.light {
		pal = lightPalette
	}
	d := &drawer{pal: pal, classes: map[string]string{}}
	w, h := vt.Size()

	// The window body takes the most common background of the outermost
	// columns and the top row (where an application paints its ground,
	// not its content), so a painted theme ground fills the frame edge to
	// edge.
	count := map[string]int{}
	for y := 0; y < h; y++ {
		count[d.resolve(vt.Cell(0, y).Style).bg]++
		count[d.resolve(vt.Cell(w-1, y).Style).bg]++
	}
	for x := 0; x < w; x++ {
		count[d.resolve(vt.Cell(x, 0).Style).bg]++
	}
	d.body = pal.bg
	best := -1
	for c, n := range count {
		if n > best || (n == best && c < d.body) {
			d.body, best = c, n
		}
	}

	ox, oy := 0.0, 0.0
	totalW, totalH := float64(w)*cellW, float64(h)*cellH
	if o.frame {
		ox, oy = padX, barH+padY*0.6
		totalW += 2 * padX
		totalH += barH + padY*1.6
	}

	var bgs, glyphs, lines strings.Builder
	cursorX, cursorY, cursorShape := -1, -1, ""
	if o.cursor != "" {
		f := strings.Fields(o.cursor)
		if len(f) >= 2 {
			cursorX, _ = strconv.Atoi(f[0])
			cursorY, _ = strconv.Atoi(f[1])
			cursorShape = "block"
			if len(f) >= 3 && f[2] != "default" && f[2] != "" {
				cursorShape = f[2]
			}
		}
	}

	for y := 0; y < h; y++ {
		py := oy + float64(y)*cellH
		// Backgrounds, merged into runs.
		runStart, runColor := 0, ""
		flush := func(end int) {
			if runColor != "" && runColor != d.body && end > runStart {
				fmt.Fprintf(&bgs, `<rect x="%s" y="%s" width="%s" height="%s" fill="%s"/>`+"\n",
					num(ox+float64(runStart)*cellW), num(py), num(float64(end-runStart)*cellW+0.3), num(cellH+0.3), runColor)
			}
		}
		for x := 0; x < w; x++ {
			c := vt.Cell(x, y)
			bg := d.resolve(c.Style).bg
			if bg != runColor {
				flush(x)
				runStart, runColor = x, bg
			}
		}
		flush(w)

		for x := 0; x < w; x++ {
			c := vt.Cell(x, y)
			if c.Width == 0 {
				continue
			}
			cs := d.resolve(c.Style)
			px := ox + float64(x)*cellW
			cells := float64(max(1, c.Width))
			isCursor := x == cursorX && y == cursorY
			if isCursor {
				cc := cs.fg
				if o.cursorColor != "" {
					cc = o.cursorColor
				}
				switch cursorShape {
				case "bar":
					fmt.Fprintf(&lines, `<rect x="%s" y="%s" width="2" height="%s" fill="%s"/>`+"\n", num(px), num(py+1), num(cellH-2), cc)
				case "underline":
					fmt.Fprintf(&lines, `<rect x="%s" y="%s" width="%s" height="2" fill="%s"/>`+"\n", num(px), num(py+cellH-2.5), num(cellW*cells), cc)
				default:
					fmt.Fprintf(&bgs, `<rect x="%s" y="%s" width="%s" height="%s" fill="%s"/>`+"\n", num(px), num(py), num(cellW*cells), num(cellH), cc)
					cs.fg = cs.bg
				}
			}
			txt := c.Text
			if strings.TrimSpace(txt) != "" {
				if !vector(&lines, txt, px, py, cs.fg) {
					tx := px
					if c.Width == 2 {
						tx += cellW // centre wide glyphs in their two cells
						fmt.Fprintf(&glyphs, `<text class="%s" x="%s" y="%s" text-anchor="middle">%s</text>`+"\n",
							d.class(cs), num(tx), num(py+baseline), html.EscapeString(txt))
					} else {
						fmt.Fprintf(&glyphs, `<text class="%s" x="%s" y="%s">%s</text>`+"\n",
							d.class(cs), num(tx), num(py+baseline), html.EscapeString(txt))
					}
				}
			}
			wpx := cellW * cells
			switch {
			case cs.curly:
				lines.WriteString(wave(px, py+cellH-2.5, wpx, cs.fg))
			case cs.dots:
				fmt.Fprintf(&lines, `<line x1="%s" y1="%s" x2="%s" y2="%s" stroke="%s" stroke-width="1" stroke-dasharray="1.4 1.6"/>`+"\n",
					num(px), num(py+cellH-2.5), num(px+wpx), num(py+cellH-2.5), cs.fg)
			case cs.under:
				fmt.Fprintf(&lines, `<rect x="%s" y="%s" width="%s" height="1" fill="%s"/>`+"\n", num(px), num(py+cellH-3), num(wpx), cs.fg)
			}
			if cs.strike {
				fmt.Fprintf(&lines, `<rect x="%s" y="%s" width="%s" height="1" fill="%s"/>`+"\n", num(px), num(py+cellH/2), num(wpx), cs.fg)
			}
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%s" height="%s" viewBox="0 0 %s %s">`+"\n",
		num(totalW), num(totalH), num(totalW), num(totalH))
	b.WriteString("<style>\n")
	fmt.Fprintf(&b, "text{font-family:%s;font-size:%spx;white-space:pre;direction:ltr;unicode-bidi:bidi-override;font-variant-ligatures:none;font-kerning:none}\n", fontStack, num(fontSize))
	for _, k := range d.order {
		fmt.Fprintf(&b, ".%s{%s}\n", d.classes[k], k)
	}
	b.WriteString("</style>\n")
	if o.frame {
		bar := mix(d.body, contrastInk(d.body), 0.06)
		edge := mix(d.body, contrastInk(d.body), 0.16)
		dot := mix(d.body, contrastInk(d.body), 0.24)
		fmt.Fprintf(&b, `<rect x="0.5" y="0.5" width="%s" height="%s" rx="%s" fill="%s" stroke="%s"/>`+"\n",
			num(totalW-1), num(totalH-1), num(radius), d.body, edge)
		// Title bar: rounded on top only.
		fmt.Fprintf(&b, `<path d="M0.5 %s V%s A%s %s 0 0 1 %s 0.5 H%s A%s %s 0 0 1 %s %s V%s Z" fill="%s"/>`+"\n",
			num(barH), num(radius+0.5), num(radius), num(radius), num(radius+0.5), num(totalW-radius-0.5),
			num(radius), num(radius), num(totalW-0.5), num(radius+0.5), num(barH), bar)
		fmt.Fprintf(&b, `<rect x="0.5" y="%s" width="%s" height="1" fill="%s"/>`+"\n", num(barH), num(totalW-1), edge)
		for i := 0; i < 3; i++ {
			fmt.Fprintf(&b, `<circle cx="%s" cy="%s" r="5.5" fill="%s"/>`+"\n", num(18+float64(i)*18), num(barH/2+0.5), dot)
		}
		if o.title != "" {
			fmt.Fprintf(&b, `<text x="%s" y="%s" text-anchor="middle" style="font-family:-apple-system,'Segoe UI',system-ui,'DejaVu Sans',sans-serif;font-size:12.5px;fill:%s;direction:ltr">%s</text>`+"\n",
				num(totalW/2), num(barH/2+4.8), mix(d.body, contrastInk(d.body), 0.55), html.EscapeString(o.title))
		}
	} else {
		fmt.Fprintf(&b, `<rect width="100%%" height="100%%" fill="%s"/>`+"\n", d.body)
	}
	b.WriteString(`<g shape-rendering="crispEdges">` + "\n")
	b.WriteString(bgs.String())
	b.WriteString("</g>\n<g>\n")
	b.WriteString(glyphs.String())
	b.WriteString("</g>\n<g>\n")
	b.WriteString(lines.String())
	b.WriteString("</g>\n</svg>\n")
	return b.String()
}

// vector draws box-drawing and block glyphs as shapes. It reports false for
// any other text.
func vector(b *strings.Builder, g string, x, y float64, ink string) bool {
	r := []rune(g)
	if len(r) != 1 {
		return false
	}
	mx, my := x+cellW/2, y+cellH/2
	x2, y2 := x+cellW, y+cellH
	line := func(x1, y1, xb, yb, w float64) {
		fmt.Fprintf(b, `<line x1="%s" y1="%s" x2="%s" y2="%s" stroke="%s" stroke-width="%s"/>`+"\n",
			num(x1), num(y1), num(xb), num(yb), ink, num(w))
	}
	rect := func(rx, ry, rw, rh float64) {
		fmt.Fprintf(b, `<rect x="%s" y="%s" width="%s" height="%s" fill="%s"/>`+"\n", num(rx), num(ry), num(rw), num(rh), ink)
	}
	// Light lines: segments from the centre to each named edge.
	segs := map[rune]string{
		'─': "lr", '│': "ud", '┼': "lrud", '├': "rud", '┤': "lud", '┬': "lrd", '┴': "lru",
		'┌': "rd", '┐': "ld", '└': "ru", '┘': "lu", '╴': "l", '╶': "r", '╵': "u", '╷': "d",
	}
	const lw = 1.0
	if s, ok := segs[r[0]]; ok {
		for _, e := range s {
			switch e {
			// Segments overrun the cell edge slightly so neighbouring
			// cells join without hairline gaps when rasterised.
			case 'l':
				line(x-0.4, my, mx+lw/2, my, lw)
			case 'r':
				line(mx-lw/2, my, x2+0.4, my, lw)
			case 'u':
				line(mx, y-0.4, mx, my+lw/2, lw)
			case 'd':
				line(mx, my-lw/2, mx, y2+0.4, lw)
			}
		}
		return true
	}
	rr := cellW / 2
	arc := func(d string) {
		fmt.Fprintf(b, `<path d="%s" fill="none" stroke="%s" stroke-width="%s"/>`+"\n", d, ink, num(lw))
	}
	switch r[0] {
	case '━':
		line(x, my, x2, my, 2.2)
	case '┃':
		line(mx, y, mx, y2, 2.4)
	case '╭':
		arc(fmt.Sprintf("M%s %s V%s A%s %s 0 0 1 %s %s H%s", num(mx), num(y2), num(my+rr), num(rr), num(rr), num(mx+rr), num(my), num(x2)))
	case '╮':
		arc(fmt.Sprintf("M%s %s H%s A%s %s 0 0 1 %s %s V%s", num(x), num(my), num(mx-rr), num(rr), num(rr), num(mx), num(my+rr), num(y2)))
	case '╰':
		arc(fmt.Sprintf("M%s %s V%s A%s %s 0 0 0 %s %s H%s", num(mx), num(y), num(my-rr), num(rr), num(rr), num(mx+rr), num(my), num(x2)))
	case '╯':
		arc(fmt.Sprintf("M%s %s H%s A%s %s 0 0 0 %s %s V%s", num(x), num(my), num(mx-rr), num(rr), num(rr), num(mx), num(my-rr), num(y)))
	case '█':
		rect(x, y, cellW+0.3, cellH+0.3)
	case '▌':
		rect(x, y, cellW/2, cellH+0.3)
	case '▐':
		rect(x+cellW/2, y, cellW/2+0.3, cellH+0.3)
	case '▍':
		rect(x, y, cellW*3/8, cellH+0.3)
	case '▎':
		rect(x, y, cellW/4, cellH+0.3)
	case '▏':
		rect(x, y, cellW/8+0.4, cellH+0.3)
	case '▕':
		rect(x2-cellW/8-0.4, y, cellW/8+0.4, cellH+0.3)
	case '▁':
		rect(x, y2-cellH/8, cellW+0.3, cellH/8)
	case '▔':
		rect(x, y, cellW+0.3, cellH/8)
	default:
		return false
	}
	return true
}

func wave(x, y, w float64, ink string) string {
	var p strings.Builder
	fmt.Fprintf(&p, "M%s %s", num(x), num(y))
	step := cellW / 4
	up := true
	for cx := x; cx < x+w-0.01; cx += step {
		dy := -1.2
		if !up {
			dy = 1.2
		}
		fmt.Fprintf(&p, " Q%s %s %s %s", num(cx+step/2), num(y+dy*2), num(cx+step), num(y))
		up = !up
	}
	return fmt.Sprintf(`<path d="%s" fill="none" stroke="%s" stroke-width="0.9"/>`+"\n", p.String(), ink)
}

func num(f float64) string {
	s := strconv.FormatFloat(f, 'f', 2, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

func parse(hex string) (r, g, b float64) {
	c, err := theme.ParseHex(hex)
	if err != nil {
		return 0, 0, 0
	}
	ri, gi, bi := c.Components()
	return float64(ri), float64(gi), float64(bi)
}

// mix moves colour a toward b by t (0 = a, 1 = b).
func mix(a, b string, t float64) string {
	ar, ag, ab := parse(a)
	br, bg, bb := parse(b)
	f := func(p, q float64) int { return int(p + (q-p)*t + 0.5) }
	return fmt.Sprintf("#%02x%02x%02x", f(ar, br), f(ag, bg), f(ab, bb))
}

// contrastInk is white on dark colours and black on light ones.
func contrastInk(c string) string {
	r, g, b := parse(c)
	if 0.299*r+0.587*g+0.114*b < 128 {
		return "#ffffff"
	}
	return "#000000"
}
