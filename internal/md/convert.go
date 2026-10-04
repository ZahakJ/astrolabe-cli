package md

import (
	"sort"
	"strconv"
	"strings"
)

// Parse parses a Markdown note. It accepts any input and never panics.
func Parse(src string) *Document {
	starts, ends, bom, crlf := splitLines(src)
	doc := &Document{
		Source:     src,
		BOM:        bom,
		CRLF:       crlf,
		RefDefs:    map[string]*LinkRefDef{},
		lineStarts: starts,
		lineEnds:   ends,
	}
	first := 0
	if fm := parseFrontmatter(src, starts, ends); fm != nil {
		doc.Frontmatter = fm
		first = fm.EndLine + 1
	}
	bp := &blockParser{src: src, starts: starts, ends: ends, refs: doc.RefDefs, footnotes: map[string]bool{}}
	root := bp.parse(first)
	c := &converter{
		doc:      doc,
		ctx:      &inlineCtx{refs: doc.RefDefs, footnotes: bp.footnotes, fnIndex: map[string]int{}},
		slugs:    map[string]bool{},
		slugNext: map[string]int{},
	}
	doc.Blocks = c.blocks(root.children)
	c.numberFootnotes()
	return doc
}

// ParseBytes is Parse for a byte slice.
func ParseBytes(src []byte) *Document { return Parse(string(src)) }

type converter struct {
	doc       *Document
	ctx       *inlineCtx
	slugs     map[string]bool
	slugNext  map[string]int // last numeric suffix used per base slug
	footnotes []*FootnoteDef
}

func (c *converter) inlines(segs []seg) []Inline { return parseInlines(segs, c.ctx) }

// blocks converts sibling nodes, attaching standalone block ids to the
// preceding block.
func (c *converter) blocks(nodes []*bnode) []Block {
	out := make([]Block, 0, len(nodes))
	for _, n := range nodes {
		if n.kind == bParagraph {
			if id, only := standaloneID(n.lines); only {
				if len(out) > 0 {
					if b := out[len(out)-1].BlockBase(); b.ID == "" {
						b.ID = id
					}
				}
				continue
			}
		}
		if b := c.block(n); b != nil {
			out = append(out, b)
		}
	}
	return out
}

// standaloneID reports whether a paragraph consists only of "^id".
func standaloneID(lines []seg) (string, bool) {
	if len(lines) != 1 {
		return "", false
	}
	rest, id := splitBlockID(lines[0].text)
	return id, id != "" && rest == ""
}

// splitBlockID splits a trailing Obsidian block id (" ^abc-1") off a line.
func splitBlockID(text string) (rest, id string) {
	t := strings.TrimRight(text, " \t")
	i := strings.LastIndexByte(t, '^')
	if i < 0 || i == len(t)-1 {
		return text, ""
	}
	id = t[i+1:]
	for j := 0; j < len(id); j++ {
		if !isAlnum(id[j]) && id[j] != '-' {
			return text, ""
		}
	}
	if i > 0 && !isSpaceOrTab(t[i-1]) {
		return text, ""
	}
	return strings.TrimRight(t[:i], " \t"), id
}

func (c *converter) uniqueSlug(text string) string {
	base := Slugify(text)
	s := base
	if c.slugs[s] {
		n := c.slugNext[base]
		for {
			n++
			s = base + "-" + strconv.Itoa(n)
			if !c.slugs[s] {
				break
			}
		}
		c.slugNext[base] = n
	}
	c.slugs[s] = true
	return s
}

func lastLine(lines []seg, def int) int {
	if len(lines) == 0 {
		return def
	}
	return lines[len(lines)-1].line
}

func segTexts(lines []seg) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.text
	}
	return out
}

func endOf(start int, children []Block) int {
	end := start
	if len(children) > 0 {
		if e := children[len(children)-1].BlockBase().EndLine; e > end {
			end = e
		}
	}
	return end
}

func (c *converter) paragraph(lines []seg) *Paragraph {
	if len(lines) == 0 {
		return nil
	}
	lines = append([]seg(nil), lines...)
	para := &Paragraph{Base: Base{StartLine: lines[0].line, EndLine: lastLine(lines, lines[0].line)}}
	last := &lines[len(lines)-1]
	if rest, id := splitBlockID(last.text); id != "" {
		para.ID = id
		if rest == "" {
			lines = lines[:len(lines)-1]
		} else {
			last.text = rest
		}
	}
	para.Inlines = c.inlines(lines)
	return para
}

func (c *converter) block(n *bnode) Block {
	switch n.kind {
	case bParagraph:
		if p := c.paragraph(n.lines); p != nil {
			return p
		}
		return nil
	case bHeading:
		h := &Heading{Base: Base{StartLine: n.start, EndLine: n.start}, Level: n.level, Setext: n.setext}
		if n.setext {
			h.EndLine = n.end
		}
		lines := n.lines
		if len(lines) > 0 {
			if rest, id := splitBlockID(lines[len(lines)-1].text); id != "" && rest != "" {
				lines = append([]seg(nil), lines...)
				lines[len(lines)-1].text = rest
				h.ID = id
			}
		}
		h.Inlines = c.inlines(lines)
		h.Text = PlainText(h.Inlines)
		h.Slug = c.uniqueSlug(h.Text)
		return h
	case bThematic:
		return &ThematicBreak{Base: Base{StartLine: n.start, EndLine: n.start}, Marker: n.mark}
	case bBlockQuote:
		if co := c.callout(n); co != nil {
			return co
		}
		q := &BlockQuote{Base: Base{StartLine: n.start}}
		q.Children = c.blocks(n.children)
		q.EndLine = endOf(n.start, q.Children)
		return q
	case bList:
		l := &List{Base: Base{StartLine: n.start}, Ordered: n.list.ordered, Marker: n.list.marker, Tight: n.list.tight}
		for i, it := range n.children {
			item := &ListItem{Base: Base{StartLine: it.start}, Task: it.task}
			if it.list != nil {
				if l.Ordered {
					item.Number = it.list.start
				}
				if i == 0 {
					l.Start = it.list.start
				}
			}
			item.Children = c.blocks(it.children)
			item.EndLine = endOf(it.start, item.Children)
			if len(item.Children) > 0 {
				if p, ok := item.Children[0].(*Paragraph); ok && p.ID != "" {
					item.ID = p.ID
				}
			}
			l.Items = append(l.Items, item)
		}
		if len(l.Items) > 0 {
			l.StartLine = l.Items[0].StartLine
			l.EndLine = l.Items[len(l.Items)-1].EndLine
		} else {
			l.EndLine = l.StartLine
		}
		return l
	case bCode:
		cb := &CodeBlock{Base: Base{StartLine: n.start}, Fenced: n.fenced, Fence: n.fence, Info: n.info,
			Lang: langOf(n.info), Lines: segTexts(n.lines), Closed: n.fenced && n.closed}
		if n.fenced {
			cb.FirstLine = n.start + 1
			if n.closed {
				cb.EndLine = n.end
			} else {
				cb.EndLine = lastLine(n.lines, n.start)
			}
		} else {
			cb.FirstLine = n.start
			cb.EndLine = lastLine(n.lines, n.start)
		}
		return cb
	case bMath:
		m := &MathBlock{Base: Base{StartLine: n.start}, Lines: segTexts(n.lines), FirstLine: n.start}
		if len(n.lines) > 0 {
			m.FirstLine = n.lines[0].line
		}
		if n.closed {
			m.EndLine = n.end
		} else {
			m.EndLine = lastLine(n.lines, n.start)
		}
		return m
	case bComment:
		cm := &CommentBlock{Base: Base{StartLine: n.start}, Syntax: CommentPercent, Lines: segTexts(n.lines)}
		if n.closed {
			cm.EndLine = n.end
		} else {
			cm.EndLine = lastLine(n.lines, n.start)
		}
		return cm
	case bHTML:
		lines := segTexts(n.lines)
		base := Base{StartLine: n.start, EndLine: lastLine(n.lines, n.start)}
		if n.htmlType == 2 && isPureComment(lines) {
			return &CommentBlock{Base: base, Syntax: CommentHTML, Lines: lines}
		}
		if br := onlyBreaks(n.lines); br != nil {
			// A line of nothing but <br> tags is vertical space, not HTML
			// to show verbatim.
			return &Paragraph{Base: base, Inlines: br}
		}
		return &HTMLBlock{Base: base, Lines: lines}
	case bTable:
		return c.table(n)
	case bFootnote:
		f := &FootnoteDef{Base: Base{StartLine: n.start}, Label: n.label}
		f.Children = c.blocks(n.children)
		f.EndLine = endOf(n.start, f.Children)
		c.footnotes = append(c.footnotes, f)
		return f
	case bRefDef:
		d := &LinkRefDef{Base: Base{StartLine: n.start, EndLine: n.end}}
		if n.ref != nil {
			d.Label, d.Dest, d.Title = n.ref.Label, n.ref.Dest, n.ref.Title
		}
		return d
	}
	return nil
}

// onlyBreaks returns one HardBreak per <br> tag when the lines contain
// nothing else, or nil.
func onlyBreaks(lines []seg) []Inline {
	var out []Inline
	for _, l := range lines {
		t := l.text
		i := 0
		for {
			for i < len(t) && isSpaceOrTab(t[i]) {
				i++
			}
			if i >= len(t) {
				break
			}
			m := reBreakTag.FindString(t[i:])
			if m == "" {
				return nil
			}
			out = append(out, &HardBreak{Span{l.off + i, l.off + i + len(m)}})
			i += len(m)
		}
	}
	return out
}

func isPureComment(lines []string) bool {
	j := strings.Join(lines, "\n")
	t := strings.TrimSpace(j)
	if !strings.HasPrefix(t, "<!--") {
		return false
	}
	i := strings.Index(t[4:], "-->")
	return i >= 0 && strings.TrimSpace(t[4+i+3:]) == ""
}

// langOf extracts the language from a fence info string.
func langOf(info string) string {
	f := strings.Fields(info)
	if len(f) == 0 {
		return ""
	}
	l := f[0]
	l = strings.TrimPrefix(l, "{")
	l = strings.TrimSuffix(l, "}")
	l = strings.TrimPrefix(l, ".")
	if i := strings.IndexAny(l, ",{}"); i >= 0 {
		l = l[:i]
	}
	return strings.ToLower(l)
}

// parseCalloutHeader recognises "[!type]", "[!type]+ Title", … at the
// start of t. titleStart is the byte index of the title within t.
func parseCalloutHeader(t string) (kind string, fold Fold, titleStart int, ok bool) {
	if !strings.HasPrefix(t, "[!") {
		return
	}
	j := strings.IndexByte(t, ']')
	if j < 3 {
		return
	}
	kind = t[2:j]
	if strings.ContainsAny(kind, " \t[!") {
		return "", 0, 0, false
	}
	k := j + 1
	if k < len(t) && (t[k] == '+' || t[k] == '-') {
		fold = Fold(t[k])
		k++
	}
	if k < len(t) && !isSpaceOrTab(t[k]) {
		return "", 0, 0, false
	}
	for k < len(t) && isSpaceOrTab(t[k]) {
		k++
	}
	return strings.ToLower(kind), fold, k, true
}

func (c *converter) callout(n *bnode) *Callout {
	if len(n.children) == 0 || len(n.children[0].lines) == 0 {
		return nil
	}
	first := n.children[0]
	// "> [!note] Title" directly followed by "> ---" parses as a setext
	// heading; the callout wins and the underline becomes a rule.
	setext := first.kind == bHeading && first.setext
	if first.kind != bParagraph && !setext {
		return nil
	}
	head := first.lines[0]
	kind, fold, ts, ok := parseCalloutHeader(head.text)
	if !ok {
		return nil
	}
	co := &Callout{Base: Base{StartLine: n.start}, Kind: kind, Fold: fold}
	if title := strings.TrimRight(head.text[ts:], " \t"); title != "" {
		co.Title = c.inlines([]seg{{text: title, off: head.off + ts, line: head.line}})
	}
	if p := c.paragraph(first.lines[1:]); p != nil {
		co.Children = append(co.Children, p)
	}
	if setext && first.level == 2 {
		co.Children = append(co.Children, &ThematicBreak{Base: Base{StartLine: first.end, EndLine: first.end}, Marker: '-'})
	}
	co.Children = append(co.Children, c.blocks(n.children[1:])...)
	co.EndLine = endOf(n.start, co.Children)
	return co
}

func (c *converter) table(n *bnode) *Table {
	t := &Table{Base: Base{StartLine: n.start, EndLine: lastLine(n.lines, n.start)}, Align: n.align}
	saved := c.ctx.table
	c.ctx.table = true
	defer func() { c.ctx.table = saved }()
	for i, l := range n.lines {
		row := &TableRow{Line: l.line}
		cells, _ := splitRow(l.text)
		for j := 0; j < len(n.align); j++ {
			cell := &TableCell{}
			if j < len(cells) {
				ct := cells[j]
				abs := l.off + ct.start
				cell.Span = Span{abs, abs + len(ct.text)}
				if ct.text != "" {
					cell.Inlines = c.inlines([]seg{{text: ct.text, off: abs, line: l.line}})
				}
			} else {
				e := l.off + len(l.text)
				cell.Span = Span{e, e}
			}
			row.Cells = append(row.Cells, cell)
		}
		if i == 0 {
			t.Header = row
		} else {
			t.Rows = append(t.Rows, row)
		}
	}
	return t
}

func (c *converter) numberFootnotes() {
	for _, f := range c.footnotes {
		key := normalizeLabel(f.Label)
		if i, ok := c.ctx.fnIndex[key]; ok {
			f.Index = i
			continue
		}
		c.ctx.fnNext++
		c.ctx.fnIndex[key] = c.ctx.fnNext
		f.Index = c.ctx.fnNext
	}
	defs := append([]*FootnoteDef(nil), c.footnotes...)
	sort.SliceStable(defs, func(i, j int) bool { return defs[i].Index < defs[j].Index })
	c.doc.Footnotes = defs
}
