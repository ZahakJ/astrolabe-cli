// Package vault is folio's notes store: it finds the vault root, scans the
// tree of Markdown files into an in-memory index (titles, aliases, tags,
// headings, links, tasks), resolves links the way Obsidian does, searches
// file contents, manages daily notes and capture, and performs every write
// atomically with conflict detection.
//
// Nothing is persisted inside the vault except the notes themselves: the
// index lives in memory for one run (DESIGN.md §1, §3). The package depends
// only on the standard library; it deliberately uses a light line scanner
// that understands code fences and frontmatter rather than a full Markdown
// parse.
//
// Line numbers throughout the package are 1-based, matching the
// path:line:text convention of the CLI. Columns are 0-based byte offsets
// into the line (without its line terminator).
package vault

import (
	"fmt"
	"time"
)

// LargeFileSize is the size above which a note is indexed by title only.
const LargeFileSize = 2 << 20

// Note is the indexed metadata of one Markdown file. Notes handed out by a
// Vault are immutable snapshots: a refresh replaces the pointer rather than
// mutating it, so callers may keep and read them from any goroutine but must
// not modify them.
type Note struct {
	// Path is the vault-relative path with forward slashes, including the
	// ".md" extension. It is the note's identity.
	Path string
	// Title is the frontmatter title, else the first H1, else the file name
	// without ".md".
	Title string
	// TitleLine is the 1-based line the title came from (the frontmatter
	// title key or the H1), or 0 when the title is the file name.
	TitleLine int
	// Aliases are the frontmatter aliases, in order.
	Aliases []string
	// Tags are the note's tags without the leading '#', frontmatter tags
	// first then body tags, de-duplicated case-insensitively (the first
	// spelling wins).
	Tags []string
	// ModTime and Size are from the file's stat at index time.
	ModTime time.Time
	Size    int64
	// Words is the number of words in the body (frontmatter excluded).
	Words int
	// Lines is the number of lines in the file.
	Lines int
	// Headings in document order.
	Headings []Heading
	// Links are the outgoing links in document order.
	Links []Link
	// Tasks are the task list items in document order.
	Tasks []Task
	// Blocks are the ^block-id anchors in document order.
	Blocks []Block
	// Properties are all frontmatter key/value pairs as written (values
	// unquoted; list values joined with ", "), for display. The file is
	// never re-serialised from these.
	Properties []Property
	// FrontmatterEnd is the 1-based line of the closing frontmatter
	// delimiter, or 0 when the note has no frontmatter.
	FrontmatterEnd int
	// Date is the frontmatter "date" (else "created") parsed, or the zero
	// time. DateRaw is the value as written.
	Date    time.Time
	DateRaw string
	// Large reports that the file exceeded LargeFileSize and was indexed by
	// title only (no links, tags, tasks or headings).
	Large bool
}

// Heading is an ATX or setext heading.
type Heading struct {
	Level int    // 1..6
	Text  string // heading text without markers or a trailing ^block id
	Line  int    // 1-based
}

// Block is an Obsidian block anchor ("text ^id").
type Block struct {
	ID   string // without the caret
	Line int    // 1-based line of the block the anchor belongs to
}

// Property is one frontmatter key with its raw value.
type Property struct {
	Key   string
	Value string
	Line  int // 1-based line of the key
}

// LinkKind classifies a Link.
type LinkKind int

const (
	// KindWikilink is [[Target]], [[Target|label]], [[Target#Heading]].
	KindWikilink LinkKind = iota
	// KindMarkdown is [label](relative/path.md) (a link into the vault).
	KindMarkdown
	// KindEmbed is ![[Target]] or ![alt](path) pointing into the vault.
	KindEmbed
	// KindURL is an external link: [label](scheme:...), <scheme:...> or a
	// bare http(s) URL.
	KindURL
)

// String returns "wikilink", "markdown", "embed" or "url".
func (k LinkKind) String() string {
	switch k {
	case KindWikilink:
		return "wikilink"
	case KindMarkdown:
		return "markdown"
	case KindEmbed:
		return "embed"
	case KindURL:
		return "url"
	}
	return fmt.Sprintf("LinkKind(%d)", int(k))
}

// Link is one outgoing link.
type Link struct {
	Kind LinkKind
	// Target is the link destination without any #heading or #^block part:
	// a note name or path for wikilinks, a URL-decoded relative path for
	// Markdown links, the URL itself for KindURL. It is empty for a link to
	// a heading or block of the same note ([[#Heading]], [x](#heading)).
	Target string
	// Heading is the heading part (after '#'), Block the block id part
	// (after '#^'); at most one is set. For nested headings ("A#B") Heading
	// holds the whole chain; resolution uses the last segment.
	Heading string
	Block   string
	// Label is the display text: the part after '|' in a wikilink, the
	// bracket text of a Markdown link, empty when none was written.
	Label string
	// Embed is true for ![[...]] and ![...](...) forms; for links into the
	// vault Kind is then KindEmbed.
	Embed bool
	// Wiki is true when the link was written in [[...]] form (wikilink or
	// wiki embed); such targets resolve from the vault root first, while
	// Markdown link paths resolve from the linking note's folder first.
	Wiki bool
	// Line is 1-based; Col is the 0-based byte offset of the link's first
	// byte ('!' or '[' or the URL's first character).
	Line, Col int
	// Context is the source line containing the link, trimmed of
	// surrounding whitespace (used for backlink context).
	Context string
}

// Task is a task list item.
type Task struct {
	// State is the character between the brackets: ' ' open, 'x'/'X' done,
	// '/' in progress, '-' cancelled; other characters are kept as written.
	State rune
	// Text is the item text with the due date, priority markers and block
	// id removed and spaces collapsed. Raw is the text after "] " as
	// written.
	Text string
	Raw  string
	// Line is 1-based; Col is the 0-based byte offset of the state
	// character in the line.
	Line, Col int
	// Due is the due date, zero when none.
	Due Date
	// Priority: 3 highest (!!! or 🔺), 2 high (!! or ⏫), 1 medium (! or 🔼),
	// 0 none, -1 low (🔽), -2 lowest (⏬).
	Priority int
}

// Done reports whether the task is completed ('x' or 'X').
func (t Task) Done() bool { return t.State == 'x' || t.State == 'X' }

// Cancelled reports whether the task is cancelled ('-').
func (t Task) Cancelled() bool { return t.State == '-' }

// Open reports whether the task still needs doing (neither done nor
// cancelled; in-progress counts as open).
func (t Task) Open() bool { return !t.Done() && !t.Cancelled() }

// Date is a calendar date without time or zone, used for due dates.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// DateOf returns the calendar date of t in t's location.
func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{y, m, d}
}

// ParseDate parses "YYYY-MM-DD".
func ParseDate(s string) (Date, bool) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return Date{}, false
	}
	return DateOf(t), true
}

// IsZero reports whether d is the zero Date (no date).
func (d Date) IsZero() bool { return d.Year == 0 && d.Month == 0 && d.Day == 0 }

// Compare returns -1, 0 or +1 as d is before, equal to or after e.
func (d Date) Compare(e Date) int {
	switch {
	case d.Year != e.Year:
		return cmpInt(d.Year, e.Year)
	case d.Month != e.Month:
		return cmpInt(int(d.Month), int(e.Month))
	default:
		return cmpInt(d.Day, e.Day)
	}
}

// Before reports whether d is strictly before e.
func (d Date) Before(e Date) bool { return d.Compare(e) < 0 }

// Time returns midnight of d in loc.
func (d Date) Time(loc *time.Location) time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, loc)
}

// String formats d as "YYYY-MM-DD", or "" for the zero Date.
func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
