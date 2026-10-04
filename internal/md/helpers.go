package md

import (
	"fmt"
	"strings"
	"unicode"
)

// WalkBlocks visits blocks in document order (pre-order). When fn returns
// false the children of that block are skipped. List items are visited as
// children of their List.
func WalkBlocks(blocks []Block, fn func(Block) bool) {
	for _, b := range blocks {
		if fn(b) {
			WalkBlocks(BlockChildren(b), fn)
		}
	}
}

// WalkInlines visits inlines in order (pre-order). When fn returns false
// the children of that inline are skipped.
func WalkInlines(inlines []Inline, fn func(Inline) bool) {
	for _, n := range inlines {
		if fn(n) {
			WalkInlines(InlineChildren(n), fn)
		}
	}
}

// BlockInlines returns the inline sequences held directly by a block:
// the content of a Paragraph or Heading, the title of a Callout, and every
// cell of a Table (header first). Other blocks return nil.
func BlockInlines(b Block) [][]Inline {
	switch b := b.(type) {
	case *Paragraph:
		return [][]Inline{b.Inlines}
	case *Heading:
		return [][]Inline{b.Inlines}
	case *Callout:
		if b.Title != nil {
			return [][]Inline{b.Title}
		}
	case *Table:
		var out [][]Inline
		rows := append([]*TableRow{b.Header}, b.Rows...)
		for _, r := range rows {
			if r == nil {
				continue
			}
			for _, c := range r.Cells {
				out = append(out, c.Inlines)
			}
		}
		return out
	}
	return nil
}

// walkAllInlines visits every inline of the document, including footnote
// definitions, callout titles and table cells.
func walkAllInlines(doc *Document, fn func(Inline) bool) {
	WalkBlocks(doc.Blocks, func(b Block) bool {
		for _, seq := range BlockInlines(b) {
			WalkInlines(seq, fn)
		}
		return true
	})
}

// PlainText flattens inlines to text for titles, the outline and search:
// markup is dropped, soft breaks become spaces, hard breaks newlines,
// wikilinks their display text, tags keep their '#', comments, raw HTML
// and footnote references disappear.
func PlainText(inlines []Inline) string {
	var b strings.Builder
	plainText(&b, inlines)
	return b.String()
}

func plainText(b *strings.Builder, inlines []Inline) {
	for _, n := range inlines {
		switch n := n.(type) {
		case *Text:
			b.WriteString(n.Value)
		case *SoftBreak:
			b.WriteByte(' ')
		case *HardBreak:
			b.WriteByte('\n')
		case *Code:
			b.WriteString(n.Value)
		case *Math:
			b.WriteString(n.Value)
		case *WikiLink:
			b.WriteString(n.Display())
		case *Tag:
			b.WriteByte('#')
			b.WriteString(n.Name)
		default:
			plainText(b, InlineChildren(n))
		}
	}
}

// Slugify returns the GitHub-style anchor for a heading text: lowercase,
// letters/marks/digits/'_'/'-' kept, spaces turned into '-', everything
// else dropped. Uniqueness suffixes are added by the parser, not here.
func Slugify(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// OutlineItem is one heading of a document outline.
type OutlineItem struct {
	Level   int
	Text    string
	Slug    string
	Line    int // source line of the heading
	Heading *Heading
}

// Outline lists every heading in document order, including headings
// nested inside quotes, callouts and list items.
func Outline(doc *Document) []OutlineItem {
	var out []OutlineItem
	WalkBlocks(doc.Blocks, func(b Block) bool {
		if h, ok := b.(*Heading); ok {
			out = append(out, OutlineItem{Level: h.Level, Text: h.Text, Slug: h.Slug, Line: h.StartLine, Heading: h})
		}
		return true
	})
	return out
}

// LinkRef is a link found in a document.
type LinkRef struct {
	// Node is a *WikiLink (links and embeds), *Link or *Image.
	Node Inline
	// Line is the source line where the link starts.
	Line int
}

// Links returns every wikilink, embed, Markdown link and image in
// document order.
func Links(doc *Document) []LinkRef {
	var out []LinkRef
	walkAllInlines(doc, func(n Inline) bool {
		switch n.(type) {
		case *WikiLink, *Link, *Image:
			line, _ := doc.LineCol(n.SourceSpan().Start)
			out = append(out, LinkRef{Node: n, Line: line})
		}
		return true
	})
	return out
}

// Tags returns the document's tags without '#': frontmatter tags first,
// then body tags in order of appearance, de-duplicated case-insensitively
// (the first spelling wins).
func Tags(doc *Document) []string {
	var out []string
	seen := map[string]bool{}
	add := func(t string) {
		k := strings.ToLower(t)
		if t != "" && !seen[k] {
			seen[k] = true
			out = append(out, t)
		}
	}
	if doc.Frontmatter != nil {
		for _, t := range doc.Frontmatter.Tags {
			add(t)
		}
	}
	walkAllInlines(doc, func(n Inline) bool {
		if t, ok := n.(*Tag); ok {
			add(t.Name)
		}
		return true
	})
	return out
}

// WordCount counts the words a reader would read: text of paragraphs,
// headings, callout titles, tables, quotes and list items. Code, math,
// HTML, comments and frontmatter are not counted. Han, Hiragana, Katakana
// and Hangul characters count one word each.
func WordCount(doc *Document) int {
	n := 0
	WalkBlocks(doc.Blocks, func(b Block) bool {
		for _, seq := range BlockInlines(b) {
			n += countWords(plainTextNoCode(seq))
		}
		return true
	})
	return n
}

func plainTextNoCode(inl []Inline) string {
	var b strings.Builder
	WalkInlines(inl, func(n Inline) bool {
		switch n := n.(type) {
		case *Text:
			b.WriteString(n.Value)
		case *SoftBreak, *HardBreak:
			b.WriteByte(' ')
		case *WikiLink:
			b.WriteByte(' ')
			b.WriteString(n.Display())
			b.WriteByte(' ')
		case *Tag:
			b.WriteString(" #" + n.Name + " ")
		case *Code:
			b.WriteString(" " + n.Value + " ")
		case *Math, *Comment, *RawHTML, *FootnoteRef:
			b.WriteByte(' ')
		}
		return true
	})
	return b.String()
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
}

func countWords(s string) int {
	n := 0
	in := false
	for _, r := range s {
		switch {
		case isCJK(r):
			n++
			in = false
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r):
			if !in {
				n++
				in = true
			}
		case in && (r == '\'' || r == '’' || r == '-' || r == '_'):
			// stays inside the word ("don't", "well-known")
		default:
			in = false
		}
	}
	return n
}

// ResolveAnchor maps a link fragment to a source line. "^id" (or "#^id")
// finds the block with that Obsidian id. Anything else is a heading: the
// last '#'-separated component is compared with heading slugs exactly,
// then after Slugify, then with heading texts case-insensitively.
func ResolveAnchor(doc *Document, anchor string) (line int, ok bool) {
	a := strings.TrimSpace(strings.TrimPrefix(anchor, "#"))
	if strings.HasPrefix(a, "^") {
		id := a[1:]
		found := -1
		WalkBlocks(doc.Blocks, func(b Block) bool {
			if found < 0 && b.BlockBase().ID == id {
				found = b.BlockBase().StartLine
			}
			return found < 0
		})
		return found, found >= 0
	}
	if i := strings.LastIndexByte(a, '#'); i >= 0 {
		a = strings.TrimSpace(a[i+1:])
	}
	if a == "" {
		return -1, false
	}
	outline := Outline(doc)
	for _, h := range outline {
		if h.Slug == a {
			return h.Line, true
		}
	}
	s := Slugify(a)
	for _, h := range outline {
		if h.Slug == s {
			return h.Line, true
		}
	}
	for _, h := range outline {
		if strings.EqualFold(strings.TrimSpace(h.Text), a) {
			return h.Line, true
		}
	}
	return -1, false
}

// Dump renders the tree in a compact one-line-per-block form for tests and
// debugging. With positions, each block is prefixed by its line range and
// each inline is suffixed by its byte span. The format is not stable.
func Dump(doc *Document, positions bool) string {
	var b strings.Builder
	if fm := doc.Frontmatter; fm != nil {
		fmt.Fprintf(&b, "frontmatter")
		if positions {
			fmt.Fprintf(&b, "@%d-%d", fm.StartLine, fm.EndLine)
		}
		fmt.Fprintf(&b, "{title=%q tags=%q aliases=%q date=%q", fm.Title, fm.Tags, fm.Aliases, fm.Date)
		for _, f := range fm.Fields {
			fmt.Fprintf(&b, " %s:%q", f.Key, f.Value)
		}
		b.WriteString("}\n")
	}
	for _, bl := range doc.Blocks {
		dumpBlock(&b, bl, positions)
		b.WriteByte('\n')
	}
	return b.String()
}

func dumpBlocks(b *strings.Builder, blocks []Block, pos bool) {
	b.WriteByte('{')
	for i, bl := range blocks {
		if i > 0 {
			b.WriteByte(' ')
		}
		dumpBlock(b, bl, pos)
	}
	b.WriteByte('}')
}

func dumpBlock(b *strings.Builder, bl Block, pos bool) {
	base := bl.BlockBase()
	if pos {
		fmt.Fprintf(b, "%d-%d:", base.StartLine, base.EndLine)
	}
	switch n := bl.(type) {
	case *Paragraph:
		b.WriteString("p")
		dumpInlines(b, n.Inlines, pos)
	case *Heading:
		fmt.Fprintf(b, "h%d", n.Level)
		dumpInlines(b, n.Inlines, pos)
		fmt.Fprintf(b, "#%s", n.Slug)
	case *ThematicBreak:
		b.WriteString("hr")
	case *BlockQuote:
		b.WriteString("quote")
		dumpBlocks(b, n.Children, pos)
	case *Callout:
		fmt.Fprintf(b, "callout(%s", n.Kind)
		if n.Fold != FoldNone {
			fmt.Fprintf(b, "%c", n.Fold)
		}
		b.WriteString(")")
		if n.Title != nil {
			dumpInlines(b, n.Title, pos)
		}
		dumpBlocks(b, n.Children, pos)
	case *List:
		if n.Ordered {
			fmt.Fprintf(b, "ol(%d%c", n.Start, n.Marker)
		} else {
			fmt.Fprintf(b, "ul(%c", n.Marker)
		}
		if n.Tight {
			b.WriteString(",tight")
		} else {
			b.WriteString(",loose")
		}
		b.WriteString(")")
		blocks := make([]Block, len(n.Items))
		for i, it := range n.Items {
			blocks[i] = it
		}
		dumpBlocks(b, blocks, pos)
	case *ListItem:
		b.WriteString("li")
		if n.Task != nil {
			fmt.Fprintf(b, "[%c]", n.Task.State)
			if pos {
				fmt.Fprintf(b, "@%d:%d", n.Task.Line, n.Task.Col)
			}
		}
		dumpBlocks(b, n.Children, pos)
	case *CodeBlock:
		fmt.Fprintf(b, "code(%s)%q", n.Lang, strings.Join(n.Lines, "\n"))
		if n.Fenced && !n.Closed {
			b.WriteString("!unclosed")
		}
	case *MathBlock:
		fmt.Fprintf(b, "math%q", strings.Join(n.Lines, "\n"))
	case *HTMLBlock:
		fmt.Fprintf(b, "html%q", strings.Join(n.Lines, "\n"))
	case *CommentBlock:
		fmt.Fprintf(b, "comment%q", strings.Join(n.Lines, "\n"))
	case *Table:
		b.WriteString("table(")
		for i, a := range n.Align {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString([]string{"-", "l", "c", "r"}[a])
		}
		b.WriteString(")")
		rows := append([]*TableRow{n.Header}, n.Rows...)
		for _, r := range rows {
			b.WriteString("{")
			for i, c := range r.Cells {
				if i > 0 {
					b.WriteByte('|')
				}
				dumpInlines(b, c.Inlines, pos)
			}
			b.WriteString("}")
		}
	case *FootnoteDef:
		fmt.Fprintf(b, "fndef(%s:%d)", n.Label, n.Index)
		dumpBlocks(b, n.Children, pos)
	case *LinkRefDef:
		fmt.Fprintf(b, "refdef(%s=%s %q)", n.Label, n.Dest, n.Title)
	}
	if base.ID != "" {
		fmt.Fprintf(b, "^%s", base.ID)
	}
}

func dumpInlines(b *strings.Builder, inl []Inline, pos bool) {
	b.WriteByte('[')
	for i, n := range inl {
		if i > 0 {
			b.WriteByte(' ')
		}
		dumpInline(b, n, pos)
	}
	b.WriteByte(']')
}

func dumpInline(b *strings.Builder, n Inline, pos bool) {
	switch n := n.(type) {
	case *Text:
		fmt.Fprintf(b, "%q", n.Value)
	case *SoftBreak:
		b.WriteString("sb")
	case *HardBreak:
		b.WriteString("br")
	case *Emphasis:
		b.WriteString("em")
		dumpInlines(b, n.Children, pos)
	case *Strong:
		b.WriteString("strong")
		dumpInlines(b, n.Children, pos)
	case *Strikethrough:
		b.WriteString("del")
		dumpInlines(b, n.Children, pos)
	case *Highlight:
		b.WriteString("mark")
		dumpInlines(b, n.Children, pos)
	case *Code:
		fmt.Fprintf(b, "code%q", n.Value)
	case *Math:
		if n.Display {
			fmt.Fprintf(b, "dmath%q", n.Value)
		} else {
			fmt.Fprintf(b, "math%q", n.Value)
		}
	case *WikiLink:
		if n.Embed {
			b.WriteByte('!')
		}
		b.WriteString("wiki(" + n.Target)
		if n.Heading != "" {
			b.WriteString("#" + n.Heading)
		}
		if n.Block != "" {
			b.WriteString("#^" + n.Block)
		}
		if n.HasLabel {
			b.WriteString("|" + n.Label)
		}
		b.WriteString(")")
	case *Link:
		kind := [...]string{"link", "reflink", "autolink", "url"}[n.Kind]
		fmt.Fprintf(b, "%s(%s", kind, n.Dest)
		if n.Title != "" {
			fmt.Fprintf(b, " %q", n.Title)
		}
		b.WriteString(")")
		dumpInlines(b, n.Children, pos)
	case *Image:
		fmt.Fprintf(b, "img(%s", n.Src)
		if n.Title != "" {
			fmt.Fprintf(b, " %q", n.Title)
		}
		b.WriteString(")")
		dumpInlines(b, n.Alt, pos)
	case *Tag:
		b.WriteString("tag(" + n.Name + ")")
	case *FootnoteRef:
		fmt.Fprintf(b, "fn(%s:%d)", n.Label, n.Index)
	case *RawHTML:
		fmt.Fprintf(b, "rawhtml%q", n.Raw)
	case *Comment:
		fmt.Fprintf(b, "hidden%q", n.Value)
	}
	if pos {
		sp := n.SourceSpan()
		fmt.Fprintf(b, "@%d-%d", sp.Start, sp.End)
	}
}
