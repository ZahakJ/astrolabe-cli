package render

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ZahakJ/folio/internal/md"
	"github.com/ZahakJ/folio/internal/theme"
)

// taskBox returns the box glyph for a task, its style, and the attribute
// for the task's text (done and cancelled tasks are faint and struck).
func (r *renderer) taskBox(t *md.Task, a attr) (string, theme.Style, attr) {
	switch t.State {
	case ' ':
		return r.g.TaskOpen, r.accent(), a
	case 'x', 'X':
		a.st = r.faint().Over(theme.Style{BG: a.st.BG}).With(theme.Strike)
		return r.g.TaskDone, r.faint(), a
	case '-':
		a.st = r.faint().Over(theme.Style{BG: a.st.BG}).With(theme.Strike)
		return r.g.TaskCancelled, r.faint(), a
	case '/':
		return r.g.TaskDoing, r.accent(), a
	}
	// A custom state: show it in the box's place, bracketed.
	return "[" + cleanText(string(t.State)) + "]", r.accent(), a
}

// Due date and priority markers (DESIGN.md §3).
var (
	dueRe  = regexp.MustCompile(`(?:📅\x{FE0F}?\s*|\bdue:\s*|@due\(\s*)(\d{4}-\d{2}-\d{2})\)?`)
	prioRe = regexp.MustCompile(`\s*(🔺|⏫|🔼|🔽|⏬)\x{FE0F}?`)
)

// taskBody renders a task item's children; the first paragraph has its due
// date and priority markers lifted into chips at its end.
func (r *renderer) taskBody(it *md.ListItem, w int, c ctx, tight bool) []row {
	if len(it.Children) == 0 {
		return nil
	}
	p, ok := it.Children[0].(*md.Paragraph)
	if !ok {
		return r.blocks(it.Children, w, c, tight)
	}
	a := c.attr(r)
	spans := r.inlines(p.Inlines, a, nil)
	var due string
	prio := 0
	for i := range spans {
		s := &spans[i]
		if s.Style.hit != 0 {
			continue
		}
		if m := dueRe.FindStringSubmatchIndex(s.Text); m != nil {
			due = s.Text[m[2]:m[3]]
			s.Text = s.Text[:m[0]] + s.Text[m[1]:]
		}
		if m := prioRe.FindStringSubmatchIndex(s.Text); m != nil {
			switch s.Text[m[2]:m[3]] {
			case "🔺":
				prio = 3
			case "⏫":
				prio = 2
			case "🔼":
				prio = 1
			case "🔽":
				prio = -1
			case "⏬":
				prio = -2
			}
			s.Text = s.Text[:m[0]] + s.Text[m[1]:]
		}
	}
	spans = trimSpaces(spans)
	if prio != 0 {
		marks := map[int]string{3: "!!!", 2: "!!", 1: "!", -1: "↓", -2: "↓↓"}[prio]
		if r.g.Name == "ascii" && prio < 0 {
			marks = strings.Repeat("v", -prio)
		}
		st := r.accent()
		if prio >= 2 && !r.t.Danger.IsDefault() {
			st = theme.Style{FG: r.t.Danger}
		}
		st.Attrs |= theme.Bold
		spans = append(spans, mk(nbsp+nbsp, theme.Style{}), mk(marks, st))
	}
	if due != "" {
		spans = append(spans, mk(nbsp+nbsp, theme.Style{}))
		spans = append(spans, r.dueChip(due, it.Task)...)
	}
	rows := r.wrapRows(spans, w, w, p.StartLine, p.EndLine, KindTask, p.ID)
	if len(it.Children) > 1 {
		rest := r.blocks(it.Children[1:], w, c, tight)
		if len(rest) > 0 && !tight {
			rows = append(rows, blankRow())
		}
		rows = append(rows, rest...)
	}
	return rows
}

// dueChip renders "due 5 Oct" (muted; danger and bold when an open task is
// overdue; "today"/"tomorrow" when close).
func (r *renderer) dueChip(raw string, t *md.Task) []sp {
	d, err := time.Parse("2006-01-02", raw)
	label := raw
	overdue := false
	if err == nil {
		today := time.Date(r.today.Year(), r.today.Month(), r.today.Day(), 0, 0, 0, 0, time.UTC)
		days := int(d.Sub(today).Hours() / 24)
		switch {
		case days == 0:
			label = "today"
		case days == 1:
			label = "tomorrow"
		case days == -1:
			label = "yesterday"
		default:
			label = strconv.Itoa(d.Day()) + " " + d.Month().String()[:3]
			if d.Year() != today.Year() {
				label += " " + strconv.Itoa(d.Year())
			}
		}
		open := t != nil && t.State != 'x' && t.State != 'X' && t.State != '-'
		overdue = open && days < 0
		if days == 0 && open {
			label = "today"
		}
	}
	key := r.faint()
	val := r.muted()
	word := "due"
	if overdue {
		word = "overdue"
		val = theme.Style{FG: r.t.Danger, Attrs: theme.Bold}
		key = theme.Style{FG: r.t.Danger}
		if r.t.Danger.IsDefault() {
			key.Attrs |= theme.Bold
		}
	}
	return []sp{mk(word+nbsp, key), mk(strings.ReplaceAll(label, " ", nbsp), val)}
}
