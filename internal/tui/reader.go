package tui

import (
	"errors"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ZahakJ/folio/internal/md"
	"github.com/ZahakJ/folio/internal/render"
	"github.com/ZahakJ/folio/internal/term"
	"github.com/ZahakJ/folio/internal/text"
	"github.com/ZahakJ/folio/internal/theme"
	"github.com/ZahakJ/folio/internal/vault"
)

// docView is a note open in the reader: its source, parse, rendered page and
// the reader's position on it.
type docView struct {
	path  string // vault-relative ("" for stdin)
	stdin bool   // `folio -`: read-only
	name  string // display name
	src   []byte
	tok   vault.StatToken
	md    *md.Document
	note  *vault.Note // index entry (may be nil before the scan or for stdin)
	words int

	page  *render.Page
	pageW int // pane width the page was rendered for
	gen   int // app.gen the page was rendered at

	cur, top int // cursor line and first visible line of the page
	// wantSet asks the next ensurePage to put the cursor on source line
	// want (-1 = top) at wantOff rows from the top (-1 = default).
	wantSet       bool
	want, wantOff int
	focus         int // Tab-highlighted link (-1 none)

	codeRun, codeOff int // horizontal scroll of the code block starting at line codeRun

	search    *regexp.Regexp
	searchStr string
	matches   []searchMatch
	mi        int
}

// searchMatch is one in-note search hit, in rendered-line cells.
type searchMatch struct{ line, x0, x1 int }

// bad reports a file that cannot be shown as a note (binary content).
func binaryContent(b []byte) bool {
	n := len(b)
	if n > 8000 {
		n = 8000
	}
	for _, c := range b[:n] {
		if c == 0 {
			return true
		}
	}
	return !utf8.Valid(b[:n]) && !utf8.Valid(b[:max(0, n-3)])
}

// loadDoc reads a note from the vault.
func (a *app) loadDoc(rel string) (*docView, error) {
	b, tok, err := a.v.ReadNote(rel)
	if err != nil {
		return nil, err
	}
	d := &docView{path: rel, name: noteName(rel), tok: tok, focus: -1}
	if n, err := a.v.Refresh(rel); err == nil {
		d.note = n
	}
	d.setSource(b)
	return d, nil
}

// setSource replaces the document text and drops the rendered page.
func (d *docView) setSource(b []byte) {
	if binaryContent(b) {
		b = []byte("> [!warning] Not a text file\n> This file does not look like Markdown text, so folio will not display it.\n")
	}
	d.src = b
	d.md = md.ParseBytes(b)
	d.words = md.WordCount(d.md)
	d.page = nil
	d.search, d.matches = nil, nil
	d.focus = -1
}

// title returns the note's title for the status bar.
func (d *docView) title() string {
	if d.md != nil && d.md.Frontmatter != nil && d.md.Frontmatter.Title != "" {
		return d.md.Frontmatter.Title
	}
	if d.note != nil && d.note.Title != "" {
		return d.note.Title
	}
	return d.name
}

// --- opening notes ------------------------------------------------------------

// openNote opens a note in the reader at a 1-based source line (0 = the
// remembered line, or the top), recording the current place in history.
func (a *app) openNote(rel string, line int) bool {
	if a.doc != nil && !a.doc.stdin && a.doc.path == rel && a.view == viewReader {
		if line > 0 {
			a.pushHistory()
			a.gotoSource(line - 1)
		}
		return true
	}
	d, err := a.loadDoc(rel)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			a.flashErr("%s no longer exists", rel)
		} else {
			a.flashErr("cannot open %s: %v", rel, err)
		}
		return false
	}
	if line <= 0 && a.recent != nil {
		if l, ok := a.recent.Line(a.v.Abs(rel)); ok && l > 0 {
			line = l
		}
	}
	a.pushHistory()
	a.show(d, line-1, -1)
	return true
}

// show makes d the current document with the cursor on source line src
// (-1 = top) and the cursor off rows from the top (-1 = default placement).
func (a *app) show(d *docView, src, off int) {
	a.remember()
	a.doc = d
	if d.stdin {
		a.stdinDoc = d
	}
	a.view = viewReader
	a.ctx.sel, a.ctx.top = 0, 0
	a.ctx.blCache, a.ctx.blFor = nil, ""
	d.page = nil
	d.wantSet, d.want, d.wantOff = true, src, off
	a.ensurePage(d)
	a.remember()
}

// pushHistory records the current place before navigating away.
func (a *app) pushHistory() {
	if e, ok := a.here(); ok {
		a.back = append(a.back, e)
		if len(a.back) > 200 {
			a.back = a.back[1:]
		}
		a.fwd = nil
	}
}

// here describes the current place.
func (a *app) here() (histEntry, bool) {
	d := a.doc
	if d == nil || a.view != viewReader {
		return histEntry{}, false
	}
	src := 0
	if d.page != nil {
		src = d.page.SourceLine(d.cur)
	}
	return histEntry{path: d.path, stdin: d.stdin, src: src, off: d.cur - d.top}, true
}

func (a *app) historyBack()    { a.historyMove(&a.back, &a.fwd, "beginning") }
func (a *app) historyForward() { a.historyMove(&a.fwd, &a.back, "end") }

func (a *app) historyMove(from, to *[]histEntry, end string) {
	for len(*from) > 0 {
		e := (*from)[len(*from)-1]
		*from = (*from)[:len(*from)-1]
		var d *docView
		if e.stdin {
			d = a.stdinDoc
		} else if a.doc != nil && !a.doc.stdin && a.doc.path == e.path {
			d = a.doc
		} else {
			nd, err := a.loadDoc(e.path)
			if err != nil {
				continue // deleted meanwhile: skip it
			}
			d = nd
		}
		if d == nil {
			continue
		}
		if cur, ok := a.here(); ok {
			*to = append(*to, cur)
		}
		if d == a.doc {
			a.gotoSourceOff(e.src, e.off)
		} else {
			a.show(d, e.src, e.off)
		}
		return
	}
	a.flash("at the %s of history", end)
}

// --- rendering ----------------------------------------------------------------

// pageWidth is the width of the page pane.
func (a *app) pageWidth() int { return a.layout().page.W }

// ensurePage renders d for the current pane width and theme if needed,
// keeping the cursor on the same source line.
func (a *app) ensurePage(d *docView) {
	w := a.pageWidth()
	if d.page != nil && d.pageW == w && d.gen == a.gen && !d.wantSet {
		return
	}
	keepSrc, keepOff := -1, -1
	if d.page != nil {
		keepSrc = d.page.SourceLine(d.cur)
		keepOff = d.cur - d.top
	}
	if d.wantSet {
		keepSrc, keepOff = d.want, d.wantOff
		d.wantSet = false
	}
	a.renderPage(d, w)
	if keepSrc >= 0 {
		d.cur = d.page.LineForSource(keepSrc)
	} else {
		d.cur = 0
	}
	d.cur = a.snapCursor(d, d.cur, 1)
	if keepOff >= 0 {
		d.top = d.cur - keepOff
	} else {
		d.top = d.cur - a.viewRows()/3
	}
	a.clampView(d)
	if d.searchStr != "" {
		a.computeMatches(d)
	}
}

func (a *app) renderPage(d *docView, w int) {
	folds := a.folds[a.foldKey(d)]
	title := d.name
	if d.stdin {
		title = ""
	}
	opt := render.Options{
		Width:       w,
		Measure:     a.measure,
		Theme:       a.th,
		Glyphs:      a.gl,
		Bidi:        a.bidi,
		Folds:       folds,
		Resolver:    &resolver{v: a.v, from: d.path, lenient: true},
		Today:       a.now(),
		PaintGround: !a.th.Ground.IsDefault(),
		Title:       title,
	}
	if !d.stdin && d.path != "" {
		opt.Backlinks = a.v.BacklinkCount(d.path)
	}
	d.page = render.Render(d.md, opt)
	d.pageW = w
	d.gen = a.gen
	d.focus = -1
	d.codeRun, d.codeOff = -1, 0
}

func (a *app) foldKey(d *docView) string {
	if d.stdin {
		return "\x00stdin"
	}
	return d.path
}

// viewRows is the number of page rows visible below the top margin.
func (a *app) viewRows() int { return max(1, a.h-2) }

// scrolloff is how many lines are kept visible above and below the cursor.
func (a *app) scrolloff() int { return min(3, (a.viewRows()-1)/2) }

// clampView adjusts top so the cursor is visible with scrolloff.
func (a *app) clampView(d *docView) {
	if d.page == nil {
		return
	}
	n := len(d.page.Lines)
	rows := a.viewRows()
	so := a.scrolloff()
	if d.cur < 0 {
		d.cur = 0
	}
	if d.cur >= n {
		d.cur = max(0, n-1)
	}
	if d.cur-so < d.top {
		d.top = d.cur - so
	}
	if d.cur+so >= d.top+rows {
		d.top = d.cur + so - rows + 1
	}
	if d.top > n-rows {
		d.top = n - rows
	}
	if d.top < 0 {
		d.top = 0
	}
}

// selectable reports whether the cursor may rest on page line i: blank
// spacing lines are skipped.
func selectable(p *render.Page, i int) bool {
	if i < 0 || i >= len(p.Lines) {
		return false
	}
	l := p.Lines[i]
	if l.Kind == render.KindBlank {
		return false
	}
	if l.Kind == render.KindCode {
		// Code: only rows with code on them. The padding rows and the
		// language label are ornament, like hairlines (and so are blank
		// code lines); a block with no code at all keeps its first row
		// selectable so it can still be reached and edited.
		if l.Code != nil && l.Code.Width > 0 {
			return true
		}
		first := i == 0 || p.Lines[i-1].Kind != render.KindCode
		return first && !codeRunHasText(p, i)
	}
	if l.Code != nil {
		return true
	}
	t := strings.TrimSpace(l.Text())
	// Hairlines (under the title, an H1, above the footnotes) are
	// ornament, not content: the cursor passes over them like blank rows.
	return t != "" && strings.Trim(t, "─-") != ""
}

// codeRunHasText reports whether the code block around line i (its
// contiguous run of code rows) has a row with code on it.
func codeRunHasText(p *render.Page, i int) bool {
	for _, dir := range []int{-1, 1} {
		for j := i; j >= 0 && j < len(p.Lines) && p.Lines[j].Kind == render.KindCode; j += dir {
			if c := p.Lines[j].Code; c != nil && c.Width > 0 {
				return true
			}
		}
	}
	return false
}

// snapCursor moves i to the nearest selectable line in direction dir (then
// the other way).
func (a *app) snapCursor(d *docView, i, dir int) int {
	p := d.page
	n := len(p.Lines)
	if n == 0 {
		return 0
	}
	i = max(0, min(i, n-1))
	for j := i; j >= 0 && j < n; j += dir {
		if selectable(p, j) {
			return j
		}
	}
	for j := i; j >= 0 && j < n; j -= dir {
		if selectable(p, j) {
			return j
		}
	}
	return i
}

// --- drawing ------------------------------------------------------------------

func (a *app) drawReader(r term.Rect) {
	d := a.doc
	if d == nil {
		return
	}
	a.ensurePage(d)
	a.clampView(d)
	p := d.page
	s := a.scr
	rows := min(a.viewRows(), r.H-1)
	cl := a.th.CursorLine
	for i := 0; i < rows; i++ {
		li := d.top + i
		if li >= len(p.Lines) {
			break
		}
		y := r.Y + 1 + i
		line := p.Lines[li]
		if line.Code != nil && d.codeOff > 0 && a.inCodeRun(d, li) {
			line = line.Scrolled(d.codeOff)
		}
		spans := line.Spans
		if d.focus >= 0 && a.linkOnLine(d, d.focus, li) {
			spans = p.FocusSpans(li, d.focus, a.th.LinkFocus)
			if line.Code != nil && d.codeOff > 0 {
				spans = line.Spans
			}
		}
		s.PutSpans(r.X, y, spans, r.X+r.W)
		for mi, m := range d.matches {
			if m.line != li {
				continue
			}
			st := a.th.Search
			if mi == d.mi {
				st = a.th.SearchCurrent
			}
			s.Restyle(term.Rect{X: r.X + m.x0, Y: y, W: m.x1 - m.x0, H: 1}, func(o theme.Style) theme.Style {
				return st.Over(o)
			})
		}
		if li == d.cur {
			if !cl.BG.IsDefault() {
				x0 := r.X + p.Left
				s.Restyle(term.Rect{X: x0, Y: y, W: min(p.Measure, r.W-p.Left), H: 1}, func(o theme.Style) theme.Style {
					if o.BG == a.th.Ground || o.BG.IsDefault() {
						o.BG = cl.BG
					}
					return o
				})
			}
			s.SetCell(r.X+p.CursorX, y, a.gl.Bar, a.st.cursor)
		}
	}
}

// scrollPercent returns the reader's position as "Top", "Bot", "All" or
// "NN%", Vim style ("" when there is no page).
func (a *app) scrollPercent() string {
	d := a.doc
	if d == nil || d.page == nil {
		return ""
	}
	n, rows := len(d.page.Lines), a.viewRows()
	switch {
	case n <= rows:
		return ""
	case d.top <= 0:
		return "Top"
	case d.top >= n-rows:
		return "Bot"
	}
	return itoa(d.top*100/(n-rows)) + "%"
}

func (a *app) inCodeRun(d *docView, li int) bool {
	return d.codeRun >= 0 && li >= d.codeRun && li < d.codeRun+codeRunLen(d.page, d.codeRun)
}

// codeRunStart returns the first line of the code block containing line i.
func codeRunStart(p *render.Page, i int) int {
	for i > 0 && p.Lines[i-1].Code != nil {
		i--
	}
	return i
}

func codeRunLen(p *render.Page, start int) int {
	n := 0
	for start+n < len(p.Lines) && p.Lines[start+n].Code != nil {
		n++
	}
	return n
}

func (a *app) linkOnLine(d *docView, link, li int) bool {
	if link < 0 || link >= len(d.page.Links) {
		return false
	}
	for _, r := range d.page.Links[link].Regions {
		if r.Line == li {
			return true
		}
	}
	return false
}

// --- movement -----------------------------------------------------------------

// readerKey handles a key in the reader or home view.
func (a *app) readerKey(k term.KeyEvent) {
	if a.view == viewHome && len(a.keys.pending) == 0 && a.homeKey(k) {
		return
	}
	if a.tree.focus && a.treeOpen() && len(a.keys.pending) == 0 && a.treeKey(k) {
		return
	}
	if a.ctx.focus != ctxNone && a.ctxOpen() && len(a.keys.pending) == 0 && a.ctxKey(k) {
		return
	}
	if k.String() == "ctrl+c" {
		a.keys.reset()
		a.flash("type q to quit")
		return
	}
	a.dispatch(k)
}

func (a *app) needDoc() *docView {
	if a.view != viewReader || a.doc == nil || a.doc.page == nil {
		return nil
	}
	return a.doc
}

func (a *app) cursorMove(n int) {
	d := a.needDoc()
	if d == nil {
		return
	}
	dir := 1
	if n < 0 {
		dir, n = -1, -n
	}
	i := d.cur
	for ; n > 0; n-- {
		j := i + dir
		for j >= 0 && j < len(d.page.Lines) && !selectable(d.page, j) {
			j += dir
		}
		if j < 0 || j >= len(d.page.Lines) {
			break
		}
		i = j
	}
	if i == d.cur {
		// At an end: still scroll so trailing blank lines show.
		if dir > 0 {
			d.top++
		} else {
			d.top--
		}
	}
	a.setCursor(d, i)
}

// setCursor moves the cursor and drops a link highlight that is no longer
// on its line.
func (a *app) setCursor(d *docView, i int) {
	d.cur = i
	if d.focus >= 0 && !a.linkOnLine(d, d.focus, i) {
		d.focus = -1
	}
	if d.codeRun >= 0 && !a.inCodeRun(d, i) {
		d.codeRun, d.codeOff = -1, 0
	}
	a.clampView(d)
}

func (a *app) halfPage(dir int) {
	d := a.needDoc()
	if d == nil {
		return
	}
	n := max(1, a.viewRows()/2)
	d.top += dir * n
	a.setCursor(d, a.snapCursor(d, d.cur+dir*n, dir))
}

func (a *app) fullPage(dir int) {
	d := a.needDoc()
	if d == nil {
		return
	}
	n := max(1, a.viewRows()-2)
	d.top += dir * n
	a.setCursor(d, a.snapCursor(d, d.cur+dir*n, dir))
}

func (a *app) gotoTop(count int) {
	d := a.needDoc()
	if d == nil {
		return
	}
	if count > 0 {
		a.gotoSource(count - 1)
		return
	}
	d.top = 0
	a.setCursor(d, a.snapCursor(d, 0, 1))
}

func (a *app) gotoBottom(count int) {
	d := a.needDoc()
	if d == nil {
		return
	}
	if count > 0 {
		a.gotoSource(count - 1)
		return
	}
	a.setCursor(d, a.snapCursor(d, len(d.page.Lines)-1, -1))
}

// gotoSource puts the cursor on the rendered line of a 0-based source line,
// about a third down the window.
func (a *app) gotoSource(src int) { a.gotoSourceOff(src, -1) }

func (a *app) gotoSourceOff(src, off int) {
	d := a.doc
	if d == nil {
		return
	}
	a.ensurePage(d)
	i := a.snapCursor(d, d.page.LineForSource(src), 1)
	if off < 0 {
		off = a.viewRows() / 3
	}
	d.top = i - off
	a.setCursor(d, i)
}

func (a *app) nextHeading(n int) {
	d := a.needDoc()
	if d == nil {
		return
	}
	dir := 1
	if n < 0 {
		dir, n = -1, -n
	}
	i := d.cur
	moved := false
	for ; n > 0; n-- {
		j := i + dir
		for j >= 0 && j < len(d.page.Lines) && !isHeadingStart(d.page, j) {
			j += dir
		}
		if j < 0 || j >= len(d.page.Lines) {
			break
		}
		i, moved = j, true
	}
	if !moved {
		if dir > 0 {
			a.flash("no heading below")
		} else {
			a.flash("no heading above")
		}
		return
	}
	// Bring the heading near the top so its section is in view.
	d.top = i - min(a.scrolloff()+1, a.viewRows()/4)
	a.setCursor(d, i)
}

// isHeadingStart reports whether line i is the first line of a heading.
func isHeadingStart(p *render.Page, i int) bool {
	l := p.Lines[i]
	if l.Kind != render.KindHeading {
		return false
	}
	return i == 0 || p.Lines[i-1].Kind != render.KindHeading || p.Lines[i-1].SrcStart != l.SrcStart
}

func (a *app) codeScroll(dx int) {
	d := a.needDoc()
	if d == nil {
		return
	}
	l := d.page.Lines[d.cur]
	if l.Code == nil {
		return
	}
	start := codeRunStart(d.page, d.cur)
	if d.codeRun != start {
		d.codeRun, d.codeOff = start, 0
	}
	maxOff := 0
	for i := start; i < start+codeRunLen(d.page, start); i++ {
		maxOff = max(maxOff, d.page.Lines[i].Code.MaxScroll())
	}
	d.codeOff = max(0, min(d.codeOff+dx, maxOff))
}

// --- links --------------------------------------------------------------------

func (a *app) cycleLink(dir int) {
	d := a.needDoc()
	if d == nil {
		return
	}
	links := d.page.Links
	if len(links) == 0 {
		a.flash("no links in this note")
		return
	}
	next := -1
	if d.focus >= 0 {
		next = (d.focus + dir + len(links)) % len(links)
	} else if dir > 0 {
		for i, l := range links {
			if l.Line >= d.cur {
				next = i
				break
			}
		}
		if next < 0 {
			next = 0
		}
	} else {
		for i := len(links) - 1; i >= 0; i-- {
			if links[i].Line <= d.cur {
				next = i
				break
			}
		}
		if next < 0 {
			next = len(links) - 1
		}
	}
	d.focus = next
	line := links[next].Line
	if len(links[next].Regions) > 0 {
		line = links[next].Regions[0].Line
	}
	d.cur = line
	a.clampView(d)
	l := links[next]
	switch l.Status {
	case render.LinkExternal:
		a.flash("%s", l.URL)
	case render.LinkBroken:
		if isAttachment(l.Target) {
			a.flash("missing attachment: %s", l.Target)
		} else {
			a.flash("%s does not exist yet (Enter creates it)", linkName(l))
		}
	default:
		if l.Path != "" && l.Target != "" {
			a.flash("%s", l.Path)
		}
	}
}

func linkName(l render.Link) string {
	if l.Target != "" {
		return l.Target
	}
	if l.Heading != "" {
		return "#" + l.Heading
	}
	return l.Text
}

// follow follows the highlighted link, else the first link on the cursor
// line; a footnote marker jumps to its definition and a heading line
// without links toggles its fold.
func (a *app) follow() {
	d := a.needDoc()
	if d == nil {
		return
	}
	link := d.focus
	line := d.page.Lines[d.cur]
	if link < 0 {
		for _, h := range line.Hits {
			if h.Kind == render.HitLink {
				link = h.Link
				break
			}
		}
	}
	if link >= 0 {
		a.followLink(d, d.page.Links[link])
		return
	}
	for _, h := range line.Hits {
		if h.Kind == render.HitFootnote {
			if l, ok := d.page.Footnotes[h.Footnote]; ok {
				a.pushHistory()
				d.top = l - a.viewRows()/3
				a.setCursor(d, a.snapCursor(d, l, 1))
			}
			return
		}
	}
	if line.Fold != "" {
		a.foldCmd(foldToggle)
		return
	}
	a.flash("no link on this line (Tab cycles links)")
}

func (a *app) followLink(d *docView, l render.Link) {
	switch {
	case l.Status == render.LinkExternal:
		if err := a.host.Copy(l.URL); err != nil {
			a.flash("%s  (copy may not be supported here)", l.URL)
		} else {
			a.flash("copied %s", l.URL)
		}
	case l.Status == render.LinkBroken:
		target := l.Target
		if target == "" {
			a.flashErr("no heading %q in this note", l.Heading)
			return
		}
		a.offerCreate(target)
	case l.Target == "" && !d.stdin || l.Path == d.path && !d.stdin && l.Path != "":
		// Same-note anchor.
		a.jumpAnchor(d, l.Heading, l.Block, true)
	case l.Path == "":
		// Rendered while the scan was running: resolve it now.
		r := &resolver{v: a.v, from: d.path}
		p, ok := r.Resolve(render.LinkQuery{Target: l.Target, Heading: l.Heading, Block: l.Block, Wiki: l.Kind == render.LinkWiki || l.Kind == render.LinkEmbed, Embed: l.Kind == render.LinkEmbed})
		switch {
		case ok:
			l.Path = p
			a.followLink(d, l)
		case !a.v.Scanned():
			a.flash("still indexing the vault; try again in a moment")
		default:
			a.offerCreate(l.Target)
		}
	case !strings.HasSuffix(strings.ToLower(l.Path), ".md"):
		a.flash("%s %s (attachments are not displayed)", a.gl.Image, l.Path)
	default:
		if !a.openNote(l.Path, 0) {
			return
		}
		if l.Heading != "" || l.Block != "" {
			a.jumpAnchor(a.doc, l.Heading, l.Block, false)
		}
	}
}

// jumpAnchor moves to a heading or block of d.
func (a *app) jumpAnchor(d *docView, heading, block string, history bool) {
	anchor := heading
	if block != "" {
		anchor = "^" + block
	}
	if anchor == "" {
		return
	}
	line, ok := md.ResolveAnchor(d.md, anchor)
	if !ok {
		a.flashErr("no %q in %s", anchor, d.name)
		return
	}
	if history {
		a.pushHistory()
	}
	// Unfold whatever hides the target.
	a.ensurePage(d)
	a.revealSource(d, line)
	d.top = d.page.LineForSource(line) - min(2, a.viewRows()/4)
	a.setCursor(d, a.snapCursor(d, d.page.LineForSource(line), 1))
}

// revealSource opens the folds hiding source line src.
func (a *app) revealSource(d *docView, src int) {
	for iter := 0; iter < 8; iter++ {
		i := d.page.LineForSource(src)
		if i < 0 || i >= len(d.page.Lines) {
			return
		}
		l := d.page.Lines[i]
		if !l.Folded || l.Fold == "" || (l.SrcStart == src) {
			return
		}
		a.setFold(d, l.Fold, false)
		a.ensurePage(d)
	}
}

// offerCreate asks whether to create the note a broken link names. A
// broken link to an attachment (x.png, report.pdf) only says so: creating
// x.png.md would litter the vault with a note nobody asked for.
func (a *app) offerCreate(target string) {
	if a.doc != nil && a.doc.stdin && a.v == nil {
		return
	}
	if isAttachment(target) {
		a.flashErr("missing attachment: %s (not in the vault)", target)
		return
	}
	a.pushOverlay(newConfirm("Create “"+target+"”?", func(a *app) {
		title, dir := target, ""
		if i := strings.LastIndex(target, "/"); i >= 0 {
			dir, title = target[:i], target[i+1:]
		}
		title = strings.TrimSuffix(title, ".md")
		rel, err := a.v.CreateNote(title, vault.NewNoteOptions{Dir: dir})
		if err != nil {
			a.flashErr("cannot create %s: %v", target, err)
			return
		}
		a.gen++ // the link resolves now
		a.pushHistory()
		a.openEditorOn(rel, -1)
		a.startWriting()
		a.flash("created %s", rel)
	}, nil))
}

// isAttachment reports whether a link target names a file other than a
// note: its last path element has an extension that is not .md. To keep
// note names with dots ("Release 1.2", "Mr. Smith", "2026.10.04") notes, an
// extension is 1–8 ASCII letters and digits with at least one letter.
func isAttachment(target string) bool {
	base := target
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	dot := strings.LastIndexByte(base, '.')
	if dot <= 0 || dot == len(base)-1 {
		return false
	}
	ext := base[dot+1:]
	if len(ext) > 8 || strings.EqualFold(ext, "md") {
		return false
	}
	letter := false
	for i := 0; i < len(ext); i++ {
		c := ext[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			letter = true
		case c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return letter
}

// --- folds --------------------------------------------------------------------

const (
	foldToggle = iota
	foldClose
	foldOpen
)

// foldUnderCursor returns the fold id of the section under the cursor: the
// cursor line's own chevron, else the nearest heading above it.
func (a *app) foldUnderCursor(d *docView) string {
	p := d.page
	if l := p.Lines[d.cur]; l.Fold != "" {
		return l.Fold
	}
	for i := d.cur; i >= 0; i-- {
		l := p.Lines[i]
		if l.Fold != "" && l.Kind == render.KindHeading {
			return l.Fold
		}
	}
	return ""
}

func (a *app) foldCmd(op int) {
	d := a.needDoc()
	if d == nil {
		return
	}
	id := a.foldUnderCursor(d)
	if id == "" {
		a.flash("nothing to fold here")
		return
	}
	folded := false
	for _, f := range d.page.Folds {
		if f.ID == id {
			folded = f.Folded
		}
	}
	want := !folded
	switch op {
	case foldClose:
		want = true
	case foldOpen:
		want = false
	}
	// Keep the cursor on the chevron line.
	var src int
	for _, f := range d.page.Folds {
		if f.ID == id {
			src = f.SrcLine
		}
	}
	off := -1
	for i, l := range d.page.Lines {
		if l.Fold == id {
			off = i - d.top
		}
	}
	a.setFold(d, id, want)
	d.wantSet, d.want, d.wantOff = true, src, off
	a.ensurePage(d)
}

func (a *app) setFold(d *docView, id string, folded bool) {
	k := a.foldKey(d)
	m := a.folds[k]
	if m == nil {
		m = map[string]bool{}
		a.folds[k] = m
	}
	m[id] = folded
	d.page = nil
}

func (a *app) foldAll(folded bool) {
	d := a.needDoc()
	if d == nil {
		return
	}
	k := a.foldKey(d)
	m := map[string]bool{}
	for _, f := range d.page.Folds {
		if folded {
			if f.Kind == render.KindHeading {
				m[f.ID] = true
			}
		} else {
			m[f.ID] = false
		}
	}
	a.folds[k] = m
	src := d.page.SourceLine(d.cur)
	if folded {
		// Land on the heading that now hides the cursor.
		for i := d.cur; i >= 0; i-- {
			if d.page.Lines[i].Kind == render.KindHeading && d.page.Lines[i].Fold != "" {
				src = d.page.Lines[i].SrcStart
				break
			}
		}
	}
	d.page = nil
	d.wantSet, d.want, d.wantOff = true, src, -1
	a.ensurePage(d)
}

// --- tasks --------------------------------------------------------------------

func (a *app) toggleTask() {
	d := a.needDoc()
	if d == nil {
		return
	}
	var ref *render.TaskRef
	for _, h := range d.page.Lines[d.cur].Hits {
		if h.Kind == render.HitTask {
			t := h.Task
			ref = &t
			break
		}
	}
	if ref == nil {
		a.flash("no task on this line")
		return
	}
	if d.stdin {
		a.flash("read-only: this page came from stdin")
		return
	}
	a.toggleTaskAt(d.path, ref.Line+1, "")
}

// toggleTaskAt toggles the task on a 1-based line of a note and reloads the
// current page if it is that note.
func (a *app) toggleTaskAt(rel string, line int, expect string) bool {
	// Refuse if the open copy is stale: the line numbers might not match.
	if a.doc != nil && a.doc.path == rel && !a.doc.stdin {
		if _, tok, err := a.v.ReadNote(rel); err == nil && tok != a.doc.tok {
			a.reloadDoc(a.doc)
			a.flashErr("the note changed on disk; reloaded it, press x again")
			return false
		}
	}
	t, err := a.v.ToggleTask(rel, line, expect)
	if err != nil {
		a.flashErr("%v", err)
		return false
	}
	if a.doc != nil && a.doc.path == rel && !a.doc.stdin {
		a.reloadDoc(a.doc)
	}
	switch {
	case t.Done():
		a.flash("done: %s", t.Text)
	case t.Cancelled():
		a.flash("cancelled: %s", t.Text)
	default:
		a.flash("reopened: %s", t.Text)
	}
	return true
}

// reloadDoc re-reads d from disk, keeping the reader's place.
func (a *app) reloadDoc(d *docView) {
	if d.stdin {
		return
	}
	b, tok, err := a.v.ReadNote(d.path)
	if err != nil {
		a.flashErr("cannot read %s: %v", d.path, err)
		return
	}
	if n, err := a.v.Refresh(d.path); err == nil {
		d.note = n
	}
	keepSrc, keepOff := 0, -1
	if d.page != nil {
		keepSrc, keepOff = d.page.SourceLine(d.cur), d.cur-d.top
	}
	q := d.searchStr
	d.tok = tok
	d.setSource(b)
	d.wantSet, d.want, d.wantOff = true, keepSrc, keepOff
	if q != "" {
		d.searchStr = q
		d.search = compileSearch(q)
	}
	a.ensurePage(d)
}

// --- in-note search -------------------------------------------------------------

// compileSearch builds a smart-case literal matcher for q.
func compileSearch(q string) *regexp.Regexp {
	pat := regexp.QuoteMeta(q)
	if !hasUpper(q) {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil
	}
	return re
}

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// computeMatches finds the search matches on the rendered page (what the
// reader sees: markup removed, wrapped lines matched one by one).
func (a *app) computeMatches(d *docView) {
	d.matches = d.matches[:0]
	if d.search == nil || d.page == nil {
		return
	}
	for i, l := range d.page.Lines {
		t := l.Text()
		for _, m := range d.search.FindAllStringIndex(t, -1) {
			if m[1] == m[0] {
				continue
			}
			x0 := text.Width(t[:m[0]])
			x1 := x0 + text.Width(t[m[0]:m[1]])
			d.matches = append(d.matches, searchMatch{line: i, x0: x0, x1: x1})
		}
	}
	if d.mi >= len(d.matches) {
		d.mi = 0
	}
}

// startSearch runs an in-note search for q from the cursor.
func (a *app) startSearch(q string, jump bool) {
	d := a.needDoc()
	if d == nil {
		return
	}
	d.searchStr = q
	d.search = nil
	d.matches = nil
	if q == "" {
		return
	}
	d.search = compileSearch(q)
	a.computeMatches(d)
	if len(d.matches) == 0 {
		if jump {
			a.flashErr("not found: %s", q)
		}
		return
	}
	d.mi = 0
	for i, m := range d.matches {
		if m.line >= d.cur {
			d.mi = i
			break
		}
	}
	if jump {
		a.showMatch(d)
	}
}

func (a *app) showMatch(d *docView) {
	m := d.matches[d.mi]
	d.cur = m.line
	if d.focus >= 0 && !a.linkOnLine(d, d.focus, m.line) {
		d.focus = -1
	}
	a.clampView(d)
	a.flash("/%s  %d of %d", d.searchStr, d.mi+1, len(d.matches))
}

func (a *app) searchNext(n int) {
	d := a.needDoc()
	if d == nil {
		return
	}
	if d.searchStr == "" {
		a.flash("no search yet (/ searches this note)")
		return
	}
	if d.matches == nil {
		a.computeMatches(d)
	}
	if len(d.matches) == 0 {
		a.flashErr("not found: %s", d.searchStr)
		return
	}
	k := len(d.matches)
	// From the cursor: the first match after (or before) it.
	if d.matches[d.mi].line != d.cur {
		d.mi = -1
		for i, m := range d.matches {
			if (n > 0 && m.line > d.cur) || (n < 0 && m.line < d.cur) {
				if n > 0 {
					d.mi = i
					break
				}
				d.mi = i
			}
		}
		if d.mi < 0 {
			if n > 0 {
				d.mi = 0
			} else {
				d.mi = k - 1
			}
		}
		n -= sign(n)
	}
	d.mi = ((d.mi+n)%k + k) % k
	a.showMatch(d)
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}

// clearTransient drops the link highlight and the search highlight.
func (a *app) clearTransient() {
	if d := a.doc; d != nil {
		d.focus = -1
		d.search, d.searchStr, d.matches = nil, "", nil
	}
	a.ctx.focus = ctxNone
	a.tree.focus = false
}

// --- mouse --------------------------------------------------------------------

func (a *app) readerMouse(r term.Rect, m term.MouseEvent) {
	d := a.needDoc()
	if d == nil {
		return
	}
	switch m.Button {
	case term.MouseWheelUp, term.MouseWheelDown:
		dir := 3
		if m.Button == term.MouseWheelUp {
			dir = -3
		}
		d.top += dir
		n := len(d.page.Lines)
		rows := a.viewRows()
		d.top = max(0, min(d.top, n-rows))
		so := a.scrolloff()
		if d.cur < d.top+so {
			d.cur = a.snapCursor(d, d.top+so, 1)
		}
		if d.cur > d.top+rows-1-so {
			d.cur = a.snapCursor(d, d.top+rows-1-so, -1)
		}
		top := d.top
		a.setCursor(d, d.cur)
		d.top = top
		return
	case term.MouseLeft:
		if m.Action != term.MousePress {
			return
		}
	default:
		return
	}
	li := d.top + (m.Y - r.Y - 1)
	if m.Y-r.Y < 1 || li < 0 || li >= len(d.page.Lines) {
		return
	}
	x := m.X - r.X
	line := d.page.Lines[li]
	if selectable(d.page, li) {
		top := d.top
		a.setCursor(d, li)
		d.top = top
		a.clampView(d)
	}
	if h, ok := line.HitAt(x); ok {
		switch h.Kind {
		case render.HitLink:
			d.focus = h.Link
			a.followLink(d, d.page.Links[h.Link])
		case render.HitFold:
			a.foldCmd(foldToggle)
		case render.HitFootnote:
			if l, ok := d.page.Footnotes[h.Footnote]; ok {
				a.pushHistory()
				d.top = l - a.viewRows()/3
				a.setCursor(d, a.snapCursor(d, l, 1))
			}
		}
	}
}

// copyWikilink copies [[Note]] for the current note.
func (a *app) copyWikilink() {
	d := a.needDoc()
	if d == nil || d.stdin {
		a.flash("no note open")
		return
	}
	name := strings.TrimSuffix(d.path, ".md")
	// The shortest unambiguous form: the basename when it resolves here.
	if base := path.Base(name); base != name {
		if rel, ok := a.v.ResolveName(base); ok && rel == d.path {
			name = base
		}
	}
	link := "[[" + name + "]]"
	if err := a.host.Copy(link); err != nil {
		a.flash("%s  (copy may not be supported here)", link)
		return
	}
	a.flash("copied %s", link)
}
