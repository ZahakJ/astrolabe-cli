package theme

import (
	"sort"
	"strings"
)

// Callouts holds one hue per canonical callout kind (see CalloutKind).
type Callouts struct {
	Note, Abstract, Info, Todo, Tip, Success, Question, Warning,
	Failure, Danger, Bug, Example, Quote Color
}

// Theme is the semantic token set of DESIGN.md §7. Colour tokens are used
// directly by the renderer; the composite Style tokens exist where a token
// needs attributes too (so that 16-colour and monochrome variants can swap a
// painted ground for reverse video).
//
// A token equal to Default means "paint nothing": in particular Ground and
// Raised are Default in the 16-colour and monochrome variants, and renderers
// must then fall back to unpainted layouts (e.g. a faint │ bar instead of a
// raised code block).
type Theme struct {
	Name string
	// Dark reports whether the ground is dark (used for derived choices).
	Dark bool

	Ground     Color // the page / room
	Raised     Color // code blocks, overlays, status bar
	Hover      Color // soft-accent ground: selected row in lists
	Text       Color // body ink
	Muted      Color // secondary ink: quotes, properties, folders
	Faint      Color // tertiary ink: markers, chevrons, rules
	Heading    Color // H1/H2 ink
	Accent     Color // bullets, bars, chevrons, cursor bar (gold in onyx)
	AccentSoft Color // ground for ==highlight== and soft accents
	Border     Color // hairlines around panels and tables
	Danger     Color // broken links, overdue, errors
	Ok         Color // success, diff additions
	Link       Color // link ink (normally the accent)
	Math       Color // verbatim math

	CodeComment Color
	CodeString  Color
	CodeNumber  Color
	CodeKeyword Color

	Callout Callouts

	// LinkAttrs are attributes added to links (used when colour alone
	// cannot distinguish them, as in monochrome).
	LinkAttrs Attr

	Selection     Style // selected text in the editor / visual mode
	CursorLine    Style // the reader/editor cursor line ground
	Highlight     Style // ==highlight==
	LinkFocus     Style // the Tab-highlighted link
	Search        Style // search matches
	SearchCurrent Style // the current search match
	StatusBar     Style // status bar ground and default ink
	// Accent16 and Link16 are the ANSI colours the 16-colour variant uses
	// for the accent and links (Default = yellow, the classic policy).
	Accent16, Link16 Color
	// Title is the ink of a note's title and H1 headings (Default =
	// Heading).
	Title      Color
	PillRead   Style // READ mode pill
	PillNormal Style // NORMAL mode pill
	PillInsert Style // INSERT mode pill
	PillVisual Style // VISUAL mode pill
}

// Base is the page style: body ink on the ground.
func (t Theme) Base() Style { return Style{FG: t.Text, BG: t.Ground} }

// LinkStyle is the style of a link label (foreground only).
func (t Theme) LinkStyle() Style { return Style{FG: t.Link, Attrs: t.LinkAttrs} }

// CalloutColor returns the hue for a callout type such as "warning" or
// "faq"; unknown types use the note hue.
func (t Theme) CalloutColor(kind string) Color {
	c := t.Callout
	switch CalloutKind(kind) {
	case "abstract":
		return c.Abstract
	case "info":
		return c.Info
	case "todo":
		return c.Todo
	case "tip":
		return c.Tip
	case "success":
		return c.Success
	case "question":
		return c.Question
	case "warning":
		return c.Warning
	case "failure":
		return c.Failure
	case "danger":
		return c.Danger
	case "bug":
		return c.Bug
	case "example":
		return c.Example
	case "quote":
		return c.Quote
	}
	return c.Note
}

// calloutAliases maps Obsidian's callout aliases to canonical kinds.
var calloutAliases = map[string]string{
	"note": "note", "abstract": "abstract", "summary": "abstract", "tldr": "abstract",
	"info": "info", "todo": "todo", "tip": "tip", "hint": "tip", "important": "tip",
	"success": "success", "check": "success", "done": "success",
	"question": "question", "help": "question", "faq": "question",
	"warning": "warning", "caution": "warning", "attention": "warning",
	"failure": "failure", "fail": "failure", "missing": "failure",
	"danger": "danger", "error": "danger", "bug": "bug", "example": "example",
	"quote": "quote", "cite": "quote",
}

// CalloutKind canonicalises an Obsidian callout type (case-insensitive,
// aliases resolved). Unknown types return "note".
func CalloutKind(kind string) string {
	if k, ok := calloutAliases[strings.ToLower(strings.TrimSpace(kind))]; ok {
		return k
	}
	return "note"
}

// Basic16 returns the 16-colour variant of t per DESIGN.md §6: default
// foreground and background, an ANSI accent (yellow, or the theme's
// Accent16: magenta for sidereal), bright black as faint, no painted
// grounds, and attributes (bold, reverse) doing the work. The result looks
// right on dark and light terminals alike.
func (t Theme) Basic16() Theme {
	acc, link := Yellow, Yellow
	if !t.Accent16.IsDefault() {
		acc, link = t.Accent16, t.Accent16
	}
	if !t.Link16.IsDefault() {
		link = t.Link16
	}
	return Theme{
		Name:        t.Name,
		Dark:        t.Dark,
		Text:        Default,
		Muted:       Default,
		Faint:       BrightBlack,
		Heading:     Default,
		Accent:      acc,
		Border:      BrightBlack,
		Danger:      Red,
		Ok:          Green,
		Link:        link,
		Math:        Cyan,
		CodeComment: BrightBlack,
		CodeString:  Green,
		CodeNumber:  Yellow,
		CodeKeyword: Magenta,
		Callout: Callouts{
			Note: Cyan, Abstract: Cyan, Info: Cyan, Todo: Cyan, Tip: Green,
			Success: Green, Question: Yellow, Warning: Yellow, Failure: Red,
			Danger: Red, Bug: Red, Example: Magenta, Quote: BrightBlack,
		},
		Selection:     Style{Attrs: Reverse},
		CursorLine:    Style{},
		Highlight:     Style{FG: acc, Attrs: Reverse},
		LinkFocus:     Style{FG: link, Attrs: Reverse},
		Search:        Style{Attrs: Reverse},
		SearchCurrent: Style{FG: acc, Attrs: Reverse | Bold},
		StatusBar:     Style{},
		PillRead:      Style{FG: acc, Attrs: Reverse | Bold},
		PillNormal:    Style{FG: Green, Attrs: Reverse | Bold},
		PillInsert:    Style{FG: Red, Attrs: Reverse | Bold},
		PillVisual:    Style{FG: Magenta, Attrs: Reverse | Bold},
	}
}

// Mono returns the colourless variant of t: every colour Default, with
// attributes alone carrying emphasis (links underlined, selections and pills
// reversed).
func (t Theme) Mono() Theme {
	return Theme{
		Name:          t.Name,
		Dark:          t.Dark,
		LinkAttrs:     Underline,
		Selection:     Style{Attrs: Reverse},
		Highlight:     Style{Attrs: Reverse},
		LinkFocus:     Style{Attrs: Reverse | Underline},
		Search:        Style{Attrs: Reverse},
		SearchCurrent: Style{Attrs: Reverse | Bold},
		PillRead:      Style{Attrs: Reverse | Bold},
		PillNormal:    Style{Attrs: Reverse | Bold},
		PillInsert:    Style{Attrs: Reverse | Bold},
		PillVisual:    Style{Attrs: Reverse | Bold},
	}
}

// TitleInk returns the ink of titles and H1 headings.
func (t Theme) TitleInk() Color {
	if t.Title.IsDefault() {
		return t.Heading
	}
	return t.Title
}

// WithoutGround returns t with the page ground left to the terminal
// (config ground=off). Raised surfaces keep their colour.
func (t Theme) WithoutGround() Theme {
	t.Ground = Default
	t.CursorLine.BG = Default
	return t
}

var registry = map[string]Theme{}

func register(t Theme) Theme {
	registry[t.Name] = t
	return t
}

// DefaultName is the name of the default theme.
const DefaultName = "onyx"

// Lookup returns the theme called name (case-insensitive).
func Lookup(name string) (Theme, bool) {
	t, ok := registry[strings.ToLower(strings.TrimSpace(name))]
	return t, ok
}

// DefaultTheme returns the default theme, onyx.
func DefaultTheme() Theme { return registry[DefaultName] }

// Names lists the available themes: the default first, then the rest in
// cycling order (the order `Space T` steps through).
func Names() []string {
	return append([]string(nil), order...)
}

var order = []string{"onyx", "iron-gall", "parchment", "graphite", "mocha", "sidereal"}

// Next returns the name of the theme after name in cycling order.
func Next(name string) string {
	for i, n := range order {
		if n == name {
			return order[(i+1)%len(order)]
		}
	}
	return order[0]
}

func init() {
	// Keep order and registry in sync: any registered theme not listed is
	// appended alphabetically.
	var extra []string
	for n := range registry {
		found := false
		for _, o := range order {
			if o == n {
				found = true
			}
		}
		if !found {
			extra = append(extra, n)
		}
	}
	sort.Strings(extra)
	order = append(order, extra...)
}
