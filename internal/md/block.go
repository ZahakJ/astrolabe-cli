package md

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// The block parser follows the CommonMark two-phase strategy (as in the
// reference implementation commonmark.js): each line is matched against
// the open container blocks, then new block starts are tried, then the
// remainder is added to the innermost leaf. Extensions (tables, math, %%
// comments, footnotes, callouts, task items) are layered on top. The
// internal tree built here is converted into the public AST in convert.go.

type bkind uint8

const (
	bDocument bkind = iota
	bBlockQuote
	bList
	bItem
	bFootnote
	bParagraph
	bHeading
	bThematic
	bCode
	bHTML
	bMath
	bComment
	bTable
	bRefDef
)

// maxDepth caps container nesting (quotes, lists, footnotes) so that
// adversarial input cannot produce trees deep enough to hurt recursive
// consumers. Deeper markers are treated as text.
const maxDepth = 64

// seg is one line (or part of one) of block content.
type seg struct {
	text string
	off  int // byte offset of text[0] in the source
	line int // source line
}

type listData struct {
	ordered      bool
	marker       byte // bullet char or ordered delimiter
	start        int
	padding      int
	markerOffset int
	tight        bool
}

type bnode struct {
	kind     bkind
	parent   *bnode
	children []*bnode
	open     bool
	start    int // first line
	end      int // last line as closed by the parser (may include blanks)
	depth    int
	lines    []seg

	lastLineBlank   bool
	lastLineChecked bool

	list *listData // bList, bItem
	task *Task     // bItem

	// bCode
	fenced      bool
	fenceChar   byte
	fenceLen    int
	fenceOffset int
	fence       string
	info        string
	closed      bool // also bMath, bComment

	level  int // bHeading
	setext bool

	htmlType int // bHTML

	align []Align // bTable
	label string  // bFootnote
	ref   *LinkRefDef
	mark  byte // bThematic
}

func (n *bnode) lastChild() *bnode {
	if len(n.children) == 0 {
		return nil
	}
	return n.children[len(n.children)-1]
}

func acceptsLines(k bkind) bool {
	switch k {
	case bParagraph, bCode, bHTML, bMath, bComment, bTable:
		return true
	}
	return false
}

func canContain(parent, child bkind) bool {
	switch parent {
	case bDocument, bBlockQuote, bItem, bFootnote:
		return child != bItem
	case bList:
		return child == bItem
	}
	return false
}

type blockParser struct {
	src          string
	starts, ends []int

	doc, tip, oldtip, lastMatched *bnode
	allClosed                     bool

	line         string
	lineNo       int
	lineOff      int
	offset       int
	column       int
	nextNonspace int
	nextNSCol    int
	indent       int
	indented     bool
	blank        bool
	partialTab   bool
	lineConsumed bool

	refs      map[string]*LinkRefDef
	footnotes map[string]bool

	nextDollar  []int // per line: next later line containing "$$", -1 if none
	nextPercent []int // same for "%%"
}

// nextLineWith returns, for every line i, the smallest j > i such that line
// j contains pat, or -1.
func nextLineWith(src string, starts, ends []int, pat string) []int {
	out := make([]int, len(starts))
	next := -1
	for i := len(starts) - 1; i >= 0; i-- {
		out[i] = next
		if strings.Contains(src[starts[i]:ends[i]], pat) {
			next = i
		}
	}
	return out
}

func (p *blockParser) parse(firstLine int) *bnode {
	p.doc = &bnode{kind: bDocument, open: true, start: firstLine}
	p.tip = p.doc
	p.oldtip = p.doc
	p.lastMatched = p.doc
	p.allClosed = true
	if strings.Contains(p.src, "$$") {
		p.nextDollar = nextLineWith(p.src, p.starts, p.ends, "$$")
	}
	if strings.Contains(p.src, "%%") {
		p.nextPercent = nextLineWith(p.src, p.starts, p.ends, "%%")
	}
	for n := firstLine; n < len(p.starts); n++ {
		p.incorporateLine(n)
	}
	last := len(p.starts) - 1
	for p.tip != nil {
		p.finalize(p.tip, last)
	}
	return p.doc
}

func (p *blockParser) peek(i int) byte {
	if i >= 0 && i < len(p.line) {
		return p.line[i]
	}
	return 0
}

func isSpaceOrTab(c byte) bool { return c == ' ' || c == '\t' }

func (p *blockParser) findNextNonspace() {
	i := p.offset
	cols := p.column
	for i < len(p.line) {
		c := p.line[i]
		if c == ' ' {
			i++
			cols++
		} else if c == '\t' {
			i++
			cols += 4 - cols%4
		} else {
			break
		}
	}
	p.blank = i >= len(p.line)
	p.nextNonspace = i
	p.nextNSCol = cols
	p.indent = cols - p.column
	p.indented = p.indent >= 4
}

func (p *blockParser) advanceNextNonspace() {
	p.offset = p.nextNonspace
	p.column = p.nextNSCol
	p.partialTab = false
}

// advanceOffset moves forward count bytes, or count columns when columns
// is true (a tab may then be partially consumed).
func (p *blockParser) advanceOffset(count int, columns bool) {
	for count > 0 && p.offset < len(p.line) {
		c := p.line[p.offset]
		if c == '\t' {
			toTab := 4 - p.column%4
			if columns {
				p.partialTab = toTab > count
				adv := toTab
				if adv > count {
					adv = count
				}
				p.column += adv
				if !p.partialTab {
					p.offset++
				}
				count -= adv
			} else {
				p.partialTab = false
				p.column += toTab
				p.offset++
				count--
			}
		} else {
			p.partialTab = false
			p.offset++
			p.column++
			count--
		}
	}
}

func (p *blockParser) toEnd() {
	p.offset = len(p.line)
	p.partialTab = false
	p.lineConsumed = true
}

func (p *blockParser) incorporateLine(n int) {
	p.line = p.src[p.starts[n]:p.ends[n]]
	p.lineNo = n
	p.lineOff = p.starts[n]
	p.offset, p.column = 0, 0
	p.blank, p.partialTab, p.lineConsumed = false, false, false

	container := p.doc
	p.oldtip = p.tip

	// 1. Match open containers.
match:
	for {
		last := container.lastChild()
		if last == nil || !last.open {
			break
		}
		container = last
		p.findNextNonspace()
		switch p.continueBlock(container) {
		case 0:
		case 1:
			container = container.parent
			break match
		case 2:
			return
		}
	}
	p.allClosed = container == p.oldtip
	p.lastMatched = container

	// 2. New block starts.
	matchedLeaf := container.kind != bParagraph && acceptsLines(container.kind)
	for !matchedLeaf {
		p.findNextNonspace()
		if !p.indented && !maybeSpecial(p.peek(p.nextNonspace)) {
			p.advanceNextNonspace()
			break
		}
		res := 0
		for _, start := range blockStarts {
			if res = start(p, container); res != 0 {
				break
			}
		}
		if res == 0 {
			p.advanceNextNonspace()
			break
		}
		container = p.tip
		if res == 2 {
			matchedLeaf = true
		}
	}

	// 3. The rest of the line.
	if !p.allClosed && !p.blank && p.tip.kind == bParagraph {
		p.addLine() // lazy paragraph continuation
		return
	}
	p.closeUnmatched()
	if p.blank && len(container.children) > 0 {
		container.lastChild().lastLineBlank = true
	}
	t := container.kind
	llb := p.blank && !(t == bBlockQuote || t == bCode && container.fenced || t == bMath || t == bComment ||
		t == bItem && len(container.children) == 0 && container.start == n)
	for c := container; c != nil; c = c.parent {
		c.lastLineBlank = llb
	}
	switch {
	case acceptsLines(t):
		if !p.lineConsumed {
			p.addLine()
			if t == bHTML && container.htmlType >= 1 && container.htmlType <= 5 &&
				htmlBlockClose[container.htmlType].MatchString(p.line[min(p.offset, len(p.line)):]) {
				p.finalize(container, n)
			}
		}
	case p.offset < len(p.line) && !p.blank:
		p.addChild(bParagraph)
		p.advanceNextNonspace()
		p.addLine()
	}
}

func maybeSpecial(c byte) bool {
	switch c {
	case '#', '`', '~', '*', '+', '_', '=', '<', '>', '-', '$', '%', '[', '|', ':':
		return true
	}
	return c >= '0' && c <= '9'
}

func (p *blockParser) addLine() {
	off := min(p.offset, len(p.line))
	raw := p.line[off:]
	if p.tip.kind == bParagraph {
		t := strings.TrimLeft(raw, " \t")
		p.tip.lines = append(p.tip.lines, seg{text: t, off: p.lineOff + off + len(raw) - len(t), line: p.lineNo})
		return
	}
	text := raw
	if p.partialTab && off < len(p.line) {
		// Replace the partially consumed tab by the spaces it still spans.
		text = strings.Repeat(" ", 4-p.column%4) + p.line[off+1:]
	}
	p.tip.lines = append(p.tip.lines, seg{text: text, off: p.lineOff + off, line: p.lineNo})
}

func (p *blockParser) addChild(k bkind) *bnode {
	for !canContain(p.tip.kind, k) {
		p.finalize(p.tip, p.lineNo-1)
	}
	nb := &bnode{kind: k, parent: p.tip, open: true, start: p.lineNo, end: p.lineNo, depth: p.tip.depth + 1}
	p.tip.children = append(p.tip.children, nb)
	p.tip = nb
	return nb
}

func (p *blockParser) closeUnmatched() {
	if p.allClosed {
		return
	}
	for p.oldtip != p.lastMatched && p.oldtip != nil {
		parent := p.oldtip.parent
		p.finalize(p.oldtip, p.lineNo-1)
		p.oldtip = parent
	}
	p.allClosed = true
}

func (p *blockParser) finalize(b *bnode, line int) {
	above := b.parent
	b.open = false
	if line < b.start {
		line = b.start
	}
	b.end = line
	switch b.kind {
	case bParagraph:
		p.extractRefDefs(b)
	case bCode:
		if !b.fenced {
			b.lines = trimTrailingBlankSegs(b.lines)
		}
	case bHTML:
		b.lines = trimTrailingBlankSegs(b.lines)
	case bList:
		b.list.tight = listIsTight(b)
	}
	p.tip = above
}

func trimTrailingBlankSegs(l []seg) []seg {
	for len(l) > 0 && strings.TrimSpace(l[len(l)-1].text) == "" {
		l = l[:len(l)-1]
	}
	return l
}

func endsWithBlankLine(b *bnode) bool {
	for b != nil {
		if b.lastLineBlank {
			return true
		}
		if !b.lastLineChecked && (b.kind == bList || b.kind == bItem) {
			b.lastLineChecked = true
			b = b.lastChild()
		} else {
			b.lastLineChecked = true
			break
		}
	}
	return false
}

func listIsTight(list *bnode) bool {
	for i, item := range list.children {
		lastItem := i == len(list.children)-1
		if endsWithBlankLine(item) && !lastItem {
			return false
		}
		for j, sub := range item.children {
			if endsWithBlankLine(sub) && (!lastItem || j < len(item.children)-1) {
				return false
			}
		}
	}
	return true
}

// extractRefDefs removes leading link reference definitions from a
// finished paragraph and inserts them as bRefDef siblings before it. The
// paragraph is removed when nothing else remains.
func (p *blockParser) extractRefDefs(b *bnode) {
	defs, rest := p.splitRefDefs(b.lines)
	if len(defs) == 0 {
		return
	}
	parent := b.parent
	idx := -1
	for i, c := range parent.children {
		if c == b {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	var repl []*bnode
	for _, d := range defs {
		d.parent = parent
		d.depth = b.depth
		repl = append(repl, d)
	}
	if len(rest) > 0 {
		b.lines = rest
		b.start = rest[0].line
		repl = append(repl, b)
	}
	nc := make([]*bnode, 0, len(parent.children)-1+len(repl))
	nc = append(nc, parent.children[:idx]...)
	nc = append(nc, repl...)
	nc = append(nc, parent.children[idx+1:]...)
	parent.children = nc
}

// splitRefDefs parses reference definitions at the start of the given
// paragraph lines, registering them in p.refs.
func (p *blockParser) splitRefDefs(lines []seg) (defs []*bnode, rest []seg) {
	if len(lines) == 0 || !strings.HasPrefix(lines[0].text, "[") {
		return nil, lines
	}
	rest = lines
	for len(rest) > 0 && strings.HasPrefix(rest[0].text, "[") {
		// Join a bounded window of lines: a definition spans few lines.
		k := len(rest)
		if k > 8 {
			k = 8
		}
		var sb strings.Builder
		for i := 0; i < k; i++ {
			if i > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(rest[i].text)
		}
		s := sb.String()
		label, dest, title, consumed, ok := parseRefDef(s)
		if !ok {
			break
		}
		nlines := strings.Count(s[:consumed], "\n")
		if consumed >= len(s) {
			nlines = k
		}
		if nlines == 0 {
			nlines = 1
		}
		used := rest[:nlines]
		rest = rest[nlines:]
		def := &LinkRefDef{Label: label, Dest: dest, Title: title}
		key := normalizeLabel(label)
		if _, dup := p.refs[key]; !dup {
			p.refs[key] = def
		}
		node := &bnode{kind: bRefDef, start: used[0].line, end: used[len(used)-1].line, lines: used, ref: def}
		defs = append(defs, node)
	}
	return defs, rest
}

// continueBlock reports whether an open block continues on the current
// line: 0 yes, 1 no, 2 the line was fully consumed (closing fence).
func (p *blockParser) continueBlock(c *bnode) int {
	switch c.kind {
	case bList:
		return 0
	case bBlockQuote:
		if !p.indented && p.peek(p.nextNonspace) == '>' {
			p.advanceNextNonspace()
			p.advanceOffset(1, false)
			if isSpaceOrTab(p.peek(p.offset)) {
				p.advanceOffset(1, true)
			}
			return 0
		}
		return 1
	case bItem:
		if p.blank {
			if len(c.children) == 0 {
				return 1
			}
			p.advanceNextNonspace()
			return 0
		}
		if need := c.list.markerOffset + c.list.padding; p.indent >= need {
			p.advanceOffset(need, true)
			return 0
		}
		return 1
	case bFootnote:
		if p.blank {
			if len(c.children) == 0 {
				return 1
			}
			p.advanceNextNonspace()
			return 0
		}
		if p.indent >= 4 {
			p.advanceOffset(4, true)
			return 0
		}
		return 1
	case bHeading, bThematic, bRefDef:
		return 1
	case bCode:
		if c.fenced {
			rest := p.line[p.nextNonspace:]
			if p.indent <= 3 && p.peek(p.nextNonspace) == c.fenceChar {
				if n := closingFenceLen(rest, c.fenceChar); n >= c.fenceLen {
					c.closed = true
					p.finalize(c, p.lineNo)
					return 2
				}
			}
			for i := c.fenceOffset; i > 0 && isSpaceOrTab(p.peek(p.offset)); i-- {
				p.advanceOffset(1, true)
			}
			return 0
		}
		if p.indent >= 4 {
			p.advanceOffset(4, true)
			return 0
		}
		if p.blank {
			p.advanceNextNonspace()
			return 0
		}
		return 1
	case bHTML:
		if p.blank && (c.htmlType == 6 || c.htmlType == 7) {
			return 1
		}
		return 0
	case bParagraph:
		if p.blank {
			return 1
		}
		return 0
	case bTable:
		if p.blank {
			return 1
		}
		rest := p.line[p.nextNonspace:]
		if !strings.Contains(rest, "|") || !p.indented && startsOtherBlock(rest) {
			return 1
		}
		return 0
	case bMath, bComment:
		delim := "$$"
		if c.kind == bComment {
			delim = "%%"
		}
		rest := p.line[min(p.offset, len(p.line)):]
		if i := strings.Index(rest, delim); i >= 0 {
			if c.kind == bComment {
				c.lines = append(c.lines, seg{text: rest, off: p.lineOff + p.offset, line: p.lineNo})
			} else if strings.TrimSpace(rest[:i]) != "" {
				c.lines = append(c.lines, seg{text: rest[:i], off: p.lineOff + p.offset, line: p.lineNo})
			}
			c.closed = true
			p.finalize(c, p.lineNo)
			return 2
		}
		return 0
	}
	return 0
}

// closingFenceLen returns the length of a closing fence of char c at the
// start of s (only blanks may follow), or 0.
func closingFenceLen(s string, c byte) int {
	n := 0
	for n < len(s) && s[n] == c {
		n++
	}
	if n < 3 || strings.TrimLeft(s[n:], " \t") != "" {
		return 0
	}
	return n
}

// startsOtherBlock reports whether s (at the first non-blank) begins a
// block that interrupts a table.
func startsOtherBlock(s string) bool {
	if s == "" {
		return false
	}
	switch s[0] {
	case '>':
		return true
	case '#':
		_, ok := atxMarker(s)
		return ok
	case '`', '~':
		return fenceRun(s) >= 3
	case '-', '*', '+':
		if isThematicBreak(s) {
			return true
		}
		return len(s) >= 2 && isSpaceOrTab(s[1])
	}
	return false
}

// ---------------------------------------------------------------------------
// Block starts. Each returns 0 (no match), 1 (container started; keep
// looking for starts in the rest of the line) or 2 (leaf started).

var blockStarts = []func(*blockParser, *bnode) int{
	startBlockQuote,
	startATX,
	startFence,
	startMath,
	startPercentComment,
	startHTML,
	startTable,
	startSetext,
	startThematic,
	startFootnote,
	startListItem,
	startIndentedCode,
}

func startBlockQuote(p *blockParser, c *bnode) int {
	if p.indented || p.peek(p.nextNonspace) != '>' || c.depth >= maxDepth {
		return 0
	}
	p.advanceNextNonspace()
	p.advanceOffset(1, false)
	if isSpaceOrTab(p.peek(p.offset)) {
		p.advanceOffset(1, true)
	}
	p.closeUnmatched()
	p.addChild(bBlockQuote)
	return 1
}

// atxMarker returns the level of an ATX heading marker at the start of s.
func atxMarker(s string) (int, bool) {
	n := 0
	for n < len(s) && s[n] == '#' {
		n++
	}
	if n == 0 || n > 6 {
		return 0, false
	}
	if n < len(s) && !isSpaceOrTab(s[n]) {
		return 0, false
	}
	return n, true
}

func startATX(p *blockParser, _ *bnode) int {
	if p.indented {
		return 0
	}
	rest := p.line[p.nextNonspace:]
	level, ok := atxMarker(rest)
	if !ok {
		return 0
	}
	p.closeUnmatched()
	h := p.addChild(bHeading)
	h.level = level
	contentStart := p.nextNonspace + level
	content := p.line[contentStart:]
	trimmed := strings.TrimLeft(content, " \t")
	contentStart += len(content) - len(trimmed)
	content = stripClosingHashes(trimmed)
	h.lines = []seg{{text: content, off: p.lineOff + contentStart, line: p.lineNo}}
	p.toEnd()
	return 2
}

// stripClosingHashes removes an optional closing sequence of '#'
// (preceded by a blank, or making up the whole content) and trailing
// blanks.
func stripClosingHashes(s string) string {
	s = strings.TrimRight(s, " \t")
	i := len(s)
	for i > 0 && s[i-1] == '#' {
		i--
	}
	if i == len(s) {
		return s
	}
	if i == 0 {
		return ""
	}
	if isSpaceOrTab(s[i-1]) {
		return strings.TrimRight(s[:i], " \t")
	}
	return s
}

func fenceRun(s string) int {
	if s == "" || s[0] != '`' && s[0] != '~' {
		return 0
	}
	n := 0
	for n < len(s) && s[n] == s[0] {
		n++
	}
	return n
}

func startFence(p *blockParser, _ *bnode) int {
	if p.indented {
		return 0
	}
	rest := p.line[p.nextNonspace:]
	n := fenceRun(rest)
	if n < 3 {
		return 0
	}
	info := strings.TrimSpace(rest[n:])
	if rest[0] == '`' && strings.IndexByte(info, '`') >= 0 {
		return 0
	}
	p.closeUnmatched()
	b := p.addChild(bCode)
	b.fenced = true
	b.fenceChar = rest[0]
	b.fenceLen = n
	b.fence = rest[:n]
	b.fenceOffset = p.indent
	b.info = unescapeString(info)
	p.toEnd()
	return 2
}

func startMath(p *blockParser, _ *bnode) int {
	if p.indented || p.nextDollar == nil {
		return 0
	}
	rest := p.line[p.nextNonspace:]
	if !strings.HasPrefix(rest, "$$") {
		return 0
	}
	after := rest[2:]
	afterOff := p.lineOff + p.nextNonspace + 2
	if i := strings.Index(after, "$$"); i >= 0 {
		if strings.TrimSpace(after[i+2:]) != "" {
			return 0 // "$$x$$ and more": inline display math in a paragraph
		}
		p.closeUnmatched()
		b := p.addChild(bMath)
		if strings.TrimSpace(after[:i]) != "" {
			b.lines = []seg{{text: after[:i], off: afterOff, line: p.lineNo}}
		}
		b.closed = true
		p.finalize(b, p.lineNo)
		p.toEnd()
		return 2
	}
	if p.nextDollar[p.lineNo] < 0 {
		return 0 // never closed: not math
	}
	p.closeUnmatched()
	b := p.addChild(bMath)
	if strings.TrimSpace(after) != "" {
		b.lines = []seg{{text: after, off: afterOff, line: p.lineNo}}
	}
	p.toEnd()
	return 2
}

func startPercentComment(p *blockParser, _ *bnode) int {
	if p.indented || p.nextPercent == nil {
		return 0
	}
	rest := p.line[p.nextNonspace:]
	if !strings.HasPrefix(rest, "%%") {
		return 0
	}
	raw := seg{text: rest, off: p.lineOff + p.nextNonspace, line: p.lineNo}
	if i := strings.Index(rest[2:], "%%"); i >= 0 {
		if strings.TrimSpace(rest[2+i+2:]) != "" {
			return 0 // inline comment followed by text
		}
		p.closeUnmatched()
		b := p.addChild(bComment)
		b.lines = []seg{raw}
		b.closed = true
		p.finalize(b, p.lineNo)
		p.toEnd()
		return 2
	}
	if p.nextPercent[p.lineNo] < 0 {
		return 0
	}
	p.closeUnmatched()
	b := p.addChild(bComment)
	b.lines = []seg{raw}
	p.toEnd()
	return 2
}

const (
	reTagName  = `[A-Za-z][A-Za-z0-9-]*`
	reAttrName = `[a-zA-Z_:][a-zA-Z0-9_.:-]*`
	reAttrVal  = `(?:[^"'=<>` + "`" + `\x00-\x20]+|'[^']*'|"[^"]*")`
	reOpenTag  = `<` + reTagName + `(?:\s+` + reAttrName + `(?:\s*=\s*` + reAttrVal + `)?)*\s*/?>`
	reCloseTag = `</` + reTagName + `\s*>`
)

var htmlBlockOpen = [8]*regexp.Regexp{
	nil,
	regexp.MustCompile(`(?i)^<(?:script|pre|style|textarea)(?:\s|>|$)`),
	regexp.MustCompile(`^<!--`),
	regexp.MustCompile(`^<[?]`),
	regexp.MustCompile(`^<![A-Za-z]`),
	regexp.MustCompile(`^<!\[CDATA\[`),
	regexp.MustCompile(`(?i)^</?(?:address|article|aside|base|basefont|blockquote|body|caption|center|col|colgroup|dd|details|dialog|dir|div|dl|dt|fieldset|figcaption|figure|footer|form|frame|frameset|h[1-6]|head|header|hr|html|iframe|legend|li|link|main|menu|menuitem|nav|noframes|ol|optgroup|option|p|param|search|section|summary|table|tbody|td|tfoot|th|thead|title|tr|track|ul)(?:\s|/?>|$)`),
	regexp.MustCompile(`^(?:` + reOpenTag + `|` + reCloseTag + `)\s*$`),
}

var htmlBlockClose = [6]*regexp.Regexp{
	nil,
	regexp.MustCompile(`(?i)</(?:script|pre|style|textarea)>`),
	regexp.MustCompile(`-->`),
	regexp.MustCompile(`\?>`),
	regexp.MustCompile(`>`),
	regexp.MustCompile(`\]\]>`),
}

func startHTML(p *blockParser, c *bnode) int {
	if p.indented || p.peek(p.nextNonspace) != '<' {
		return 0
	}
	s := p.line[p.nextNonspace:]
	for t := 1; t <= 7; t++ {
		if !htmlBlockOpen[t].MatchString(s) {
			continue
		}
		if t == 7 && (c.kind == bParagraph || !p.allClosed && !p.blank && p.tip.kind == bParagraph) {
			return 0
		}
		p.closeUnmatched()
		b := p.addChild(bHTML)
		b.htmlType = t
		return 2
	}
	return 0
}

func startTable(p *blockParser, c *bnode) int {
	if p.indented || c.kind != bParagraph || len(c.lines) == 0 {
		return 0
	}
	rest := p.line[p.nextNonspace:]
	align, ok := parseDelimRow(rest)
	if !ok {
		return 0
	}
	header := c.lines[len(c.lines)-1]
	cells, pipe := splitRow(header.text)
	if !pipe || len(cells) != len(align) {
		return 0
	}
	p.closeUnmatched()
	parent := c.parent
	if len(c.lines) > 1 {
		c.lines = c.lines[:len(c.lines)-1]
		p.finalize(c, c.lines[len(c.lines)-1].line)
	} else {
		parent.children = parent.children[:len(parent.children)-1]
		p.tip = parent
	}
	// finalize may have extracted reference definitions; the table goes
	// after whatever remains.
	t := p.addChild(bTable)
	t.start = header.line
	t.align = align
	t.lines = []seg{header}
	p.toEnd()
	return 2
}

func startSetext(p *blockParser, c *bnode) int {
	if p.indented || c.kind != bParagraph {
		return 0
	}
	rest := strings.TrimRight(p.line[p.nextNonspace:], " \t")
	if rest == "" || rest[0] != '=' && rest[0] != '-' || strings.Trim(rest, rest[:1]) != "" {
		return 0
	}
	p.closeUnmatched()
	defs, lines := p.splitRefDefs(c.lines)
	if len(lines) == 0 {
		return 0
	}
	if len(defs) > 0 {
		parent := c.parent
		parent.children = parent.children[:len(parent.children)-1]
		for _, d := range defs {
			d.parent = parent
			d.depth = c.depth
			parent.children = append(parent.children, d)
		}
		parent.children = append(parent.children, c)
	}
	c.kind = bHeading
	c.setext = true
	c.level = 1
	if rest[0] == '-' {
		c.level = 2
	}
	c.lines = lines
	c.start = lines[0].line
	p.tip = c
	p.toEnd()
	return 2
}

// isThematicBreak reports whether s (at the first non-blank) is ***, ---
// or ___ with optional blanks between.
func isThematicBreak(s string) bool {
	if s == "" {
		return false
	}
	ch := s[0]
	if ch != '*' && ch != '-' && ch != '_' {
		return false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ch:
			n++
		case ' ', '\t':
		default:
			return false
		}
	}
	return n >= 3
}

func startThematic(p *blockParser, _ *bnode) int {
	if p.indented {
		return 0
	}
	rest := p.line[p.nextNonspace:]
	if !isThematicBreak(rest) {
		return 0
	}
	p.closeUnmatched()
	b := p.addChild(bThematic)
	b.mark = rest[0]
	p.toEnd()
	return 2
}

func startFootnote(p *blockParser, c *bnode) int {
	if p.indented || c.depth >= maxDepth {
		return 0
	}
	rest := p.line[p.nextNonspace:]
	if !strings.HasPrefix(rest, "[^") {
		return 0
	}
	end := 2
	for end < len(rest) && rest[end] != ']' && rest[end] != '[' && rest[end] != ' ' && rest[end] != '\t' {
		end++
	}
	if end == 2 || end+1 >= len(rest) || rest[end] != ']' || rest[end+1] != ':' {
		return 0
	}
	label := rest[2:end]
	p.closeUnmatched()
	b := p.addChild(bFootnote)
	b.label = label
	p.footnotes[normalizeLabel(label)] = true
	p.advanceNextNonspace()
	p.advanceOffset(end+2, false)
	p.findNextNonspace()
	p.advanceNextNonspace()
	return 1
}

func startListItem(p *blockParser, c *bnode) int {
	if p.indented && c.kind != bList || c.depth >= maxDepth {
		return 0
	}
	if p.indent >= 4 {
		return 0
	}
	rest := p.line[p.nextNonspace:]
	if rest == "" {
		return 0
	}
	d := &listData{markerOffset: p.indent, tight: true}
	mlen := 0
	switch rest[0] {
	case '-', '*', '+':
		d.marker = rest[0]
		mlen = 1
	default:
		i := 0
		num := 0
		for i < len(rest) && i < 9 && rest[i] >= '0' && rest[i] <= '9' {
			num = num*10 + int(rest[i]-'0')
			i++
		}
		if i == 0 || i >= len(rest) || rest[i] != '.' && rest[i] != ')' {
			return 0
		}
		if c.kind == bParagraph && num != 1 {
			return 0
		}
		d.ordered = true
		d.start = num
		d.marker = rest[i]
		mlen = i + 1
	}
	if mlen < len(rest) && !isSpaceOrTab(rest[mlen]) {
		return 0
	}
	if c.kind == bParagraph && strings.TrimSpace(rest[mlen:]) == "" {
		return 0
	}
	p.advanceNextNonspace()
	p.advanceOffset(mlen, true)
	spStartCol, spStartOff := p.column, p.offset
	for {
		p.advanceOffset(1, true)
		if !(p.column-spStartCol < 5 && isSpaceOrTab(p.peek(p.offset))) {
			break
		}
	}
	blankItem := p.offset >= len(p.line)
	spaces := p.column - spStartCol
	if spaces >= 5 || spaces < 1 || blankItem {
		d.padding = mlen + 1
		p.column, p.offset = spStartCol, spStartOff
		p.partialTab = false
		if isSpaceOrTab(p.peek(p.offset)) {
			p.advanceOffset(1, true)
		}
	} else {
		d.padding = mlen + spaces
	}
	p.closeUnmatched()
	if p.tip.kind != bList || !listsMatch(p.tip.list, d) {
		l := p.addChild(bList)
		l.list = d
	}
	item := p.addChild(bItem)
	ld := *d
	item.list = &ld

	// Task checkbox: "[c]" followed by a blank or the end of the line.
	if !blankItem && spaces < 5 && p.peek(p.offset) == '[' {
		r, w := utf8.DecodeRuneInString(p.line[p.offset+1:])
		if w > 0 && r != ']' && r != '[' && r != '\t' && r != utf8.RuneError {
			cl := p.offset + 1 + w
			if p.peek(cl) == ']' && (cl+1 >= len(p.line) || isSpaceOrTab(p.line[cl+1])) {
				item.task = &Task{
					State:  r,
					Line:   p.lineNo,
					Col:    p.offset + 1,
					Offset: p.lineOff + p.offset + 1,
					Width:  w,
				}
				p.offset = cl + 1
				p.column += 2 + w
				p.partialTab = false
				for p.offset < len(p.line) && isSpaceOrTab(p.line[p.offset]) {
					p.advanceOffset(1, false)
				}
			}
		}
	}
	return 1
}

func listsMatch(a, b *listData) bool {
	return a != nil && b != nil && a.ordered == b.ordered && a.marker == b.marker
}

func startIndentedCode(p *blockParser, _ *bnode) int {
	if !p.indented || p.tip.kind == bParagraph || p.blank {
		return 0
	}
	p.advanceOffset(4, true)
	p.closeUnmatched()
	p.addChild(bCode)
	return 2
}
