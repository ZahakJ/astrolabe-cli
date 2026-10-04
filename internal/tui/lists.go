package tui

import (
	"sort"
	"strings"
	"time"

	"github.com/ZahakJ/folio/internal/md"
	"github.com/ZahakJ/folio/internal/term"
	"github.com/ZahakJ/folio/internal/text"
	"github.com/ZahakJ/folio/internal/vault"
)

// --- tags ---------------------------------------------------------------------------

// tagsOverlay lists the vault's tags with counts; Enter lists the notes
// carrying the selected tag (Space #).
type tagsOverlay struct {
	in   lineInput
	list listView
	all  []vault.TagCount
	rows []tagRow
	q    string
	n    int // vault size when built
}

type tagRow struct {
	tc  vault.TagCount
	pos []int
}

func (a *app) openTags() {
	o := &tagsOverlay{}
	o.refresh(a, true)
	a.pushOverlay(o)
}

func (o *tagsOverlay) refresh(a *app, force bool) {
	if force || o.n != a.v.Len() {
		o.all = a.v.Tags()
		o.n = a.v.Len()
		force = true
	}
	q := strings.TrimSpace(o.in.text)
	if !force && q == o.q {
		return
	}
	o.q = q
	o.rows = o.rows[:0]
	if q == "" {
		for _, t := range o.all {
			o.rows = append(o.rows, tagRow{tc: t})
		}
	} else {
		m := text.NewMatcher(strings.TrimPrefix(q, "#"))
		type scored struct {
			tagRow
			s int
		}
		var ss []scored
		for _, t := range o.all {
			if r, ok := m.Match(t.Tag); ok {
				ss = append(ss, scored{tagRow{tc: t, pos: append([]int(nil), r.Positions...)}, r.Score})
			}
		}
		sort.SliceStable(ss, func(i, j int) bool { return ss[i].s > ss[j].s })
		for _, s := range ss {
			o.rows = append(o.rows, s.tagRow)
		}
	}
	o.list.setLen(len(o.rows))
	o.list.sel, o.list.top = 0, 0
}

func (o *tagsOverlay) key(a *app, k term.KeyEvent) {
	if k.String() == "enter" {
		if o.list.sel < len(o.rows) {
			a.pushOverlay(newTagNotes(a, o.rows[o.list.sel].tc.Tag))
		}
		return
	}
	if o.list.listKey(k, 10, false) {
		return
	}
	if handled, changed := o.in.handleKey(k); handled && changed {
		o.refresh(a, false)
	}
}

func (o *tagsOverlay) paste(a *app, s string) {
	o.in.insert(s)
	o.refresh(a, false)
}

func (o *tagsOverlay) mouse(a *app, m term.MouseEvent) { wheelList(&o.list, m) }

func wheelList(l *listView, m term.MouseEvent) {
	switch m.Button {
	case term.MouseWheelDown:
		l.move(3)
	case term.MouseWheelUp:
		l.move(-3)
	}
}

func (o *tagsOverlay) draw(a *app) {
	o.refresh(a, false)
	s := a.scr
	r := panelRect(a.w, a.h, 56, 22)
	in := a.drawPanel(r, "Tags")
	x := s.PutString(in.X, in.Y, "#", a.st.pAccent)
	x++
	count := plural(len(o.rows), "tag", "tags")
	cw := text.Width(count)
	s.PutString(in.X+in.W-cw, in.Y, count, a.st.pFaint)
	cx := o.in.draw(s, x, in.Y, in.X+in.W-cw-1-x, a.st.panel)
	a.panelSeparator(r, in.Y+1)
	s.SetCursor(cx, in.Y)
	s.SetCursorShape(term.CursorBar)
	s.ShowCursor(true)
	body := term.Rect{X: in.X, Y: in.Y + 2, W: in.W, H: in.H - 2}
	if len(o.rows) == 0 {
		msg := "no tags yet"
		if o.q != "" {
			msg = "no matching tags"
		}
		s.PutStringClip(body.X, body.Y, msg, a.st.pMuted, body.X+body.W)
		return
	}
	o.list.scroll(body.H)
	for i := 0; i < body.H; i++ {
		ri := o.list.top + i
		if ri >= len(o.rows) {
			break
		}
		y := body.Y + i
		sel := ri == o.list.sel
		a.drawRow(body, y, sel)
		st, ac, fa := a.st.panel, a.st.pAccent, a.st.pFaint
		if sel {
			st, ac, fa = a.st.onSel(st), a.st.onSel(ac), a.st.onSel(fa)
		}
		row := o.rows[ri]
		n := itoa(row.tc.Count)
		x := s.PutString(body.X, y, "#", ac)
		a.putHighlighted(x, y, row.tc.Tag, st, row.pos, body.X+body.W-len(n)-2)
		s.PutString(body.X+body.W-len(n), y, n, fa)
	}
}

// --- notes list (notes with a tag) --------------------------------------------------

// noteList is a plain list of notes (the notes carrying a tag). Enter opens
// one.
type noteList struct {
	title string
	notes []*vault.Note
	list  listView
}

func newTagNotes(a *app, tag string) *noteList {
	o := &noteList{title: "#" + tag, notes: a.v.NotesWithTag(tag)}
	sort.SliceStable(o.notes, func(i, j int) bool { return o.notes[i].ModTime.After(o.notes[j].ModTime) })
	o.list.setLen(len(o.notes))
	return o
}

func (o *noteList) key(a *app, k term.KeyEvent) {
	switch k.String() {
	case "enter", "l":
		if o.list.sel < len(o.notes) {
			n := o.notes[o.list.sel]
			a.closeAll()
			a.leaveEditorThen(func() { a.openNote(n.Path, 0) })
		}
		return
	case "backspace", "h":
		a.closeOverlay()
		return
	}
	o.list.listKey(k, 10, true)
}

func (o *noteList) paste(a *app, s string) {}

func (o *noteList) mouse(a *app, m term.MouseEvent) { wheelList(&o.list, m) }

func (o *noteList) draw(a *app) {
	s := a.scr
	r := panelRect(a.w, a.h, 64, len(o.notes)+4)
	in := a.drawPanel(r, o.title)
	head := plural(len(o.notes), "note", "notes") + "  " + a.gl.Dot + "  Enter opens " + a.gl.Dot + " Backspace returns"
	s.PutStringClip(in.X, in.Y, head, a.st.pFaint, in.X+in.W)
	body := term.Rect{X: in.X, Y: in.Y + 1, W: in.W, H: in.H - 1}
	o.list.scroll(body.H)
	for i := 0; i < body.H; i++ {
		ri := o.list.top + i
		if ri >= len(o.notes) {
			break
		}
		y := body.Y + i
		sel := ri == o.list.sel
		a.drawRow(body, y, sel)
		st, mu := a.st.panel, a.st.pMuted
		if sel {
			st, mu = a.st.onSel(st), a.st.onSel(mu)
		}
		n := o.notes[ri]
		p := strings.TrimSuffix(n.Path, ".md")
		pw := min(text.Width(p), body.W/2)
		title := n.Title
		if title == "" {
			title = noteName(n.Path)
		}
		title, p = a.dispS(title), a.dispS(p)
		s.PutStringClip(body.X, y, text.Truncate(title, body.W-pw-2, a.gl.Ellipsis), st, body.X+body.W-pw-2)
		s.PutStringClip(body.X+body.W-pw, y, text.TruncateLeft(p, pw, a.gl.Ellipsis), mu, body.X+body.W)
	}
}

// --- agenda -------------------------------------------------------------------------

// agenda lists open tasks across the vault grouped Overdue / Today /
// Upcoming / Undated (Space t). x toggles, Enter jumps.
type agenda struct {
	rows []agendaRow
	list listView // over selectable rows only
	idx  []int    // list position → index in rows
	n    int
	done map[string]bool // tasks toggled in this session ("path:line"), kept visible
}

type agendaRow struct {
	head  string
	count int
	ref   vault.TaskRef
}

func (a *app) openAgenda() {
	o := &agenda{done: map[string]bool{}}
	o.build(a)
	a.pushOverlay(o)
}

func (o *agenda) build(a *app) {
	keepPath, keepLine := "", 0
	if o.list.sel < len(o.idx) && len(o.idx) > 0 {
		r := o.rows[o.idx[o.list.sel]]
		keepPath, keepLine = r.ref.Path, r.ref.Task.Line
	}
	all := a.v.Tasks(true)
	var ts []vault.TaskRef
	for _, t := range all {
		if t.Task.Open() || o.done[t.Path+":"+itoa(t.Task.Line)] {
			ts = append(ts, t)
		}
	}
	g := vault.GroupTasks(ts, vault.DateOf(a.now()))
	o.rows, o.idx = o.rows[:0], o.idx[:0]
	add := func(name string, refs []vault.TaskRef) {
		if len(refs) == 0 {
			return
		}
		o.rows = append(o.rows, agendaRow{head: name, count: len(refs)})
		for _, r := range refs {
			o.idx = append(o.idx, len(o.rows))
			o.rows = append(o.rows, agendaRow{ref: r})
		}
	}
	add("Overdue", g.Overdue)
	add("Today", g.Today)
	add("Upcoming", g.Upcoming)
	add("Undated", g.Undated)
	o.list.setLen(len(o.idx))
	for i, ri := range o.idx {
		if o.rows[ri].ref.Path == keepPath && o.rows[ri].ref.Task.Line == keepLine {
			o.list.sel = i
		}
	}
	o.n = a.v.Len()
}

func (o *agenda) current() *vault.TaskRef {
	if o.list.sel >= len(o.idx) || len(o.idx) == 0 {
		return nil
	}
	return &o.rows[o.idx[o.list.sel]].ref
}

func (o *agenda) key(a *app, k term.KeyEvent) {
	switch k.String() {
	case "x", "space":
		t := o.current()
		if t == nil {
			return
		}
		if a.toggleTaskAt(t.Path, t.Task.Line, t.Task.Raw) {
			o.done[t.Path+":"+itoa(t.Task.Line)] = true
		}
		o.build(a)
		return
	case "enter", "l":
		t := o.current()
		if t == nil {
			return
		}
		ref := *t
		a.closeAll()
		a.leaveEditorThen(func() { a.openNote(ref.Path, ref.Task.Line) })
		return
	case "q":
		a.closeOverlay()
		return
	}
	o.list.listKey(k, 10, true)
}

func (o *agenda) paste(a *app, s string) {}

func (o *agenda) mouse(a *app, m term.MouseEvent) { wheelList(&o.list, m) }

func (o *agenda) draw(a *app) {
	if o.n != a.v.Len() {
		o.build(a)
	}
	s := a.scr
	r := panelRect(a.w, a.h, 80, 24)
	in := a.drawPanel(r, "Agenda")
	open := 0
	for _, row := range o.rows {
		if row.head == "" && row.ref.Task.Open() {
			open++
		}
	}
	head := plural(open, "open task", "open tasks")
	if !a.v.Scanned() {
		head += "  " + a.gl.Dot + "  indexing" + a.gl.Ellipsis
	}
	s.PutStringClip(in.X, in.Y, head, a.st.pMuted, in.X+in.W)
	// In a narrow window the note column is dropped so the task text gets
	// the whole row; the selected task's note is named in the header
	// instead of the key hint.
	narrow := a.w < agendaNarrow
	hint := "x toggles " + a.gl.Dot + " Enter opens"
	if narrow && len(o.idx) > 0 {
		hint = text.Truncate(a.dispS(taskTitle(o.rows[o.idx[o.list.sel]].ref)), max(0, in.W-text.Width(head)-2), a.gl.Ellipsis)
	}
	s.PutStringClip(in.X+in.W-text.Width(hint), in.Y, hint, a.st.pFaint, in.X+in.W)
	a.panelSeparator(r, in.Y+1)
	body := term.Rect{X: in.X, Y: in.Y + 2, W: in.W, H: in.H - 2}
	if len(o.idx) == 0 {
		s.PutStringClip(body.X, body.Y, "no open tasks "+a.gl.Dot+" write - [ ] in any note", a.st.pMuted, body.X+body.W)
		return
	}
	// Scroll by rows so that headings stay attached to their tasks.
	selRow := o.idx[o.list.sel]
	top := 0
	if selRow >= body.H {
		top = selRow - body.H + 1
	}
	if o.list.sel == 0 {
		top = 0
	}
	if selRow < top {
		top = selRow
	}
	today := vault.DateOf(a.now())
	// Fixed columns: note title on the right, due date before it, so the
	// chips line up down the list.
	cols := agendaCols{narrow: narrow}
	for _, row := range o.rows {
		if row.head != "" {
			continue
		}
		cols.title = max(cols.title, text.Width(a.dispS(taskTitle(row.ref))))
		if !row.ref.Task.Due.IsZero() {
			cols.due = max(cols.due, text.Width(cols.date(row.ref.Task.Due, today)))
		}
		if row.ref.Task.Priority > 0 && !narrow {
			cols.prio = max(cols.prio, min(row.ref.Task.Priority, 3))
		}
	}
	cols.title = min(cols.title, body.W/4)
	if narrow {
		cols.title = 0
	}
	for i := 0; i < body.H; i++ {
		ri := top + i
		if ri >= len(o.rows) {
			break
		}
		y := body.Y + i
		row := o.rows[ri]
		if row.head != "" {
			hs := a.st.pAccent.With(boldAttr)
			if row.head == "Overdue" {
				hs = a.st.pDanger.With(boldAttr)
			}
			x := s.PutString(body.X, y, row.head, hs)
			s.PutString(x+1, y, itoa(row.count), a.st.pFaint)
			continue
		}
		sel := ri == selRow
		o.drawTask(a, body, y, row.ref, sel, today, cols)
	}
}

// agendaNarrow is the window width below which the agenda drops its note
// column.
const agendaNarrow = 70

// agendaCols are the widths of the agenda's right-hand columns. A narrow
// agenda has no note or priority column and shorter dates.
type agendaCols struct {
	title, due, prio int
	narrow           bool
}

// date is the due chip: shortDate, or in a narrow agenda a compact form
// ("today", a weekday within the week ahead, "2 Oct", "Feb 2031").
func (c agendaCols) date(d, today vault.Date) string {
	if !c.narrow {
		return shortDate(d, today)
	}
	t := d.Time(time.UTC)
	days := int(t.Sub(today.Time(time.UTC)).Hours() / 24)
	switch {
	case days == 0:
		return "today"
	case days > 0 && days < 7:
		return t.Format("Mon")
	case d.Year != today.Year:
		return t.Format("Jan 2006")
	}
	return t.Format("2 Jan")
}

func taskTitle(t vault.TaskRef) string {
	if t.Title != "" {
		return t.Title
	}
	return noteName(t.Path)
}

func (o *agenda) drawTask(a *app, body term.Rect, y int, t vault.TaskRef, sel bool, today vault.Date, cols agendaCols) {
	s := a.scr
	a.drawRow(body, y, sel)
	st, mu, fa, ac, dg := a.st.panel, a.st.pMuted, a.st.pFaint, a.st.pAccent, a.st.pDanger
	if sel {
		st, mu, fa, ac, dg = a.st.onSel(st), a.st.onSel(mu), a.st.onSel(fa), a.st.onSel(ac), a.st.onSel(dg)
	}
	box := a.gl.TaskOpen
	switch {
	case t.Task.Done():
		box = a.gl.TaskDone
		st = st.With(strikeAttr)
		st.FG = fa.FG
	case t.Task.Cancelled():
		box = a.gl.TaskCancelled
		st = st.With(strikeAttr)
		st.FG = fa.FG
	case t.Task.State == '/':
		box = a.gl.TaskDoing
	}
	maxX := body.X + body.W
	x := s.PutString(body.X+1, y, box, ac)
	x++
	rx := maxX - cols.title
	if cols.title > 0 {
		s.PutStringClip(rx, y, text.Truncate(a.dispS(taskTitle(t)), cols.title, a.gl.Ellipsis), fa, maxX)
	} else {
		rx = maxX + 1 // no note column: the due chip ends at the edge
	}
	if cols.due > 0 {
		rx -= cols.due + 2
		if !t.Task.Due.IsZero() {
			due := cols.date(t.Task.Due, today)
			dueSt := mu
			if t.Task.Due.Before(today) && t.Task.Open() {
				dueSt = dg.With(boldAttr)
			}
			s.PutString(rx+cols.due-text.Width(due), y, due, dueSt)
		}
	}
	if cols.prio > 0 {
		rx -= cols.prio + 2
		if t.Task.Priority > 0 {
			p := strings.Repeat("!", min(t.Task.Priority, 3))
			s.PutString(rx+cols.prio-len(p), y, p, dg)
		}
	}
	txt, _ := md.CleanLine(t.Task.Text, nil) // as the reader shows it
	prio := ""
	if cols.narrow && t.Task.Priority > 0 {
		prio = " " + strings.Repeat("!", min(t.Task.Priority, 3)) // inline, no column
	}
	end := s.PutStringClip(x, y, text.Truncate(a.dispS(txt), rx-2-x-len(prio), a.gl.Ellipsis), st, rx-2)
	if prio != "" {
		s.PutStringClip(end, y, prio, dg, rx-2)
	}
}

// shortDate formats a due date relative to today: "today", "tomorrow",
// "Mon 5 Oct", or with the year when it differs.
func shortDate(d, today vault.Date) string {
	switch {
	case d == today:
		return "today"
	case d.Compare(today) == 1 && d.Time(time.UTC).Sub(today.Time(time.UTC)).Hours() <= 24:
		return "tomorrow"
	case d.Compare(today) == -1 && today.Time(time.UTC).Sub(d.Time(time.UTC)).Hours() <= 24:
		return "yesterday"
	}
	t := d.Time(time.UTC)
	if d.Year != today.Year {
		return t.Format("2 Jan 2006")
	}
	return t.Format("Mon 2 Jan")
}
