package vault

import (
	"bytes"
	"fmt"
	"sort"
	"unicode/utf8"
)

// TaskRef is a task with the note it lives in.
type TaskRef struct {
	Path  string // vault-relative path of the note
	Title string // the note's title
	Task  Task
}

// TaskGroups partitions tasks relative to a day (DESIGN.md §4.1 `astrolabe
// tasks`).
type TaskGroups struct {
	Overdue  []TaskRef // due before today
	Today    []TaskRef // due today
	Upcoming []TaskRef // due after today
	Undated  []TaskRef // no due date
}

// Tasks returns the tasks across the vault, sorted by SortTasks. With all
// false only open tasks (not done, not cancelled) are returned.
func (v *Vault) Tasks(all bool) []TaskRef {
	var out []TaskRef
	for _, n := range v.Notes() {
		for _, t := range n.Tasks {
			if all || t.Open() {
				out = append(out, TaskRef{Path: n.Path, Title: n.Title, Task: t})
			}
		}
	}
	SortTasks(out)
	return out
}

// SortTasks orders tasks by due date (dated first, earliest first), then
// priority (highest first), then note path, then line.
func SortTasks(ts []TaskRef) {
	sort.SliceStable(ts, func(i, j int) bool {
		a, b := ts[i].Task, ts[j].Task
		if a.Due.IsZero() != b.Due.IsZero() {
			return !a.Due.IsZero()
		}
		if c := a.Due.Compare(b.Due); c != 0 {
			return c < 0
		}
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		if ts[i].Path != ts[j].Path {
			return ts[i].Path < ts[j].Path
		}
		return a.Line < b.Line
	})
}

// GroupTasks splits tasks into Overdue / Today / Upcoming / Undated
// relative to today. Each group keeps the SortTasks order.
func GroupTasks(ts []TaskRef, today Date) TaskGroups {
	sorted := append([]TaskRef(nil), ts...)
	SortTasks(sorted)
	var g TaskGroups
	for _, t := range sorted {
		switch {
		case t.Task.Due.IsZero():
			g.Undated = append(g.Undated, t)
		case t.Task.Due.Before(today):
			g.Overdue = append(g.Overdue, t)
		case t.Task.Due.Compare(today) == 0:
			g.Today = append(g.Today, t)
		default:
			g.Upcoming = append(g.Upcoming, t)
		}
	}
	return g
}

// TaskChangedError is returned by ToggleTask when the line is no longer the
// expected task (the file changed since it was rendered).
type TaskChangedError struct {
	Path string
	Line int
	Why  string
}

func (e *TaskChangedError) Error() string {
	return fmt.Sprintf("%s:%d: %s", e.Path, e.Line, e.Why)
}

// ToggleTask flips the task on line (1-based) of note rel and returns the
// task as it now is. Exactly one character of the file is rewritten (the
// state between the brackets); all other bytes, line endings included, are
// preserved, and the write is atomic and conflict-checked.
//
// Transitions: ' ' → 'x'; 'x'/'X' → ' '; '/' (in progress) → 'x';
// '-' (cancelled) and any custom state → ' '.
//
// If expect is not empty it must equal the task's Raw text as the caller
// knew it; otherwise a *TaskChangedError is returned and nothing is
// written. A *TaskChangedError is also returned when the line is not a task
// (or lies inside a code fence or frontmatter).
func (v *Vault) ToggleTask(rel string, line int, expect string) (Task, error) {
	rel, err := cleanRel(rel)
	if err != nil {
		return Task{}, err
	}
	data, tok, err := v.ReadNote(rel)
	if err != nil {
		return Task{}, err
	}
	n := parseNote(rel, data, tok.ModTime, tok.Size)
	if n.Large {
		return Task{}, &TaskChangedError{rel, line, "file too large to edit tasks in place"}
	}
	var task *Task
	for i := range n.Tasks {
		if n.Tasks[i].Line == line {
			task = &n.Tasks[i]
			break
		}
	}
	if task == nil {
		return Task{}, &TaskChangedError{rel, line, "line is not a task"}
	}
	if expect != "" && task.Raw != expect {
		return Task{}, &TaskChangedError{rel, line, "task text changed on disk"}
	}
	start, ok := lineOffset(data, line)
	if !ok {
		return Task{}, &TaskChangedError{rel, line, "line out of range"}
	}
	off := start + task.Col
	if bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) && line == 1 {
		off += 3 // the scanner saw the first line without its BOM
	}
	_, w := utf8.DecodeRune(data[off:])
	var next byte
	switch task.State {
	case ' ':
		next = 'x'
	case 'x', 'X', '-':
		next = ' '
	case '/':
		next = 'x'
	default:
		next = ' '
	}
	out := make([]byte, 0, len(data)-w+1)
	out = append(out, data[:off]...)
	out = append(out, next)
	out = append(out, data[off+w:]...)
	if _, err := v.WriteNote(rel, out, &tok); err != nil {
		return Task{}, err
	}
	t := *task
	t.State = rune(next)
	return t, nil
}

// lineOffset returns the byte offset of the start of 1-based line ln.
func lineOffset(data []byte, ln int) (int, bool) {
	if ln < 1 {
		return 0, false
	}
	off := 0
	for i := 1; i < ln; i++ {
		j := bytes.IndexByte(data[off:], '\n')
		if j < 0 {
			return 0, false
		}
		off += j + 1
	}
	if off > len(data) {
		return 0, false
	}
	return off, true
}
