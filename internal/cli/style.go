package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/ZahakJ/astrolabe-cli/internal/md"
	"github.com/ZahakJ/astrolabe-cli/internal/term"
	"github.com/ZahakJ/astrolabe-cli/internal/text"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
)

// painter styles human output. When off (stdout is not a terminal) every
// method returns its input unchanged, so the same code paths produce the
// plain form.
//
// CLI output is printed into the user's own terminal, so the painter only
// sets foreground colours and attributes and never paints a ground: the
// themes' grounds belong to the TUI and to `astrolabe render`.
type painter struct {
	on    bool
	enc   term.Encoder
	th    theme.Theme
	g     theme.Glyphs
	width int
	bidi  bool
	mono  bool
	// human: stdout is a terminal (even an unstyled TERM=dumb one), so
	// lines may be cleaned of Markdown syntax and put in visual order.
	// Piped output stays raw and in logical order.
	human bool
}

func newPainter(on bool, d *Display) *painter {
	p := &painter{on: on, g: d.Glyphs, width: d.Caps.Width, bidi: d.Caps.Bidi}
	if p.width <= 0 {
		p.width = 80
	}
	p.enc = d.Caps.Encoder()
	p.enc.Hyperlinks = false // CLI output may be copied; keep it simple
	p.th = term.ThemeFor(d.Theme, d.Caps.Profile)
	p.mono = d.Caps.Profile == term.ProfileNone
	return p
}

// style applies st to s.
func (p *painter) style(s string, st theme.Style) string {
	if !p.on || s == "" || st == (theme.Style{}) {
		return s
	}
	st.BG = theme.Default
	return p.enc.Styled(s, st)
}

func (p *painter) fg(c theme.Color, a theme.Attr) theme.Style { return theme.Style{FG: c, Attrs: a} }

// The semantic inks of DESIGN.md §7, foreground only. Body text and
// headings keep the terminal's own foreground (headings in bold): the
// themes' ivory or sepia inks assume the theme's ground, which the CLI does
// not paint, whereas the mid-tone accent, muted, faint and danger inks read
// on dark and light terminals alike.
func (p *painter) accent(s string) string  { return p.style(s, p.fg(p.th.Accent, 0)) }
func (p *painter) link(s string) string    { return p.style(s, p.th.LinkStyle()) }
func (p *painter) heading(s string) string { return p.style(s, theme.Style{Attrs: theme.Bold}) }
func (p *painter) text(s string) string    { return s }
func (p *painter) danger(s string) string  { return p.style(s, p.fg(p.th.Danger, 0)) }
func (p *painter) ok(s string) string      { return p.style(s, p.fg(p.th.Ok, 0)) }
func (p *painter) muted(s string) string {
	if p.mono {
		return s
	}
	return p.style(s, p.fg(p.th.Muted, 0))
}
func (p *painter) faint(s string) string {
	if p.mono {
		return p.style(s, theme.Style{Attrs: theme.Faint})
	}
	return p.style(s, p.fg(p.th.Faint, 0))
}

// match is the style of a search match inside a result line.
func (p *painter) match(s string) string {
	st := theme.Style{FG: p.th.Accent, Attrs: theme.Bold}
	if p.mono {
		st = theme.Style{Attrs: theme.Bold | theme.Underline}
	}
	return p.style(s, st)
}

// visual prepares a line for display in a terminal that does no bidi
// (DESIGN.md §6); piped output is never reordered or shaped.
func (p *painter) visual(s string) string {
	if !p.human || !p.bidi || !text.HasRTL(s) {
		return s
	}
	return text.Visual(s, text.BaseDirection(s))
}

// visualPath is visual for a path: Arabic names are shaped and reordered,
// but the path itself reads left to right (folders first, ".md" last).
func (p *painter) visualPath(s string) string {
	if !p.human || !p.bidi || !text.HasRTL(s) {
		return s
	}
	return text.VisualPath(s)
}

// clean shows one raw source line (a task's text) as the reader would:
// emphasis, link and list syntax removed. Human output only; a pipe gets the
// raw line, which is stable and greppable.
func (p *painter) clean(s string) string {
	if !p.human {
		return s
	}
	c, _ := md.CleanLine(s, nil)
	return c
}

// rule returns a hairline n cells wide.
func (p *painter) rule(n int) string {
	if n <= 0 {
		return ""
	}
	return p.style(strings.Repeat(p.g.Rule, n/max(1, text.Width(p.g.Rule))), theme.Style{FG: p.th.Border})
}

// cell is one column value of a table row with its own styling applied
// after padding.
type cell struct {
	s     string
	paint func(string) string
	right bool // right-align
}

// table prints aligned rows: column widths are measured on the unstyled
// text, then each cell is padded and styled. The last column is not padded.
// flex names the column that is truncated when the row exceeds width
// (-1 = none).
func (p *painter) table(rows [][]cell, indent, gap, width, flex int) []string {
	if len(rows) == 0 {
		return nil
	}
	n := 0
	for _, r := range rows {
		n = max(n, len(r))
	}
	ws := make([]int, n)
	for _, r := range rows {
		for i, c := range r {
			ws[i] = max(ws[i], text.Width(c.s))
		}
	}
	if flex >= 0 && flex < n && width > 0 {
		total := indent + gap*(n-1)
		for _, w := range ws {
			total += w
		}
		if over := total - width; over > 0 {
			ws[flex] = max(8, ws[flex]-over)
		}
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		var b strings.Builder
		b.WriteString(strings.Repeat(" ", indent))
		for i, c := range r {
			s := text.Truncate(c.s, ws[i], p.g.Ellipsis)
			pad := ws[i] - text.Width(s)
			last := i == len(r)-1
			if c.right {
				b.WriteString(strings.Repeat(" ", pad))
			}
			if c.paint != nil {
				s = c.paint(s)
			}
			b.WriteString(s)
			if !c.right && !last {
				b.WriteString(strings.Repeat(" ", pad))
			}
			if !last {
				b.WriteString(strings.Repeat(" ", gap))
			}
		}
		out = append(out, strings.TrimRight(b.String(), " "))
	}
	return out
}

// splitPath separates "a/b/c.md" into the folder part "a/b/" and the
// name "c.md".
func splitPath(rel string) (dir, name string) {
	if i := strings.LastIndexByte(rel, '/'); i >= 0 {
		return rel[:i+1], rel[i+1:]
	}
	return "", rel
}

// pathInk shows a vault-relative path with its folders muted and the name
// in accent.
func (p *painter) pathInk(rel string) string {
	dir, name := splitPath(rel)
	return p.muted(p.visualPath(dir)) + p.accent(p.visualPath(name))
}

// ago formats t relative to now for human output: "just now", "5 min ago",
// "3 h ago", "yesterday", "Mon", "3 Oct", "3 Oct 2025".
func ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case t.IsZero():
		return ""
	case d < 0:
		return t.Format("2 Jan 15:04")
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d/time.Minute))
	}
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	days := int(day(y2, m2, d2).Sub(day(y1, m1, d1)).Hours() / 24)
	switch {
	case days == 0:
		return fmt.Sprintf("%d h ago", int(d/time.Hour))
	case days == 1:
		return "yesterday"
	case days < 7:
		return t.Format("Mon")
	case y1 == y2:
		return t.Format("2 Jan")
	}
	return t.Format("2 Jan 2006")
}

// plural returns "1 note" / "3 notes".
func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%s %s", commas(n), many)
}

// commas formats n with thousands separators.
func commas(n int) string {
	s := fmt.Sprint(n)
	if n < 0 {
		return "-" + commas(-n)
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
