package tui

import (
	"sort"
	"strings"
	"time"

	"github.com/ZahakJ/folio/internal/md"
	"github.com/ZahakJ/folio/internal/render"
	"github.com/ZahakJ/folio/internal/term"
	"github.com/ZahakJ/folio/internal/text"
	"github.com/ZahakJ/folio/internal/vault"
)

// finder is the fuzzy note finder (Ctrl-p, Space f), the recent-notes list
// (Space r) and the `folio pick` picker. It matches on title, path and
// aliases; an empty query lists notes most recent first.
type finder struct {
	in     lineInput
	list   listView
	recent bool // Space r: only recently visited and modified notes
	pick   bool // folio pick: Enter returns the path

	cands   []finderCand
	candLen int // vault size when cands was built
	results []finderResult
	query   string
	took    time.Duration // last ranking time (shown with FOLIO_DEBUG=1)

	preview    *render.Page
	previewFor string
	previewW   int
	previewGen int
}

type finderCand struct {
	note *vault.Note
	key  string // "title  path  aliases" matched against
	tl   int    // byte length of the title part in key
	pl   int    // byte offset where the path ends in key
	rank int    // recency rank (lower = more recent)
}

type finderResult struct {
	c     *finderCand
	score int
	pos   []int
}

func (a *app) openFinder(recent bool) {
	f := &finder{recent: recent}
	f.refresh(a, true)
	a.pushOverlay(f)
}

func newPicker(a *app, query string) *finder {
	f := &finder{pick: true}
	f.in.set(query)
	f.refresh(a, true)
	return f
}

// buildCands (re)builds the candidate list from the index.
func (f *finder) buildCands(a *app) {
	notes := a.v.Notes()
	rank := map[string]int{}
	r := 0
	if a.recent != nil {
		for _, e := range a.recent.Entries() {
			if rel, err := a.v.Rel(e.Path); err == nil {
				if _, seen := rank[rel]; !seen {
					rank[rel] = r
					r++
				}
			}
		}
	}
	// The note on screen goes last, so Ctrl-p Enter returns to the
	// previous one.
	cur := a.curPath()
	if rk, ok := rank[cur]; ok && rk == 0 {
		for p, v := range rank {
			rank[p] = v - 1
		}
		delete(rank, cur)
	}
	visited := r
	for _, n := range a.v.RecentNotes() {
		if _, seen := rank[n.Path]; !seen {
			rank[n.Path] = r
			r++
		}
	}
	f.cands = f.cands[:0]
	for _, n := range notes {
		rk, ok := rank[n.Path]
		if !ok {
			rk = r
		}
		if n.Path == cur {
			rk = r + 1
		}
		if f.recent && rk >= max(visited, 30) {
			continue
		}
		title := n.Title
		if title == "" {
			title = noteName(n.Path)
		}
		key := title + "  " + n.Path
		c := finderCand{note: n, tl: len(title), pl: len(key), rank: rk}
		if len(n.Aliases) > 0 {
			key += "  " + strings.Join(n.Aliases, "  ")
		}
		c.key = key
		f.cands = append(f.cands, c)
	}
	f.candLen = a.v.Len()
}

// refresh re-ranks the candidates for the current query.
func (f *finder) refresh(a *app, force bool) {
	if force || f.candLen != a.v.Len() {
		f.buildCands(a)
		force = true
	}
	q := strings.TrimSpace(f.in.text)
	if !force && q == f.query {
		return
	}
	f.query = q
	t0 := time.Now()
	defer func() { f.took = time.Since(t0) }()
	f.results = f.results[:0]
	if q == "" {
		for i := range f.cands {
			f.results = append(f.results, finderResult{c: &f.cands[i]})
		}
		sort.SliceStable(f.results, func(i, j int) bool {
			return f.results[i].c.rank < f.results[j].c.rank
		})
	} else {
		m := text.NewMatcher(q)
		for i := range f.cands {
			if r, ok := m.Match(f.cands[i].key); ok {
				f.results = append(f.results, finderResult{c: &f.cands[i], score: r.Score, pos: append([]int(nil), r.Positions...)})
			}
		}
		sort.SliceStable(f.results, func(i, j int) bool {
			ri, rj := f.results[i], f.results[j]
			if ri.score != rj.score {
				return ri.score > rj.score
			}
			if ri.c.rank != rj.c.rank {
				return ri.c.rank < rj.c.rank
			}
			return len(ri.c.note.Path) < len(rj.c.note.Path)
		})
	}
	f.list.setLen(len(f.results))
	f.list.sel, f.list.top = 0, 0
}

func (f *finder) selected() *vault.Note {
	if f.list.sel < 0 || f.list.sel >= len(f.results) {
		return nil
	}
	return f.results[f.list.sel].c.note
}

func (f *finder) key(a *app, k term.KeyEvent) {
	switch k.String() {
	case "enter":
		n := f.selected()
		if n == nil {
			return
		}
		if f.pick {
			a.result.Path = n.Path
			a.quit = true
			return
		}
		a.closeAll()
		a.leaveEditorThen(func() { a.openNote(n.Path, 0) })
		return
	}
	if f.list.listKey(k, 10, false) {
		return
	}
	if handled, changed := f.in.handleKey(k); handled && changed {
		f.refresh(a, false)
	}
}

func (f *finder) paste(a *app, s string) {
	f.in.insert(s)
	f.refresh(a, false)
}

func (f *finder) closed(a *app) {
	if f.pick {
		a.cancelPick()
	}
}

func (f *finder) mouse(a *app, m term.MouseEvent) {
	switch m.Button {
	case term.MouseWheelDown:
		f.list.move(3)
	case term.MouseWheelUp:
		f.list.move(-3)
	}
}

func (f *finder) draw(a *app) {
	f.refresh(a, false)
	s := a.scr
	h := 22
	if f.pick {
		h = 24
	}
	r := panelRect(a.w, a.h, 80, h)
	title := "Find note"
	if f.recent {
		title = "Recent notes"
	}
	in := a.drawPanel(r, title)

	// Input row.
	x := s.PutString(in.X, in.Y, a.gl.Crumb, a.st.pAccent)
	x++
	count := fmtCount(len(f.results)) + " / " + fmtCount(len(f.cands))
	if !a.v.Scanned() {
		count += " " + a.gl.Ellipsis
	}
	if a.cfg.Debug {
		count = itoa(int(f.took.Microseconds())) + " µs  " + count
	}
	cw := text.Width(count)
	s.PutString(in.X+in.W-cw, in.Y, count, a.st.pFaint)
	cx := f.in.draw(s, x, in.Y, in.X+in.W-cw-1-x, a.st.panel)
	if f.in.text == "" {
		ph := "type to filter; empty lists recent notes"
		if f.recent {
			ph = "type to filter recent notes"
		}
		s.PutStringClip(x, in.Y, ph, a.st.pFaint, in.X+in.W-cw-1)
	}
	a.panelSeparator(r, in.Y+1)
	s.SetCursor(cx, in.Y)
	s.SetCursorShape(term.CursorBar)
	s.ShowCursor(true)

	body := term.Rect{X: in.X, Y: in.Y + 2, W: in.W, H: in.H - 2}
	listW := body.W
	showPreview := body.W >= 66 && body.H >= 6
	if showPreview {
		listW = body.W * 9 / 20
		sx := body.X + listW + 1
		s.VLine(sx, r.Y+2, r.H-3, a.gl.VLine, a.st.border)
		s.SetCell(sx, in.Y+1, a.gl.TeeDown, a.st.border)
		s.SetCell(sx, r.Y+r.H-1, a.gl.TeeUp, a.st.border)
		f.drawPreview(a, term.Rect{X: sx + 1, Y: body.Y, W: r.X + r.W - 1 - sx - 1, H: body.H})
	}
	f.list.scroll(body.H)
	if len(f.results) == 0 {
		msg := "no matching notes"
		if a.v.Len() == 0 {
			msg = "the vault has no notes yet (Space n creates one)"
		}
		s.PutStringClip(body.X, body.Y, msg, a.st.pMuted, body.X+listW)
		return
	}
	for i := 0; i < body.H; i++ {
		ri := f.list.top + i
		if ri >= len(f.results) {
			break
		}
		f.drawRow(a, term.Rect{X: body.X, Y: body.Y + i, W: listW, H: 1}, f.results[ri], ri == f.list.sel)
	}
}

// drawRow draws one result: the title with matched characters in accent,
// then the path muted, right-aligned.
func (f *finder) drawRow(a *app, r term.Rect, res finderResult, sel bool) {
	if sel {
		a.drawRow(r, r.Y, true)
	}
	st, mu := a.st.panel, a.st.pMuted
	if sel {
		st, mu = a.st.onSel(st), a.st.onSel(mu)
	}
	c := res.c
	title := c.key[:c.tl]
	p := strings.TrimSuffix(c.note.Path, ".md")
	pathStart := c.tl + 2
	// When the title is the file name, the folder alone says where it is.
	if strings.EqualFold(noteName(c.note.Path), title) {
		p = pathDir(c.note.Path)
	}
	var tpos, ppos []int
	for _, b := range res.pos {
		switch {
		case b < c.tl:
			tpos = append(tpos, b)
		case b >= pathStart && b < pathStart+len(p):
			ppos = append(ppos, b-pathStart)
		}
	}
	// The ranking may have matched the query in the path (its file name
	// often repeats the title); highlight the title's own match then.
	if len(tpos) == 0 && f.query != "" {
		if m, ok := text.NewMatcher(f.query).Match(title); ok {
			tpos = m.Positions
		}
	}
	maxX := r.X + r.W
	avail := r.W
	// When the whole path does not fit beside the title, the folder alone
	// says where the note lives: a left-truncated "…026-09-30" or a tail
	// repeating the title is noise.
	if dir := pathDir(c.note.Path); dir != p && text.Width(title)+2+text.Width(p) > avail {
		var keep []int
		for _, b := range ppos {
			if b < len(dir) {
				keep = append(keep, b)
			}
		}
		p, ppos = dir, keep
	}
	if d, ok := a.disp(title); ok {
		title, tpos = d, nil
	}
	if d, ok := a.disp(p); ok {
		p, ppos = d, nil
	}
	tw := text.Width(title)
	pw := text.Width(p)
	// The title gets the room it needs first; the path takes the rest.
	if tw+2+pw > avail {
		room := avail - min(tw, avail*2/3) - 2
		switch {
		case room >= 8 && pw <= room:
			// The path fits beside a shortened title.
		case room >= 8:
			cut := cutLeft(p, room-text.Width(a.gl.Ellipsis))
			np := a.gl.Ellipsis + p[cut:]
			var adj []int
			for _, b := range ppos {
				if b >= cut {
					adj = append(adj, b-cut+len(a.gl.Ellipsis))
				}
			}
			p, ppos, pw = np, adj, text.Width(np)
		default:
			p, pw = "", 0
		}
	}
	titleMax := maxX
	if pw > 0 {
		titleMax = maxX - pw - 2
	}
	if tw > titleMax-r.X {
		title = text.Truncate(title, titleMax-r.X, a.gl.Ellipsis)
	}
	a.putHighlighted(r.X, r.Y, title, st, tpos, titleMax)
	if pw > 0 {
		a.putHighlighted(maxX-pw, r.Y, p, mu, ppos, maxX)
	}
}

// cutLeft returns the byte offset from which the tail of s fits in w cells.
func cutLeft(s string, w int) int {
	gs := text.Graphemes(s)
	width := 0
	for i := len(gs) - 1; i >= 0; i-- {
		if width+gs[i].Width > w {
			if i+1 < len(gs) {
				return gs[i+1].Offset
			}
			return len(s)
		}
		width += gs[i].Width
	}
	return 0
}

// drawPreview renders the selected note into r (a live preview).
func (f *finder) drawPreview(a *app, r term.Rect) {
	n := f.selected()
	if n == nil || r.W < 20 {
		return
	}
	if f.preview == nil || f.previewFor != n.Path || f.previewW != r.W || f.previewGen != a.gen {
		f.preview = a.previewPage(n.Path, r.W)
		f.previewFor, f.previewW, f.previewGen = n.Path, r.W, a.gen
	}
	if f.preview == nil {
		a.scr.PutStringClip(r.X+1, r.Y, "(cannot read this note)", a.st.pFaint, r.X+r.W)
		return
	}
	for i := 0; i < r.H && i < len(f.preview.Lines); i++ {
		a.scr.PutSpans(r.X, r.Y+i, f.preview.Lines[i].Spans, r.X+r.W)
	}
}

// previewPage renders a note for a preview pane of width w on the raised
// ground of panels.
func (a *app) previewPage(rel string, w int) *render.Page {
	b, _, err := a.v.ReadNote(rel)
	if err != nil {
		return nil
	}
	if binaryContent(b) {
		b = []byte("*not a text file*\n")
	}
	if len(b) > 64<<10 {
		// A preview shows the beginning; don't lay out a whole book.
		cut := 64 << 10
		for cut < len(b) && b[cut] != '\n' {
			cut++
		}
		b = b[:cut]
	}
	th := a.th
	if !th.Raised.IsDefault() {
		th.Ground = th.Raised
	}
	return render.Render(md.ParseBytes(b), render.Options{
		Width:       w,
		Measure:     a.measure,
		Theme:       th,
		Glyphs:      a.gl,
		Bidi:        a.bidi,
		Resolver:    &resolver{v: a.v, from: rel},
		Today:       a.now(),
		PaintGround: !th.Ground.IsDefault(),
		Title:       noteName(rel),
	})
}
