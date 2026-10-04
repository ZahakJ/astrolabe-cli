package cli

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ZahakJ/folio/internal/theme"
	"github.com/ZahakJ/folio/internal/vault"
)

type taskJSON struct {
	Path     string `json:"path"`
	Abs      string `json:"abs"`
	Title    string `json:"title"`
	Line     int    `json:"line"`
	State    string `json:"state"`
	Status   string `json:"status"`
	Text     string `json:"text"`
	Raw      string `json:"raw"`
	Due      string `json:"due,omitempty"`
	Priority int    `json:"priority"`
	Group    string `json:"group"`
}

// taskStatus names a task state.
func taskStatus(t vault.Task) string {
	switch {
	case t.Done():
		return "done"
	case t.Cancelled():
		return "cancelled"
	case t.State == '/':
		return "in-progress"
	case t.State == ' ':
		return "open"
	}
	return "other"
}

// cmdTasks is `folio tasks`: open tasks grouped Overdue / Today / Upcoming /
// Undated.
func (a *app) cmdTasks(args []string) error {
	fs := newFlagSet("tasks")
	all := fs.Bool("-a", "--all")
	dueOnly := fs.Bool("", "--due")
	asJSON := fs.Bool("", "--json")
	pos, err := fs.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("tasks", "unexpected argument %q", pos[0])
	}
	v, err := a.vault()
	if err != nil {
		return err
	}
	if err := a.requireRoot(v); err != nil {
		return err
	}
	if v, err = a.scannedVault(); err != nil {
		return err
	}
	ts := v.Tasks(*all)
	if *dueOnly {
		kept := ts[:0:0]
		for _, t := range ts {
			if !t.Task.Due.IsZero() {
				kept = append(kept, t)
			}
		}
		ts = kept
	}
	now := a.now()
	today := vault.DateOf(now)
	g := vault.GroupTasks(ts, today)
	groups := []struct {
		name string
		ts   []vault.TaskRef
	}{{"overdue", g.Overdue}, {"today", g.Today}, {"upcoming", g.Upcoming}, {"undated", g.Undated}}
	if len(ts) == 0 {
		if *asJSON {
			_ = a.writeJSON([]taskJSON{})
		}
		what := "open tasks"
		if *all {
			what = "tasks"
		}
		if *dueOnly {
			what += " with a due date"
		}
		return a.nothing("no %s", what)
	}
	switch {
	case *asJSON:
		out := []taskJSON{}
		for _, gr := range groups {
			for _, t := range gr.ts {
				out = append(out, taskJSON{Path: t.Path, Abs: v.Abs(t.Path), Title: t.Title, Line: t.Task.Line,
					State: string(t.Task.State), Status: taskStatus(t.Task), Text: t.Task.Text, Raw: t.Task.Raw,
					Due: t.Task.Due.String(), Priority: t.Task.Priority, Group: gr.name})
			}
		}
		return a.writeJSON(out)
	case !a.human():
		for _, gr := range groups {
			for _, t := range gr.ts {
				s := fmt.Sprintf("%s:%d:[%c] %s", a.openablePath(t.Path), t.Task.Line, t.Task.State, oneLine(t.Task.Text))
				if !t.Task.Due.IsZero() {
					s += " due:" + t.Task.Due.String()
				}
				if t.Task.Priority > 0 {
					s += " " + strings.Repeat("!", t.Task.Priority)
				}
				a.out("%s\n", s)
			}
		}
		return nil
	}

	p := a.painter()
	titles := map[string]string{
		"overdue": "Overdue", "today": "Today", "upcoming": "Upcoming", "undated": "Undated",
	}
	inks := map[string]func(string) string{
		"overdue":  func(s string) string { return p.style(s, theme.Style{FG: p.th.Danger, Attrs: theme.Bold}) },
		"today":    func(s string) string { return p.style(s, theme.Style{FG: p.th.Accent, Attrs: theme.Bold}) },
		"upcoming": p.heading,
		"undated":  func(s string) string { return p.style(s, theme.Style{FG: p.th.Muted, Attrs: theme.Bold}) },
	}
	// One table across all groups so the columns line up between them.
	var rows [][]cell
	var groupOf []string
	for _, gr := range groups {
		for _, t := range gr.ts {
			rows = append(rows, a.taskRow(p, t, gr.name, today, now))
			groupOf = append(groupOf, gr.name)
		}
	}
	lines := p.table(rows, 4, 2, p.width, 1)
	first := true
	open, overdue := 0, len(g.Overdue)
	for i, l := range lines {
		if i == 0 || groupOf[i] != groupOf[i-1] {
			if !first {
				a.out("\n")
			}
			first = false
			n := 0
			for _, gr := range groups {
				if gr.name == groupOf[i] {
					n = len(gr.ts)
				}
			}
			a.out("  %s %s\n", inks[groupOf[i]](titles[groupOf[i]]), p.faint(fmt.Sprintf("%s %d", p.g.Dot, n)))
		}
		a.out("%s\n", l)
	}
	for _, t := range ts {
		if t.Task.Open() {
			open++
		}
	}
	summary := plural(open, "open task", "open tasks")
	if overdue > 0 {
		summary += fmt.Sprintf(" %s %d overdue", p.g.Dot, overdue)
	}
	a.out("\n  %s\n", p.faint(summary))
	return nil
}

// taskRow lays out one task: state box, text (+ priority), due chip, note.
func (a *app) taskRow(p *painter, t vault.TaskRef, group string, today vault.Date, now time.Time) []cell {
	box, boxInk := p.g.TaskOpen, p.accent
	textInk := p.text
	switch {
	case t.Task.Done():
		box, boxInk = p.g.TaskDone, p.faint
		textInk = func(s string) string { return p.style(s, theme.Style{FG: p.th.Faint, Attrs: theme.Strike}) }
	case t.Task.Cancelled():
		box, boxInk = p.g.TaskCancelled, p.faint
		textInk = func(s string) string { return p.style(s, theme.Style{FG: p.th.Faint, Attrs: theme.Strike}) }
	case t.Task.State == '/':
		box = p.g.TaskDoing
	}
	txt := p.visual(p.clean(t.Task.Text))
	if t.Task.Priority > 0 {
		pr := strings.Repeat("!", t.Task.Priority)
		inner := textInk
		textInk = func(s string) string {
			if strings.HasSuffix(s, " "+pr) {
				return inner(strings.TrimSuffix(s, " "+pr)) + " " + p.danger(pr)
			}
			return inner(s)
		}
		txt += " " + pr
	}
	due, dueInk := "", p.muted
	if !t.Task.Due.IsZero() && group != "today" {
		due = dueLabel(t.Task.Due, today)
		if group == "overdue" {
			dueInk = p.danger
		}
	} else if group == "today" {
		due, dueInk = "today", p.accent
	}
	return []cell{
		{s: box, paint: boxInk},
		{s: txt, paint: textInk},
		{s: due, paint: dueInk},
		{s: p.visual(p.clean(t.Title)), paint: p.muted},
	}
}

// dueLabel is the human form of a due date relative to today.
func dueLabel(d, today vault.Date) string {
	t := d.Time(time.UTC)
	days := int(t.Sub(today.Time(time.UTC)).Hours() / 24)
	switch {
	case days == 0:
		return "today"
	case days == 1:
		return "tomorrow"
	case days == -1:
		return "yesterday"
	case days > 1 && days < 7:
		return t.Format("Mon")
	case d.Year == today.Year:
		return t.Format("2 Jan")
	}
	return t.Format("2 Jan 2006")
}

// cmdTags is `folio tags`: tags with counts.
func (a *app) cmdTags(args []string) error {
	fs := newFlagSet("tags")
	asJSON := fs.Bool("", "--json")
	byCount := fs.Bool("-c", "--count")
	pos, err := fs.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("tags", "unexpected argument %q (to list a tag's notes: folio ls --tag %s)", pos[0], strings.TrimPrefix(pos[0], "#"))
	}
	v, err := a.vault()
	if err != nil {
		return err
	}
	if err := a.requireRoot(v); err != nil {
		return err
	}
	if v, err = a.scannedVault(); err != nil {
		return err
	}
	tags := v.Tags()
	if len(tags) == 0 {
		if *asJSON {
			_ = a.writeJSON([]vault.TagCount{})
		}
		return a.nothing("no tags in %s", v.Root())
	}
	// Order by path segments so nested tags sit under their parent
	// ("a", "a/b", "a-c" rather than "a", "a-c", "a/b").
	sort.SliceStable(tags, func(i, j int) bool {
		if *byCount && tags[i].Count != tags[j].Count {
			return tags[i].Count > tags[j].Count
		}
		return segLess(tags[i].Tag, tags[j].Tag)
	})
	switch {
	case *asJSON:
		type tagJSON struct {
			Tag   string `json:"tag"`
			Count int    `json:"count"`
		}
		out := make([]tagJSON, 0, len(tags))
		for _, t := range tags {
			out = append(out, tagJSON{t.Tag, t.Count})
		}
		return a.writeJSON(out)
	case !a.human():
		for _, t := range tags {
			a.out("%s\t%d\n", t.Tag, t.Count)
		}
		return nil
	}
	p := a.painter()
	rows := make([][]cell, 0, len(tags))
	for _, t := range tags {
		label, ink := "#"+t.Tag, func(s string) string {
			if strings.HasPrefix(s, "#") {
				return p.accent("#") + p.text(p.visual(s[1:]))
			}
			return p.text(s)
		}
		if i := strings.LastIndexByte(t.Tag, '/'); i >= 0 && !*byCount {
			depth := strings.Count(t.Tag, "/")
			label = strings.Repeat("  ", depth) + "/" + t.Tag[i+1:]
			ink = func(s string) string {
				ind := len(s) - len(strings.TrimLeft(s, " "))
				rest := s[ind:]
				return s[:ind] + p.faint("/") + p.muted(p.visual(strings.TrimPrefix(rest, "/")))
			}
		}
		rows = append(rows, []cell{{s: label, paint: ink}, {s: fmt.Sprint(t.Count), paint: p.faint, right: true}})
	}
	for _, l := range p.table(rows, 2, 3, p.width, 0) {
		a.out("%s\n", l)
	}
	top := 0
	for _, t := range tags {
		if !strings.Contains(t.Tag, "/") {
			top++
		}
	}
	a.out("\n  %s\n", p.faint(plural(top, "tag", "tags")))
	return nil
}

// segLess orders tags by their "/"-separated segments, case-insensitively.
func segLess(a, b string) bool {
	as, bs := strings.Split(strings.ToLower(a), "/"), strings.Split(strings.ToLower(b), "/")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] != bs[i] {
			return as[i] < bs[i]
		}
	}
	return len(as) < len(bs)
}
