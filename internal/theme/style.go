package theme

import "strings"

// Attr is a set of text attributes.
type Attr uint16

// Text attributes. CurlyUnderline and DottedUnderline imply an underline; an
// encoder that cannot express them falls back to a plain underline.
const (
	Bold Attr = 1 << iota
	Faint
	Italic
	Underline
	CurlyUnderline
	DottedUnderline
	Strike
	Reverse

	// AttrNone is the empty attribute set.
	AttrNone Attr = 0
)

var attrNames = []struct {
	a    Attr
	name string
}{
	{Bold, "bold"}, {Faint, "faint"}, {Italic, "italic"}, {Underline, "underline"},
	{CurlyUnderline, "curly"}, {DottedUnderline, "dotted"}, {Strike, "strike"},
	{Reverse, "reverse"},
}

// Has reports whether all attributes in b are set in a.
func (a Attr) Has(b Attr) bool { return a&b == b }

// AnyUnderline reports whether any underline variant is set.
func (a Attr) AnyUnderline() bool { return a&(Underline|CurlyUnderline|DottedUnderline) != 0 }

// String lists the attributes joined by "+", or "none".
func (a Attr) String() string {
	if a == 0 {
		return "none"
	}
	var parts []string
	for _, n := range attrNames {
		if a&n.a != 0 {
			parts = append(parts, n.name)
		}
	}
	return strings.Join(parts, "+")
}

// Style is how a cell is drawn: foreground, background, attributes and an
// optional hyperlink target. The zero Style is the terminal default.
// Styles are comparable with ==.
type Style struct {
	FG    Color
	BG    Color
	Attrs Attr
	// Link, when non-empty, is a URL emitted as an OSC 8 hyperlink on
	// terminals that support it. It never changes how the cell looks.
	Link string
}

// Fg returns s with foreground c.
func (s Style) Fg(c Color) Style { s.FG = c; return s }

// Bg returns s with background c.
func (s Style) Bg(c Color) Style { s.BG = c; return s }

// With returns s with attributes a added.
func (s Style) With(a Attr) Style { s.Attrs |= a; return s }

// Without returns s with attributes a removed.
func (s Style) Without(a Attr) Style { s.Attrs &^= a; return s }

// WithLink returns s carrying hyperlink url.
func (s Style) WithLink(url string) Style { s.Link = url; return s }

// Over layers s on top of base: s's non-default colours win, attributes are
// combined, and s's link wins when set.
func (s Style) Over(base Style) Style {
	out := base
	if !s.FG.IsDefault() {
		out.FG = s.FG
	}
	if !s.BG.IsDefault() {
		out.BG = s.BG
	}
	out.Attrs |= s.Attrs
	if s.Link != "" {
		out.Link = s.Link
	}
	return out
}
