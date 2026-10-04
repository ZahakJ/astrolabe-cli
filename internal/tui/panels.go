package tui

import (
	"path"
	"sort"
	"strings"

	"github.com/ZahakJ/astrolabe-cli/internal/md"
	"github.com/ZahakJ/astrolabe-cli/internal/render"
	"github.com/ZahakJ/astrolabe-cli/internal/term"
	"github.com/ZahakJ/astrolabe-cli/internal/text"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
	"github.com/ZahakJ/astrolabe-cli/internal/vault"
)

// Context panel focus.
const (
	ctxNone = iota
	ctxOutline
	ctxBacklinks
)

// contextPanel is the right-hand panel: the outline above the backlinks
// with their context lines (DESIGN.md §4.2).
type contextPanel struct {
	auto   bool // follow the default (open at ≥ 124 columns)
	open   bool // explicit state once toggled
	opened bool // opened by a focus command: close again on unfocus
	focus  int
	sel    int
	top    int

	blCache []vault.Backlink
	blFor   string
	blGen   int
	plain   map[string]string // backlink context line → plain text
}

// ctxRow is one row of the context panel.
type ctxRow struct {
	kind    int // 0 heading text, 1 outline entry, 2 backlink title, 3 backlink context, 4 blank, 5 note
	text    string
	level   int
	current bool
	out     *render.OutlineEntry
	bl      *vault.Backlink
	// pick makes a context row selectable: the second and later links
	// from one note are listed under its title once, by context alone.
	pick bool
}

func (a *app) backlinks() []vault.Backlink {
	d := a.doc
	if d == nil || d.stdin {
		return nil
	}
	if a.ctx.blFor != d.path || a.ctx.blGen != a.gen {
		a.ctx.blCache = a.v.Backlinks(d.path)
		a.ctx.blFor, a.ctx.blGen = d.path, a.gen
	}
	return a.ctx.blCache
}

// ctxRows builds the panel's rows for a panel of inner width w.
func (a *app) ctxRows(w int) []ctxRow {
	d := a.doc
	if d == nil || d.page == nil {
		return nil
	}
	var rows []ctxRow
	rows = append(rows, ctxRow{kind: 0, text: "Outline"})
	out := d.page.Outline
	if len(out) == 0 {
		rows = append(rows, ctxRow{kind: 5, text: "no headings"})
	}
	minLevel := 6
	for _, o := range out {
		minLevel = min(minLevel, o.Level)
	}
	curSrc := d.page.SourceLine(d.cur)
	current := -1
	for i, o := range out {
		if o.SrcLine <= curSrc {
			current = i
		}
	}
	for i := range out {
		rows = append(rows, ctxRow{kind: 1, text: out[i].Text, level: out[i].Level - minLevel, current: i == current, out: &out[i]})
	}
	rows = append(rows, ctxRow{kind: 4})
	bls := a.backlinks()
	// The count is of notes, like the title block's "N backlinks".
	head := "Backlinks"
	notes := 0
	for i := range bls {
		if i == 0 || bls[i].From != bls[i-1].From {
			notes++
		}
	}
	if notes > 0 {
		head += "  " + itoa(notes)
	}
	rows = append(rows, ctxRow{kind: 0, text: head})
	if len(bls) == 0 {
		msg := "no notes link here"
		if !a.v.Scanned() {
			msg = "indexing" + a.gl.Ellipsis
		}
		if d.stdin {
			msg = "stdin has no backlinks"
		}
		rows = append(rows, ctxRow{kind: 5, text: msg})
	}
	for i := range bls {
		b := &bls[i]
		title := b.Title
		if title == "" {
			title = noteName(b.From)
		}
		again := i > 0 && bls[i-1].From == b.From
		if !again {
			rows = append(rows, ctxRow{kind: 2, text: title, bl: b})
		}
		if a.ctx.plain == nil || len(a.ctx.plain) > 4096 {
			a.ctx.plain = map[string]string{}
		}
		ctxt, ok := a.ctx.plain[b.Link.Context]
		if !ok {
			ctxt = plainContext(b.Link.Context)
			a.ctx.plain[b.Link.Context] = ctxt
		}
		wrapped := text.Wrap(ctxt, max(8, w-2))
		for i, l := range wrapped[:min(2, len(wrapped))] {
			if i == 1 && len(wrapped) > 2 {
				l = text.Truncate(l+" "+wrapped[2], max(8, w-2), a.gl.Ellipsis)
			}
			rows = append(rows, ctxRow{kind: 3, text: l, bl: b, pick: again && i == 0})
		}
	}
	return rows
}

// ctxSelectable reports whether a row can be selected.
func ctxSelectable(r ctxRow) bool { return r.kind == 1 || r.kind == 2 || r.pick }

func (a *app) toggleCtxFocus(which int) {
	if a.view != viewReader || a.doc == nil {
		a.flash("open a note first")
		return
	}
	c := &a.ctx
	if c.focus == which && a.ctxOpen() {
		// Pressed again: close the panel (Esc or Tab only returns focus
		// to the page and keeps it open).
		c.focus = ctxNone
		c.auto, c.open, c.opened = false, false, false
		return
	}
	if !a.ctxOpen() {
		c.auto, c.open, c.opened = false, true, true
	}
	c.focus = which
	a.tree.focus = false
	// Select the first row of that section (or the current heading).
	rows := a.ctxRows(ctxWidth - 4)
	c.sel = -1
	for i, r := range rows {
		if which == ctxOutline && r.kind == 1 && (c.sel < 0 || r.current) {
			c.sel = i
		}
		if which == ctxBacklinks && r.kind == 2 {
			c.sel = i
			break
		}
	}
	if c.sel < 0 {
		c.sel = 0
		for i, r := range rows {
			if ctxSelectable(r) {
				c.sel = i
				break
			}
		}
	}
}

func (a *app) ctxKey(k term.KeyEvent) bool {
	c := &a.ctx
	rows := a.ctxRows(a.layout().ctx.W - 4)
	move := func(dir int) {
		for i := c.sel + dir; i >= 0 && i < len(rows); i += dir {
			if ctxSelectable(rows[i]) {
				c.sel = i
				return
			}
		}
	}
	switch k.String() {
	case "j", "down":
		move(1)
	case "k", "up":
		move(-1)
	case "g", "home":
		c.sel = -1
		move(1)
	case "G", "end":
		c.sel = len(rows)
		move(-1)
	case "esc", "tab":
		c.focus = ctxNone
		if c.opened {
			c.auto, c.open, c.opened = false, false, false
		}
	case "enter", "l":
		if c.sel < 0 || c.sel >= len(rows) {
			return true
		}
		a.ctxActivate(rows[c.sel])
	default:
		return false
	}
	return true
}

func (a *app) ctxActivate(r ctxRow) {
	d := a.doc
	switch {
	case r.out != nil:
		a.pushHistory()
		a.revealSource(d, r.out.SrcLine)
		i := d.page.LineForSource(r.out.SrcLine)
		d.top = i - min(2, a.viewRows()/4)
		a.setCursor(d, a.snapCursor(d, i, 1))
	case r.bl != nil:
		bl := *r.bl
		a.ctx.focus = ctxNone
		a.openNote(bl.From, bl.Link.Line)
	}
}

func (a *app) drawContext(r term.Rect, over bool) {
	s := a.scr
	d := a.doc
	if d == nil {
		return
	}
	a.ensurePage(d)
	fg, mu, fa, ac := a.st.base, a.st.muted, a.st.faint, a.st.accent
	if over {
		fg, mu, fa, ac = a.st.panel, a.st.pMuted, a.st.pFaint, a.st.pAccent
		s.Fill(r, " ", a.st.panel)
		s.VLine(r.X, r.Y, r.H, a.gl.VLine, a.st.border)
	} else {
		s.VLine(r.X-1, r.Y, r.H, a.gl.VLine, a.st.pageRule)
	}
	inner := term.Rect{X: r.X + 2, Y: r.Y + 1, W: r.W - 3, H: r.H - 1}
	rows := a.ctxRows(inner.W)
	c := &a.ctx
	if c.sel >= len(rows) {
		c.sel = len(rows) - 1
	}
	// Scroll the selection (or the current heading) into view.
	anchor := c.sel
	if c.focus == ctxNone {
		anchor = 0
		for i, row := range rows {
			if row.current {
				anchor = i
			}
		}
	}
	if anchor < c.top {
		c.top = anchor
	}
	if anchor >= c.top+inner.H {
		c.top = anchor - inner.H + 1
	}
	if c.focus == ctxNone && c.top > 0 && len(rows)-c.top < inner.H {
		c.top = max(0, len(rows)-inner.H)
	}
	for i := 0; i < inner.H; i++ {
		ri := c.top + i
		if ri >= len(rows) {
			break
		}
		row := rows[ri]
		y := inner.Y + i
		sel := c.focus != ctxNone && ri == c.sel
		st := fg
		switch row.kind {
		case 0:
			st = mu.With(boldAttr)
			if c.focus == ctxOutline && strings.HasPrefix(row.text, "Outline") ||
				c.focus == ctxBacklinks && strings.HasPrefix(row.text, "Backlinks") {
				st = ac.With(boldAttr)
			}
		case 1:
			st = mu
			if row.level == 0 {
				st = fg
			}
			if row.current {
				st = ac
			}
		case 2:
			st = fg
		case 3:
			st = fa
		case 5:
			st = fa.With(theme.Italic)
		}
		if sel {
			a.scr.Fill(term.Rect{X: inner.X - 1, Y: y, W: inner.W + 1, H: 1}, " ", a.st.sel)
			a.scr.SetCell(inner.X-1, y, a.gl.Selected, a.st.selBar)
			st = a.st.onSel(st)
		}
		x := inner.X
		switch row.kind {
		case 1:
			x += 2 * row.level
		case 3:
			x += 2
		}
		s.PutStringClip(x, y, a.dispFit(row.text, inner.X+inner.W-x), st, inner.X+inner.W)
	}
}

func (a *app) ctxMouse(r term.Rect, m term.MouseEvent) {
	c := &a.ctx
	switch m.Button {
	case term.MouseWheelDown:
		c.top += 3
	case term.MouseWheelUp:
		c.top = max(0, c.top-3)
	case term.MouseLeft:
		if m.Action != term.MousePress {
			return
		}
		rows := a.ctxRows(r.W - 3)
		i := c.top + m.Y - (r.Y + 1)
		if i >= 0 && i < len(rows) && ctxSelectable(rows[i]) {
			c.sel = i
			a.ctxActivate(rows[i])
		}
	}
}

// --- tree -----------------------------------------------------------------------

// treePanel is the left-hand file tree (Space e): folders collapsible,
// notes sorted, the current note marked.
type treePanel struct {
	open     bool
	focus    bool
	expanded map[string]bool
	sel, top int
	rows     []treeRow
	builtN   int
	builtCur string
	dirty    bool
}

type treeRow struct {
	path  string // folder path (no trailing slash) or note path
	name  string
	depth int
	dir   bool
}

func (a *app) toggleTree() {
	t := &a.tree
	switch {
	case !t.open:
		t.open, t.focus = true, true
		a.ctx.focus = ctxNone
		t.reveal(a)
	case !t.focus:
		t.focus = true
		a.ctx.focus = ctxNone
		t.reveal(a)
	default:
		t.open, t.focus = false, false
	}
}

func (a *app) curPath() string {
	if a.doc != nil && !a.doc.stdin {
		return a.doc.path
	}
	return ""
}

// reveal expands the folders of the current note and selects it.
func (t *treePanel) reveal(a *app) {
	if t.expanded == nil {
		t.expanded = map[string]bool{}
	}
	cur := a.curPath()
	for dir := path.Dir(cur); cur != "" && dir != "." && dir != "/"; dir = path.Dir(dir) {
		t.expanded[dir] = true
	}
	t.build(a)
	for i, r := range t.rows {
		if r.path == cur {
			t.sel = i
		}
	}
}

func (t *treePanel) build(a *app) {
	if t.expanded == nil {
		t.expanded = map[string]bool{}
	}
	notes := a.v.Notes()
	type node struct {
		dirs  map[string]*node
		notes []string
	}
	root := &node{dirs: map[string]*node{}}
	for _, n := range notes {
		parts := strings.Split(n.Path, "/")
		cur := root
		for _, p := range parts[:len(parts)-1] {
			nx := cur.dirs[p]
			if nx == nil {
				nx = &node{dirs: map[string]*node{}}
				cur.dirs[p] = nx
			}
			cur = nx
		}
		cur.notes = append(cur.notes, n.Path)
	}
	t.rows = t.rows[:0]
	var walk func(n *node, prefix string, depth int)
	walk = func(n *node, prefix string, depth int) {
		var dirs []string
		for d := range n.dirs {
			dirs = append(dirs, d)
		}
		sort.Slice(dirs, func(i, j int) bool { return lessFold(dirs[i], dirs[j]) })
		for _, d := range dirs {
			p := d
			if prefix != "" {
				p = prefix + "/" + d
			}
			t.rows = append(t.rows, treeRow{path: p, name: d, depth: depth, dir: true})
			if t.expanded[p] {
				walk(n.dirs[d], p, depth+1)
			}
		}
		sort.Slice(n.notes, func(i, j int) bool { return lessFold(n.notes[i], n.notes[j]) })
		for _, p := range n.notes {
			t.rows = append(t.rows, treeRow{path: p, name: noteName(p), depth: depth})
		}
	}
	walk(root, "", 0)
	t.builtN = a.v.Len()
	t.builtCur = a.curPath()
	t.dirty = false
	if t.sel >= len(t.rows) {
		t.sel = max(0, len(t.rows)-1)
	}
}

func lessFold(a, b string) bool {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if la != lb {
		return la < lb
	}
	return a < b
}

func (a *app) treeKey(k term.KeyEvent) bool {
	t := &a.tree
	if t.dirty || t.builtN != a.v.Len() {
		t.build(a)
	}
	if len(t.rows) == 0 {
		if k.String() == "esc" {
			t.focus = false
			return true
		}
		return false
	}
	row := t.rows[t.sel]
	switch k.String() {
	case "j", "down":
		t.sel = min(t.sel+1, len(t.rows)-1)
	case "k", "up":
		t.sel = max(t.sel-1, 0)
	case "g", "home":
		t.sel = 0
	case "G", "end":
		t.sel = len(t.rows) - 1
	case "ctrl+d":
		t.sel = min(t.sel+a.viewRows()/2, len(t.rows)-1)
	case "ctrl+u":
		t.sel = max(t.sel-a.viewRows()/2, 0)
	case "h", "left":
		if row.dir && t.expanded[row.path] {
			t.expanded[row.path] = false
			t.build(a)
		} else if parent := path.Dir(row.path); parent != "." {
			for i, r := range t.rows {
				if r.dir && r.path == parent {
					t.sel = i
				}
			}
		}
	case "l", "right", "enter":
		if row.dir {
			if k.String() == "enter" || !t.expanded[row.path] {
				t.expanded[row.path] = !t.expanded[row.path]
				t.build(a)
			} else if t.sel+1 < len(t.rows) {
				t.sel++
			}
			return true
		}
		t.focus = false
		a.openNote(row.path, 0)
	case "esc", "tab":
		t.focus = false
	default:
		return false
	}
	return true
}

func (a *app) drawTree(r term.Rect, over bool) {
	s := a.scr
	t := &a.tree
	if t.dirty || t.builtN != a.v.Len() || t.builtCur != a.curPath() {
		t.build(a)
	}
	fg, mu, ac := a.st.base, a.st.muted, a.st.accent
	if over {
		fg, mu, ac = a.st.panel, a.st.pMuted, a.st.pAccent
		s.Fill(r, " ", a.st.panel)
		s.VLine(r.X+r.W-1, r.Y, r.H, a.gl.VLine, a.st.border)
	} else {
		s.VLine(r.X+r.W, r.Y, r.H, a.gl.VLine, a.st.pageRule)
	}
	inner := term.Rect{X: r.X + 2, Y: r.Y + 1, W: r.W - 3, H: r.H - 1}
	head := filepathBase(a.v.Root())
	hs := mu.With(boldAttr)
	if t.focus {
		hs = ac.With(boldAttr)
	}
	s.PutStringClip(inner.X, r.Y, text.Truncate(head, inner.W, a.gl.Ellipsis), hs, inner.X+inner.W)
	inner.Y++
	inner.H--
	if len(t.rows) == 0 {
		s.PutStringClip(inner.X, inner.Y, "no notes", mu, inner.X+inner.W)
		return
	}
	if t.sel < t.top {
		t.top = t.sel
	}
	if t.sel >= t.top+inner.H {
		t.top = t.sel - inner.H + 1
	}
	t.top = max(0, min(t.top, len(t.rows)-inner.H))
	cur := a.curPath()
	for i := 0; i < inner.H; i++ {
		ri := t.top + i
		if ri >= len(t.rows) {
			break
		}
		row := t.rows[ri]
		y := inner.Y + i
		x := inner.X + 2*row.depth
		st := fg
		var mark string
		ms := a.st.faint
		if over {
			ms = a.st.pFaint
		}
		if row.dir {
			mark = a.gl.FoldClosed
			if t.expanded[row.path] {
				mark = a.gl.FoldOpen
			}
			st = mu
		}
		if !row.dir && row.path == cur {
			st = ac.With(boldAttr)
		}
		sel := t.focus && ri == t.sel
		if sel {
			s.Fill(term.Rect{X: inner.X - 1, Y: y, W: inner.W + 1, H: 1}, " ", a.st.sel)
			s.SetCell(inner.X-1, y, a.gl.Selected, a.st.selBar)
			st, ms = a.st.onSel(st), a.st.onSel(ms)
		}
		if mark != "" {
			s.PutString(x, y, mark, ms)
		}
		x += text.Width(a.gl.FoldOpen) + 1
		name := a.dispS(row.name)
		if row.dir {
			name += "/"
		}
		s.PutStringClip(x, y, text.Truncate(name, inner.X+inner.W-x, a.gl.Ellipsis), st, inner.X+inner.W)
	}
}

func (a *app) treeMouse(r term.Rect, m term.MouseEvent) {
	t := &a.tree
	switch m.Button {
	case term.MouseWheelDown:
		t.top += 3
		t.sel = min(max(t.sel, t.top), len(t.rows)-1)
	case term.MouseWheelUp:
		t.top = max(0, t.top-3)
	case term.MouseLeft:
		if m.Action != term.MousePress {
			return
		}
		i := t.top + m.Y - (r.Y + 2)
		if i >= 0 && i < len(t.rows) {
			t.sel = i
			t.focus = true
			a.ctx.focus = ctxNone
			a.treeKey(term.KeyEvent{Key: term.KeyEnter})
		}
	}
}

func filepathBase(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// plainContext turns a backlink's source line into readable text: list and
// quote markers, link brackets and emphasis markers removed.
func plainContext(line string) string {
	line = strings.TrimSpace(line)
	if cells := tableCells(line); len(cells) > 1 {
		// A table row: its cells, readable, separated by a middle dot.
		var parts []string
		for _, c := range cells {
			if t := plainContext(c); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, " · ")
	}
	doc := md.Parse(line)
	var parts []string
	md.WalkBlocks(doc.Blocks, func(b md.Block) bool {
		for _, in := range md.BlockInlines(b) {
			if t := strings.TrimSpace(md.PlainText(in)); t != "" {
				parts = append(parts, t)
			}
		}
		return true
	})
	if len(parts) == 0 {
		return line
	}
	return strings.Join(parts, " ")
}

// tableCells splits a Markdown table row ("| a | b |") into its trimmed
// cells; it returns nil for any other line.
func tableCells(line string) []string {
	var out []string
	for _, sp := range md.TableCellSpans(line) {
		out = append(out, line[sp[0]:sp[1]])
	}
	return out
}
