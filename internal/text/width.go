package text

import (
	"strings"

	"github.com/rivo/uniseg"
)

// Grapheme is one user-perceived character (an extended grapheme cluster)
// with its terminal cell width and byte offset in the source string.
type Grapheme struct {
	Text   string
	Width  int
	Offset int
}

// Graphemes splits s into grapheme clusters.
func Graphemes(s string) []Grapheme {
	out := make([]Grapheme, 0, len(s))
	state := -1
	off := 0
	for len(s) > 0 {
		var g string
		var w int
		g, s, w, state = uniseg.FirstGraphemeClusterInString(s, state)
		out = append(out, Grapheme{Text: g, Width: clusterWidth(g, w), Offset: off})
		off += len(g)
	}
	return out
}

// EachGrapheme calls fn for every grapheme cluster of s in order with its
// cell width; iteration stops when fn returns false. It does not allocate.
func EachGrapheme(s string, fn func(g string, width int) bool) {
	state := -1
	for len(s) > 0 {
		var g string
		var w int
		g, s, w, state = uniseg.FirstGraphemeClusterInString(s, state)
		if !fn(g, clusterWidth(g, w)) {
			return
		}
	}
}

// clusterWidth adjusts uniseg's width: control characters occupy no cells
// (callers expand tabs before measuring).
func clusterWidth(g string, w int) int {
	if len(g) == 1 && (g[0] < 0x20 || g[0] == 0x7f) {
		return 0
	}
	return w
}

// GraphemeWidth returns the cell width of a single grapheme cluster.
func GraphemeWidth(g string) int {
	if g == "" {
		return 0
	}
	if len(g) == 1 {
		if g[0] < 0x20 || g[0] == 0x7f {
			return 0
		}
		return 1
	}
	return uniseg.StringWidth(g)
}

// Width returns the number of terminal cells s occupies, measured per
// grapheme cluster (wide East Asian characters and emoji count 2, combining
// marks 0). Control characters count 0.
func Width(s string) int {
	ascii := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x80 || c < 0x20 || c == 0x7f {
			ascii = false
			break
		}
	}
	if ascii {
		return len(s)
	}
	w := 0
	EachGrapheme(s, func(_ string, gw int) bool { w += gw; return true })
	return w
}

// ExpandTabs replaces each tab with spaces up to the next multiple of
// tabWidth cells (measured from the start of s).
func ExpandTabs(s string, tabWidth int) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	if tabWidth <= 0 {
		tabWidth = 4
	}
	var b strings.Builder
	col := 0
	EachGrapheme(s, func(g string, w int) bool {
		switch g {
		case "\t":
			n := tabWidth - col%tabWidth
			b.WriteString(strings.Repeat(" ", n))
			col += n
		case "\n":
			b.WriteString(g)
			col = 0
		default:
			b.WriteString(g)
			col += w
		}
		return true
	})
	return b.String()
}

// Truncate shortens s to at most width cells, ending it with ellipsis when
// anything was cut. If even the ellipsis does not fit, s is cut hard. A wide
// grapheme that would straddle the limit is dropped, never split.
func Truncate(s string, width int, ellipsis string) string {
	if width <= 0 {
		return ""
	}
	if Width(s) <= width {
		return s
	}
	ew := Width(ellipsis)
	if ew > width {
		ellipsis, ew = "", 0
	}
	limit := width - ew
	var b strings.Builder
	used := 0
	EachGrapheme(s, func(g string, w int) bool {
		if used+w > limit {
			return false
		}
		b.WriteString(g)
		used += w
		return true
	})
	b.WriteString(ellipsis)
	return b.String()
}

// TruncateLeft shortens s to at most width cells by cutting from the start,
// prefixing ellipsis when anything was cut (useful for paths and
// breadcrumbs, whose end matters most).
func TruncateLeft(s string, width int, ellipsis string) string {
	if width <= 0 {
		return ""
	}
	if Width(s) <= width {
		return s
	}
	ew := Width(ellipsis)
	if ew > width {
		ellipsis, ew = "", 0
	}
	limit := width - ew
	gs := Graphemes(s)
	used := 0
	i := len(gs)
	for i > 0 && used+gs[i-1].Width <= limit {
		i--
		used += gs[i].Width
	}
	if i == len(gs) {
		return ellipsis
	}
	return ellipsis + s[gs[i].Offset:]
}

// Alignment is a horizontal alignment.
type Alignment int

// Alignments.
const (
	AlignLeft Alignment = iota
	AlignRight
	AlignCenter
)

// PadRight pads s with spaces on the right to width cells. Strings already
// at least width cells wide are returned unchanged.
func PadRight(s string, width int) string {
	if n := width - Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// PadLeft pads s with spaces on the left to width cells.
func PadLeft(s string, width int) string {
	if n := width - Width(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

// Center pads s on both sides to width cells; an odd remainder goes right.
func Center(s string, width int) string {
	n := width - Width(s)
	if n <= 0 {
		return s
	}
	l := n / 2
	return strings.Repeat(" ", l) + s + strings.Repeat(" ", n-l)
}

// Align pads s to width cells with the given alignment.
func Align(s string, width int, a Alignment) string {
	switch a {
	case AlignRight:
		return PadLeft(s, width)
	case AlignCenter:
		return Center(s, width)
	}
	return PadRight(s, width)
}

// Fit truncates s (with ellipsis) and pads it so it is exactly width cells
// wide. When a wide grapheme cannot fit the result is padded with a space.
func Fit(s string, width int, a Alignment, ellipsis string) string {
	return Align(Truncate(s, width, ellipsis), width, a)
}

// Slice returns the part of s covering cells [from, to). A wide grapheme cut
// by either edge is replaced by spaces for the cells that remain, so the
// result is always exactly the requested width when s is long enough.
func Slice(s string, from, to int) string {
	if to <= from {
		return ""
	}
	var b strings.Builder
	col := 0
	EachGrapheme(s, func(g string, w int) bool {
		end := col + w
		switch {
		case end <= from:
		case col >= to:
			return false
		case col >= from && end <= to:
			b.WriteString(g)
		default:
			lo, hi := max(col, from), min(end, to)
			b.WriteString(strings.Repeat(" ", hi-lo))
		}
		col = end
		return true
	})
	return b.String()
}
