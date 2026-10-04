package theme

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Color is a terminal colour: the terminal's default, a 24-bit RGB value, or
// an ANSI palette index (0–255). The zero value is Default.
//
// Encoding: bit 24 marks RGB (low 24 bits = 0xRRGGBB), bit 25 marks an ANSI
// index (low 8 bits). Colours are comparable with ==.
type Color uint32

const (
	// Default is the terminal's own foreground or background colour.
	Default Color = 0

	rgbFlag   Color = 1 << 24
	indexFlag Color = 1 << 25
)

// The eight basic ANSI colours and their bright variants, as palette indexes.
// Their actual appearance is chosen by the user's terminal palette.
const (
	Black Color = indexFlag | iota
	Red
	Green
	Yellow
	Blue
	Magenta
	Cyan
	White
	BrightBlack
	BrightRed
	BrightGreen
	BrightYellow
	BrightBlue
	BrightMagenta
	BrightCyan
	BrightWhite
)

// RGB returns the 24-bit colour (r, g, b).
func RGB(r, g, b uint8) Color {
	return rgbFlag | Color(r)<<16 | Color(g)<<8 | Color(b)
}

// ANSI returns the palette colour with index i (0–15 basic, 16–255 extended).
func ANSI(i uint8) Color { return indexFlag | Color(i) }

// ParseHex parses "#rrggbb", "rrggbb" or "#rgb".
func ParseHex(s string) (Color, error) {
	h := strings.TrimPrefix(s, "#")
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	if len(h) != 6 {
		return Default, fmt.Errorf("theme: bad colour %q", s)
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return Default, fmt.Errorf("theme: bad colour %q", s)
	}
	return rgbFlag | Color(v), nil
}

// Hex is ParseHex for literals known to be valid; it panics on a bad value.
func Hex(s string) Color {
	c, err := ParseHex(s)
	if err != nil {
		panic(err)
	}
	return c
}

// IsDefault reports whether c is the terminal default colour.
func (c Color) IsDefault() bool { return c&(rgbFlag|indexFlag) == 0 }

// IsRGB reports whether c is a 24-bit colour.
func (c Color) IsRGB() bool { return c&rgbFlag != 0 }

// IsIndexed reports whether c is an ANSI palette index.
func (c Color) IsIndexed() bool { return c&indexFlag != 0 && c&rgbFlag == 0 }

// Index returns the palette index of an indexed colour (0 otherwise).
func (c Color) Index() uint8 {
	if !c.IsIndexed() {
		return 0
	}
	return uint8(c)
}

// Components returns the red, green and blue components of an RGB colour.
// Indexed colours return the xterm default palette value; Default returns 0,0,0.
func (c Color) Components() (r, g, b uint8) {
	switch {
	case c.IsRGB():
		return uint8(c >> 16), uint8(c >> 8), uint8(c)
	case c.IsIndexed():
		return XtermPalette(c.Index())
	}
	return 0, 0, 0
}

// String returns "#rrggbb", "ansi:N" or "default".
func (c Color) String() string {
	switch {
	case c.IsRGB():
		r, g, b := c.Components()
		return fmt.Sprintf("#%02x%02x%02x", r, g, b)
	case c.IsIndexed():
		return fmt.Sprintf("ansi:%d", c.Index())
	}
	return "default"
}

var basic16 = [16][3]uint8{
	{0, 0, 0}, {205, 0, 0}, {0, 205, 0}, {205, 205, 0},
	{0, 0, 238}, {205, 0, 205}, {0, 205, 205}, {229, 229, 229},
	{127, 127, 127}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
	{92, 92, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
}

// CubeLevels are the six channel intensities of the xterm 6×6×6 colour cube
// (palette indexes 16–231).
var CubeLevels = [6]uint8{0, 95, 135, 175, 215, 255}

// XtermPalette returns the RGB value of palette index i in xterm's default
// palette: 0–15 basic colours, 16–231 the colour cube, 232–255 the grey ramp.
func XtermPalette(i uint8) (r, g, b uint8) {
	switch {
	case i < 16:
		p := basic16[i]
		return p[0], p[1], p[2]
	case i < 232:
		n := i - 16
		return CubeLevels[n/36], CubeLevels[(n/6)%6], CubeLevels[n%6]
	default:
		v := 8 + 10*(i-232)
		return v, v, v
	}
}

// Blend mixes c towards d by t (0 = c, 1 = d). Both must be RGB; otherwise c
// is returned unchanged.
func Blend(c, d Color, t float64) Color {
	if !c.IsRGB() || !d.IsRGB() {
		return c
	}
	r1, g1, b1 := c.Components()
	r2, g2, b2 := d.Components()
	mix := func(a, b uint8) uint8 {
		return uint8(math.Round(float64(a) + (float64(b)-float64(a))*t))
	}
	return RGB(mix(r1, r2), mix(g1, g2), mix(b1, b2))
}

// Luminance is the WCAG relative luminance of c (0 black … 1 white).
func Luminance(c Color) float64 {
	r, g, b := c.Components()
	lin := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

// Contrast is the WCAG contrast ratio between two colours (1 … 21).
func Contrast(a, b Color) float64 {
	la, lb := Luminance(a), Luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}
