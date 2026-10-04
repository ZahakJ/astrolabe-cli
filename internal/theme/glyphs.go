package theme

import "strings"

// Glyphs is a set of symbols used by the renderer and the TUI (DESIGN.md §6).
// In the Unicode set every glyph is a single cell wide and present in DejaVu
// Sans Mono, Menlo and Consolas-class fonts; no Nerd Font or emoji code
// points are used. In the ASCII set some glyphs are wider than one cell
// ("[ ]", "..."), so callers must measure them with text.Width rather than
// assume a width.
type Glyphs struct {
	Name string

	Brand         string    // ✦ status bar mark and home screen
	Bullets       [3]string // list bullets by depth: • ◦ ▪
	TaskOpen      string    // ☐
	TaskDone      string    // ☑
	TaskDoing     string    // ◐ in progress
	TaskCancelled string    // ☒

	Bar         string // ▎ quote/callout bar and the reader's cursor
	Rule        string // ─ hairline
	VLine       string // │ table column separator / code frame
	Cross       string // ┼ table rule crossing
	TeeDown     string // ┬
	TeeUp       string // ┴
	TeeRight    string // ├
	TeeLeft     string // ┤
	CornerTL    string // ╭ overlay panel corners
	CornerTR    string // ╮
	CornerBL    string // ╰
	CornerBR    string // ╯
	FoldOpen    string // ▾
	FoldClosed  string // ▸
	External    string // ↗ after external link labels
	Ellipsis    string // …
	Dot         string // · separator in chips and the status bar
	Dirty       string // ● unsaved marker
	Crumb       string // › breadcrumb separator
	Image       string // ▣ image / embed placeholder
	Ornament    string // ·  ✦  · thematic break
	ArrowUp     string // ↑ scroll hints
	ArrowDown   string // ↓
	Return      string // ⏎ "Enter" in key legends
	Selected    string // ▎ left bar marking the selected overlay row
	ScrollBar   string // │ scroll track
	ScrollThumb string // ┃ scroll thumb

	callouts map[string]string
}

// Callout returns the glyph for a callout type (aliases resolved; unknown
// types get the note glyph).
func (g Glyphs) Callout(kind string) string {
	if s, ok := g.callouts[CalloutKind(kind)]; ok {
		return s
	}
	return g.callouts["note"]
}

// CalloutKinds lists the canonical callout kinds in a stable order.
func CalloutKinds() []string {
	return []string{"note", "abstract", "info", "todo", "tip", "success", "question",
		"warning", "failure", "danger", "bug", "example", "quote"}
}

// UnicodeGlyphs is the default glyph set.
//
// Deviation from DESIGN.md §5: the design names ℹ (note) and ※ (bug) as
// callout glyphs, but neither is present in DejaVu Sans Mono, which §6 sets
// as the floor. The note/info callouts therefore use ¶ (a pilcrow, at home on
// a manuscript page) and bug uses ✱.
var UnicodeGlyphs = Glyphs{
	Name:          "unicode",
	Brand:         "✦",
	Bullets:       [3]string{"•", "◦", "▪"},
	TaskOpen:      "☐",
	TaskDone:      "☑",
	TaskDoing:     "◐",
	TaskCancelled: "☒",
	Bar:           "▎",
	Rule:          "─",
	VLine:         "│",
	Cross:         "┼",
	TeeDown:       "┬",
	TeeUp:         "┴",
	TeeRight:      "├",
	TeeLeft:       "┤",
	CornerTL:      "╭",
	CornerTR:      "╮",
	CornerBL:      "╰",
	CornerBR:      "╯",
	FoldOpen:      "▾",
	FoldClosed:    "▸",
	External:      "↗",
	Ellipsis:      "…",
	Dot:           "·",
	Dirty:         "●",
	Crumb:         "›",
	Image:         "▣",
	Ornament:      "·  ✦  ·",
	ArrowUp:       "↑",
	ArrowDown:     "↓",
	Return:        "⏎",
	Selected:      "▎",
	ScrollBar:     "│",
	ScrollThumb:   "┃",
	callouts: map[string]string{
		"note": "¶", "abstract": "✎", "info": "¶", "todo": "☐", "tip": "☞",
		"success": "✓", "question": "?", "warning": "!", "failure": "✗",
		"danger": "✗", "bug": "✱", "example": "≡", "quote": "❝",
	},
}

// ASCIIGlyphs is the 7-bit fallback set (--ascii, or a non-UTF-8 locale).
var ASCIIGlyphs = Glyphs{
	Name:          "ascii",
	Brand:         "*",
	Bullets:       [3]string{"*", "-", "+"},
	TaskOpen:      "[ ]",
	TaskDone:      "[x]",
	TaskDoing:     "[/]",
	TaskCancelled: "[-]",
	Bar:           "|",
	Rule:          "-",
	VLine:         "|",
	Cross:         "+",
	TeeDown:       "+",
	TeeUp:         "+",
	TeeRight:      "+",
	TeeLeft:       "+",
	CornerTL:      "+",
	CornerTR:      "+",
	CornerBL:      "+",
	CornerBR:      "+",
	FoldOpen:      "v",
	FoldClosed:    ">",
	External:      "^",
	Ellipsis:      "...",
	Dot:           "-",
	Dirty:         "*",
	Crumb:         ">",
	Image:         "#",
	Ornament:      "-  *  -",
	ArrowUp:       "^",
	ArrowDown:     "v",
	Return:        "Enter",
	Selected:      "|",
	ScrollBar:     "|",
	ScrollThumb:   "#",
	callouts: map[string]string{
		"note": "i", "abstract": "~", "info": "i", "todo": "[ ]", "tip": "->",
		"success": "+", "question": "?", "warning": "!", "failure": "x",
		"danger": "x", "bug": "#", "example": "=", "quote": "\"",
	},
}

// GlyphSet returns the glyph set called name ("unicode" or "ascii",
// case-insensitive).
func GlyphSet(name string) (Glyphs, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "unicode", "utf-8", "utf8":
		return UnicodeGlyphs, true
	case "ascii":
		return ASCIIGlyphs, true
	}
	return Glyphs{}, false
}

// GlyphsFor returns ASCIIGlyphs when ascii is true, else UnicodeGlyphs.
func GlyphsFor(ascii bool) Glyphs {
	if ascii {
		return ASCIIGlyphs
	}
	return UnicodeGlyphs
}
