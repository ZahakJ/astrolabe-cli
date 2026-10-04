package term

import (
	"math"
	"os"
	"strconv"

	"github.com/ZahakJ/astrolabe-cli/internal/text"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
	xterm "golang.org/x/term"
)

func isTerminal(f *os.File) bool { return f != nil && xterm.IsTerminal(int(f.Fd())) }

func getSize(f *os.File) (int, int, error) { return xterm.GetSize(int(f.Fd())) }

// Encoder serialises theme styles as SGR escape sequences for a colour
// profile. The zero Encoder emits attributes only (ProfileNone). Encoders
// are small values; copy them freely.
type Encoder struct {
	Profile Profile
	// StyledUnderline enables curly (4:3) and dotted (4:4) underlines;
	// otherwise they become plain underlines.
	StyledUnderline bool
	// Hyperlinks enables OSC 8 output for Style.Link (see AppendLink).
	Hyperlinks bool
	// BidiRuns emits whole lines (Screen rows, AppendSpans) for a terminal
	// that reverses right-to-left runs itself (DESIGN.md §6, bidi=runs):
	// the text of each such run is emitted reversed, styles stay on their
	// cells. Styled and AppendSGR, which see only fragments, ignore it; see
	// RunsLine for text already serialised.
	BidiRuns bool
}

// Downsample reduces st to what the profile can show: in 256 colours RGB
// becomes the nearest palette entry; in 16 colours RGB foregrounds map to
// the nearest basic hue (or the default ink for greys) and grounds are
// dropped; with no colour every colour is dropped. Attributes and links are
// kept. Indexed colours pass through where the profile can show them.
//
// For the semantic 16-colour policy of DESIGN.md §6 (accent → yellow, faint
// → bright black, …) render with ThemeFor(t, Profile16), which maps tokens
// by meaning rather than by hue; Downsample is the fallback for stray RGB.
func Downsample(st theme.Style, p Profile) theme.Style {
	switch p {
	case ProfileTrueColor:
		return st
	case Profile256:
		st.FG = to256(st.FG)
		st.BG = to256(st.BG)
	case Profile16:
		st.FG = to16(st.FG)
		if st.BG.IsRGB() || (st.BG.IsIndexed() && st.BG.Index() >= 16) {
			st.BG = theme.Default
		}
	default:
		st.FG, st.BG = theme.Default, theme.Default
	}
	return st
}

func to256(c theme.Color) theme.Color {
	if c.IsRGB() {
		r, g, b := c.Components()
		return theme.ANSI(Nearest256(r, g, b))
	}
	return c
}

func to16(c theme.Color) theme.Color {
	switch {
	case c.IsRGB():
		r, g, b := c.Components()
		return Nearest16(r, g, b)
	case c.IsIndexed() && c.Index() >= 16:
		r, g, b := c.Components()
		return Nearest16(r, g, b)
	}
	return c
}

// ThemeFor returns the variant of t to render with on profile p: t itself
// for truecolour and 256 colours (the encoder downsamples), the semantic
// Basic16 variant for 16 colours and the attribute-only Mono variant for no
// colour.
func ThemeFor(t theme.Theme, p Profile) theme.Theme {
	switch p {
	case Profile16:
		return t.Basic16()
	case ProfileNone:
		return t.Mono()
	}
	return t
}

// AppendSGR appends the escape sequence that switches the terminal to st,
// starting from a full reset ("\x1b[0;…m"), so the result does not depend on
// the previous state. Links are not included (see AppendLink).
func (e Encoder) AppendSGR(dst []byte, st theme.Style) []byte {
	st = Downsample(st, e.Profile)
	dst = append(dst, "\x1b[0"...)
	a := st.Attrs
	if a&theme.Bold != 0 {
		dst = append(dst, ";1"...)
	}
	if a&theme.Faint != 0 {
		dst = append(dst, ";2"...)
	}
	if a&theme.Italic != 0 {
		dst = append(dst, ";3"...)
	}
	switch {
	case a&theme.CurlyUnderline != 0 && e.StyledUnderline:
		dst = append(dst, ";4:3"...)
	case a&theme.DottedUnderline != 0 && e.StyledUnderline:
		dst = append(dst, ";4:4"...)
	case a.AnyUnderline():
		dst = append(dst, ";4"...)
	}
	if a&theme.Reverse != 0 {
		dst = append(dst, ";7"...)
	}
	if a&theme.Strike != 0 {
		dst = append(dst, ";9"...)
	}
	dst = appendColor(dst, st.FG, false)
	dst = appendColor(dst, st.BG, true)
	return append(dst, 'm')
}

// SGR returns AppendSGR as a string.
func (e Encoder) SGR(st theme.Style) string { return string(e.AppendSGR(nil, st)) }

func appendColor(dst []byte, c theme.Color, bg bool) []byte {
	switch {
	case c.IsRGB():
		r, g, b := c.Components()
		if bg {
			dst = append(dst, ";48;2;"...)
		} else {
			dst = append(dst, ";38;2;"...)
		}
		dst = strconv.AppendUint(dst, uint64(r), 10)
		dst = append(dst, ';')
		dst = strconv.AppendUint(dst, uint64(g), 10)
		dst = append(dst, ';')
		dst = strconv.AppendUint(dst, uint64(b), 10)
	case c.IsIndexed():
		i := int(c.Index())
		base := 30
		if bg {
			base = 40
		}
		switch {
		case i < 8:
			dst = append(dst, ';')
			dst = strconv.AppendInt(dst, int64(base+i), 10)
		case i < 16:
			dst = append(dst, ';')
			dst = strconv.AppendInt(dst, int64(base+60+i-8), 10)
		default:
			if bg {
				dst = append(dst, ";48;5;"...)
			} else {
				dst = append(dst, ";38;5;"...)
			}
			dst = strconv.AppendInt(dst, int64(i), 10)
		}
	}
	return dst
}

// AppendLink appends an OSC 8 sequence opening a hyperlink to url, or
// closing the current one when url is empty. It appends nothing when
// hyperlinks are disabled.
func (e Encoder) AppendLink(dst []byte, url string) []byte {
	if !e.Hyperlinks {
		return dst
	}
	dst = append(dst, "\x1b]8;;"...)
	for i := 0; i < len(url); i++ {
		// Strip control bytes so a URL cannot end the sequence early.
		if c := url[i]; c >= 0x20 && c != 0x7f {
			dst = append(dst, c)
		}
	}
	return append(dst, "\x1b\\"...)
}

// Reset is the SGR sequence restoring default attributes and colours.
const Reset = "\x1b[0m"

// Styled wraps s in the SGR sequence for st and a reset, adding an OSC 8
// hyperlink when st has a link and hyperlinks are enabled. With
// ProfileNone and a zero style it returns s unchanged.
func (e Encoder) Styled(s string, st theme.Style) string {
	if s == "" {
		return ""
	}
	plain := st.Attrs == 0 && (e.Profile == ProfileNone || (st.FG.IsDefault() && st.BG.IsDefault()))
	if plain && (st.Link == "" || !e.Hyperlinks) {
		return s
	}
	var b []byte
	if st.Link != "" {
		b = e.AppendLink(b, st.Link)
	}
	if !plain {
		b = e.AppendSGR(b, st)
	}
	b = append(b, s...)
	if !plain {
		b = append(b, Reset...)
	}
	if st.Link != "" {
		b = e.AppendLink(b, "")
	}
	return string(b)
}

// AppendSpans serialises a line of styled spans, emitting SGR only when the
// style changes and OSC 8 around linked spans, and ending with a reset when
// anything was styled. It is the pipe/`astrolabe render` counterpart of drawing
// spans on a Screen. With ProfileNone and no links the text is appended
// unchanged except for attributes.
func (e Encoder) AppendSpans(dst []byte, spans []text.Span[theme.Style]) []byte {
	if e.BidiRuns {
		spans = runsSpans(spans)
	}
	var cur theme.Style
	styled := false
	link := ""
	for _, sp := range spans {
		if sp.Text == "" {
			continue
		}
		st := Downsample(sp.Style, e.Profile)
		if e.Hyperlinks && st.Link != link {
			dst = e.AppendLink(dst, st.Link)
			link = st.Link
		}
		st.Link = ""
		if st != cur {
			if st == (theme.Style{}) {
				dst = append(dst, Reset...)
			} else {
				dst = e.AppendSGR(dst, st)
			}
			cur = st
			styled = styled || st != (theme.Style{})
		}
		dst = append(dst, sp.Text...)
	}
	if link != "" {
		dst = e.AppendLink(dst, "")
	}
	if styled && cur != (theme.Style{}) {
		dst = append(dst, Reset...)
	}
	return dst
}

// Nearest256 returns the xterm palette index (16–255) closest to an RGB
// colour, searching the 6×6×6 cube and the 24-step grey ramp with a
// perceptual ("redmean") distance. The 16 basic colours are never chosen
// because their values depend on the user's palette.
func Nearest256(r, g, b uint8) uint8 {
	// Nearest cube coordinate per channel.
	ci := func(v uint8) int {
		best, bd := 0, 1<<30
		for i, l := range theme.CubeLevels {
			d := int(v) - int(l)
			if d < 0 {
				d = -d
			}
			if d < bd {
				best, bd = i, d
			}
		}
		return best
	}
	ri, gi, bi := ci(r), ci(g), ci(b)
	cube := uint8(16 + 36*ri + 6*gi + bi)
	cr, cg, cb := theme.CubeLevels[ri], theme.CubeLevels[gi], theme.CubeLevels[bi]
	best, bestD := cube, colorDist(r, g, b, cr, cg, cb)
	// Grey ramp candidates around the mean.
	avg := (int(r) + int(g) + int(b)) / 3
	gi0 := (avg - 8) / 10
	for k := gi0 - 1; k <= gi0+1; k++ {
		if k < 0 || k > 23 {
			continue
		}
		v := uint8(8 + 10*k)
		if d := colorDist(r, g, b, v, v, v); d < bestD {
			best, bestD = uint8(232+k), d
		}
	}
	// Neighbouring cube cells can beat the per-channel nearest one under a
	// weighted metric; check the 3×3×3 neighbourhood.
	for dr := -1; dr <= 1; dr++ {
		for dg := -1; dg <= 1; dg++ {
			for db := -1; db <= 1; db++ {
				a, bb, c := ri+dr, gi+dg, bi+db
				if a < 0 || a > 5 || bb < 0 || bb > 5 || c < 0 || c > 5 {
					continue
				}
				d := colorDist(r, g, b, theme.CubeLevels[a], theme.CubeLevels[bb], theme.CubeLevels[c])
				if d < bestD {
					best, bestD = uint8(16+36*a+6*bb+c), d
				}
			}
		}
	}
	return best
}

// colorDist is the "redmean" weighted Euclidean distance, a cheap
// approximation of perceived colour difference.
func colorDist(r1, g1, b1, r2, g2, b2 uint8) float64 {
	rm := (float64(r1) + float64(r2)) / 2
	dr := float64(r1) - float64(r2)
	dg := float64(g1) - float64(g2)
	db := float64(b1) - float64(b2)
	return (2+rm/256)*dr*dr + 4*dg*dg + (2+(255-rm)/256)*db*db
}

// Nearest16 maps an RGB colour to a basic ANSI hue for the 16-colour
// profile. Low-chroma colours (greys, ivory, sepia) return Default so
// body text stays in the terminal's own ink on dark and light backgrounds;
// dark low-saturation colours return BrightBlack. Saturated colours return
// the closest of red, yellow, green, cyan, blue, magenta by hue.
func Nearest16(r, g, b uint8) theme.Color {
	h, _, l := hsl(r, g, b)
	// Chroma (max−min) rather than HSL saturation, which overstates the
	// colourfulness of very light tints such as ivory.
	chroma := float64(max(r, g, b)-min(r, g, b)) / 255
	if chroma < 0.15 || l < 0.08 {
		if l < 0.45 && l >= 0.15 {
			return theme.BrightBlack
		}
		return theme.Default
	}
	switch {
	case h < 20 || h >= 330:
		return theme.Red
	case h < 70:
		return theme.Yellow
	case h < 165:
		return theme.Green
	case h < 200:
		return theme.Cyan
	case h < 260:
		return theme.Blue
	default:
		return theme.Magenta
	}
}

func hsl(r, g, b uint8) (h, s, l float64) {
	rf, gf, bf := float64(r)/255, float64(g)/255, float64(b)/255
	mx := math.Max(rf, math.Max(gf, bf))
	mn := math.Min(rf, math.Min(gf, bf))
	l = (mx + mn) / 2
	if mx == mn {
		return 0, 0, l
	}
	d := mx - mn
	if l > 0.5 {
		s = d / (2 - mx - mn)
	} else {
		s = d / (mx + mn)
	}
	switch mx {
	case rf:
		h = (gf - bf) / d
		if gf < bf {
			h += 6
		}
	case gf:
		h = (bf-rf)/d + 2
	default:
		h = (rf-gf)/d + 4
	}
	return h * 60, s, l
}
