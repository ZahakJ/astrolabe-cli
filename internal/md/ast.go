// Package md parses Markdown as written in Obsidian-style vaults (CommonMark
// in practice, GitHub Flavored Markdown, and the Obsidian extensions) into a
// block/inline syntax tree in which every node records where it came from in
// the source file.
//
// # Entry point
//
//	doc := md.Parse(src)          // never panics, never fails
//
// Parse accepts any byte string (invalid UTF-8, NUL bytes, CRLF or CR line
// endings, a UTF-8 BOM) and always returns a *Document.
//
// # Source positions
//
// Two coordinate systems are used, both referring to the ORIGINAL source
// passed to Parse (Document.Source), frontmatter included:
//
//   - Blocks carry line numbers: Base.StartLine and Base.EndLine are 0-based
//     and inclusive. EndLine never counts trailing blank lines, so a
//     paragraph on lines 4–6 followed by a blank line has EndLine 6.
//   - Inlines carry byte offsets: Span.Start and Span.End form a half-open
//     range of byte offsets into Document.Source. An inline that crosses a
//     line break (an emphasis run, a soft break) still has a single Span; the
//     bytes inside it may include container prefixes such as "> " that belong
//     to enclosing blocks. Document.LineCol converts an offset to a 0-based
//     line and a 0-based byte column.
//
// Line numbering: lines are separated by "\n", "\r\n" or a lone "\r". Line 0
// begins after a leading UTF-8 BOM, if any (so columns on line 0 do not count
// the BOM, while byte offsets do — offsets are always raw file offsets).
//
// # Block tree
//
// Document.Blocks holds the top-level blocks (frontmatter is NOT among them;
// see Document.Frontmatter). Every block type embeds Base and satisfies the
// Block interface. The concrete types, and the blocks they may contain, are:
//
//	*Paragraph       Inlines
//	*Heading         Level 1–6, Inlines, Text (plain), Slug (unique in the document)
//	*ThematicBreak   ---, ***, ___
//	*BlockQuote      Children []Block
//	*Callout         Kind ("note", "warning", … lowercased), Fold, Title inlines, Children
//	*List            Ordered, Start, Tight, Items []*ListItem
//	*ListItem        Task (nil unless a task), Children []Block
//	*CodeBlock       Fenced, Info, Lang, Lines (raw), FirstLine
//	*MathBlock       $$ … $$, Lines (raw)
//	*HTMLBlock       Lines (raw)
//	*CommentBlock    %% … %% or <!-- … --> on their own lines; Hidden by definition
//	*Table           Align, Header, Rows (cells hold Inlines)
//	*FootnoteDef     [^label]: …, Label, Index, Children
//	*LinkRefDef      [label]: dest "title" (not displayed; kept so every line maps to a block)
//
// Renderers should skip *CommentBlock and *LinkRefDef, and may render
// *FootnoteDef at the end of the page using Document.Footnotes (which is
// ordered by Index). Children of ListItem / BlockQuote / Callout / FootnoteDef
// are ordinary blocks, so lists nest by appearing as a child *List of a
// *ListItem. In a tight list (List.Tight) paragraphs of items should be
// rendered without blank lines between them.
//
// Obsidian block ids ("text ^abc-1" at the end of a paragraph or heading,
// or a paragraph holding only "^abc-1" after a table/list/quote) are
// removed from the text and stored in Base.ID of that block, or of the
// preceding sibling block for a standalone id paragraph (which is then
// dropped). A ListItem whose first paragraph carries an id also gets it.
// Ids are [A-Za-z0-9-]+ and must be preceded by a blank.
//
// Details a renderer needs:
//
//   - Callouts: the first line "[!type]+ Title" is consumed; body lines that
//     followed it in the same paragraph become the first child Paragraph.
//     A callout with no title has Title == nil.
//   - Task items: the "[ ]" box is not part of the item's text; an item
//     may be a task with no children ("- [ ]" alone). Toggle by rewriting
//     Source[Task.Offset : Task.Offset+Task.Width].
//   - A line made only of <br> tags becomes a Paragraph of HardBreaks.
//   - An HTML block that is exactly one <!-- … --> comment becomes a
//     CommentBlock (Syntax CommentHTML).
//   - Unclosed constructs degrade to text: emphasis without a closer,
//     "$$" or "%%" never closed later in the file, "[[" without "]]". An
//     unclosed code fence runs to the end of its container (Closed false),
//     as in CommonMark.
//   - Text is passed through byte-for-byte (NUL bytes and invalid UTF-8
//     included); sanitising control characters is the renderer's job.
//   - Container nesting is capped at 64 levels; deeper "> " or list
//     markers are kept as text.
//
// # Inline tree
//
// Inline content is a []Inline. Containers hold Children []Inline. The
// concrete types are:
//
//	*Text          Value (escapes and entities already decoded)
//	*SoftBreak     a line ending inside a paragraph
//	*HardBreak     two trailing spaces, a trailing backslash, or <br>
//	*Emphasis      *a* / _a_
//	*Strong        **a** / __a__   (***a*** is Strong{Emphasis{a}} or Emphasis{Strong{a}})
//	*Strikethrough ~~a~~
//	*Highlight     ==a==
//	*Code          `a` (Value with the CommonMark space stripping applied)
//	*Math          $a$ or $$a$$ inside a paragraph (Display for the latter)
//	*WikiLink      [[Target#Heading|Label]], [[Target#^block]], ![[embed]]
//	*Link          [text](dest "title"), [text][ref], <https://…>, bare URLs
//	*Image         ![alt](src "title")
//	*Tag           #tag, #nested/tag (Name has no '#')
//	*FootnoteRef   [^label]
//	*RawHTML       inline HTML tags, verbatim
//	*Comment       %%…%% or <!-- … --> inside a paragraph (hidden)
//
// Adjacent text is merged into one *Text, and empty texts are dropped.
// Links never contain other links: a bare URL inside link text stays text.
//
// # Helpers
//
// PlainText, Outline, Links, Tags, WordCount, ResolveAnchor, Slugify,
// WalkBlocks, WalkInlines and BlockInlines cover what the reader, the
// outline, the exporter and the vault need; Dump prints a tree for
// debugging.
package md

// Span is a half-open range [Start, End) of byte offsets into
// Document.Source. Every inline node embeds one.
type Span struct {
	Start, End int
}

// SourceSpan returns the span itself; it lets any inline node report its
// position through the Inline interface.
func (s Span) SourceSpan() Span { return s }

// Base is embedded in every block node.
type Base struct {
	// StartLine and EndLine are the 0-based, inclusive source lines the
	// block occupies (container prefixes such as "> " included, trailing
	// blank lines excluded).
	StartLine, EndLine int
	// ID is the Obsidian block id ("abc-1" for "^abc-1"), or "".
	ID string
}

// BlockBase returns the embedded Base so generic code can read positions
// and ids of any block.
func (b *Base) BlockBase() *Base { return b }

// Block is implemented by every block node type listed in the package
// documentation.
type Block interface {
	BlockBase() *Base
	isBlock()
}

// Inline is implemented by every inline node type listed in the package
// documentation.
type Inline interface {
	SourceSpan() Span
	isInline()
}

// Document is the result of Parse.
type Document struct {
	// Source is the input exactly as passed to Parse.
	Source string
	// Frontmatter is the leading YAML block, or nil.
	Frontmatter *Frontmatter
	// Blocks are the top-level blocks after the frontmatter.
	Blocks []Block
	// Footnotes lists every footnote definition, ordered by Index (first
	// reference order; unreferenced definitions follow in source order).
	Footnotes []*FootnoteDef
	// RefDefs maps normalised reference labels to link reference
	// definitions (the first definition of a label wins).
	RefDefs map[string]*LinkRefDef
	// BOM reports a leading UTF-8 byte order mark; CRLF reports that the
	// first line ending in the file is "\r\n". Both are informational for
	// writers that must preserve them.
	BOM, CRLF bool

	lineStarts []int // byte offset where each line's content starts
	lineEnds   []int // byte offset where each line's content ends (before EOL)
}

// Fold is the fold marker of a callout.
type Fold byte

// Callout fold states.
const (
	FoldNone   Fold = 0   // no marker: not foldable
	FoldOpen   Fold = '+' // "[!type]+": foldable, initially open
	FoldClosed Fold = '-' // "[!type]-": foldable, initially folded
)

// Align is a table column alignment taken from the delimiter row.
type Align byte

// Table column alignments.
const (
	AlignNone Align = iota
	AlignLeft
	AlignCenter
	AlignRight
)

// ---------------------------------------------------------------------------
// Blocks

// Paragraph is a run of text lines.
type Paragraph struct {
	Base
	Inlines []Inline
}

// Heading is an ATX ("## x") or setext ("x\n---") heading.
type Heading struct {
	Base
	Level   int  // 1–6
	Setext  bool // underlined form
	Inlines []Inline
	// Text is PlainText(Inlines).
	Text string
	// Slug is the GitHub-style anchor, made unique within the document by
	// appending "-1", "-2", … in document order.
	Slug string
}

// ThematicBreak is a horizontal rule.
type ThematicBreak struct {
	Base
	Marker byte // '-', '*' or '_'
}

// BlockQuote is a "> " quote that is not a callout.
type BlockQuote struct {
	Base
	Children []Block
}

// Callout is an Obsidian callout: a block quote whose first line is
// "[!type]" optionally followed by '+' or '-' and a title.
type Callout struct {
	Base
	// Kind is the type, lowercased ("note", "tip", "warning", …). Unknown
	// kinds are kept as written (lowercased).
	Kind string
	Fold Fold
	// Title holds the title inlines; nil when no title was written (the
	// renderer then shows the capitalised kind).
	Title    []Inline
	Children []Block
}

// List is a bullet or ordered list.
type List struct {
	Base
	Ordered bool
	// Start is the number of the first item of an ordered list.
	Start int
	// Marker is the bullet ('-', '*', '+') or the ordered delimiter ('.', ')').
	Marker byte
	// Tight reports a list with no blank lines between items or their
	// children.
	Tight bool
	Items []*ListItem
}

// ListItem is one item of a List.
type ListItem struct {
	Base
	// Number is the number written on an ordered item (0 for bullets).
	Number int
	// Task is non-nil for a task item ("- [ ] x").
	Task     *Task
	Children []Block
}

// Task describes the checkbox of a task list item. Toggling a task means
// replacing the bytes Source[Offset:Offset+Width] (normally one byte).
type Task struct {
	// State is the character between the brackets: ' ' open, 'x'/'X' done,
	// '/' in progress, '-' cancelled; any other character is kept.
	State rune
	// Line is the 0-based source line and Col the 0-based byte column of
	// the state character within that line.
	Line, Col int
	// Offset is the byte offset of the state character in Document.Source;
	// Width is its length in bytes.
	Offset, Width int
}

// Done reports whether the task is checked ('x' or 'X').
func (t *Task) Done() bool { return t != nil && (t.State == 'x' || t.State == 'X') }

// CodeBlock is a fenced or indented code block.
type CodeBlock struct {
	Base
	Fenced bool
	// Fence is the opening fence as written ("```", "~~~~"), "" if indented.
	Fence string
	// Info is the full info string after the fence; Lang is its first word,
	// lowercased, with Pandoc-style "{.lang}" braces and dots removed.
	Info, Lang string
	// Lines are the raw content lines with container prefixes and fence
	// indentation removed (no line terminators).
	Lines []string
	// FirstLine is the source line of Lines[0] (content lines are
	// contiguous in the source).
	FirstLine int
	// Closed is false for a fence that runs to the end of its container.
	Closed bool
}

// MathBlock is display math between "$$" lines (or "$$ … $$" on one line).
type MathBlock struct {
	Base
	Lines     []string
	FirstLine int
}

// HTMLBlock is raw HTML kept verbatim.
type HTMLBlock struct {
	Base
	Lines []string
}

// CommentSyntax tells which comment syntax produced a comment node.
type CommentSyntax byte

// Comment syntaxes.
const (
	CommentPercent CommentSyntax = iota // %% … %%
	CommentHTML                         // <!-- … -->
)

// CommentBlock is a hidden comment occupying whole lines. Renderers must
// not display it.
type CommentBlock struct {
	Base
	Syntax CommentSyntax
	// Lines are the raw lines, delimiters included.
	Lines []string
}

// Table is a GFM pipe table.
type Table struct {
	Base
	// Align has one entry per column.
	Align  []Align
	Header *TableRow
	// Rows are the body rows; each has exactly len(Align) cells (missing
	// cells are empty, excess cells are dropped).
	Rows []*TableRow
}

// TableRow is one table line.
type TableRow struct {
	Line  int
	Cells []*TableCell
}

// TableCell is one cell; its Span covers the trimmed cell text (empty
// padding cells have an empty span at the end of the row).
type TableCell struct {
	Span
	Inlines []Inline
}

// FootnoteDef is a footnote definition "[^label]: text" with its indented
// continuation.
type FootnoteDef struct {
	Base
	Label string
	// Index is the 1-based number shown for the footnote.
	Index    int
	Children []Block
}

// LinkRefDef is a link reference definition. It is not displayed.
type LinkRefDef struct {
	Base
	Label, Dest, Title string
}

func (*Paragraph) isBlock()     {}
func (*Heading) isBlock()       {}
func (*ThematicBreak) isBlock() {}
func (*BlockQuote) isBlock()    {}
func (*Callout) isBlock()       {}
func (*List) isBlock()          {}
func (*ListItem) isBlock()      {}
func (*CodeBlock) isBlock()     {}
func (*MathBlock) isBlock()     {}
func (*HTMLBlock) isBlock()     {}
func (*CommentBlock) isBlock()  {}
func (*Table) isBlock()         {}
func (*FootnoteDef) isBlock()   {}
func (*LinkRefDef) isBlock()    {}

// ---------------------------------------------------------------------------
// Inlines

// Text is literal text.
type Text struct {
	Span
	Value string
}

// SoftBreak is a line ending inside a paragraph; render as a space.
type SoftBreak struct{ Span }

// HardBreak is a forced line break.
type HardBreak struct{ Span }

// Emphasis is italic text.
type Emphasis struct {
	Span
	Children []Inline
}

// Strong is bold text.
type Strong struct {
	Span
	Children []Inline
}

// Strikethrough is ~~struck~~ text.
type Strikethrough struct {
	Span
	Children []Inline
}

// Highlight is ==highlighted== text.
type Highlight struct {
	Span
	Children []Inline
}

// Code is an inline code span.
type Code struct {
	Span
	Value string
}

// Math is inline TeX, shown verbatim.
type Math struct {
	Span
	Value   string
	Display bool // written as $$…$$
}

// WikiLink is an Obsidian link [[Target#Heading|Label]] or embed ![[…]].
type WikiLink struct {
	Span
	// Target is the note or file name/path as written ("" for a link to a
	// heading of the same note, e.g. [[#Heading]]).
	Target string
	// Heading is the text after '#' (may itself contain '#' for nested
	// headings); Block is the id after "#^". At most one is non-empty.
	Heading, Block string
	// Label is the text after '|' ("" when absent; see HasLabel).
	Label    string
	HasLabel bool
	// Embed is true for "![[…]]".
	Embed bool
}

// Display returns the text a reader should show for the link: the label,
// else the target (plus " > heading" when present).
func (w *WikiLink) Display() string {
	if w.HasLabel && w.Label != "" {
		return w.Label
	}
	s := w.Target
	switch {
	case w.Heading != "" && s != "":
		s += " > " + w.Heading
	case w.Heading != "":
		s = w.Heading
	case w.Block != "" && s != "":
		s += " > ^" + w.Block
	case w.Block != "":
		s = "^" + w.Block
	}
	return s
}

// LinkKind tells how a Link was written.
type LinkKind byte

// Link kinds.
const (
	LinkInline    LinkKind = iota // [text](dest)
	LinkReference                 // [text][ref], [text][], [text]
	LinkAutolink                  // <https://…>, <a@b.c>
	LinkBare                      // https://…, www.… in running text
)

// Link is a Markdown link.
type Link struct {
	Span
	Kind LinkKind
	// Dest is the destination with escapes and entities decoded (for a
	// "www." bare link "http://" is prefixed; for an e-mail autolink
	// "mailto:" is prefixed).
	Dest  string
	Title string
	// External is true when Dest has a URI scheme (https:, mailto:, …).
	External bool
	// Path and Fragment are set for non-external destinations: the
	// percent-decoded path ("sub/My Note.md") and the decoded part after
	// '#' ("Heading"), either may be empty.
	Path, Fragment string
	// Label is the normalised reference label for LinkReference.
	Label    string
	Children []Inline
}

// Image is a Markdown image ![alt](src).
type Image struct {
	Span
	Src, Title     string
	External       bool
	Path, Fragment string
	Alt            []Inline
}

// Tag is an Obsidian tag; Name excludes the leading '#'.
type Tag struct {
	Span
	Name string
}

// FootnoteRef is a footnote reference [^label].
type FootnoteRef struct {
	Span
	Label string
	// Index is the footnote number, or 0 when no definition exists.
	Index int
}

// RawHTML is an inline HTML tag kept verbatim.
type RawHTML struct {
	Span
	Raw string
}

// Comment is an inline comment; it must not be displayed.
type Comment struct {
	Span
	Syntax CommentSyntax
	Value  string // the text between the delimiters
}

func (*Text) isInline()          {}
func (*SoftBreak) isInline()     {}
func (*HardBreak) isInline()     {}
func (*Emphasis) isInline()      {}
func (*Strong) isInline()        {}
func (*Strikethrough) isInline() {}
func (*Highlight) isInline()     {}
func (*Code) isInline()          {}
func (*Math) isInline()          {}
func (*WikiLink) isInline()      {}
func (*Link) isInline()          {}
func (*Image) isInline()         {}
func (*Tag) isInline()           {}
func (*FootnoteRef) isInline()   {}
func (*RawHTML) isInline()       {}
func (*Comment) isInline()       {}

// InlineChildren returns the children of a container inline (Emphasis,
// Strong, Strikethrough, Highlight, Link, and the Alt of Image), or nil.
func InlineChildren(n Inline) []Inline {
	switch n := n.(type) {
	case *Emphasis:
		return n.Children
	case *Strong:
		return n.Children
	case *Strikethrough:
		return n.Children
	case *Highlight:
		return n.Children
	case *Link:
		return n.Children
	case *Image:
		return n.Alt
	}
	return nil
}

// BlockChildren returns the child blocks of a container block (BlockQuote,
// Callout, List items, ListItem, FootnoteDef), or nil.
func BlockChildren(b Block) []Block {
	switch b := b.(type) {
	case *BlockQuote:
		return b.Children
	case *Callout:
		return b.Children
	case *ListItem:
		return b.Children
	case *FootnoteDef:
		return b.Children
	case *List:
		out := make([]Block, len(b.Items))
		for i, it := range b.Items {
			out[i] = it
		}
		return out
	}
	return nil
}
