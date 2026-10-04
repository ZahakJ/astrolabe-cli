package render

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/folio/internal/md"
	"github.com/ZahakJ/folio/internal/term"
	"github.com/ZahakJ/folio/internal/term/termtest"
	"github.com/ZahakJ/folio/internal/text"
	"github.com/ZahakJ/folio/internal/theme"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

var testToday = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

const typographyPath = "../../examples/vault/Typography.md"

func baseOpts(width int) Options {
	return Options{Width: width, Theme: theme.IronGall, Glyphs: theme.UnicodeGlyphs,
		Resolver: testResolver(), Today: testToday}
}

func renderSrc(src string, opt Options) *Page { return Render(md.Parse(src), opt) }

// inputs returns the golden inputs: testdata/*.md plus the showcase note.
func inputs(t testing.TB) map[string]string {
	t.Helper()
	out := map[string]string{}
	files, _ := filepath.Glob("testdata/*.md")
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		out[strings.TrimSuffix(filepath.Base(f), ".md")] = string(b)
	}
	if b, err := os.ReadFile(typographyPath); err == nil {
		out["typography"] = string(b)
	}
	if len(out) == 0 {
		t.Fatal("no golden inputs")
	}
	return out
}

// TestGolden compares plain renderings with testdata/*.golden. Run with
// -update to rewrite them after an intended change (and read the diff).
func TestGolden(t *testing.T) {
	type variant struct {
		suffix string
		width  int
		ascii  bool
	}
	variants := []variant{{"80", 80, false}, {"44", 44, false}, {"80.ascii", 80, true}}
	for name, src := range inputs(t) {
		for _, v := range variants {
			opt := baseOpts(v.width)
			opt.Glyphs = theme.GlyphsFor(v.ascii)
			opt.Theme = theme.IronGall
			got := Plain(src, opt)
			path := filepath.Join("testdata", name+"."+v.suffix+".golden")
			if *update {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%s: %v (run with -update)", path, err)
			}
			if got != string(want) {
				t.Errorf("%s differs from golden; run go test -update and review the diff\n%s", path, firstDiff(string(want), got))
			}
		}
	}
}

func firstDiff(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < max(len(al), len(bl)); i++ {
		var x, y string
		if i < len(al) {
			x = al[i]
		}
		if i < len(bl) {
			y = bl[i]
		}
		if x != y {
			return fmt.Sprintf("line %d:\nwant %q\ngot  %q", i+1, x, y)
		}
	}
	return ""
}

// TestWidths checks the alignment invariants on every input at many widths,
// in every glyph set, with bidi on and off, with and without ground: no
// line exceeds the pane, Width matches the spans, painted lines are exactly
// the pane width, and hit regions lie inside the line.
func TestWidths(t *testing.T) {
	for name, src := range inputs(t) {
		doc := md.Parse(src)
		for w := 20; w <= 160; w += 7 {
			for _, ascii := range []bool{false, true} {
				for _, bidi := range []bool{false, true} {
					for _, ground := range []bool{false, true} {
						opt := baseOpts(w)
						opt.Glyphs = theme.GlyphsFor(ascii)
						opt.Bidi = bidi
						opt.PaintGround = ground
						p := Render(doc, opt)
						for i, l := range p.Lines {
							got := text.SpansWidth(l.Spans)
							if got != l.Width {
								t.Fatalf("%s w=%d line %d: Width %d, spans %d", name, w, i, l.Width, got)
							}
							if got > w {
								t.Fatalf("%s w=%d ascii=%v bidi=%v line %d is %d cells: %q", name, w, ascii, bidi, i, got, l.Text())
							}
							if ground && got != w {
								t.Fatalf("%s w=%d painted line %d is %d cells", name, w, i, got)
							}
							for _, h := range l.Hits {
								if h.X0 < 0 || h.X1 > got || h.X0 >= h.X1 {
									t.Fatalf("%s w=%d line %d: bad hit %+v (width %d)", name, w, i, h, got)
								}
							}
						}
						checkTables(t, name, w, p)
					}
				}
			}
		}
	}
}

// checkTables asserts that within each run of table lines (not stacked)
// every column separator sits in the same cell column as the header's.
func checkTables(t *testing.T, name string, w int, p *Page) {
	t.Helper()
	g := theme.UnicodeGlyphs
	var cols []int
	for i, l := range p.Lines {
		if l.Kind != KindTable {
			cols = nil
			continue
		}
		var seps []int
		x := 0
		for _, sp := range l.Spans {
			for _, gr := range text.Graphemes(sp.Text) {
				if (gr.Text == g.VLine || gr.Text == g.Cross) && sp.Style.FG == theme.IronGall.Border {
					seps = append(seps, x)
				}
				x += gr.Width
			}
		}
		if len(seps) == 0 {
			cols = nil // stacked layout or a record separator
			continue
		}
		if cols == nil {
			cols = seps
			continue
		}
		// Lines may stop early when trailing cells are empty, but every
		// separator they draw must line up.
		for k, s := range seps {
			if k >= len(cols) || cols[k] != s {
				t.Fatalf("%s w=%d line %d: separators %v, header %v\n%q", name, w, i, seps, cols, l.Text())
			}
		}
	}
}

func TestColumnWidths(t *testing.T) {
	cases := []struct {
		nat   []int
		avail int
		want  []int
		ok    bool
	}{
		{[]int{5, 10, 3}, 40, []int{5, 10, 3}, true},   // fits: natural
		{[]int{5, 30, 20}, 40, []int{5, 14, 15}, true}, // widest shrinks first, then shares
		{[]int{10, 10}, 15, []int{6, 6}, true},         // equal columns shrink evenly
		{[]int{2, 40}, 20, []int{2, 15}, true},         // narrow column keeps its width
		{[]int{30, 30, 30, 30}, 30, nil, false},        // would fall below MinColumn
		{[]int{4, 4, 4}, 12, []int{2, 2, 2}, false},    // natural 4 → 2 < min(4, 6)
		{[]int{1}, 1, []int{1}, true},                  // degenerate
		{[]int{8, 8, 8}, 8*3 + 2*sepWidth, []int{8, 8, 8}, true},
	}
	for _, c := range cases {
		got, ok := ColumnWidths(c.nat, nil, c.avail)
		if ok != c.ok {
			t.Errorf("ColumnWidths(%v, %d) ok = %v, want %v (%v)", c.nat, c.avail, ok, c.ok, got)
			continue
		}
		if !ok {
			continue
		}
		total := sepWidth * (len(got) - 1)
		for _, w := range got {
			total += w
		}
		if total > c.avail {
			t.Errorf("ColumnWidths(%v, %d) = %v, total %d exceeds avail", c.nat, c.avail, got, total)
		}
		if fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("ColumnWidths(%v, %d) = %v, want %v", c.nat, c.avail, got, c.want)
		}
	}
	// Floors: a long word raises the floor (capped), so a column that would
	// cut a word mid-way triggers the stacked layout.
	if f := ColumnFloor(20, 13); f != MaxWordFloor {
		t.Errorf("ColumnFloor(20, 13) = %d", f)
	}
	if f := ColumnFloor(3, 3); f != 3 {
		t.Errorf("ColumnFloor(3, 3) = %d", f)
	}
	if _, ok := ColumnWidths([]int{20, 20}, []int{12, 12}, 20); ok {
		t.Error("columns below their floors should not be accepted")
	}
}

func TestTableStackedFallback(t *testing.T) {
	src := "| alpha | bravo | charlie | delta | echo |\n|---|---|---|---|---|\n| one two three | four | five | six | seven |\n"
	wide := renderSrc(src, baseOpts(80)).Plain()
	if !strings.Contains(wide, "│") {
		t.Fatalf("wide table should be a grid:\n%s", wide)
	}
	narrow := renderSrc(src, baseOpts(30)).Plain()
	if strings.Contains(narrow, "│") || !strings.Contains(narrow, "charlie  five") {
		t.Fatalf("narrow table should be stacked records:\n%s", narrow)
	}
}

// TestTableOverflow checks a top-level table may use the pane beyond the
// measure before shrinking.
func TestTableOverflow(t *testing.T) {
	row := "| " + strings.Repeat("x", 45) + " | " + strings.Repeat("y", 45) + " |\n"
	src := "| a | b |\n|---|---|\n" + row
	p := renderSrc(src, baseOpts(120))
	for _, l := range p.Lines {
		if strings.Contains(l.Text(), "xxxx") && !strings.Contains(l.Text(), strings.Repeat("x", 45)) {
			t.Fatalf("table wrapped although the pane had room:\n%s", p.Plain())
		}
	}
}

func lineWith(p *Page, s string) (int, Line) {
	for i, l := range p.Lines {
		if strings.Contains(l.Text(), s) {
			return i, l
		}
	}
	return -1, Line{}
}

func TestLinksAndHits(t *testing.T) {
	src := "Go to [[Target|the target]], [[Ghost]] and [site](https://example.org).\n\n- [ ] a task\n"
	p := renderSrc(src, baseOpts(80))
	if len(p.Links) != 3 {
		t.Fatalf("links = %d, want 3: %+v", len(p.Links), p.Links)
	}
	want := []struct {
		text   string
		status LinkStatus
		kind   LinkKind
	}{{"the target", LinkResolved, LinkWiki}, {"Ghost", LinkBroken, LinkWiki}, {"site", LinkExternal, LinkMarkdown}}
	for i, w := range want {
		l := p.Links[i]
		if l.Text != w.text || l.Status != w.status || l.Kind != w.kind {
			t.Errorf("link %d = %+v, want %+v", i, l, w)
		}
		if len(l.Regions) == 0 {
			t.Fatalf("link %d has no region", i)
		}
		r := l.Regions[0]
		got := text.Slice(p.Lines[r.Line].Text(), r.X0, r.X1)
		if !strings.HasPrefix(got, w.text) {
			t.Errorf("link %d region covers %q, want %q", i, got, w.text)
		}
		if p.LinkAt(r.Line, r.X0) != i {
			t.Errorf("LinkAt(%d,%d) = %d, want %d", r.Line, r.X0, p.LinkAt(r.Line, r.X0), i)
		}
	}
	if p.Links[0].Path != "Target.md" || p.Links[2].URL != "https://example.org" {
		t.Errorf("paths: %+v", p.Links)
	}
	// The external link's region includes its ↗ mark, which carries the
	// OSC 8 target.
	r := p.Links[2].Regions[0]
	if got := text.Slice(p.Lines[r.Line].Text(), r.X0, r.X1); got != "site ↗" {
		t.Errorf("external region %q", got)
	}
	i, l := lineWith(p, "a task")
	if i < 0 {
		t.Fatal("task line missing")
	}
	var task *Hit
	for k := range l.Hits {
		if l.Hits[k].Kind == HitTask {
			task = &l.Hits[k]
		}
	}
	if task == nil {
		t.Fatalf("no task hit on %q: %+v", l.Text(), l.Hits)
	}
	if got := text.Slice(l.Text(), task.X0, task.X1); got != "☐" {
		t.Errorf("task hit covers %q", got)
	}
	doc := md.Parse(src)
	if doc.Source[task.Task.Offset:task.Task.Offset+task.Task.Width] != " " || task.Task.Line != 2 {
		t.Errorf("task ref %+v does not point at the state character", task.Task)
	}
}

func TestLinkWrapsRegions(t *testing.T) {
	src := "Some words before [[Target|a label of several words that wraps]] after.\n"
	p := renderSrc(src, baseOpts(30))
	if len(p.Links) != 1 || len(p.Links[0].Regions) < 2 {
		t.Fatalf("expected one link over several lines: %+v\n%s", p.Links, p.Plain())
	}
	var parts []string
	for _, r := range p.Links[0].Regions {
		parts = append(parts, text.Slice(p.Lines[r.Line].Text(), r.X0, r.X1))
	}
	if got := strings.Join(parts, " "); got != "a label of several words that wraps" {
		t.Errorf("regions cover %q", got)
	}
	focused := p.FocusSpans(p.Links[0].Regions[0].Line, 0, theme.IronGall.LinkFocus)
	found := false
	for _, s := range focused {
		if s.Style.BG == theme.IronGall.LinkFocus.BG && strings.Contains(s.Text, "a") {
			found = true
		}
	}
	if !found {
		t.Error("FocusSpans did not restyle the link")
	}
}

func TestSourceMapping(t *testing.T) {
	src := "# Title\n\npara line one\npara line two\n\n```go\na := 1\nb := 2\n```\n\n- item\n\n%% hidden %%\n\nlast\n"
	opt := baseOpts(80)
	p := renderSrc(src, opt)
	doc := md.Parse(src)
	for s := 0; s < doc.LineCount(); s++ {
		i := p.LineForSource(s)
		if i < 0 || i >= len(p.Lines) {
			t.Fatalf("LineForSource(%d) = %d out of range", s, i)
		}
	}
	check := func(src int, want string) {
		t.Helper()
		i := p.LineForSource(src)
		if !strings.Contains(p.Lines[i].Text(), want) {
			t.Errorf("source line %d → rendered %d %q, want it to contain %q", src, i, p.Lines[i].Text(), want)
		}
		if back := p.SourceLine(i); back != src {
			t.Errorf("SourceLine(%d) = %d, want %d", i, back, src)
		}
	}
	check(2, "para line one")
	check(6, "a := 1")
	check(7, "b := 2")
	check(10, "item")
	check(14, "last")
	// A hidden comment maps to the next block.
	if i := p.LineForSource(12); !strings.Contains(p.Lines[i].Text(), "last") {
		t.Errorf("hidden line maps to %q", p.Lines[i].Text())
	}
	// A soft-wrapped paragraph maps each source line to the row showing it.
	p2 := renderSrc("alpha beta gamma\ndelta epsilon zeta\neta theta iota\n", baseOpts(80))
	if p2.SourceLine(0) != 0 {
		t.Errorf("SourceLine(0) = %d", p2.SourceLine(0))
	}
}

func TestFolds(t *testing.T) {
	src := "# Top\n\nintro\n\n## A\n\nalpha text\n\n### A1\n\nnested\n\n## B\n\nbeta text\n\n> [!note]- Closed\n> secret\n\n> [!tip]+ Open\n> shown\n"
	opt := baseOpts(80)
	opt.NoTitleBlock = true
	p := renderSrc(src, opt)
	ids := map[string]FoldInfo{}
	for _, f := range p.Folds {
		ids[f.ID] = f
	}
	for _, id := range []string{"h:top", "h:a", "h:a1", "h:b", "c:1", "c:2"} {
		if _, ok := ids[id]; !ok {
			t.Fatalf("fold %q missing: %+v", id, p.Folds)
		}
	}
	if !ids["c:1"].Folded || ids["c:2"].Folded {
		t.Errorf("callout defaults wrong: %+v", p.Folds)
	}
	plain := p.Plain()
	if strings.Contains(plain, "secret") || !strings.Contains(plain, "shown") {
		t.Errorf("callout fold not applied:\n%s", plain)
	}
	// Fold section A: its body and its subsection disappear, B stays.
	opt.Folds = map[string]bool{"h:a": true, "c:1": false}
	p = renderSrc(src, opt)
	plain = p.Plain()
	if strings.Contains(plain, "alpha text") || strings.Contains(plain, "nested") || !strings.Contains(plain, "beta text") {
		t.Fatalf("heading fold wrong:\n%s", plain)
	}
	if !strings.Contains(plain, "secret") {
		t.Errorf("explicit open state ignored:\n%s", plain)
	}
	i, l := lineWith(p, "A")
	for k, ln := range p.Lines {
		if ln.Fold == "h:a" {
			i, l = k, ln
		}
	}
	if !l.Folded || !strings.Contains(l.Text(), "▸") || !strings.Contains(l.Text(), "… 5 lines") {
		t.Errorf("folded heading line %d: %q %+v", i, l.Text(), l)
	}
	if h, ok := l.HitAt(p.ChevronX); !ok || h.Kind != HitFold || h.Fold != "h:a" {
		t.Errorf("no fold hit at the chevron: %+v", l.Hits)
	}
	// Hidden source lines map to the folded heading.
	if got := p.LineForSource(6); got != i {
		t.Errorf("hidden line maps to %d, want %d", got, i)
	}
	for _, o := range p.Outline {
		if o.Slug == "a1" && o.Visible {
			t.Errorf("outline entry inside a fold is visible: %+v", o)
		}
		if o.Slug == "b" && (!o.Visible || !strings.Contains(p.Lines[o.Line].Text(), "B")) {
			t.Errorf("outline entry B: %+v", o)
		}
	}
	// Folding the top heading hides everything below it.
	opt.Folds = map[string]bool{"h:top": true}
	if plain := renderSrc(src, opt).Plain(); strings.Contains(plain, "intro") || strings.Contains(plain, "beta") {
		t.Errorf("H1 fold should hide all:\n%s", plain)
	}
}

func TestTitleBlock(t *testing.T) {
	b, err := os.ReadFile("testdata/title.md")
	if err != nil {
		t.Fatal(err)
	}
	opt := baseOpts(80)
	opt.Backlinks = 2
	p := renderSrc(string(b), opt)
	plain := p.Plain()
	if strings.Count(plain, "Frontmatter Title") != 1 {
		t.Errorf("title printed %d times:\n%s", strings.Count(plain, "Frontmatter Title"), plain)
	}
	if !strings.Contains(plain, "2 Jan 2026 · #one #two · 2 backlinks") {
		t.Errorf("chip line missing:\n%s", plain)
	}
	if !strings.Contains(plain, "▸ properties · 2") || strings.Contains(plain, "draft") {
		t.Errorf("properties should be folded:\n%s", plain)
	}
	opt.Folds = map[string]bool{"properties": false}
	plain = renderSrc(string(b), opt).Plain()
	if !strings.Contains(plain, "status   draft") || !strings.Contains(plain, "aliases  Alias") {
		t.Errorf("open properties:\n%s", plain)
	}
	// No frontmatter, no H1: no title block.
	if p := renderSrc("just text\n", baseOpts(80)); p.Lines[0].Kind != KindParagraph {
		t.Errorf("unexpected title block: %+v", p.Lines[0])
	}
	// Frontmatter without a title falls back to Options.Title.
	opt = baseOpts(80)
	opt.Title = "File Name"
	if plain := renderSrc("---\nstatus: x\n---\nbody\n", opt).Plain(); !strings.Contains(plain, "File Name") {
		t.Errorf("fallback title missing:\n%s", plain)
	}
}

func TestCodeScroll(t *testing.T) {
	long := strings.Repeat("abcdefghij", 12)
	src := "```\n" + long + "\nshort\n```\n"
	p := renderSrc(src, baseOpts(60))
	i, l := lineWith(p, "abcdef")
	if i < 0 || l.Code == nil {
		t.Fatalf("code line missing viewport: %+v", l)
	}
	if !strings.HasSuffix(strings.TrimRight(l.Text(), " "), "…") {
		t.Errorf("long line not clipped with an ellipsis: %q", l.Text())
	}
	s := l.Scrolled(10)
	if s.Width != l.Width || text.SpansWidth(s.Spans) != l.Width {
		t.Errorf("scrolled width %d, want %d", text.SpansWidth(s.Spans), l.Width)
	}
	view := text.Slice(s.Text(), l.Code.X, l.Code.X+l.Code.View)
	if !strings.HasPrefix(view, "…") || !strings.Contains(view, "bcdefghij") {
		t.Errorf("scrolled view %q", view)
	}
	end := l.Scrolled(1 << 20)
	view = text.Slice(end.Text(), l.Code.X, l.Code.X+l.Code.View)
	if !strings.HasSuffix(strings.TrimRight(view, " "), "hij") {
		t.Errorf("fully scrolled view %q should show the line's end", view)
	}
	if m := l.Code.MaxScroll(); m <= 0 {
		t.Errorf("MaxScroll = %d", m)
	}
	if _, sl := lineWith(p, "short"); sl.Code.MaxScroll() != 0 {
		t.Errorf("short line should not scroll")
	}
}

func TestSanitize(t *testing.T) {
	src := "evil \x1b[31mred\x1b]0;title\x07 text \u202ereversed \x00nul\n\n```\nesc\x1bcode\n```\n"
	p := renderSrc(src, baseOpts(80))
	enc := term.Encoder{Profile: term.ProfileNone}
	out := p.ANSI(enc)
	for _, bad := range []string{"\x1b[31m", "\x1b]0", "\x07", "\u202e", "\x00"} {
		if strings.Contains(out, bad) {
			t.Errorf("output contains %q", bad)
		}
	}
	if !strings.Contains(p.Plain(), "evil [31mred]0;title text reversed nul") {
		t.Errorf("text lost: %q", p.Plain())
	}
	// Invalid UTF-8 becomes U+FFFD.
	if p := renderSrc("bad \xff byte\n", baseOpts(80)); !strings.Contains(p.Plain(), "bad \uFFFD byte") {
		t.Errorf("invalid UTF-8: %q", p.Plain())
	}
}

// TestANSIRoundTrip feeds the ANSI serialisation to a virtual terminal and
// checks the screen shows exactly the plain text, in every profile.
func TestANSIRoundTrip(t *testing.T) {
	b, err := os.ReadFile(typographyPath)
	if err != nil {
		t.Skip(err)
	}
	for _, prof := range []term.Profile{term.ProfileTrueColor, term.Profile256, term.Profile16, term.ProfileNone} {
		opt := baseOpts(80)
		opt.Theme = term.ThemeFor(theme.IronGall, prof)
		opt.PaintGround = prof >= term.Profile256
		p := renderSrc(string(b), opt)
		out := p.ANSI(term.Encoder{Profile: prof, Hyperlinks: true, StyledUnderline: true})
		vt := termtest.New(80, len(p.Lines)+2)
		_, _ = vt.Write([]byte(strings.ReplaceAll(out, "\n", "\r\n")))
		for i, l := range p.Lines {
			want := strings.TrimRight(l.Text(), " ")
			if got := strings.TrimRight(vt.Row(i), " "); got != want {
				t.Fatalf("profile %v line %d:\nwant %q\ngot  %q", prof, i, want, got)
			}
		}
		// Styles survive: the title is bold.
		if st := vt.Style(p.Left, 0); st.Attrs&theme.Bold == 0 {
			t.Errorf("profile %v: title not bold: %+v", prof, st)
		}
	}
}

func TestPaintGround(t *testing.T) {
	opt := baseOpts(60)
	opt.PaintGround = true
	p := renderSrc("hello\n\nworld\n", opt)
	for i, l := range p.Lines {
		for _, s := range l.Spans {
			if s.Style.BG.IsDefault() {
				t.Fatalf("line %d span %q has no ground", i, s.Text)
			}
		}
	}
	// Mono and Basic16 themes paint nothing even when asked.
	opt.Theme = theme.IronGall.Basic16()
	p = renderSrc("hello\n\n`code`\n\n```\nx\n```\n", opt)
	for _, l := range p.Lines {
		for _, s := range l.Spans {
			if !s.Style.BG.IsDefault() {
				t.Fatalf("16-colour page painted a ground: %+v", s)
			}
		}
	}
	if !strings.Contains(p.Plain(), "`code`") || !strings.Contains(p.Plain(), "│ x") {
		t.Errorf("unpainted forms missing:\n%s", p.Plain())
	}
}

func TestRTL(t *testing.T) {
	src := "## عنوان\n\nنص عربي.\n\n- بند\n"
	opt := baseOpts(40)
	off := renderSrc(src, opt)
	_, l := lineWith(off, "نص عربي.")
	if !l.RTL || strings.TrimRight(l.Text(), " ") == "" || !strings.HasSuffix(strings.TrimRight(l.Text(), " "), "نص عربي.") {
		t.Errorf("bidi off: logical order, right aligned; got %q", l.Text())
	}
	if l.Width != off.Left+off.Measure {
		t.Errorf("RTL line should end at the measure's right edge: width %d, want %d", l.Width, off.Left+off.Measure)
	}
	_, b := lineWith(off, "بند")
	if !strings.HasSuffix(strings.TrimRight(b.Text(), " "), "بند •") {
		t.Errorf("bullet not mirrored: %q", b.Text())
	}
	opt.Bidi = true
	on := renderSrc(src, opt)
	_, l2 := lineWith(on, ".")
	want := text.Visual("نص عربي.", text.RTL)
	if !strings.HasSuffix(strings.TrimRight(l2.Text(), " "), want) {
		t.Errorf("bidi on: want visual %q, got %q", want, l2.Text())
	}
	// Plain output never reorders.
	if pl := Plain(src, opt); !strings.Contains(pl, "نص عربي.") {
		t.Errorf("Plain reordered text: %q", pl)
	}
}

func TestEmbed(t *testing.T) {
	p := renderSrc("![[Target]]\n\n![[Astrolabe#The rete]]\n\n![[Ghost]]\n", baseOpts(80))
	plain := p.Plain()
	if !strings.Contains(plain, "▣ Target") || !strings.Contains(plain, "▎ target body") {
		t.Errorf("note embed:\n%s", plain)
	}
	if !strings.Contains(plain, "▣ Astrolabe › The rete") || !strings.Contains(plain, "▎ • Vega") || strings.Contains(plain, "Each plate") {
		t.Errorf("section embed:\n%s", plain)
	}
	if !strings.Contains(plain, "▣ Ghost") {
		t.Errorf("broken embed placeholder:\n%s", plain)
	}
	// Long embeds stop after EmbedLines lines.
	p = renderSrc("![[Astrolabe]]\n", baseOpts(80))
	n := 0
	for _, l := range p.Lines {
		if l.Kind == KindEmbed && strings.HasPrefix(strings.TrimSpace(l.Text()), "▎") {
			n++
		}
	}
	if n != EmbedLines+1 { // the lines plus the ellipsis line
		t.Errorf("embed preview has %d lines:\n%s", n, p.Plain())
	}
}

func TestHighlighter(t *testing.T) {
	th := theme.IronGall
	color := func(lang, line, tok string) theme.Color {
		h := newHighlighter(lang, th)
		for _, s := range h.line(line, theme.Style{FG: th.Text}) {
			if strings.Contains(s.Text, tok) {
				return s.Style.st.FG
			}
		}
		return theme.Default
	}
	cases := []struct {
		lang, line, tok string
		want            theme.Color
	}{
		{"go", `x := "str" // note`, `"str"`, th.CodeString},
		{"go", `x := "str" // note`, "// note", th.CodeComment},
		{"go", `return 42`, "return", th.CodeKeyword},
		{"go", `return 42`, "42", th.CodeNumber},
		{"python", `def f(): pass  # c`, "def", th.CodeKeyword},
		{"py", `s = 'x'`, "'x'", th.CodeString},
		{"bash", `echo $HOME # c`, "# c", th.CodeComment},
		{"bash", `echo a#b`, "a#b", th.Text},
		{"json", `{"k": 1}`, `"k"`, th.CodeKeyword},
		{"yaml", `key: true`, "key", th.CodeKeyword},
		{"sql", `select * from t`, "select", th.CodeKeyword},
		{"rust", `let mut x = 1;`, "let", th.CodeKeyword},
		{"lua", `local x = 1 -- c`, "-- c", th.CodeComment},
		{"c", `int x = 0; /* c */`, "/* c */", th.CodeComment},
		{"ts", "const s = `t`", "`t`", th.CodeString},
		{"diff", "+added", "+added", th.Ok},
		{"diff", "-removed", "-removed", th.Danger},
		{"nonsense", "return 1", "return 1", th.Text},
	}
	for _, c := range cases {
		if got := color(c.lang, c.line, c.tok); got != c.want {
			t.Errorf("%s %q: %q coloured %v, want %v", c.lang, c.line, c.tok, got, c.want)
		}
	}
	// Block comments and raw strings carry over lines.
	h := newHighlighter("go", th)
	h.line("a /* start", theme.Style{})
	if s := h.line("still comment */ b", theme.Style{}); s[0].Style.st.FG != th.CodeComment {
		t.Errorf("block comment did not continue: %+v", s)
	}
	h.line("s := `raw", theme.Style{})
	if s := h.line("more` x", theme.Style{}); s[0].Style.st.FG != th.CodeString {
		t.Errorf("raw string did not continue: %+v", s)
	}
}

func TestDueChips(t *testing.T) {
	src := "- [ ] late due:2026-10-01\n- [x] done late due:2026-10-01\n- [ ] now 📅 2026-10-04\n- [ ] later @due(2027-01-02)\n"
	p := renderSrc(src, baseOpts(80))
	plain := p.Plain()
	for _, want := range []string{"late  overdue 1 Oct", "done late  due 1 Oct", "now  due today", "later  due 2 Jan 2027"} {
		if !strings.Contains(plain, want) {
			t.Errorf("missing %q in\n%s", want, plain)
		}
	}
	_, l := lineWith(p, "overdue")
	found := false
	for _, s := range l.Spans {
		if strings.Contains(s.Text, "1 Oct") && s.Style.FG == theme.IronGall.Danger {
			found = true
		}
	}
	if !found {
		t.Errorf("overdue chip not in danger ink: %+v", l.Spans)
	}
}

// BenchmarkRender100K renders a ~100 KB note (DESIGN.md: re-render on
// resize and fold must be fast).
func BenchmarkRender100K(b *testing.B) {
	src := bigNote(100 << 10)
	doc := md.Parse(src)
	opt := baseOpts(100)
	opt.Bidi = true
	opt.PaintGround = true
	b.SetBytes(int64(len(src)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Render(doc, opt)
	}
}

func bigNote(n int) string {
	b, err := os.ReadFile(typographyPath)
	unit := string(b)
	if err != nil {
		unit = "# H\n\nSome **text** with a [[link]] and `code`.\n\n- a\n- b\n"
	}
	// Drop the frontmatter of repeated copies.
	if i := strings.Index(unit, "\n---\n"); strings.HasPrefix(unit, "---\n") && i > 0 {
		unit = unit[i+5:]
	}
	var sb strings.Builder
	for sb.Len() < n {
		sb.WriteString(unit)
		sb.WriteString("\n")
	}
	return sb.String()
}

func TestRenderSpeed(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	src := bigNote(100 << 10)
	doc := md.Parse(src)
	opt := baseOpts(100)
	start := time.Now()
	const runs = 5
	for i := 0; i < runs; i++ {
		Render(doc, opt)
	}
	if per := time.Since(start) / runs; per > ciSlack(150*time.Millisecond) {
		t.Errorf("rendering 100 KB took %v per run", per)
	}
}

// An Arabic first tag must not flip the chip line: each tag is ordered on
// its own and the date and backlink count keep reading left to right.
func TestTitleChipsBidi(t *testing.T) {
	src := "---\ntitle: الأسطرلاب\ndate: 2026-03-14\ntags: [فلك, navigation]\n---\n\nنص.\n"
	opt := baseOpts(80)
	opt.Bidi = true
	opt.Backlinks = 4
	p := renderSrc(src, opt)
	_, l := lineWith(p, "backlinks")
	got := strings.TrimSpace(l.Text())
	want := "14 Mar 2026 · #" + text.Visual("فلك", text.RTL) + " #navigation · 4 backlinks"
	if got != want {
		t.Errorf("chip line:\n got %q\nwant %q", got, want)
	}
	// Under a right-to-left title the chip line is right-aligned with it.
	if lead := len(l.Text()) - len(strings.TrimLeft(l.Text(), " ")); lead <= p.Left {
		t.Errorf("chip line should be right-aligned under an RTL title, starts at %d (left %d)", lead, p.Left)
	}
}

// Folded sections stack one blank row apart (zM reads as a contents list).
func TestFoldedSectionsSpacing(t *testing.T) {
	src := "## A\n\ntext\n\n## B\n\nmore\n\n## C\n\nend\n"
	opt := baseOpts(60)
	opt.Folds = map[string]bool{"h:a": true, "h:b": true}
	p := renderSrc(src, opt)
	ia, _ := lineWith(p, "A")
	ib, _ := lineWith(p, "B")
	ic, _ := lineWith(p, "C")
	if ib-ia != 2 {
		t.Errorf("folded A to folded B: %d rows apart, want 2", ib-ia)
	}
	if ic-ib != 2 {
		t.Errorf("folded B to C: %d rows apart, want 2", ic-ib)
	}
	opt.Folds = nil
	p = renderSrc(src, opt)
	ib, _ = lineWith(p, "B")
	ic, _ = lineWith(p, "C")
	if ic-ib != 5 { // B, blank, more, blank, blank, C
		t.Errorf("open sections: B to C %d rows apart, want 5", ic-ib)
	}
}

// In a mirrored (RTL) table ":--" means the start side, which is the right.
func TestRTLTableAlignment(t *testing.T) {
	src := "| الجزء | وظيفته |\n|:--|:--|\n| أ | ب ت |\n"
	p := renderSrc(src, baseOpts(60))
	_, l := lineWith(p, "أ")
	txt := strings.TrimRight(l.Text(), " ")
	if !strings.HasSuffix(txt, "│     أ") {
		t.Errorf("RTL cell with :-- should sit at the right of its column: %q", txt)
	}
}

// A right-to-left note embedded in a page carries its bar on the right.
func TestEmbedRTLBar(t *testing.T) {
	opt := baseOpts(60)
	opt.Resolver = fakeResolver{notes: map[string]string{"Arabic.md": "# عنوان\n\nنص عربي طويل.\n"}}
	p := renderSrc("![[Arabic]]\n", opt)
	_, l := lineWith(p, "نص عربي")
	txt := strings.TrimRight(l.Text(), " ")
	if !strings.HasSuffix(txt, "نص عربي طويل. "+opt.Glyphs.Bar) {
		t.Errorf("RTL embed body should end with the bar: %q", txt)
	}
}

// A tag chip never wraps at its slash.
func TestTitleChipsDoNotBreakTags(t *testing.T) {
	src := "---\ntitle: T\ndate: 2026-03-14\ntags: [showcase, manuscripts, navigation/astrolabe]\n---\n\nx\n"
	for _, w := range []int{40, 50, 60, 64} {
		p := renderSrc(src, baseOpts(w))
		for _, l := range p.Lines {
			txt := strings.TrimSpace(l.Text())
			if strings.HasSuffix(txt, "#navigation/") || strings.HasPrefix(txt, "astrolabe") {
				t.Errorf("width %d: tag broken across lines at %q", w, txt)
			}
		}
		if _, l := lineWith(p, "#navigation/astrolabe"); l.Width == 0 {
			t.Errorf("width %d: tag not on one line", w)
		}
	}
}

// ciSlack widens timing budgets on shared CI runners (CI is set by GitHub
// Actions and most CI systems), whose noisy neighbours make wall-clock
// checks flaky; locally the strict budget applies.
func ciSlack(d time.Duration) time.Duration {
	if os.Getenv("CI") != "" {
		return 10 * d
	}
	return d
}
