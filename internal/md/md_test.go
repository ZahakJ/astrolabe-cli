package md

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// The expected trees use the Dump format (without positions): one line per
// top-level block; text in quotes; em/strong/del/mark/code/math/wiki/link/
// url/autolink/reflink/img/tag/fn/rawhtml/hidden for inlines; sb and br for
// soft and hard breaks; {…} for child blocks; ^id for block ids.
var shapeCases = []struct {
	name, in, want string
}{
	// Documents with little or nothing in them.
	{"empty", "", ""},
	{"blank lines", "\n\n  \n", ""},
	{"frontmatter only", "---\ntitle: x\n---\n", `frontmatter{title="x" tags=[] aliases=[] date="" title:"x"}`},
	{"empty frontmatter", "---\n---\n", `frontmatter{title="" tags=[] aliases=[] date=""}`},
	{"unclosed frontmatter is a rule", "---\ntitle: x\n", "hr\np[\"title: x\"]"},

	// Headings.
	{"atx", "# ATX #\n## closing ##   \n###### six\n####### seven\n#notheading",
		`h1["ATX"]#atx
h2["closing"]#closing
h6["six"]#six
p["####### seven" sb tag(notheading)]`},
	{"setext", "Setext\n===\n\nH2\n--\n", "h1[\"Setext\"]#setext\nh2[\"H2\"]#h2"},
	{"setext multi-line", "a\nb\n---", `h2["a" sb "b"]#a-b`},
	{"setext after refdef", "[r]: /url\nSetext\n---", "refdef(r=/url \"\")\nh2[\"Setext\"]#setext"},
	{"heading slugs unique", "# A b\n# A b\n# A-b", "h1[\"A b\"]#a-b\nh1[\"A b\"]#a-b-1\nh1[\"A-b\"]#a-b-2"},
	{"heading slug drops punctuation", "## What's *new*? (v2)", `h2["What's " em["new"] "? (v2)"]#whats-new-v2`},
	{"heading block id", "# Heading ^hid", `h1["Heading"]#heading^hid`},
	{"arabic heading slug", "# مرحبا بالعالم", `h1["مرحبا بالعالم"]#مرحبا-بالعالم`},
	{"empty heading", "#\n", `h1[]#`},

	// Paragraphs, breaks, escapes, entities.
	{"soft and hard breaks", "Hello  \nworld\\\nagain<br/>\nend", `p["Hello" br "world" br "again" br "end"]`},
	{"escapes", `\*not\* \[x\] \# \\ \a`, `p["*not* [x] # \\ \\a"]`},
	{"entities", "&amp; &copy; &#65; &#x41; &bogus; &#0;", `p["& © A A &bogus; �"]`},
	{"br only line", "<br>\n\ntext", "p[br]\np[\"text\"]"},
	{"indented continuation is not code", "para\n    not code", `p["para" sb "not code"]`},

	// Emphasis family.
	{"emphasis kinds", "*a* _b_ **c** __d__ ***e*** ~~f~~ ==g==",
		`p[em["a"] " " em["b"] " " strong["c"] " " strong["d"] " " em[strong["e"]] " " del["f"] " " mark["g"]]`},
	{"nested emphasis", "*a **b** c*", `p[em["a " strong["b"] " c"]]`},
	{"mixed nesting", "mixed **bold *both* bold** end", `p["mixed " strong["bold " em["both"] " bold"] " end"]`},
	{"intraword underscore", "snake_case_word and _emph_ and __strong__", `p["snake_case_word and " em["emph"] " and " strong["strong"]]`},
	{"intraword star", "*foo*bar **foo**bar __foo__bar _foo_bar", `p[em["foo"] "bar " strong["foo"] "bar __foo__bar _foo_bar"]`},
	{"unterminated emphasis", "**unterminated *emph", `p["**unterminated *emph"]`},
	{"unterminated across lines", "*foo **bar\nbaz", `p["*foo **bar" sb "baz"]`},
	{"highlight needs flanking", "a == b == c and ==x==", `p["a == b == c and " mark["x"]]`},
	{"strike needs two tildes", "~one~ ~~two~~ ~~~three~~~", `p["~one~ " del["two"] " ~~~three~~~"]`},

	// Code spans.
	{"code spans", "``code `with` ticks``  and ` x `", `p[code"code ` + "`with`" + ` ticks" "  and " code"x"]`},
	{"code protects markup", "`code with $math$ and #tag and [[link]]`", `p[code"code with $math$ and #tag and [[link]]"]`},
	{"unclosed backticks", "a `b ``c", "p[\"a `b ``c\"]"},

	// Wikilinks.
	{"wikilinks", "[[a|b]] [[Note#^abc]] [[#Local]] [[folder/N#H#Sub]] [[a]b]] [[]]",
		`p[wiki(a|b) " " wiki(Note#^abc) " " wiki(#Local) " " wiki(folder/N#H#Sub) " [[a]b]] [[]]"]`},
	{"embeds", "![[Note#Heading|alias]] and ![[pic.png|200]]", `p[!wiki(Note#Heading|alias) " and " !wiki(pic.png|200)]`},
	{"wikilink not across lines", "[[a\nb]]", `p["[[a" sb "b]]"]`},

	// Markdown links and images.
	{"inline links", "[x](<my note.md>) [y](a\\)b) [z](foo(bar)) [w]() [t](https://x.io \"title\" )",
		`p[link(my note.md)["x"] " " link(a)b)["y"] " " link(foo(bar))["z"] " " link()["w"] " " link(https://x.io "title")["t"]]`},
	{"invalid link stays text", "[bad](a b)", `p["[bad](a b)"]`},
	{"reference links", "[ref] and [text][ref] and [ref][] ![alt *x*](i.png \"t\")\n\n[ref]: https://r.example \"Title\"",
		`p[reflink(https://r.example "Title")["ref"] " and " reflink(https://r.example "Title")["text"] " and " reflink(https://r.example "Title")["ref"] " " img(i.png "t")["alt " em["x"]]]
refdef(ref=https://r.example "Title")`},
	{"undefined reference", "[nope] and [a][nope]", `p["[nope] and [a][nope]"]`},
	{"links do not nest", "[link [inner](b)](a)", `p["[link " link(b)["inner"] "](a)"]`},
	{"url inside link text", "[https://x.com/a](https://x.com/b)", `p[link(https://x.com/b)["https://x.com/a"]]`},
	{"autolinks and html", "<a@b.co> <http://x.y/z?q=1> <span class=\"x\">hi</span> a<br>b <!-- c --> d",
		`p[autolink(mailto:a@b.co)["a@b.co"] " " autolink(http://x.y/z?q=1)["http://x.y/z?q=1"] " " rawhtml"<span class=\"x\">" "hi" rawhtml"</span>" " a" br "b " hidden" c " " d"]`},
	{"not a tag", "hello <b>bold</b> <notatag", `p["hello " rawhtml"<b>" "bold" rawhtml"</b>" " <notatag"]`},
	{"bare urls trim punctuation", "www.example.com, and (https://x.io/a_(b)) and https://x.io/a.",
		`p[url(http://www.example.com)["www.example.com"] ", and (" url(https://x.io/a_(b))["https://x.io/a_(b)"] ") and " url(https://x.io/a)["https://x.io/a"] "."]`},
	{"bare url with fragment is not a tag", "https://x.com/#frag and http://a.b/c#d",
		`p[url(https://x.com/#frag)["https://x.com/#frag"] " and " url(http://a.b/c#d)["http://a.b/c#d"]]`},
	{"bare url needs a host", "http://localhost:8080/x and https://", `p[url(http://localhost:8080/x)["http://localhost:8080/x"] " and https://"]`},

	// Tags.
	{"tags", "#tag #1 #a1 C#sharp (#paren) #/bad #ok/ x#no", `p[tag(tag) " #1 " tag(a1) " C#sharp (" tag(paren) ") #/bad " tag(ok) "/ x#no"]`},
	{"tag charset", "emoji 🎉 and #tag🎉 and #tag_1-x and #2026-10", `p["emoji 🎉 and " tag(tag) "🎉 and " tag(tag_1-x) " and #2026-10"]`},
	{"arabic tags", "**ابدأ** النص *مائل* #الوسم/فرعي", `p[strong["ابدأ"] " النص " em["مائل"] " " tag(الوسم/فرعي)]`},
	{"line-start tag is not a heading", "#heading-like-tag\n## Real", "p[tag(heading-like-tag)]\nh2[\"Real\"]#real"},

	// Math.
	{"inline math and prices", "$$E=mc^2$$ inline and $a$b and \\$5 and $ x$ and $x $ and $5 and $10",
		`p[dmath"E=mc^2" " inline and " math"a" "b and $5 and $ x$ and $x $ and $5 and $10"]`},
	{"math block", "$$\na\nb\n$$", `math"a\nb"`},
	{"one-line math block", "$$ x^2 $$", `math" x^2 "`},
	{"unclosed math is text", "$$ not closed\nmore", `p["$$ not closed" sb "more"]`},
	{"math in list", "- $$\n  x\n  $$", `ul(-,tight){li{math"x"}}`},

	// Comments.
	{"percent comments", "%%one-line%%\ntext %%inline%% more", "comment\"%%one-line%%\"\np[\"text \" hidden\"inline\" \" more\"]"},
	{"multi-paragraph percent comment", "a\n\n%%\nmulti\n\npara comment\n%%\n\nb", "p[\"a\"]\ncomment\"%%\\nmulti\\n\\npara comment\\n%%\"\np[\"b\"]"},
	{"html comment block", "<!--\nmulti\nline\n-->\nafter", "comment\"<!--\\nmulti\\nline\\n-->\"\np[\"after\"]"},
	{"html block", "<div>\nhtml\n</div>\n\nafter", "html\"<div>\\nhtml\\n</div>\"\np[\"after\"]"},
	{"unclosed percent is text", "50%% off\n\n%% never closed", "p[\"50%% off\"]\np[\"%% never closed\"]"},
	{"inline percent pair across lines", "a %%b\nc%% d", `p["a " hidden"b\nc" " d"]`},

	// Block quotes and callouts.
	{"quote with lazy line and nesting", "> quote\nlazy\n> > nested", `quote{p["quote" sb "lazy"] quote{p["nested"]}}`},
	{"quote then code", "> quote\n\n    code", "quote{p[\"quote\"]}\ncode()\"code\""},
	{"callout", "> [!NOTE]\n> body", `callout(note){p["body"]}`},
	{"callout title and fold", "> [!faq]+ Open?\n\n> [!x]-", "callout(faq+)[\"Open?\"]{}\ncallout(x-){}"},
	{"callout title inlines", "> [!warning]- Be *careful*\n> body line\n> - item",
		`callout(warning-)["Be " em["careful"]]{p["body line"] ul(-,tight){li{p["item"]}}}`},
	{"callout with table", "> [!tip] Title\n> | a | b |\n> |---|---|\n> | 1 | 2 |",
		`callout(tip)["Title"]{table(-,-){["a"]|["b"]}{["1"]|["2"]}}`},
	{"callout in list", "- > [!warning]\n  > inside list\n- next", `ul(-,tight){li{callout(warning){p["inside list"]}} li{p["next"]}}`},
	{"task in callout", "> [!info]+\n> - [ ] task in callout", `callout(info+){ul(-,tight){li[ ]{p["task in callout"]}}}`},
	{"not a callout", "> [!not a callout]\n> text", `quote{p["[!not a callout]" sb "text"]}`},
	{"callout title then rule", "> [!note] T\n> ---\n> body", `callout(note)["T"]{hr p["body"]}`},
	{"nested callout", "> [!note]\n> > [!tip] Inner\n> > text", `callout(note){callout(tip)["Inner"]{p["text"]}}`},

	// Lists.
	{"tight vs loose", "- a\n- b\n\n- c", `ul(-,loose){li{p["a"]} li{p["b"]} li{p["c"]}}`},
	{"ordered start and delimiter", "1. one\n2. two\n\n5) five", "ol(1.,tight){li{p[\"one\"]} li{p[\"two\"]}}\nol(5),tight){li{p[\"five\"]}}"},
	{"only 1. interrupts a paragraph", "text\n2. not a list\n1. list", "p[\"text\" sb \"2. not a list\"]\nol(1.,tight){li{p[\"list\"]}}"},
	{"nested list with code", "1. x\n   - y\n     ```js\n     z\n     ```\n   - w",
		`ol(1.,tight){li{p["x"] ul(-,tight){li{p["y"] code(js)"z"} li{p["w"]}}}}`},
	{"code block in loose item", "1. a\n\n   ```\n   code\n   ```\n2. b", `ol(1.,loose){li{p["a"] code()"code"} li{p["b"]}}`},
	{"fence as first item content", "- ```\n  code in item\n  ```\n- b", `ul(-,tight){li{code()"code in item"} li{p["b"]}}`},
	{"list in quote with lazy line", "> - a\n>   - b\n> c", `quote{ul(-,tight){li{p["a"] ul(-,tight){li{p["b" sb "c"]}}}}}`},
	{"two blank lines", "- a\n\n\n- b", `ul(-,loose){li{p["a"]} li{p["b"]}}`},
	{"thematic breaks vs lists", "* * *\n___\n- - -", "hr\nhr\nhr"},
	{"tabs", "-\tone\n\t-\ttwo", `ul(-,tight){li{p["one"] ul(-,tight){li{p["two"]}}}}`},

	// Tasks.
	{"task states", "- [ ]\n- [/] doing\n- [-] cancelled\n- [?] q\n- [ab] no\n- [x]no\n- [X] Done",
		`ul(-,tight){li[ ]{} li[/]{p["doing"]} li[-]{p["cancelled"]} li[?]{p["q"]} li{p["[ab] no"]} li{p["[x]no"]} li[X]{p["Done"]}}`},
	{"task with inlines and continuation", "- [ ] task with **bold** and [[link]]\n  continuation",
		`ul(-,tight){li[ ]{p["task with " strong["bold"] " and " wiki(link) sb "continuation"]}}`},
	{"ordered task", "1. [x] done", `ol(1.,tight){li[x]{p["done"]}}`},
	{"link is not a task", "- [a](b) text", `ul(-,tight){li{p[link(b)["a"] " text"]}}`},

	// Code blocks.
	{"fenced", "```go\nfunc main() {}\n```", `code(go)"func main() {}"`},
	{"tilde fence with attrs", "~~~py {.x}\nprint()\n~~~~", `code(py)"print()"`},
	{"pandoc lang", "```{.Python}\nx\n```", `code(python)"x"`},
	{"unterminated fence", "```\nunclosed\n\n# not heading", `code()"unclosed\n\n# not heading"!unclosed`},
	{"indented code", "    code\n      more\n\n    end", `code()"code\n  more\n\nend"`},
	{"tab indented code", "\t\tdeep tab code", `code()"\tdeep tab code"`},
	{"backtick info cannot contain backtick", "``` a`b\nx", "p[\"``` a`b\" sb \"x\"]"},
	{"fence keeps blank lines and markup", "```\n# x\n\n- y\n```", `code()"# x\n\n- y"`},

	// Tables.
	{"table", "| a | b |\n|:--|--:|\n| [[x|y]] | `a|b` \\| c |",
		`table(l,r){["a"]|["b"]}{[wiki(x|y)]|[code"a|b" " | c"]}`},
	{"table without outer pipes", "a | b\n:-:|--\n1|2", `table(c,-){["a"]|["b"]}{["1"]|["2"]}`},
	{"table pads and truncates rows", "| a |\n|---|\n| 1 |\n| 2 | extra |\nnot row", "table(-){[\"a\"]}{[\"1\"]}{[\"2\"]}\np[\"not row\"]"},
	{"table header mismatch", "| a | b |\n|---|\n", `p["| a | b |" sb "|---|"]`},
	{"table escaped wikilink pipe", "| [[x\\|y]] | z |\n|---|---|", `table(-,-){[wiki(x|y)]|["z"]}`},
	{"table unclosed code span", "| a |\n|---|\n| `unclosed | b |", "table(-){[\"a\"]}{[\"`unclosed\"]}"},
	{"table after paragraph line", "intro\n| a |\n|---|\n| 1 |", "p[\"intro\"]\ntable(-){[\"a\"]}{[\"1\"]}"},
	{"table ended by heading", "| a |\n|---|\n# H", "table(-){[\"a\"]}\nh1[\"H\"]#h"},
	{"empty cells", "| a | b |\n|---|---|\n|  | x |", `table(-,-){["a"]|["b"]}{[]|["x"]}`},

	// Footnotes.
	{"footnotes", "Text[^1] and [^missing].\n\n[^1]: The *note*.", "p[\"Text\" fn(1:1) \" and \" fn(missing:0) \".\"]\nfndef(1:1){p[\"The \" em[\"note\"] \".\"]}"},
	{"footnote numbering follows references", "[^b] [^a]\n\n[^a]: A\n[^b]: B\n[^c]: C",
		"p[fn(b:1) \" \" fn(a:2)]\nfndef(a:2){p[\"A\"]}\nfndef(b:1){p[\"B\"]}\nfndef(c:3){p[\"C\"]}"},
	{"footnote multi-paragraph", "[^a]: first para\n\n    second para\n\nafter", "fndef(a:1){p[\"first para\"] p[\"second para\"]}\np[\"after\"]"},

	// Block ids.
	{"block ids", "para ^id1\n\n- item ^id2\n\n| t |\n|---|\n| 1 |\n\n^tblid",
		"p[\"para\"]^id1\nul(-,tight){li{p[\"item\"]^id2}^id2}\ntable(-){[\"t\"]}{[\"1\"]}^tblid"},
	{"caret inside text is not an id", "x^2 and a ^b c", `p["x^2 and a ^b c"]`},
	{"id on last line of paragraph", "line one\n^last", `p["line one"]^last`},

	// Robustness.
	{"nul and invalid utf8", "\x00\xff\xfe broken", "p[\"\\x00\\xff\\xfe broken\"]"},
	{"crlf", "a\r\nb\r\n\r\n# H\r\n", "p[\"a\" sb \"b\"]\nh1[\"H\"]#h"},
	{"lone cr", "a\rb\r\rc", "p[\"a\" sb \"b\"]\np[\"c\"]"},
	{"bom", "\ufeff# Title\n", `h1["Title"]#title`},
	{"bom frontmatter", "\ufeff---\ntitle: T\n---\nx", "frontmatter{title=\"T\" tags=[] aliases=[] date=\"\" title:\"T\"}\np[\"x\"]"},
}

func TestShapes(t *testing.T) {
	for _, tc := range shapeCases {
		t.Run(tc.name, func(t *testing.T) {
			doc := Parse(tc.in)
			got := strings.TrimRight(Dump(doc, false), "\n")
			if got != tc.want {
				t.Errorf("input %q\n got: %s\nwant: %s", tc.in, got, tc.want)
			}
			checkInvariants(t, tc.in, doc)
		})
	}
}

func TestBlockLines(t *testing.T) {
	src := "---\ntitle: T\n---\n# H\n\npara\nmore\n\n- a\n- b\n\n  cont\n\n```\nx\n```\n\n> q\n> r\n\n| a |\n|---|\n| 1 |\n"
	doc := Parse(src)
	got := strings.TrimRight(Dump(doc, true), "\n")
	// Line numbers include the frontmatter.
	want := []string{
		"frontmatter@0-2",
		"3-3:h1",
		"5-6:p",
		"8-11:ul(-,loose){8-8:li{8-8:p",
		"9-11:li{9-9:p",
		"11-11:p",
		"13-15:code",
		"17-18:quote{17-18:p",
		"20-22:table",
	}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in\n%s", w, got)
		}
	}
	cb := doc.Blocks[3].(*CodeBlock)
	if cb.FirstLine != 14 || !cb.Closed {
		t.Errorf("code FirstLine=%d Closed=%v", cb.FirstLine, cb.Closed)
	}
	tbl := doc.Blocks[5].(*Table)
	if tbl.Header.Line != 20 || tbl.Rows[0].Line != 22 {
		t.Errorf("table rows at %d,%d", tbl.Header.Line, tbl.Rows[0].Line)
	}
}

func TestInlineSpans(t *testing.T) {
	src := "> a *b* [[c|d]]\n> e #tag"
	doc := Parse(src)
	q := doc.Blocks[0].(*BlockQuote)
	p := q.Children[0].(*Paragraph)
	var got []string
	WalkInlines(p.Inlines, func(n Inline) bool {
		sp := n.SourceSpan()
		got = append(got, src[sp.Start:sp.End])
		return true
	})
	want := []string{"a ", "*b*", "b", " ", "[[c|d]]", "\n> ", "e ", "#tag"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("spans %q, want %q", got, want)
	}
	// Spans are raw file offsets even with a BOM and CRLF.
	src2 := "\ufeffx\r\n**y**"
	doc2 := Parse(src2)
	inl := doc2.Blocks[0].(*Paragraph).Inlines
	if s := inl[2].SourceSpan(); src2[s.Start:s.End] != "**y**" {
		t.Errorf("strong span %v = %q", s, src2[s.Start:s.End])
	}
	if l, c := doc2.LineCol(inl[2].SourceSpan().Start); l != 1 || c != 0 {
		t.Errorf("LineCol = %d:%d", l, c)
	}
	// Table cells.
	src3 := "| a | `b` |\n|---|---|\n| [[x|y]] | z |"
	tbl := Parse(src3).Blocks[0].(*Table)
	c := tbl.Rows[0].Cells[0]
	if src3[c.Start:c.End] != "[[x|y]]" {
		t.Errorf("cell span %q", src3[c.Start:c.End])
	}
	w := c.Inlines[0].(*WikiLink)
	if src3[w.Start:w.End] != "[[x|y]]" || w.Target != "x" || w.Label != "y" {
		t.Errorf("wiki %+v", w)
	}
}

func TestTaskToggle(t *testing.T) {
	src := "intro\n\n- [ ] one\n\t- [x] two\n> - [/] three\n1. [-] four\n- [→] five\n"
	doc := Parse(src)
	var tasks []*Task
	WalkBlocks(doc.Blocks, func(b Block) bool {
		if it, ok := b.(*ListItem); ok && it.Task != nil {
			tasks = append(tasks, it.Task)
		}
		return true
	})
	if len(tasks) != 5 {
		t.Fatalf("found %d tasks", len(tasks))
	}
	wantLines := []int{2, 3, 4, 5, 6}
	wantStates := []rune{' ', 'x', '/', '-', '→'}
	for i, tk := range tasks {
		if tk.Line != wantLines[i] || tk.State != wantStates[i] {
			t.Errorf("task %d: line %d state %q", i, tk.Line, tk.State)
		}
		line := doc.Line(tk.Line)
		if !strings.HasPrefix(line[tk.Col:], string(tk.State)+"]") {
			t.Errorf("task %d: col %d of %q", i, tk.Col, line)
		}
	}
	// Toggling rewrites exactly the state bytes.
	tk := tasks[0]
	toggled := src[:tk.Offset] + "x" + src[tk.Offset+tk.Width:]
	doc2 := Parse(toggled)
	if !doc2.Blocks[1].(*List).Items[0].Task.Done() {
		t.Errorf("toggle failed: %q", toggled)
	}
}

func TestFrontmatter(t *testing.T) {
	cases := []struct {
		in              string
		title, date, cr string
		tags, aliases   []string
		fields          int
		firstBodyLine   int
		hasFM           bool
	}{
		{in: "---\ntitle: \"My Note\"\ntags: [alpha, \"#beta\"]\naliases:\n  - First\n  - Second\ndate: 2026-10-01\n---\nbody",
			title: "My Note", date: "2026-10-01", tags: []string{"alpha", "beta"}, aliases: []string{"First", "Second"}, fields: 4, firstBodyLine: 8, hasFM: true},
		{in: "---\ntags: #a #b, c\naliases: x, y z\ncreated: 2026-01-01\n---\n",
			cr: "2026-01-01", tags: []string{"a", "b", "c"}, aliases: []string{"x", "y z"}, fields: 3, hasFM: true},
		{in: "---\ntags:\n  - one\n  - \"#two\"\n  - one\ntitle: 'It''s'\n# comment\nempty:\n---\n",
			title: "It's", tags: []string{"one", "two"}, fields: 3, hasFM: true},
		{in: "---\ntag: solo\nalias: [\"A, B\", C]\n...\n", tags: []string{"solo"}, aliases: []string{"A, B", "C"}, fields: 2, hasFM: true},
		{in: "---\nurl: https://example.org/x\n---\n", fields: 1, hasFM: true},
		{in: "--- \nx: 1\n---\n", fields: 1, hasFM: true},
		{in: "\n---\nx: 1\n---\n"},
		{in: "----\nx: 1\n----\n"},
	}
	for _, tc := range cases {
		doc := Parse(tc.in)
		fm := doc.Frontmatter
		if (fm != nil) != tc.hasFM {
			t.Errorf("%q: frontmatter present = %v", tc.in, fm != nil)
			continue
		}
		if fm == nil {
			continue
		}
		if fm.Title != tc.title || fm.Date != tc.date || fm.Created != tc.cr {
			t.Errorf("%q: title %q date %q created %q", tc.in, fm.Title, fm.Date, fm.Created)
		}
		if !reflect.DeepEqual(fm.Tags, tc.tags) || !reflect.DeepEqual(fm.Aliases, tc.aliases) {
			t.Errorf("%q: tags %q aliases %q", tc.in, fm.Tags, fm.Aliases)
		}
		if len(fm.Fields) != tc.fields {
			t.Errorf("%q: %d fields: %+v", tc.in, len(fm.Fields), fm.Fields)
		}
		if tc.firstBodyLine > 0 && doc.Blocks[0].BlockBase().StartLine != tc.firstBodyLine {
			t.Errorf("%q: body starts at %d", tc.in, doc.Blocks[0].BlockBase().StartLine)
		}
	}
	fm := Parse("---\nurl: https://example.org/x\nlist:\n  - a\n  - b\n---\n").Frontmatter
	if f, ok := fm.Get("URL"); !ok || f.Value != "https://example.org/x" || f.Line != 1 {
		t.Errorf("Get url = %+v %v", f, ok)
	}
	if f, _ := fm.Get("list"); !reflect.DeepEqual(f.List(), []string{"a", "b"}) || len(f.Lines) != 2 {
		t.Errorf("list field %+v", f)
	}
}

func TestOutlineAndAnchors(t *testing.T) {
	src := "# Top\n\ntext ^para1\n\n## Sub *one*\n\n> [!note]\n> ## In callout\n\n## Sub one\n\n- item ^li\n"
	doc := Parse(src)
	out := Outline(doc)
	var got []string
	for _, o := range out {
		got = append(got, strings.Repeat("#", o.Level)+" "+o.Text+" "+o.Slug+" @"+itoa(o.Line))
	}
	want := []string{"# Top top @0", "## Sub one sub-one @4", "## In callout in-callout @7", "## Sub one sub-one-1 @9"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("outline %q", got)
	}
	for anchor, line := range map[string]int{
		"Top": 0, "#top": 0, "sub-one": 4, "Sub one": 4, "sub-one-1": 9, "Note#Sub one": 4,
		"^para1": 2, "#^li": 11, "in callout": 7,
	} {
		if l, ok := ResolveAnchor(doc, anchor); !ok || l != line {
			t.Errorf("ResolveAnchor(%q) = %d,%v want %d", anchor, l, ok, line)
		}
	}
	if _, ok := ResolveAnchor(doc, "missing"); ok {
		t.Error("missing anchor resolved")
	}
	if _, ok := ResolveAnchor(doc, "^nope"); ok {
		t.Error("missing block id resolved")
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestLinks(t *testing.T) {
	src := "[[A]] and [b](B.md)\n\n> ![[img.png]]\n\n| x |\n|---|\n| [[C#h]] |\n\n[^1]: see https://x.org\n\n[^1]"
	doc := Parse(src)
	var got []string
	for _, l := range Links(doc) {
		switch n := l.Node.(type) {
		case *WikiLink:
			got = append(got, "wiki:"+n.Target+"@"+itoa(l.Line))
		case *Link:
			got = append(got, "link:"+n.Dest+"@"+itoa(l.Line))
		case *Image:
			got = append(got, "img:"+n.Src+"@"+itoa(l.Line))
		}
	}
	want := []string{"wiki:A@0", "link:B.md@0", "wiki:img.png@2", "wiki:C@6", "link:https://x.org@8"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("links %q", got)
	}
}

func TestLinkDestinations(t *testing.T) {
	doc := Parse("[a](sub/My%20Note.md#Some%20Heading) [b](https://x.org/a%20b) [c](<two words.md>) [d](mailto:a@b.c) [e](C:\\x)")
	var links []*Link
	WalkInlines(doc.Blocks[0].(*Paragraph).Inlines, func(n Inline) bool {
		if l, ok := n.(*Link); ok {
			links = append(links, l)
		}
		return true
	})
	if len(links) != 5 {
		t.Fatalf("%d links", len(links))
	}
	if l := links[0]; l.External || l.Path != "sub/My Note.md" || l.Fragment != "Some Heading" {
		t.Errorf("relative link %+v", l)
	}
	if l := links[1]; !l.External || l.Path != "" || l.Dest != "https://x.org/a%20b" {
		t.Errorf("external link %+v", l)
	}
	if l := links[2]; l.Path != "two words.md" {
		t.Errorf("angle link %+v", l)
	}
	if l := links[3]; !l.External {
		t.Errorf("mailto link %+v", l)
	}
	if l := links[4]; l.External {
		t.Errorf("drive path treated as URL %+v", l)
	}
}

func TestTagsAndWordCount(t *testing.T) {
	src := "---\ntags: [Proj]\n---\n# Title here\n\nOne two, three #proj #Idea/x.\n\n```\nnot counted at all\n```\n\n- don't stop\n- 日本語\n\n%% hidden words %%\n\n| a b | c |\n|---|---|\n| d | e |"
	doc := Parse(src)
	if got := Tags(doc); !reflect.DeepEqual(got, []string{"Proj", "Idea/x"}) {
		t.Errorf("tags %q", got)
	}
	// title here(2) one two three proj idea x(6) don't stop(2) 日本語(3) a b c d e(5)
	if n := WordCount(doc); n != 18 {
		t.Errorf("word count %d", n)
	}
}

func TestPlainText(t *testing.T) {
	doc := Parse("A *b* `c` [[T#H]] [[x|lbl]] [l](u) ![alt](i) #tag $m$ %%hid%% <b>x</b>[^1]\nnext")
	got := PlainText(doc.Blocks[0].(*Paragraph).Inlines)
	want := "A b c T > H lbl l alt #tag m  x next"
	if got != want {
		t.Errorf("PlainText = %q, want %q", got, want)
	}
}

func TestDocumentLines(t *testing.T) {
	doc := Parse("\ufeffa\r\nbb\rccc\n")
	if doc.LineCount() != 3 || doc.Line(0) != "a" || doc.Line(1) != "bb" || doc.Line(2) != "ccc" || doc.Line(3) != "" {
		t.Errorf("lines: %d %q %q %q", doc.LineCount(), doc.Line(0), doc.Line(1), doc.Line(2))
	}
	if !doc.BOM || !doc.CRLF {
		t.Errorf("BOM=%v CRLF=%v", doc.BOM, doc.CRLF)
	}
	if doc.LineStart(1) != 6 {
		t.Errorf("LineStart(1) = %d", doc.LineStart(1))
	}
	for off, want := range map[int][2]int{0: {0, 0}, 3: {0, 0}, 4: {0, 1}, 5: {0, 1}, 6: {1, 0}, 9: {2, 0}, 100: {2, 3}} {
		if l, c := doc.LineCol(off); l != want[0] || c != want[1] {
			t.Errorf("LineCol(%d) = %d:%d, want %v", off, l, c, want)
		}
	}
}

func TestCleanLine(t *testing.T) {
	for _, c := range []struct {
		raw   string
		match string // the matched substring of raw (first occurrence)
		want  string
		hl    string // the highlighted text in want
	}{
		{"weakest. A retry **budget** caps retries", "budget", "weakest. A retry budget caps retries", "budget"},
		{"- [ ] Priya — retry budget proposal 📅 2026-10-07", "retry", "Priya — retry budget proposal 📅 2026-10-07", "retry"},
		{"like [[Astrolabe#The rete]], a link", "rete", "like Astrolabe > The rete, a link", "Astrolabe > The rete"},
		{"## Budgets, not counts", "counts", "Budgets, not counts", "counts"},
		{"> quoted `code here` end", "code", "quoted code here end", "code"},
		{"```go", "go", "```go", "go"},
		{"| Retry rate | how **much** work | above 10% |", "much", "Retry rate · how much work · above 10%", "much"},
		{"|---|---|", "-", "|---|---|", "-"},
	} {
		i := strings.Index(c.raw, c.match)
		got, rs := CleanLine(c.raw, [][2]int{{i, i + len(c.match)}})
		if got != c.want {
			t.Errorf("CleanLine(%q) = %q, want %q", c.raw, got, c.want)
			continue
		}
		if len(rs) != 1 {
			t.Errorf("CleanLine(%q): ranges %v", c.raw, rs)
			continue
		}
		if c.hl != "" && got[rs[0][0]:rs[0][1]] != c.hl {
			t.Errorf("CleanLine(%q): highlighted %q, want %q", c.raw, got[rs[0][0]:rs[0][1]], c.hl)
		}
	}
}
