package md

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The inline parser follows the CommonMark algorithm: a single left-to-right
// scan builds a flat list of nodes, emphasis-like delimiter runs go on a
// delimiter stack and brackets on a bracket stack, and "process emphasis"
// pairs them up. Extensions (wikilinks, tags, math, comments, highlight,
// strikethrough, bare URLs, footnote references) are recognised during the
// scan. Every search for a closing marker is cached so that the scan stays
// linear on adversarial input.

// inlineCtx carries document-wide state needed while parsing inlines.
type inlineCtx struct {
	refs      map[string]*LinkRefDef
	footnotes map[string]bool // defined labels (normalised)
	fnIndex   map[string]int  // assigned footnote numbers
	fnNext    int
	table     bool // parsing a table cell: "\|" is a literal pipe everywhere
	slab      []inode
}

// newNode returns a zeroed inode from a slab, cutting per-node
// allocations; the slab lives only as long as one Parse call.
func (c *inlineCtx) newNode() *inode {
	if len(c.slab) == 0 {
		c.slab = make([]inode, 256)
	}
	n := &c.slab[0]
	c.slab = c.slab[1:]
	return n
}

func (c *inlineCtx) footnoteIndex(label string) int {
	key := normalizeLabel(label)
	if !c.footnotes[key] {
		return 0
	}
	if i, ok := c.fnIndex[key]; ok {
		return i
	}
	c.fnNext++
	c.fnIndex[key] = c.fnNext
	return c.fnNext
}

type ikind uint8

const (
	iText ikind = iota
	iLeaf
	iEmph
	iStrong
	iStrike
	iHigh
	iLink
	iImage
)

type inode struct {
	kind               ikind
	start, end         int // buffer offsets
	text               string
	leaf               Inline
	link               *Link
	img                *Image
	parent, prev, next *inode
	first, last        *inode
}

func (n *inode) appendChild(c *inode) {
	c.parent = n
	c.prev = n.last
	c.next = nil
	if n.last != nil {
		n.last.next = c
	} else {
		n.first = c
	}
	n.last = c
}

func (c *inode) unlink() {
	if c.prev != nil {
		c.prev.next = c.next
	} else if c.parent != nil {
		c.parent.first = c.next
	}
	if c.next != nil {
		c.next.prev = c.prev
	} else if c.parent != nil {
		c.parent.last = c.prev
	}
	c.parent, c.prev, c.next = nil, nil, nil
}

func (n *inode) insertAfter(c *inode) {
	c.unlink()
	c.parent = n.parent
	c.prev = n
	c.next = n.next
	if n.next != nil {
		n.next.prev = c
	} else if n.parent != nil {
		n.parent.last = c
	}
	n.next = c
}

type delim struct {
	node              *inode
	ch                byte
	num, origNum      int
	canOpen, canClose bool
	prev, next        *delim
}

type bracket struct {
	node         *inode
	prev         *bracket
	prevDelim    *delim
	index        int // buffer offset just after '[' (content start)
	image        bool
	active       bool
	bracketAfter bool
}

// findCache memoises "first occurrence of a fixed pattern at or after p".
// If the last search from f found a (or nothing, a == -1), any later
// search starting in [f, a] has the same answer.
type findCache struct {
	from, at int
	valid    bool
}

func (c *findCache) find(s string, p int, pat string) int {
	if c.valid && p >= c.from && (c.at < 0 || c.at >= p) {
		return c.at
	}
	at := -1
	if p <= len(s) {
		if i := strings.Index(s[p:], pat); i >= 0 {
			at = p + i
		}
	}
	c.from, c.at, c.valid = p, at, true
	return at
}

type inlineParser struct {
	s        string
	pos      int
	root     *inode
	delims   *delim
	brackets *bracket
	ctx      *inlineCtx

	noTicks   map[int]int // backtick run length -> position from which no run exists
	wikiClose findCache
	pctClose  findCache
	htmlClose findCache
	dmClose   findCache // "$$"
	mathFrom  int       // single-dollar closer cache
	mathAt    int
	mathValid bool

	segStarts []int
	segs      []seg
}

// parseInlines parses the given content lines into inline nodes whose
// spans refer to the original source.
func parseInlines(segs []seg, ctx *inlineCtx) []Inline {
	if len(segs) == 0 {
		return nil
	}
	starts := make([]int, len(segs))
	var s string
	if len(segs) == 1 {
		s = segs[0].text
	} else {
		n := len(segs) - 1
		for _, sg := range segs {
			n += len(sg.text)
		}
		var b strings.Builder
		b.Grow(n)
		for i, sg := range segs {
			if i > 0 {
				b.WriteByte('\n')
			}
			starts[i] = b.Len()
			b.WriteString(sg.text)
		}
		s = b.String()
	}
	s = strings.TrimRight(s, " \t\n")
	if s == "" {
		return nil
	}
	p := &inlineParser{s: s, root: &inode{}, ctx: ctx, segStarts: starts, segs: segs}
	for p.pos < len(p.s) {
		p.parseOne()
	}
	p.processEmphasis(nil)
	return p.build(p.root.first, false)
}

// mapPos converts a buffer offset to a source offset.
func (p *inlineParser) mapPos(pos int) int {
	i := sort.Search(len(p.segStarts), func(i int) bool { return p.segStarts[i] > pos }) - 1
	if i < 0 {
		i = 0
	}
	d := pos - p.segStarts[i]
	if d > len(p.segs[i].text) {
		d = len(p.segs[i].text)
	}
	if d < 0 {
		d = 0
	}
	return p.segs[i].off + d
}

func (p *inlineParser) span(a, b int) Span { return Span{p.mapPos(a), p.mapPos(b)} }

func (p *inlineParser) addText(text string, start, end int) *inode {
	n := p.ctx.newNode()
	n.kind, n.text, n.start, n.end = iText, text, start, end
	p.root.appendChild(n)
	return n
}

func (p *inlineParser) addLeaf(x Inline, start, end int) {
	n := p.ctx.newNode()
	n.kind, n.leaf, n.start, n.end = iLeaf, x, start, end
	p.root.appendChild(n)
}

var specialByte = func() (t [256]bool) {
	for _, c := range []byte("\n\\`*_~=[!]<&$%#") {
		t[c] = true
	}
	return
}()

func (p *inlineParser) parseOne() {
	s := p.s
	c := s[p.pos]
	switch c {
	case '\n':
		p.newline()
	case '\\':
		p.backslash()
	case '`':
		p.backticks()
	case '*', '_':
		p.delimRun(c)
	case '~', '=':
		p.delimRun(c)
	case '[':
		p.openBracket()
	case '!':
		p.bang()
	case ']':
		p.closeBracket()
	case '<':
		p.lessThan()
	case '&':
		if r, n := decodeEntity(s[p.pos:]); n > 0 {
			p.addText(r, p.pos, p.pos+n)
			p.pos += n
		} else {
			p.addText("&", p.pos, p.pos+1)
			p.pos++
		}
	case '$':
		p.dollar()
	case '%':
		p.percent()
	case '#':
		p.hash()
	default:
		p.text()
	}
}

func isURLBoundary(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '*', '_', '~', '(', '"', '\'':
		return true
	}
	return false
}

func (p *inlineParser) text() {
	s := p.s
	if p.pos == 0 || isURLBoundary(s[p.pos-1]) {
		if p.bareURL() {
			return
		}
	}
	start := p.pos
	i := p.pos + 1
	for i < len(s) {
		c := s[i]
		if specialByte[c] {
			break
		}
		if (c == 'h' || c == 'w' || c == 'H' || c == 'W') && isURLBoundary(s[i-1]) && bareURLEnd(s, i) > i {
			break
		}
		i++
	}
	p.addText(s[start:i], start, i)
	p.pos = i
}

// bareURLEnd returns the end of a GFM extended autolink (http://, https://
// or www.) starting at i, or i when there is none.
func bareURLEnd(s string, i int) int {
	rest := s[i:]
	var n int
	switch {
	case hasPrefixFold(rest, "https://"):
		n = 8
	case hasPrefixFold(rest, "http://"):
		n = 7
	case hasPrefixFold(rest, "www."):
		n = 4
	default:
		return i
	}
	// The host must start with an alphanumeric or a non-ASCII character.
	if n >= len(rest) {
		return i
	}
	if c := rest[n]; !(isAlnum(c) || c >= 0x80) {
		return i
	}
	j := n
	for j < len(rest) {
		c := rest[j]
		if c == ' ' || c == '\t' || c == '\n' || c == '<' || c < 0x20 {
			break
		}
		if c >= 0x80 {
			r, w := utf8.DecodeRuneInString(rest[j:])
			if unicode.IsSpace(r) {
				break
			}
			j += w
			continue
		}
		j++
	}
	u := rest[:j]
	// Trim trailing punctuation, unbalanced ')' and entity-like suffixes.
	for len(u) > n {
		last := u[len(u)-1]
		switch {
		case strings.IndexByte("?!.,:*_~'\";]", last) >= 0:
			if last == ';' {
				k := len(u) - 2
				for k >= 0 && isAlnum(u[k]) {
					k--
				}
				if k >= 0 && u[k] == '&' && k < len(u)-2 {
					u = u[:k]
					continue
				}
			}
			u = u[:len(u)-1]
			continue
		case last == ')':
			if strings.Count(u, "(") < strings.Count(u, ")") {
				u = u[:len(u)-1]
				continue
			}
		}
		break
	}
	if len(u) <= n {
		return i
	}
	return i + len(u)
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

func (p *inlineParser) bareURL() bool {
	end := bareURLEnd(p.s, p.pos)
	if end <= p.pos {
		return false
	}
	u := p.s[p.pos:end]
	dest := u
	if hasPrefixFold(u, "www.") {
		dest = "http://" + u
	}
	l := &Link{Kind: LinkBare, Dest: dest, External: true}
	n := &inode{kind: iLink, link: l, start: p.pos, end: end}
	n.appendChild(&inode{kind: iText, text: u, start: p.pos, end: end})
	p.root.appendChild(n)
	p.pos = end
	return true
}

func (p *inlineParser) newline() {
	nl := p.pos
	p.pos++
	breakStart := nl
	hard := false
	if last := p.root.last; last != nil && last.kind == iText && strings.HasSuffix(last.text, " ") {
		trimmed := strings.TrimRight(last.text, " ")
		k := len(last.text) - len(trimmed)
		hard = k >= 2
		last.text = trimmed
		last.end -= k
		breakStart = last.end
	}
	for p.pos < len(p.s) && isSpaceOrTab(p.s[p.pos]) {
		p.pos++
	}
	if hard {
		p.addLeaf(&HardBreak{}, breakStart, nl+1)
	} else {
		p.addLeaf(&SoftBreak{}, breakStart, nl+1)
	}
}

func (p *inlineParser) backslash() {
	s := p.s
	if p.pos+1 < len(s) {
		c := s[p.pos+1]
		if c == '\n' {
			p.addLeaf(&HardBreak{}, p.pos, p.pos+2)
			p.pos += 2
			for p.pos < len(s) && isSpaceOrTab(s[p.pos]) {
				p.pos++
			}
			return
		}
		if isASCIIPunct(c) {
			p.addText(s[p.pos+1:p.pos+2], p.pos, p.pos+2)
			p.pos += 2
			return
		}
	}
	p.addText("\\", p.pos, p.pos+1)
	p.pos++
}

func (p *inlineParser) backticks() {
	s := p.s
	start := p.pos
	n := 0
	for start+n < len(s) && s[start+n] == '`' {
		n++
	}
	after := start + n
	if p.noTicks == nil {
		p.noTicks = map[int]int{}
	}
	if from, ok := p.noTicks[n]; !ok || after < from {
		if q := findBacktickRun(s, after, n); q >= 0 {
			v := s[after:q]
			v = strings.ReplaceAll(v, "\n", " ")
			if len(v) >= 2 && v[0] == ' ' && v[len(v)-1] == ' ' && strings.Trim(v, " ") != "" {
				v = v[1 : len(v)-1]
			}
			if p.ctx.table {
				v = strings.ReplaceAll(v, `\|`, "|")
			}
			p.addLeaf(&Code{Value: v}, start, q+n)
			p.pos = q + n
			return
		}
		p.noTicks[n] = after
	}
	p.addText(s[start:after], start, after)
	p.pos = after
}

func isWhite(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || unicode.IsSpace(r) }
func isPunctRune(r rune) bool {
	if r < 0x80 {
		return isASCIIPunct(byte(r))
	}
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}

func (p *inlineParser) delimRun(c byte) {
	s := p.s
	start := p.pos
	n := 0
	for start+n < len(s) && s[start+n] == c {
		n++
	}
	p.pos = start + n
	node := p.addText(s[start:p.pos], start, p.pos)
	if (c == '~' || c == '=') && n != 2 {
		return
	}
	before, after := '\n', '\n'
	if start > 0 {
		before, _ = utf8.DecodeLastRuneInString(s[:start])
	}
	if p.pos < len(s) {
		after, _ = utf8.DecodeRuneInString(s[p.pos:])
	}
	afterWS, beforeWS := isWhite(after), isWhite(before)
	afterP, beforeP := isPunctRune(after), isPunctRune(before)
	left := !afterWS && (!afterP || beforeWS || beforeP)
	right := !beforeWS && (!beforeP || afterWS || afterP)
	var canOpen, canClose bool
	if c == '_' {
		canOpen = left && (!right || beforeP)
		canClose = right && (!left || afterP)
	} else {
		canOpen, canClose = left, right
	}
	if !canOpen && !canClose {
		return
	}
	d := &delim{node: node, ch: c, num: n, origNum: n, canOpen: canOpen, canClose: canClose, prev: p.delims}
	if p.delims != nil {
		p.delims.next = d
	}
	p.delims = d
}

func (p *inlineParser) removeDelim(d *delim) {
	if d.prev != nil {
		d.prev.next = d.next
	}
	if d.next != nil {
		d.next.prev = d.prev
	} else {
		p.delims = d.prev
	}
}

type bottomKey struct {
	ch      byte
	canOpen bool
	mod     int
}

func (p *inlineParser) processEmphasis(stackBottom *delim) {
	var bottoms map[bottomKey]*delim
	var closer *delim
	for d := p.delims; d != nil && d != stackBottom; d = d.prev {
		closer = d
	}
	for closer != nil {
		if !closer.canClose {
			closer = closer.next
			continue
		}
		key := bottomKey{closer.ch, closer.canOpen, closer.origNum % 3}
		bottom, hasBottom := bottoms[key]
		if !hasBottom {
			bottom = stackBottom
		}
		opener := closer.prev
		found := false
		for opener != nil && opener != stackBottom && opener != bottom {
			if opener.ch == closer.ch && opener.canOpen {
				if closer.ch == '*' || closer.ch == '_' {
					odd := (closer.canOpen || opener.canClose) && closer.origNum%3 != 0 &&
						(opener.origNum+closer.origNum)%3 == 0
					if !odd {
						found = true
						break
					}
				} else if opener.num == 2 && closer.num == 2 {
					found = true
					break
				}
			}
			opener = opener.prev
		}
		if !found {
			if bottoms == nil {
				bottoms = map[bottomKey]*delim{}
			}
			bottoms[key] = closer.prev
			next := closer.next
			if !closer.canOpen {
				p.removeDelim(closer)
			}
			closer = next
			continue
		}
		use := 2
		kind := iStrong
		switch closer.ch {
		case '~':
			kind = iStrike
		case '=':
			kind = iHigh
		default:
			if closer.num < 2 || opener.num < 2 {
				use = 1
				kind = iEmph
			}
		}
		on, cn := opener.node, closer.node
		opener.num -= use
		closer.num -= use
		on.text = on.text[:len(on.text)-use]
		on.end -= use
		cn.text = cn.text[use:]
		cn.start += use
		em := &inode{kind: kind, start: on.end, end: cn.start}
		for t := on.next; t != nil && t != cn; {
			nx := t.next
			t.unlink()
			em.appendChild(t)
			t = nx
		}
		on.insertAfter(em)
		// Remove delimiters between opener and closer.
		opener.next = closer
		closer.prev = opener
		if opener.num == 0 {
			on.unlink()
			p.removeDelim(opener)
		}
		if closer.num == 0 {
			next := closer.next
			cn.unlink()
			p.removeDelim(closer)
			closer = next
		}
	}
	for p.delims != nil && p.delims != stackBottom {
		p.removeDelim(p.delims)
	}
}

func (p *inlineParser) openBracket() {
	s := p.s
	if p.pos+1 < len(s) && s[p.pos+1] == '[' && p.wikilink(p.pos, p.pos+2, false) {
		return
	}
	if p.pos+1 < len(s) && s[p.pos+1] == '^' && p.footnoteRef() {
		return
	}
	node := p.addText("[", p.pos, p.pos+1)
	p.pushBracket(node, p.pos+1, false)
	p.pos++
}

func (p *inlineParser) pushBracket(node *inode, index int, image bool) {
	if p.brackets != nil {
		p.brackets.bracketAfter = true
	}
	p.brackets = &bracket{node: node, prev: p.brackets, prevDelim: p.delims, index: index, image: image, active: true}
}

func (p *inlineParser) bang() {
	s := p.s
	if p.pos+2 < len(s) && s[p.pos+1] == '[' && s[p.pos+2] == '[' && p.wikilink(p.pos, p.pos+3, true) {
		return
	}
	if p.pos+1 < len(s) && s[p.pos+1] == '[' {
		node := p.addText("![", p.pos, p.pos+2)
		p.pushBracket(node, p.pos+2, true)
		p.pos += 2
		return
	}
	p.addText("!", p.pos, p.pos+1)
	p.pos++
}

// wikilink parses [[…]] whose content starts at contentStart.
func (p *inlineParser) wikilink(start, contentStart int, embed bool) bool {
	s := p.s
	q := p.wikiClose.find(s, contentStart, "]]")
	if q < 0 {
		return false
	}
	content := s[contentStart:q]
	if content == "" || strings.ContainsAny(content, "\n[]") {
		return false
	}
	w := &WikiLink{Embed: embed}
	ref := content
	if i := strings.IndexByte(content, '|'); i >= 0 {
		ref = content[:i]
		if strings.HasSuffix(ref, `\`) {
			ref = ref[:len(ref)-1]
		}
		w.Label = strings.TrimSpace(content[i+1:])
		w.HasLabel = true
	}
	if i := strings.IndexByte(ref, '#'); i >= 0 {
		frag := strings.TrimSpace(ref[i+1:])
		if strings.HasPrefix(frag, "^") {
			w.Block = strings.TrimSpace(frag[1:])
		} else {
			w.Heading = frag
		}
		ref = ref[:i]
	}
	w.Target = strings.TrimSpace(ref)
	if w.Target == "" && w.Heading == "" && w.Block == "" {
		return false
	}
	p.addLeaf(w, start, q+2)
	p.pos = q + 2
	return true
}

func (p *inlineParser) footnoteRef() bool {
	s := p.s
	i := p.pos + 2
	for i < len(s) && s[i] != ']' && s[i] != '[' && s[i] != ' ' && s[i] != '\t' && s[i] != '\n' && i-p.pos < 200 {
		i++
	}
	if i == p.pos+2 || i >= len(s) || s[i] != ']' {
		return false
	}
	label := s[p.pos+2 : i]
	p.addLeaf(&FootnoteRef{Label: label, Index: p.ctx.footnoteIndex(label)}, p.pos, i+1)
	p.pos = i + 1
	return true
}

// linkLabel scans a link label "[…]" starting at s[i] == '['. It returns
// the label content and the index after ']'.
func linkLabel(s string, i int) (string, int, bool) {
	if i >= len(s) || s[i] != '[' {
		return "", i, false
	}
	for j := i + 1; j < len(s) && j-i <= 1000; j++ {
		switch s[j] {
		case '\\':
			j++
		case '[':
			return "", i, false
		case ']':
			return s[i+1 : j], j + 1, true
		}
	}
	return "", i, false
}

func (p *inlineParser) closeBracket() {
	s := p.s
	closePos := p.pos
	p.pos++
	opener := p.brackets
	if opener == nil {
		p.addText("]", closePos, closePos+1)
		return
	}
	if !opener.active {
		p.addText("]", closePos, closePos+1)
		p.brackets = opener.prev
		return
	}
	var dest, title, label string
	kind := LinkInline
	matched := false
	end := p.pos
	if p.pos < len(s) && s[p.pos] == '(' {
		j, _ := skipBlank(s, p.pos+1, true)
		if d, de, ok := parseLinkDest(s, j); ok {
			k, _ := skipBlank(s, de, true)
			t := ""
			okTitle := true
			if k > de && k < len(s) && (s[k] == '"' || s[k] == '\'' || s[k] == '(') {
				if tt, te, tok := parseLinkTitle(s, k); tok {
					t = tt
					k, _ = skipBlank(s, te, true)
				} else {
					okTitle = false
				}
			}
			if okTitle && k < len(s) && s[k] == ')' {
				dest, title, matched, end = d, t, true, k+1
			}
		}
	}
	if !matched {
		kind = LinkReference
		lab, after, ok := linkLabel(s, p.pos)
		var ref string
		switch {
		case ok && strings.TrimSpace(lab) != "":
			ref = lab
		case !opener.bracketAfter:
			ref = s[opener.index:closePos]
			if !ok {
				after = p.pos
			}
		}
		if ref != "" {
			if def, found := p.ctx.refs[normalizeLabel(ref)]; found {
				dest, title, matched, end = def.Dest, def.Title, true, after
				label = normalizeLabel(ref)
			}
		}
	}
	if !matched {
		p.brackets = opener.prev
		p.addText("]", closePos, closePos+1)
		return
	}
	n := &inode{start: opener.node.start, end: end}
	ext, path, frag := splitDest(dest)
	if opener.image {
		n.kind = iImage
		n.img = &Image{Src: dest, Title: title, External: ext, Path: path, Fragment: frag}
	} else {
		n.kind = iLink
		n.link = &Link{Kind: kind, Dest: dest, Title: title, External: ext, Path: path, Fragment: frag, Label: label}
	}
	for t := opener.node.next; t != nil; {
		nx := t.next
		t.unlink()
		n.appendChild(t)
		t = nx
	}
	p.root.appendChild(n)
	p.processEmphasis(opener.prevDelim)
	opener.node.unlink()
	p.brackets = opener.prev
	if !opener.image {
		for b := p.brackets; b != nil; b = b.prev {
			if !b.image {
				b.active = false
			}
		}
	}
	p.pos = end
}

var (
	reAutolinkURI   = regexp.MustCompile(`^<[A-Za-z][A-Za-z0-9.+-]{1,31}:[^\x00-\x20<>]*>`)
	reAutolinkEmail = regexp.MustCompile(`^<[a-zA-Z0-9.!#$%&'*+/=?^_{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*>`)
	reInlineTag     = regexp.MustCompile(`^(?:` + reOpenTag + `|` + reCloseTag + `|<\?[\s\S]*?\?>|<![A-Za-z][^>]*>|<!\[CDATA\[[\s\S]*?\]\]>)`)
	reBreakTag      = regexp.MustCompile(`(?i)^<br\s*/?>`)
)

// maxTagLen bounds how far an inline HTML tag or autolink may extend.
const maxTagLen = 4096

func (p *inlineParser) lessThan() {
	s := p.s
	start := p.pos
	window := s[start:min(len(s), start+maxTagLen)]
	if m := reAutolinkURI.FindString(window); m != "" {
		u := unescapeString(m[1 : len(m)-1])
		l := &Link{Kind: LinkAutolink, Dest: u, External: true}
		n := &inode{kind: iLink, link: l, start: start, end: start + len(m)}
		n.appendChild(&inode{kind: iText, text: m[1 : len(m)-1], start: start + 1, end: start + len(m) - 1})
		p.root.appendChild(n)
		p.pos += len(m)
		return
	}
	if m := reAutolinkEmail.FindString(window); m != "" {
		addr := m[1 : len(m)-1]
		l := &Link{Kind: LinkAutolink, Dest: "mailto:" + addr, External: true}
		n := &inode{kind: iLink, link: l, start: start, end: start + len(m)}
		n.appendChild(&inode{kind: iText, text: addr, start: start + 1, end: start + len(m) - 1})
		p.root.appendChild(n)
		p.pos += len(m)
		return
	}
	if strings.HasPrefix(window, "<!--") {
		if q := p.htmlClose.find(s, start+4, "-->"); q >= 0 {
			p.addLeaf(&Comment{Syntax: CommentHTML, Value: s[start+4 : q]}, start, q+3)
			p.pos = q + 3
			return
		}
	}
	if m := reBreakTag.FindString(window); m != "" {
		p.addLeaf(&HardBreak{}, start, start+len(m))
		p.pos += len(m)
		for p.pos < len(s) && s[p.pos] == '\n' {
			// "<br>" at the end of a line already breaks; swallow the
			// line ending so it does not add a second (soft) break.
			p.pos++
			for p.pos < len(s) && isSpaceOrTab(s[p.pos]) {
				p.pos++
			}
			break
		}
		return
	}
	if m := reInlineTag.FindString(window); m != "" {
		p.addLeaf(&RawHTML{Raw: m}, start, start+len(m))
		p.pos += len(m)
		return
	}
	p.addText("<", start, start+1)
	p.pos++
}

func (p *inlineParser) dollar() {
	s := p.s
	start := p.pos
	if start+1 < len(s) && s[start+1] == '$' {
		if q := p.dmClose.find(s, start+2, "$$"); q >= 0 && strings.TrimSpace(s[start+2:q]) != "" {
			p.addLeaf(&Math{Value: strings.TrimSpace(s[start+2 : q]), Display: true}, start, q+2)
			p.pos = q + 2
			return
		}
		p.addText("$$", start, start+2)
		p.pos += 2
		return
	}
	if start+1 < len(s) && !isSpaceOrTab(s[start+1]) && s[start+1] != '\n' {
		if q := p.mathCloser(start + 2); q >= 0 {
			p.addLeaf(&Math{Value: s[start+1 : q]}, start, q+1)
			p.pos = q + 1
			return
		}
	}
	p.addText("$", start, start+1)
	p.pos++
}

// isMathCloser reports whether s[q] can close inline $…$ math: '$' with a
// non-blank before it and no digit or '$' after it.
func isMathCloser(s string, q int) bool {
	if s[q] != '$' || q == 0 {
		return false
	}
	b := s[q-1]
	if b == ' ' || b == '\t' || b == '\n' || b == '\\' || b == '$' {
		return false
	}
	if q+1 < len(s) && (isDigit(s[q+1]) || s[q+1] == '$') {
		return false
	}
	return true
}

func (p *inlineParser) mathCloser(from int) int {
	if p.mathValid && from >= p.mathFrom && (p.mathAt < 0 || p.mathAt >= from) {
		return p.mathAt
	}
	at := -1
	for i := from; i < len(p.s); i++ {
		if p.s[i] == '$' && isMathCloser(p.s, i) {
			at = i
			break
		}
	}
	p.mathFrom, p.mathAt, p.mathValid = from, at, true
	return at
}

func (p *inlineParser) percent() {
	s := p.s
	start := p.pos
	if start+1 < len(s) && s[start+1] == '%' {
		if q := p.pctClose.find(s, start+2, "%%"); q >= 0 {
			p.addLeaf(&Comment{Syntax: CommentPercent, Value: s[start+2 : q]}, start, q+2)
			p.pos = q + 2
			return
		}
		p.addText("%%", start, start+2)
		p.pos += 2
		return
	}
	p.addText("%", start, start+1)
	p.pos++
}

// isTagRune reports whether r may appear in a tag name after '#'.
func isTagRune(r rune) bool {
	return r == '_' || r == '-' || r == '/' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r)
}

// tagEnd returns the end of a tag name starting at s[i] (just after '#'),
// or i when there is no valid tag (empty, or digits only).
func tagEnd(s string, i int) int {
	j := i
	nonDigit := false
	for j < len(s) {
		r, w := utf8.DecodeRuneInString(s[j:])
		if !isTagRune(r) {
			break
		}
		if !(r >= '0' && r <= '9') && r != '/' && r != '-' && r != '_' {
			nonDigit = true
		}
		j += w
	}
	for j > i && s[j-1] == '/' {
		j--
	}
	if !nonDigit || j == i || s[i] == '/' {
		return i
	}
	return j
}

func (p *inlineParser) hash() {
	s := p.s
	start := p.pos
	boundary := start == 0
	if !boundary {
		r, _ := utf8.DecodeLastRuneInString(s[:start])
		boundary = isWhite(r) || strings.ContainsRune("(*_~=[", r)
	}
	if boundary {
		if e := tagEnd(s, start+1); e > start+1 {
			p.addLeaf(&Tag{Name: s[start+1 : e]}, start, e)
			p.pos = e
			return
		}
	}
	p.addText("#", start, start+1)
	p.pos++
}

// ---------------------------------------------------------------------------
// Conversion of the internal list into public nodes.

func setSpan(x Inline, sp Span) {
	switch n := x.(type) {
	case *Text:
		n.Span = sp
	case *SoftBreak:
		n.Span = sp
	case *HardBreak:
		n.Span = sp
	case *Emphasis:
		n.Span = sp
	case *Strong:
		n.Span = sp
	case *Strikethrough:
		n.Span = sp
	case *Highlight:
		n.Span = sp
	case *Code:
		n.Span = sp
	case *Math:
		n.Span = sp
	case *WikiLink:
		n.Span = sp
	case *Link:
		n.Span = sp
	case *Image:
		n.Span = sp
	case *Tag:
		n.Span = sp
	case *FootnoteRef:
		n.Span = sp
	case *RawHTML:
		n.Span = sp
	case *Comment:
		n.Span = sp
	}
}

func (p *inlineParser) build(first *inode, inLink bool) []Inline {
	var out []Inline
	// Adjacent text pieces are collected and joined once, so that a long
	// run of fragments (e.g. thousands of unmatched delimiters) stays
	// linear.
	var pend []string
	var pendSpan Span
	flush := func() {
		if len(pend) == 0 {
			return
		}
		v := pend[0]
		if len(pend) > 1 {
			v = strings.Join(pend, "")
		}
		out = append(out, &Text{Span: pendSpan, Value: v})
		pend = pend[:0]
	}
	appendText := func(text string, sp Span) {
		if text == "" {
			return
		}
		if len(pend) == 0 {
			pendSpan = sp
		} else if sp.End > pendSpan.End {
			pendSpan.End = sp.End
		}
		pend = append(pend, text)
	}
	push := func(x Inline) {
		flush()
		out = append(out, x)
	}
	for n := first; n != nil; n = n.next {
		if n.kind == iText {
			appendText(n.text, p.span(n.start, n.end))
			continue
		}
		sp := p.span(n.start, n.end)
		switch n.kind {
		case iLeaf:
			setSpan(n.leaf, sp)
			push(n.leaf)
		case iEmph:
			push(&Emphasis{Span: sp, Children: p.build(n.first, inLink)})
		case iStrong:
			push(&Strong{Span: sp, Children: p.build(n.first, inLink)})
		case iStrike:
			push(&Strikethrough{Span: sp, Children: p.build(n.first, inLink)})
		case iHigh:
			push(&Highlight{Span: sp, Children: p.build(n.first, inLink)})
		case iLink:
			if inLink {
				// Links never nest: keep the inner link's content only.
				for _, c := range p.build(n.first, true) {
					if t, ok := c.(*Text); ok {
						appendText(t.Value, t.Span)
					} else {
						push(c)
					}
				}
				continue
			}
			l := n.link
			l.Span = sp
			l.Children = p.build(n.first, true)
			push(l)
		case iImage:
			img := n.img
			img.Span = sp
			img.Alt = p.build(n.first, true)
			push(img)
		}
	}
	flush()
	return out
}
