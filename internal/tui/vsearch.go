package tui

import (
	"context"
	"strings"
	"time"

	"github.com/ZahakJ/astrolabe-cli/internal/md"
	"github.com/ZahakJ/astrolabe-cli/internal/term"
	"github.com/ZahakJ/astrolabe-cli/internal/text"
	"github.com/ZahakJ/astrolabe-cli/internal/vault"
)

// searchDebounce is how long typing must pause before the vault is searched.
const searchDebounce = 120 * time.Millisecond

// searchLimit caps the results of one vault search.
const searchLimit = 500

// vaultSearch is the live full-text search overlay (Space /, Space s).
type vaultSearch struct {
	in      lineInput
	list    listView
	at      time.Time // when to run the pending query (zero = none)
	seq     int       // query generation; stale results are dropped
	cancel  context.CancelFunc
	running bool
	query   string // query of the results shown
	results []vault.Result
	err     string
	took    time.Duration
}

func (a *app) openVaultSearch() {
	a.pushOverlay(&vaultSearch{})
}

func (o *vaultSearch) changed(a *app) {
	o.at = a.now().Add(searchDebounce)
	if strings.TrimSpace(o.in.text) == "" {
		o.stop()
		o.at = time.Time{}
		o.results, o.query, o.err = nil, "", ""
		o.list.setLen(0)
	}
}

func (o *vaultSearch) stop() {
	if o.cancel != nil {
		o.cancel()
		o.cancel = nil
	}
	o.seq++
	o.running = false
}

func (o *vaultSearch) deadline() time.Time { return o.at }

func (o *vaultSearch) tick(a *app, now time.Time) {
	o.at = time.Time{}
	o.start(a)
}

// start runs the current query in the background, cancelling the previous
// one.
func (o *vaultSearch) start(a *app) {
	o.stop()
	q := strings.TrimSpace(o.in.text)
	if q == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	o.cancel = cancel
	o.running = true
	seq := o.seq
	v := a.v
	t0 := time.Now()
	a.goAsync(func() func() {
		res, err := v.Search(ctx, q, vault.SearchOptions{Limit: searchLimit})
		took := time.Since(t0)
		return func() {
			if seq != o.seq {
				return // superseded or closed
			}
			o.running = false
			o.cancel = nil
			cancel()
			if err != nil {
				if ctx.Err() == nil {
					o.err = err.Error()
				}
				return
			}
			o.err = ""
			o.query = q
			o.results = res
			o.took = took
			o.list.setLen(len(res))
			o.list.sel, o.list.top = 0, 0
		}
	})
}

func (o *vaultSearch) closed(a *app) { o.stop() }

func (o *vaultSearch) key(a *app, k term.KeyEvent) {
	if k.String() == "enter" {
		if o.at.IsZero() && !o.running || len(o.results) > 0 {
			if o.list.sel < len(o.results) {
				r := o.results[o.list.sel]
				o.stop()
				a.closeAll()
				a.leaveEditorThen(func() {
					if a.openNote(r.Path, r.Line) && r.Line > 0 && a.doc != nil {
						a.highlightLine(r)
					}
				})
			}
			return
		}
		// Enter before the debounce fired: search now.
		o.at = time.Time{}
		o.start(a)
		return
	}
	if o.list.listKey(k, 8, false) {
		return
	}
	if handled, changed := o.in.handleKey(k); handled && changed {
		o.changed(a)
	}
}

// highlightLine marks the search terms on the page after jumping to a hit.
func (a *app) highlightLine(r vault.Result) {
	if len(r.Matches) == 0 {
		return
	}
	m := r.Matches[0]
	if m[0] < 0 || m[1] > len(r.Text) || m[0] >= m[1] {
		return
	}
	term := r.Text[m[0]:m[1]]
	d := a.doc
	cur, top := d.cur, d.top
	a.startSearch(term, false)
	d.cur, d.top = cur, top
	for i, mm := range d.matches {
		if mm.line >= d.cur {
			d.mi = i
			break
		}
	}
}

func (o *vaultSearch) paste(a *app, s string) {
	o.in.insert(s)
	o.changed(a)
}

func (o *vaultSearch) mouse(a *app, m term.MouseEvent) {
	switch m.Button {
	case term.MouseWheelDown:
		o.list.move(3)
	case term.MouseWheelUp:
		o.list.move(-3)
	}
}

func (o *vaultSearch) draw(a *app) {
	s := a.scr
	r := panelRect(a.w, a.h, 80, 24)
	in := a.drawPanel(r, "Search the vault")
	x := s.PutString(in.X, in.Y, "/", a.st.pAccent)
	x++
	status := ""
	switch {
	case o.err != "":
		status = "bad query"
	case o.running || !o.at.IsZero():
		status = "searching" + a.gl.Ellipsis
	case o.query != "":
		status = plural(len(o.results), "match", "matches")
		if len(o.results) >= searchLimit {
			status = fmtCount(searchLimit) + "+ matches"
		}
		if a.cfg.Debug {
			status += " " + a.gl.Dot + " " + itoa(int(o.took.Milliseconds())) + " ms"
		}
	}
	sw := text.Width(status)
	s.PutString(in.X+in.W-sw, in.Y, status, a.st.pFaint)
	cx := o.in.draw(s, x, in.Y, in.X+in.W-sw-1-x, a.st.panel, a.bidi)
	if o.in.text == "" {
		s.PutStringClip(x, in.Y, "words, \"re:\" regexp, tag:x path:x title:x", a.st.pFaint, in.X+in.W)
	}
	a.panelSeparator(r, in.Y+1)
	s.SetCursor(cx, in.Y)
	s.SetCursorShape(term.CursorBar)
	s.ShowCursor(true)

	body := term.Rect{X: in.X, Y: in.Y + 2, W: in.W, H: in.H - 2}
	if o.err != "" {
		s.PutStringClip(body.X, body.Y, o.err, a.st.pDanger, body.X+body.W)
		return
	}
	if o.query != "" && len(o.results) == 0 && !o.running {
		s.PutStringClip(body.X, body.Y, "nothing matches", a.st.pMuted, body.X+body.W)
		return
	}
	// Each result takes two rows: the note and line, then the context.
	rowsPer := 2
	if body.H < 8 {
		rowsPer = 1
	}
	visible := max(1, body.H/rowsPer)
	o.list.scroll(visible)
	for i := 0; i < visible; i++ {
		ri := o.list.top + i
		if ri >= len(o.results) {
			break
		}
		y := body.Y + i*rowsPer
		o.drawResult(a, term.Rect{X: body.X, Y: y, W: body.W, H: rowsPer}, o.results[ri], ri == o.list.sel)
	}
}

func (o *vaultSearch) drawResult(a *app, r term.Rect, res vault.Result, sel bool) {
	s := a.scr
	st, mu, fa := a.st.panel, a.st.pMuted, a.st.pFaint
	if sel {
		for y := r.Y; y < r.Y+r.H; y++ {
			a.drawRow(term.Rect{X: r.X, Y: y, W: r.W, H: 1}, y, true)
		}
		st, mu, fa = a.st.onSel(st), a.st.onSel(mu), a.st.onSel(fa)
	}
	hl := a.th.Search.Over(st)
	maxX := r.X + r.W
	title := res.Title
	if title == "" {
		title = noteName(res.Path)
	}
	where := a.dispPath(strings.TrimSuffix(res.Path, ".md"))
	if res.Line > 0 {
		where += ":" + itoa(res.Line)
	}
	// Show the matched line as the reader would, not as raw Markdown.
	res.Text, res.Matches = md.CleanLine(res.Text, res.Matches)
	if r.H == 1 {
		// Compact: title, then the context.
		x := s.PutStringClip(r.X, r.Y, a.dispFit(title, r.W/3), mu, maxX)
		x += 2
		ctxText, ranges := contextAround(res, maxX-x, a.gl.Ellipsis)
		ctxText, ranges = a.dispRanges(ctxText, ranges)
		a.putRanges(x, r.Y, ctxText, st, hl, ranges, maxX)
		return
	}
	x := r.X
	if res.Class == vault.RankTitle {
		ranges := res.Matches
		if res.Title != "" {
			title, ranges = a.dispRangesFit(res.Title, res.Matches, r.W-2)
		} else {
			title = a.dispFit(title, r.W-2)
		}
		a.putRanges(x, r.Y, title, st.With(boldAttr), hl.With(boldAttr), ranges, maxX)
		s.PutStringClip(r.X, r.Y+1, text.TruncateLeft(where, r.W, a.gl.Ellipsis), fa, maxX)
		return
	}
	x = s.PutStringClip(x, r.Y, a.dispFit(title, r.W*2/3), st.With(boldAttr), maxX)
	pw := text.Width(where)
	if x+2+pw > maxX {
		where = text.TruncateLeft(where, max(0, maxX-x-2), a.gl.Ellipsis)
		pw = text.Width(where)
	}
	s.PutStringClip(maxX-pw, r.Y, where, fa, maxX)
	ctxText, ranges := contextAround(res, r.W-2, a.gl.Ellipsis)
	ctxText, ranges = a.dispRanges(ctxText, ranges)
	a.putRanges(r.X+2, r.Y+1, ctxText, mu, hl, ranges, maxX)
}

// contextAround trims a result line to w cells around its first match and
// returns the shifted match ranges.
func contextAround(res vault.Result, w int, ell string) (string, [][2]int) {
	t := strings.ReplaceAll(res.Text, "\t", " ")
	// Trim leading blanks (keeping ranges aligned).
	lead := len(t) - len(strings.TrimLeft(t, " "))
	t = t[lead:]
	ranges := shiftRanges(res.Matches, -lead, len(t))
	if w <= 0 || text.Width(t) <= w || len(ranges) == 0 {
		return t, ranges
	}
	// Start a little before the first match so it is in view.
	first := ranges[0][0]
	if text.Width(t[:first]) < w/3 {
		return t, ranges
	}
	start := 0
	before := w / 4
	for _, g := range text.Graphemes(t[:first]) {
		if text.Width(t[g.Offset:first]) <= before {
			start = g.Offset
			break
		}
	}
	out := ell + t[start:]
	return out, shiftRanges(ranges, len(ell)-start, len(out))
}

func shiftRanges(rs [][2]int, by, limit int) [][2]int {
	var out [][2]int
	for _, r := range rs {
		a, b := r[0]+by, r[1]+by
		if b <= 0 || a >= limit {
			continue
		}
		out = append(out, [2]int{max(a, 0), min(b, limit)})
	}
	return out
}
