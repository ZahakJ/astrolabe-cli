package md

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// checkInvariants verifies structural guarantees that every consumer
// relies on: positions are in range and ordered, task offsets point at the
// state character, and the helpers run.
func checkInvariants(t *testing.T, src string, doc *Document) {
	t.Helper()
	nlines := doc.LineCount()
	var errs []string
	fail := func(format string, args ...any) {
		if len(errs) < 10 {
			errs = append(errs, fmt.Sprintf(format, args...))
		}
	}
	var checkInl func([]Inline, Span)
	checkInl = func(inl []Inline, parent Span) {
		prevEnd := -1
		for _, n := range inl {
			sp := n.SourceSpan()
			if sp.Start < 0 || sp.End > len(src) || sp.Start > sp.End {
				fail("inline %T span %v out of range (len %d)", n, sp, len(src))
			}
			if sp.Start < parent.Start || sp.End > parent.End {
				fail("inline %T span %v outside parent %v", n, sp, parent)
			}
			if sp.Start < prevEnd {
				fail("inline %T span %v overlaps previous end %d", n, sp, prevEnd)
			}
			prevEnd = sp.End
			if tx, ok := n.(*Text); ok && tx.Value == "" {
				fail("empty text node at %v", sp)
			}
			checkInl(InlineChildren(n), sp)
		}
	}
	all := Span{0, len(src)}
	prevStart := -1
	WalkBlocks(doc.Blocks, func(b Block) bool {
		base := b.BlockBase()
		if base.StartLine < 0 || base.EndLine < base.StartLine || base.EndLine >= nlines && nlines > 0 {
			fail("block %T lines %d-%d out of range (%d lines)", b, base.StartLine, base.EndLine, nlines)
		}
		if base.StartLine < prevStart {
			// Pre-order traversal visits blocks in source order.
			fail("block %T starts at %d before previous block start %d", b, base.StartLine, prevStart)
		}
		prevStart = base.StartLine
		for _, seq := range BlockInlines(b) {
			checkInl(seq, all)
		}
		if it, ok := b.(*ListItem); ok && it.Task != nil {
			tk := it.Task
			if tk.Offset < 0 || tk.Offset+tk.Width > len(src) {
				fail("task offset %d out of range", tk.Offset)
			} else if r, _ := utf8.DecodeRuneInString(src[tk.Offset:]); r != tk.State {
				fail("task offset %d holds %q, want %q", tk.Offset, r, tk.State)
			}
			if l, c := doc.LineCol(tk.Offset); l != tk.Line || c != tk.Col {
				fail("task line/col %d:%d, LineCol says %d:%d", tk.Line, tk.Col, l, c)
			}
		}
		for _, child := range BlockChildren(b) {
			cb := child.BlockBase()
			if cb.StartLine < base.StartLine || cb.EndLine > base.EndLine {
				fail("child %T %d-%d outside parent %T %d-%d", child, cb.StartLine, cb.EndLine, b, base.StartLine, base.EndLine)
			}
		}
		return true
	})
	_ = Outline(doc)
	_ = Links(doc)
	_ = Tags(doc)
	_ = WordCount(doc)
	_ = Dump(doc, true)
	for _, h := range Outline(doc) {
		if h.Slug == "" {
			continue
		}
		if l, ok := ResolveAnchor(doc, h.Slug); !ok || l > h.Line {
			fail("ResolveAnchor(%q) = %d,%v; heading at %d", h.Slug, l, ok, h.Line)
		}
	}
	if len(errs) > 0 {
		t.Fatalf("invariants violated for %q:\n  %s", src, strings.Join(errs, "\n  "))
	}
}

func FuzzParse(f *testing.F) {
	seeds := []string{
		"", "# h\n", "---\ntitle: a\ntags: [x]\n---\nbody",
		"- [ ] a\n  - [x] b\n\n1. c",
		"> [!note]+ T\n> body\n>\n> - x",
		"| a | b |\n|:-|-:|\n| [[x|y]] | `a|b` |",
		"```go\nx\n```\n~~~\n",
		"*a **b*** _c_ ~~d~~ ==e== `f` $g$ $$h$$",
		"[a](b \"c\") [d][e] ![f](g) <h@i.j> https://k.l/m #tag [^1]\n\n[e]: /x\n[^1]: n",
		"%% c %%\n<!-- d -->\n<div>\n</div>",
		"\t- x\n\t\tcode\n",
		"a\r\nb\rc\n",
		"\xef\xbb\xbf- [x] bom",
		"[[a]] ![[b#c|d]] [[e#^f]]",
		"text ^id\n\n^lone",
		"$$\nx\n$$\n",
		"***\n___\n- - -",
		"[x]: <y> 'z'\n[x]",
		"> > > deep\n> > back",
		"[[[[[[[[a]]]]]]]] ((((( ))))) <<<<< >>>>>",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		doc := Parse(src)
		checkInvariants(t, src, doc)
	})
}

// pathological inputs that defeat naive implementations (quadratic
// scans, deep recursion).
func TestPathological(t *testing.T) {
	cases := map[string]string{
		"nested quotes":   strings.Repeat(">", 50000) + " x",
		"nested lists":    strings.Repeat("- ", 20000) + "x",
		"open brackets":   strings.Repeat("[", 50000),
		"wiki opens":      strings.Repeat("[[a ", 30000),
		"emphasis":        strings.Repeat("*a _b ", 30000),
		"backtick runs":   strings.Repeat("`` ` ", 30000),
		"dollars":         strings.Repeat("$a ", 40000),
		"percent":         strings.Repeat("%% a\n", 1) + strings.Repeat("x %% ", 30000),
		"html opens":      strings.Repeat("<a href=\"", 20000),
		"comments":        strings.Repeat("<!-- ", 30000),
		"link dests":      strings.Repeat("[a](", 30000),
		"ref labels":      strings.Repeat("[a]", 30000) + "\n\n[a]: /u",
		"tags":            strings.Repeat("#a", 50000),
		"urls":            strings.Repeat("https://a.b/(", 20000),
		"table rows":      "|a|b|\n|-|-|\n" + strings.Repeat("|`x|[[y|\n", 20000),
		"fences":          strings.Repeat("```\n", 20000),
		"math lines":      strings.Repeat("$$ a\n", 20000),
		"many footnotes":  strings.Repeat("[^a] ", 20000) + "\n\n[^a]: x",
		"long line":       strings.Repeat("word ", 100000),
		"closers no open": strings.Repeat("a* ", 50000),
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			doc := Parse(src)
			checkInvariants(t, src, doc)
		})
	}
}
