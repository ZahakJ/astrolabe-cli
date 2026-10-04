package vault

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func parse(s string) *Note {
	return parseNote("dir/My Note.md", []byte(s), time.Time{}, int64(len(s)))
}

func TestTitleRules(t *testing.T) {
	cases := []struct {
		src, title string
		line       int
	}{
		{"---\ntitle: From FM\n---\n# Heading\n", "From FM", 2},
		{"---\ntitle: \"Quoted\"\n---\n", "Quoted", 2},
		{"intro\n\n# First H1\n# Second\n", "First H1", 3},
		{"## Only H2\n", "My Note", 0},
		{"Setext Title\n============\n\nbody\n", "Setext Title", 1},
		{"```\n# not a heading\n```\n", "My Note", 0},
		{"", "My Note", 0},
	}
	for _, c := range cases {
		n := parse(c.src)
		if n.Title != c.title || n.TitleLine != c.line {
			t.Errorf("%q: title %q line %d, want %q line %d", c.src, n.Title, n.TitleLine, c.title, c.line)
		}
	}
}

func TestFrontmatter(t *testing.T) {
	src := "---\n" +
		"title: Trip\n" +
		"tags: [travel, \"#plans/2026\"]\n" +
		"aliases:\n  - Journey\n  - 'The Trip'\n" +
		"date: 2026-10-03\n" +
		"rating: 5\n" +
		"---\n" +
		"Body #inline\n"
	n := parse(src)
	if !reflect.DeepEqual(n.Tags, []string{"travel", "plans/2026", "inline"}) {
		t.Errorf("tags %v", n.Tags)
	}
	if !reflect.DeepEqual(n.Aliases, []string{"Journey", "The Trip"}) {
		t.Errorf("aliases %v", n.Aliases)
	}
	if n.DateRaw != "2026-10-03" || n.Date.Year() != 2026 || n.Date.Day() != 3 {
		t.Errorf("date %q %v", n.DateRaw, n.Date)
	}
	if n.FrontmatterEnd != 9 {
		t.Errorf("frontmatter end %d", n.FrontmatterEnd)
	}
	var keys []string
	for _, p := range n.Properties {
		keys = append(keys, p.Key+"="+p.Value)
	}
	want := []string{"title=Trip", "tags=travel, #plans/2026", "aliases=Journey, The Trip", "date=2026-10-03", "rating=5"}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("properties %v", keys)
	}
	// String-form tags and created fallback.
	n = parse("---\ntags: a, b c\ncreated: 2025-01-02 10:30\n---\n")
	if !reflect.DeepEqual(n.Tags, []string{"a", "b", "c"}) || n.Date.Hour() != 10 {
		t.Errorf("tags %v date %v", n.Tags, n.Date)
	}
	// Unclosed frontmatter is body text.
	n = parse("---\ntitle: x\n# Real\n")
	if n.Title != "Real" || n.FrontmatterEnd != 0 {
		t.Errorf("unclosed: %q %d", n.Title, n.FrontmatterEnd)
	}
}

func TestHeadings(t *testing.T) {
	src := "# One #\n## Two ^blk\nText\n---\n####### seven\n#nospace\n   ### Three\n    # code\n"
	n := parse(src)
	got := []Heading{}
	got = append(got, n.Headings...)
	want := []Heading{{1, "One", 1}, {2, "Two", 2}, {2, "Text", 3}, {3, "Three", 7}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("headings %+v", got)
	}
	if len(n.Blocks) != 1 || n.Blocks[0] != (Block{"blk", 2}) {
		t.Errorf("blocks %+v", n.Blocks)
	}
	// "---" after a list item is a rule, not a setext heading.
	n = parse("- item\n---\n")
	if len(n.Headings) != 0 {
		t.Errorf("list setext: %+v", n.Headings)
	}
}

func TestFencesAndCodeIgnored(t *testing.T) {
	src := "Real [[Link]] #tag\n" +
		"```go\n[[Fake]] #fake\n- [ ] fake task\n# fake heading\n```\n" +
		"~~~~\n```\n[[Fake2]]\n~~~~\n" +
		"- item\n  ```\n  [[Fake3]]\n  ```\n" +
		"> ```\n> [[Fake4]]\n> ```\n" +
		"Inline `[[Fake5]] #fake` and ``a ` [[Fake6]]`` text\n" +
		"%% [[Fake7]] %% and <!-- #fake --> visible [[Real2]]\n" +
		"%%\n[[Fake8]]\n%%\n" +
		"<!--\n#fake\n-->\n"
	n := parse(src)
	var targets []string
	for _, l := range n.Links {
		targets = append(targets, l.Target)
	}
	if !reflect.DeepEqual(targets, []string{"Link", "Real2"}) {
		t.Errorf("links %v", targets)
	}
	if !reflect.DeepEqual(n.Tags, []string{"tag"}) {
		t.Errorf("tags %v", n.Tags)
	}
	if len(n.Tasks) != 0 || len(n.Headings) != 0 {
		t.Errorf("tasks %v headings %v", n.Tasks, n.Headings)
	}
}

func TestLinks(t *testing.T) {
	src := "See [[Note]], [[folder/Other|label]], [[Note#Sec tion]], [[Note#^abc]], [[#Local]].\n" +
		"Embed ![[img.png]] and ![[Doc#Part]].\n" +
		"Md [text](rel/path.md) [sp](My%20Note.md#Head) [ext](https://example.com/a_(b)) ![alt](pics/a.png)\n" +
		"Bare https://example.org/x?y=1. and <mailto:someone@example.com> and [anchor](#top)\n" +
		"| [[Table\\|alias]] |\n"
	n := parse(src)
	type L struct {
		Kind                          LinkKind
		Target, Heading, Block, Label string
		Line, Col                     int
	}
	var got []L
	for _, l := range n.Links {
		got = append(got, L{l.Kind, l.Target, l.Heading, l.Block, l.Label, l.Line, l.Col})
	}
	want := []L{
		{KindWikilink, "Note", "", "", "", 1, 4},
		{KindWikilink, "folder/Other", "", "", "label", 1, 14},
		{KindWikilink, "Note", "Sec tion", "", "", 1, 38},
		{KindWikilink, "Note", "", "abc", "", 1, 57},
		{KindWikilink, "", "Local", "", "", 1, 72},
		{KindEmbed, "img.png", "", "", "", 2, 6},
		{KindEmbed, "Doc", "Part", "", "", 2, 23},
		{KindMarkdown, "rel/path.md", "", "", "text", 3, 3},
		{KindMarkdown, "My Note.md", "Head", "", "sp", 3, 23},
		{KindURL, "https://example.com/a_(b)", "", "", "ext", 3, 47},
		{KindEmbed, "pics/a.png", "", "", "alt", 3, 80},
		{KindURL, "https://example.org/x?y=1", "", "", "", 4, 5},
		{KindURL, "mailto:someone@example.com", "", "", "", 4, 36},
		{KindMarkdown, "", "top", "", "anchor", 4, 69},
		{KindWikilink, "Table", "", "", "alias", 5, 2},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d links: %+v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("link %d: got %+v want %+v", i, got[i], want[i])
		}
	}
	if n.Links[0].Context != strings.TrimSpace(strings.Split(src, "\n")[0]) {
		t.Errorf("context %q", n.Links[0].Context)
	}
	if !n.Links[0].Wiki || n.Links[7].Wiki {
		t.Error("Wiki flag")
	}
}

func TestTags(t *testing.T) {
	src := "#top line #nested/deep/ and #تأمل and #2026 and #v2 issue#3\n" +
		"url https://x.com/#frag and [x](http://y/#a) and `#code`\n" +
		"## heading #inhead\n" +
		"#Top again (case)\n"
	n := parse(src)
	want := []string{"top", "nested/deep", "تأمل", "v2", "inhead"}
	if !reflect.DeepEqual(n.Tags, want) {
		t.Errorf("tags %v want %v", n.Tags, want)
	}
}

func TestTasks(t *testing.T) {
	src := "- [ ] plain\n" +
		"  * [x] done 📅 2026-10-05 ⏫\n" +
		"1. [/] progress due:2026-01-02 !!\n" +
		"> - [-] cancelled @due(2026-03-04) 🔽\n" +
		"- [ ] urgent !!! ^id1\n" +
		"- [?] custom\n" +
		"- [ ]\n" +
		"- [] not a task\n" +
		"-[ ] not a task\n" +
		"- [ ]not a task\n" +
		"Hello! not priority\n"
	n := parse(src)
	type T struct {
		State     rune
		Text      string
		Line, Col int
		Due       string
		Prio      int
	}
	var got []T
	for _, k := range n.Tasks {
		got = append(got, T{k.State, k.Text, k.Line, k.Col, k.Due.String(), k.Priority})
	}
	want := []T{
		{' ', "plain", 1, 3, "", 0},
		{'x', "done", 2, 5, "2026-10-05", 2},
		{'/', "progress", 3, 4, "2026-01-02", 2},
		{'-', "cancelled", 4, 5, "2026-03-04", -1},
		{' ', "urgent", 5, 3, "", 3},
		{'?', "custom", 6, 3, "", 0},
		{' ', "", 7, 3, "", 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tasks\n got %+v\nwant %+v", got, want)
	}
	if n.Tasks[1].Raw != "done 📅 2026-10-05 ⏫" {
		t.Errorf("raw %q", n.Tasks[1].Raw)
	}
	if !n.Tasks[2].Open() || n.Tasks[1].Open() || n.Tasks[3].Open() || !n.Tasks[3].Cancelled() {
		t.Error("Open/Cancelled")
	}
}

func TestCRLFAndBOM(t *testing.T) {
	src := "\uFEFF---\r\ntitle: Win\r\ntags: [a]\r\n---\r\n# H\r\n- [ ] task [[Link]]\r\n"
	n := parse(src)
	if n.Title != "Win" || len(n.Tags) != 1 || n.FrontmatterEnd != 4 {
		t.Fatalf("title %q tags %v fm %d", n.Title, n.Tags, n.FrontmatterEnd)
	}
	if len(n.Headings) != 1 || n.Headings[0].Text != "H" || n.Headings[0].Line != 5 {
		t.Errorf("headings %+v", n.Headings)
	}
	if len(n.Tasks) != 1 || n.Tasks[0].Text != "task [[Link]]" || n.Tasks[0].Line != 6 {
		t.Errorf("tasks %+v", n.Tasks)
	}
	if len(n.Links) != 1 || n.Links[0].Target != "Link" {
		t.Errorf("links %+v", n.Links)
	}
	if n.Lines != 6 {
		t.Errorf("lines %d", n.Lines)
	}
}

func TestWordsAndLarge(t *testing.T) {
	n := parse("---\ntitle: not counted words here\n---\nOne two, three — 4 كلمة\n")
	if n.Words != 5 {
		t.Errorf("words %d", n.Words)
	}
	big := "# Big One\n" + strings.Repeat("filler [[Link]] #tag\n", LargeFileSize/20+10)
	n = parseNote("big.md", []byte(big), time.Time{}, int64(len(big)))
	if !n.Large || n.Title != "Big One" || len(n.Links) != 0 || len(n.Tags) != 0 {
		t.Errorf("large: %v %q %d %d", n.Large, n.Title, len(n.Links), len(n.Tags))
	}
}

func TestLoneBlockID(t *testing.T) {
	n := parse("Para text\n^lone\n")
	if len(n.Blocks) != 1 || n.Blocks[0].Line != 1 {
		t.Errorf("%+v", n.Blocks)
	}
}
