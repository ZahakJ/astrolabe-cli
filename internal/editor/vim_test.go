package editor

import (
	"testing"
)

type keyCase struct {
	name string
	in   string
	keys string
	want string
	mode Mode
}

func runCases(t *testing.T, cases []keyCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, c.in)
			h.keys(c.keys)
			if got := h.state(); got != c.want {
				t.Errorf("%q + %q\n got: %q\nwant: %q", c.in, c.keys, got, c.want)
			}
			if h.ed.Mode() != c.mode {
				t.Errorf("mode = %v, want %v", h.ed.Mode(), c.mode)
			}
		})
	}
}

func TestMotions(t *testing.T) {
	runCases(t, []keyCase{
		{"w", "█foo bar baz", "w", "foo █bar baz", ModeNormal},
		{"2w", "█foo bar baz", "2w", "foo bar █baz", ModeNormal},
		{"w punct", "█foo.bar baz", "w", "foo█.bar baz", ModeNormal},
		{"W", "█foo.bar baz", "W", "foo.bar █baz", ModeNormal},
		{"w next line", "foo █bar\n  baz", "w", "foo bar\n  █baz", ModeNormal},
		{"w stops at empty line", "█foo\n\nbar", "w", "foo\n█\nbar", ModeNormal},
		{"w at end of buffer", "foo █bar", "w", "foo ba█r", ModeNormal},
		{"b", "foo bar █baz", "b", "foo █bar baz", ModeNormal},
		{"b mid word", "foo ba█r", "b", "foo █bar", ModeNormal},
		{"b prev line", "foo\n█bar", "b", "█foo\nbar", ModeNormal},
		{"B", "a foo.ba█r", "B", "a █foo.bar", ModeNormal},
		{"e", "█foo bar", "e", "fo█o bar", ModeNormal},
		{"ee", "█foo bar", "ee", "foo ba█r", ModeNormal},
		{"e from end", "fo█o bar", "e", "foo ba█r", ModeNormal},
		{"E", "█a.b c", "E", "a.█b c", ModeNormal},
		{"ge", "foo █bar", "ge", "fo█o bar", ModeNormal},
		{"0", "  ab█c", "0", "█  abc", ModeNormal},
		{"^", "█  abc", "^", "  █abc", ModeNormal},
		{"$", "█abc", "$", "ab█c", ModeNormal},
		{"2$", "█abc\ndef", "2$", "abc\nde█f", ModeNormal},
		{"j clamps", "ab█c\nd", "j", "abc\n█d", ModeNormal},
		{"j remembers column", "ab█cd\nx\nabcd", "jj", "abcd\nx\nab█cd", ModeNormal},
		{"$ sticks", "a█b\nabcdef", "$j", "ab\nabcde█f", ModeNormal},
		{"k", "abc\na█b", "k", "a█bc\nab", ModeNormal},
		{"3j logical", "█a\nb\nc\nd", "3j", "a\nb\nc\n█d", ModeNormal},
		{"h stops", "█abc", "h", "█abc", ModeNormal},
		{"3l", "█abcde", "3l", "abc█de", ModeNormal},
		{"l at end", "ab█c", "l", "ab█c", ModeNormal},
		{"f", "█abcabc", "fc", "ab█cabc", ModeNormal},
		{"2f", "█abcabc", "2fc", "abcab█c", ModeNormal},
		{"t", "█abc", "tc", "a█bc", ModeNormal},
		{"F", "abca█b", "Fa", "abc█ab", ModeNormal},
		{"T", "abca█b", "Ta", "abca█b", ModeNormal},
		{"f;", "█a,b,c", "f,;", "a,b█,c", ModeNormal},
		{"f;,", "█a,b,c", "f,;,", "a█,b,c", ModeNormal},
		{"t; skips adjacent", "█a,b,c", "t,;", "a,█b,c", ModeNormal},
		{"%", "█(a [b] c)", "%", "(a [b] c█)", ModeNormal},
		{"% back", "(a [b] c█)", "%", "█(a [b] c)", ModeNormal},
		{"% finds bracket", "x█ = f(a)", "%", "x = f(a█)", ModeNormal},
		{"% multiline", "█{\n  a\n}", "%", "{\n  a\n█}", ModeNormal},
		{"}", "█a\nb\n\nc", "}", "a\nb\n█\nc", ModeNormal},
		{"} to end", "█a\nbc", "}", "a\nb█c", ModeNormal},
		{"{", "a\n\nb\n█c", "{", "a\n█\nb\nc", ModeNormal},
		{"G", "█a\nb\n  c", "G", "a\nb\n  █c", ModeNormal},
		{"gg", "a\nb\n█c", "gg", "█a\nb\nc", ModeNormal},
		{"2G", "█a\n  b\nc", "2G", "a\n  █b\nc", ModeNormal},
		{"50%", "█1\n2\n3\n4", "50%", "1\n█2\n3\n4", ModeNormal},
		{"enter", "█a\n  b", "<CR>", "a\n  █b", ModeNormal},
		{"-", "  a\n█b", "-", "  █a\nb", ModeNormal},
		{"space wraps", "a█b\nc", "  ", "ab\n█c", ModeNormal},
		{"backspace wraps", "ab\n█c", "<BS>", "a█b\nc", ModeNormal},
		{"arrows", "█ab\ncd", "<Right><Down>", "ab\nc█d", ModeNormal},
		{"H", "a\n█b\nc", "H", "█a\nb\nc", ModeNormal},
		{"L", "█a\nb\nc", "L", "a\nb\n█c", ModeNormal},
		{"M", "█a\nb\nc", "M", "a\n█b\nc", ModeNormal},
	})
}

func TestOperators(t *testing.T) {
	runCases(t, []keyCase{
		{"dw", "█foo bar", "dw", "█bar", ModeNormal},
		{"dw last word", "foo █bar", "dw", "foo█ ", ModeNormal},
		{"dw no join", "foo █bar\nbaz", "dw", "foo█ \nbaz", ModeNormal},
		{"dw empty line", "a\n█\nb", "dw", "a\n█b", ModeNormal},
		{"dw on blanks", "foo█   bar", "dw", "foo█bar", ModeNormal},
		{"d2w", "█a b c", "d2w", "█c", ModeNormal},
		{"2d2w", "█a b c d e", "2d2w", "█e", ModeNormal},
		{"de", "█foo bar", "de", "█ bar", ModeNormal},
		{"db", "foo █bar", "db", "█bar", ModeNormal},
		{"d$", "a█bc", "d$", "█a", ModeNormal},
		{"D", "a█bc", "D", "█a", ModeNormal},
		{"d0", "ab█c", "d0", "█c", ModeNormal},
		{"dd", "a\n█b\nc", "dd", "a\n█c", ModeNormal},
		{"dd last", "a\n█b", "dd", "█a", ModeNormal},
		{"dd only", "█a", "dd", "█", ModeNormal},
		{"2dd", "█a\nb\nc", "2dd", "█c", ModeNormal},
		{"d2j", "█a\nb\nc\nd", "d2j", "█d", ModeNormal},
		{"dk", "a\n█b\nc", "dk", "█c", ModeNormal},
		{"dG", "a\n█b\nc", "dG", "█a", ModeNormal},
		{"dgg", "a\nb\n█c\nd", "dgg", "█d", ModeNormal},
		{"d}", "█a\nb\n\nc", "d}", "█\nc", ModeNormal},
		{"d} mid line", "x █a\nb\n\nc", "d}", "x█ \n\nc", ModeNormal},
		{"dfx", "█abxcd", "dfx", "█cd", ModeNormal},
		{"dtx", "█abxcd", "dtx", "█xcd", ModeNormal},
		{"d%", "f█(a)b", "d%", "f█b", ModeNormal},
		{"dl", "a█bc", "dl", "a█c", ModeNormal},
		{"dh", "a█bc", "dh", "█bc", ModeNormal},
		{"x", "a█bc", "x", "a█c", ModeNormal},
		{"x at end", "ab█c", "x", "a█b", ModeNormal},
		{"3x", "█abcd", "3x", "█d", ModeNormal},
		{"x count past end", "a█bc", "5x", "█a", ModeNormal},
		{"X", "ab█c", "X", "a█c", ModeNormal},
		{"xp", "█ab", "xp", "b█a", ModeNormal},
		{"ddp", "█a\nb", "ddp", "b\n█a", ModeNormal},
		{"cw", "█foo bar", "cwX<Esc>", "█X bar", ModeNormal},
		{"cw end of word", "fo█o bar", "cwX<Esc>", "fo█X bar", ModeNormal},
		{"cw blanks", "foo█  bar", "cwX<Esc>", "foo█Xbar", ModeNormal},
		{"c2w", "█a b c", "c2wX<Esc>", "█X c", ModeNormal},
		{"cc keeps indent", "  █abc", "ccx<Esc>", "  █x", ModeNormal},
		{"S", "  a█bc\nd", "Sx<Esc>", "  █x\nd", ModeNormal},
		{"C", "a█bc", "Cx<Esc>", "a█x", ModeNormal},
		{"s", "a█bc", "sx<Esc>", "a█xc", ModeNormal},
		{"2s", "a█bcd", "2sx<Esc>", "a█xd", ModeNormal},
		{"c$ end", "ab█c", "c$<Esc>", "a█b", ModeNormal},
		{"yw P", "█foo bar", "ywP", "foo█ foo bar", ModeNormal},
		{"yyp", "█a\nb", "yyp", "a\n█a\nb", ModeNormal},
		{"yyP", "a\n█b", "yyP", "a\n█b\nb", ModeNormal},
		{"3p linewise", "█a", "yy3p", "a\n█a\na\na", ModeNormal},
		{"p charwise", "█abc", "ylp", "a█abc", ModeNormal},
		{"P charwise", "a█bc", "ylP", "a█bbc", ModeNormal},
		{"p multiline charwise", "█ab\ncd", "vjyP", "█ab\ncab\ncd", ModeNormal},
		{"yb moves cursor", "foo █bar", "yb", "█foo bar", ModeNormal},
		{"yip moves to start", "a\n█b\n\nc", "yip", "█a\nb\n\nc", ModeNormal},
		{"Y", "a█bc", "Yp", "abb█cc", ModeNormal},
		{">>", "█a", ">>", "  █a", ModeNormal},
		{"<<", "    █a", "<<", "  █a", ModeNormal},
		{"2>>", "█a\nb\nc", "2>>", "  █a\n  b\nc", ModeNormal},
		{">j", "█a\n\nc", ">2j", "  █a\n\n  c", ModeNormal},
		{"J", "█a\n  b", "J", "a█ b", ModeNormal},
		{"J paren", "█f(a\n)", "J", "f(a█)", ModeNormal},
		{"3J", "█a\nb\nc", "3J", "a b█ c", ModeNormal},
		{"gJ", "█a\n  b", "gJ", "a█  b", ModeNormal},
		{"~", "█abC", "~~~", "AB█c", ModeNormal},
		{"g~iw", "█abC d", "g~iw", "█ABc d", ModeNormal},
		{"gUU", "█ab", "gUU", "█AB", ModeNormal},
		{"r", "█abc", "rx", "█xbc", ModeNormal},
		{"3r", "█abc", "3rx", "xx█x", ModeNormal},
		{"r too many", "█ab", "3rx", "█ab", ModeNormal},
		{"r enter", "a█bc", "r<CR>", "a\n█c", ModeNormal},
	})
}

func TestTextObjects(t *testing.T) {
	runCases(t, []keyCase{
		{"ciw", "█foo bar", "ciwX<Esc>", "█X bar", ModeNormal},
		{"diw middle", "foo b█ar baz", "diw", "foo █ baz", ModeNormal},
		{"diw blanks", "foo█   bar", "diw", "foo█bar", ModeNormal},
		{"daw", "█foo bar", "daw", "█bar", ModeNormal},
		{"daw end", "foo █bar", "daw", "fo█o", ModeNormal},
		{"d2aw", "█a b c", "d2aw", "█c", ModeNormal},
		{"yiw", "foo b█ar", "yiwP", "foo ba█rbar", ModeNormal},
		{"ciW", "█a.b c", "ciWx<Esc>", "█x c", ModeNormal},
		{"di\"", `say "h█i there" ok`, `di"`, `say "█" ok`, ModeNormal},
		{"da\"", `say "h█i" ok`, `da"`, `say █ok`, ModeNormal},
		{"ci\" before", `█x "a" "b"`, `ci"z<Esc>`, `x "█z" "b"`, ModeNormal},
		{"di' on quote", `a 'b█' c`, `di'`, `a '█' c`, ModeNormal},
		{"ci(", "f(a, █b)", "ci(x<Esc>", "f(█x)", ModeNormal},
		{"da(", "f(a, █b)x", "da(", "f█x", ModeNormal},
		{"dib", "f(█a)", "dib", "f(█)", ModeNormal},
		{"di( nested", "f(a, g(█b), c)", "di(", "f(a, g(█), c)", ModeNormal},
		{"d2i(", "f(a, g(█b), c)", "d2i(", "f(█)", ModeNormal},
		{"di[", "[a █b]", "di[", "[█]", ModeNormal},
		{"di{ linewise", "f {\n  █a\n  b\n}", "di{", "f {\n█}", ModeNormal},
		{"di`", "use `c█ode` here", "di`", "use `█` here", ModeNormal},
		{"dip", "a\n█b\n\nc", "dip", "█\nc", ModeNormal},
		{"dap", "a\n█b\n\nc", "dap", "█c", ModeNormal},
		{"dap last", "a\n\nb\n█c", "dap", "█a", ModeNormal},
		{"yi( no pair", "a █b", "di(", "a █b", ModeNormal},
	})
}

func TestInsertAndRepeat(t *testing.T) {
	runCases(t, []keyCase{
		{"i", "a█b", "iX<Esc>", "a█Xb", ModeNormal},
		{"a", "a█b", "aX<Esc>", "ab█X", ModeNormal},
		{"a empty", "█", "aX<Esc>", "█X", ModeNormal},
		{"A", "█ab", "Ac<Esc>", "ab█c", ModeNormal},
		{"I", "  a█b", "Ix<Esc>", "  █xab", ModeNormal},
		{"gI", "  a█b", "gIx<Esc>", "█x  ab", ModeNormal},
		{"o", "█a\nc", "ob<Esc>", "a\n█b\nc", ModeNormal},
		{"O", "a\n█c", "Ob<Esc>", "a\n█b\nc", ModeNormal},
		{"o indent", "  █a", "ob<Esc>", "  a\n  █b", ModeNormal},
		{"o list", "- █a", "ob<Esc>", "- a\n- █b", ModeNormal},
		{"3ix", "█a", "3ix<Esc>", "xx█xa", ModeNormal},
		{"3o", "█a", "3ob<Esc>", "a\nb\nb\n█b", ModeNormal},
		{"insert stays", "█a", "ix", "x█a", ModeInsert},
		{"esc moves left", "ab█", "A<Esc>", "a█b", ModeNormal},
		{"backspace", "ab█c", "i<BS><Esc>", "█ac", ModeNormal},
		{"backspace joins", "a\n█b", "i<BS><Esc>", "█ab", ModeNormal},
		{"del joins", "a█\nb", "a<Del><Esc>", "█ab", ModeNormal},
		{"ctrl-w", "foo bar", "A<C-w><Esc>", "foo█ ", ModeNormal},
		{"ctrl-u", "  foo bar", "A<C-u><Esc>", " █ ", ModeNormal},
		{"dot dw", "█a b c", "dw.", "█c", ModeNormal},
		{"dot count", "█a b c d e", "dw3.", "█e", ModeNormal},
		{"dot keeps new count", "█a b c d e f g", "dw2..", "█f g", ModeNormal},
		{"dot ciw", "█a b", "ciwfoo<Esc>w.", "foo fo█o", ModeNormal},
		{"dot A", "█a\nb", "Ax<Esc>j.", "ax\nb█x", ModeNormal},
		{"dot 3ix", "█a", "3ix<Esc>.", "xxxx█xxa", ModeNormal},
		{"dot x", "█abcd", "x..", "█d", ModeNormal},
		{"dot dd", "█a\nb\nc", "dd.", "█c", ModeNormal},
		{"dot o", "█a", "ob<Esc>.", "a\nb\n█b", ModeNormal},
		{"dot visual", "█abcdef", "vld.", "█ef", ModeNormal},
		{"dot visual line", "█a\nb\nc\nd\ne", "Vjd.", "█e", ModeNormal},
		{"dot >", "█a\nb", ">>j.", "  a\n  █b", ModeNormal},
		{"dot r", "█abc", "rxl.", "x█xc", ModeNormal},
		{"dot after move in insert", "█ab", "ix<Right>y<Esc>.", "xa█yyb", ModeNormal},
		{"dot d/", "█a x b x c", "d/x<CR>.", "█x c", ModeNormal},
		{"timestamp", "█", "i<C-t><Esc>", "2026-10-04 09:3█0", ModeNormal},
		{"paste in insert", "█", "i", "█", ModeInsert},
	})
}

func TestUndoRedo(t *testing.T) {
	runCases(t, []keyCase{
		{"insert one group", "█a", "ixyz<Esc>u", "█a", ModeNormal},
		{"two inserts", "█a", "ix<Esc>iy<Esc>u", "█xa", ModeNormal},
		{"redo", "█a", "ix<Esc>iy<Esc>u<C-r>", "█yxa", ModeNormal},
		{"undo dd", "a\n█b\nc", "ddu", "a\n█b\nc", ModeNormal},
		{"undo count", "█abc", "xxx2u", "█bc", ModeNormal},
		{"cw is one change", "█foo bar", "cwbaz<Esc>u", "█foo bar", ModeNormal},
		{"undo substitute", "█a a\na", ":%s/a/b/g<CR>u", "█a a\na", ModeNormal},
		{"undo past start", "█a", "u", "█a", ModeNormal},
		{"undo dot", "█a b c", "dw.u", "█b c", ModeNormal},
		{"undo list continuation", "- a█", "A<CR>b<Esc>u", "- █a", ModeNormal},
	})
	h := newHarness(t, "█abc")
	h.keys("x")
	if !h.ed.Dirty() {
		t.Fatal("not dirty after x")
	}
	h.keys("u")
	if h.ed.Dirty() {
		t.Fatal("dirty after undo to the original")
	}
	h.keys("x")
	h.ed.MarkSaved()
	if h.ed.Dirty() {
		t.Fatal("dirty after MarkSaved")
	}
	h.keys("u")
	if !h.ed.Dirty() {
		t.Fatal("clean after undoing past the save")
	}
}

func TestVisual(t *testing.T) {
	runCases(t, []keyCase{
		{"v d", "█abc def", "vlld", "█ def", ModeNormal},
		{"v e y", "x █abc def", "veyP", "x ab█cabc def", ModeNormal},
		{"v j d", "a█bc\ndef", "vjd", "a█f", ModeNormal},
		{"V d", "a\n█b\nc", "Vd", "a\n█c", ModeNormal},
		{"V j d", "█a\nb\nc", "Vjd", "█c", ModeNormal},
		{"V >", "█a\nb", "Vj>", "  █a\n  b", ModeNormal},
		{"V 2>", "█a", "V2>", "    █a", ModeNormal},
		{"v o", "a█bcd", "vlohd", "█d", ModeNormal},
		{"v iw", "foo b█ar baz", "viwd", "foo █ baz", ModeNormal},
		{"v ip", "a\n█b\n\nc", "vipd", "█\nc", ModeNormal},
		{"v c", "a█bcd", "vlcX<Esc>", "a█Xd", ModeNormal},
		{"v ~", "█abc", "vl~", "█ABc", ModeNormal},
		{"v U", "█abc", "vlU", "█ABc", ModeNormal},
		{"v r", "█abc", "vlrx", "█xxc", ModeNormal},
		{"v J", "█a\nb\nc", "vjjJ", "a b█ c", ModeNormal},
		{"v p", "█foo bar", "yiwwviwp", "foo fo█o", ModeNormal},
		{"V p", "█a\nb", "yyjVp", "a\n█a", ModeNormal},
		{"v $ d", "a█bc\nd", "v$d", "a█d", ModeNormal},
		{"esc", "█abc", "vl<Esc>", "a█bc", ModeNormal},
		{"v toggle V", "█a\nb", "vVd", "█b", ModeNormal},
		{"gv", "█abc d", "vl<Esc>0gvd", "█c d", ModeNormal},
		{"v stays", "█abc", "vl", "a█bc", ModeVisual},
		{"V mode", "█abc", "V", "█abc", ModeVisualLine},
		{"v y register", "█abc", "vly$p", "abca█b", ModeNormal},
		{"v :s", "a\n█a\na", "Vj:s/a/b/<CR>", "a\nb\n█b", ModeNormal},
	})
}

func TestSearch(t *testing.T) {
	runCases(t, []keyCase{
		{"/", "█foo bar foo", "/foo<CR>", "foo bar █foo", ModeNormal},
		{"n wraps", "█foo bar foo", "/foo<CR>n", "█foo bar foo", ModeNormal},
		{"N", "foo bar █foo", "/bar<CR>N", "foo █bar foo", ModeNormal},
		{"?", "foo bar █foo", "?foo<CR>", "█foo bar foo", ModeNormal},
		{"next line", "█a\nxb\nb", "/b<CR>", "a\nx█b\nb", ModeNormal},
		{"smartcase lower matches upper", "█x Foo", "/foo<CR>", "x █Foo", ModeNormal},
		{"smartcase upper exact", "█x foo Foo", "/Foo<CR>", "x foo █Foo", ModeNormal},
		{"\\c", "█x FOO", "/foo\\c<CR>", "x █FOO", ModeNormal},
		{"magic group", "█x a abab", "/\\(ab\\)\\+<CR>", "x a █abab", ModeNormal},
		{"literal paren", "█x f(a)", "/f(<CR>", "x █f(a)", ModeNormal},
		{"dot is any", "█x a.c", "/a.c<CR>", "x █a.c", ModeNormal},
		{"star", "█foo bar foo", "*", "foo bar █foo", ModeNormal},
		{"star word boundary", "█foo foobar foo", "*", "foo foobar █foo", ModeNormal},
		{"star unicode", "█سلام x سلامت سلام", "*", "سلام x سلامت █سلام", ModeNormal},
		{"#", "foo bar █foo", "#", "█foo bar foo", ModeNormal},
		{"empty pattern repeats", "█a b a b", "/b<CR>//<CR>", "a b a █b", ModeNormal},
		{"3n", "█a a a a", "/a<CR>2n", "a a a █a", ModeNormal},
		{"esc cancels", "█abc abc", "/abc<Esc>", "█abc abc", ModeNormal},
		{"d/", "█foo bar", "d/bar<CR>", "█bar", ModeNormal},
		{"cancel d/", "█foo bar", "d/bar<Esc>x", "█oo bar", ModeNormal},
		{"history", "█a b a b", "/b<CR>/<Up><CR>", "a b a █b", ModeNormal},
	})
	h := newHarness(t, "█foo")
	h.keys("/zzz<CR>")
	if r := h.last(); !r.Error || r.Message == "" {
		t.Errorf("missing not-found error: %+v", r)
	}
	h = newHarness(t, "█foo bar foo")
	h.keys("/foo<CR>n")
	if r := h.last(); r.Message == "" {
		t.Errorf("missing wrap message")
	}
}

func TestSubstitute(t *testing.T) {
	runCases(t, []keyCase{
		{"s", "█a a", ":s/a/b/<CR>", "█b a", ModeNormal},
		{"s g", "█a a", ":s/a/b/g<CR>", "█b b", ModeNormal},
		{"%s", "█a\nb a\na", ":%s/a/x/g<CR>", "x\nb x\n█x", ModeNormal},
		{"range", "█a\na\na", ":2,3s/a/x/<CR>", "a\nx\n█x", ModeNormal},
		{"groups", "█ab", ":s/\\(a\\)\\(b\\)/\\2\\1/<CR>", "█ba", ModeNormal},
		{"ampersand", "█a", ":s/a/[&]/<CR>", "█[a]", ModeNormal},
		{"escaped amp", "█a", ":s/a/\\&/<CR>", "█&", ModeNormal},
		{"other delimiter", "█a/b", ":s#/#-#<CR>", "█a-b", ModeNormal},
		{"line break", "█a,b", ":s/,/\\r/<CR>", "a\n█b", ModeNormal},
		{"empty uses last search", "█foo x", "/x<CR>:s//y/<CR>", "█foo y", ModeNormal},
		{"case flag", "█A", ":s/a/b/i<CR>", "█b", ModeNormal},
		{"& repeats", "█a a\na a", ":s/a/b/<CR>j&", "b a\n█b a", ModeNormal},
		{"word boundary", "█cat concat cat", ":s/\\<cat\\>/dog/g<CR>", "█dog concat dog", ModeNormal},
		{"unicode", "█café", ":s/é/e/<CR>", "█cafe", ModeNormal},
	})
	h := newHarness(t, "█a\nb a a\na")
	h.keys(":%s/a/x/g<CR>")
	if r := h.last(); r.Message != "4 substitutions on 3 lines" {
		t.Errorf("message = %q", r.Message)
	}
	h = newHarness(t, "█a")
	h.keys(":s/z/y/<CR>")
	if r := h.last(); !r.Error {
		t.Errorf("no error for missing pattern: %+v", r)
	}
}

func TestExAndActions(t *testing.T) {
	type ac struct {
		in, keys string
		act      Action
		cmd      string
		err      bool
	}
	cases := []ac{
		{"█a", "<Esc>", ActionLeave, "", false},
		{"█a", "d<Esc>", ActionNone, "", false},
		{"█a", "ix<Esc><Esc>", ActionLeave, "", false},
		{"█a", ":w<CR>", ActionSave, "", false},
		{"█a", "<C-s>", ActionSave, "", false},
		{"█a", "ix<C-s>", ActionSave, "", false},
		{"█a", ":q<CR>", ActionQuit, "", false},
		{"█a", "x:q<CR>", ActionNone, "", true},
		{"█a", "x:q!<CR>", ActionQuitDiscard, "", false},
		{"█a", ":wq<CR>", ActionSaveQuit, "", false},
		{"█a", ":x<CR>", ActionSaveQuit, "", false},
		{"█a", "ZZ", ActionSaveQuit, "", false},
		{"█a", "ZQ", ActionQuitDiscard, "", false},
		{"█a", ":e Other note<CR>", ActionCommand, "e Other note", false},
		{"█a", ":theme mocha<CR>", ActionCommand, "theme mocha", false},
		{"█a", ":w other.md<CR>", ActionCommand, "w other.md", false},
		{"█a", ":set bogus<CR>", ActionNone, "", true},
	}
	for _, c := range cases {
		h := newHarness(t, c.in)
		h.keys(c.keys)
		r := h.last()
		if r.Action != c.act || r.Command != c.cmd || r.Error != c.err {
			t.Errorf("%q: got %+v, want action %v cmd %q err %v", c.keys, r, c.act, c.cmd, c.err)
		}
	}
	runCases(t, []keyCase{
		{":N", "█a\nb\n  c", ":3<CR>", "a\nb\n  █c", ModeNormal},
		{":$", "█a\nb", ":$<CR>", "a\n█b", ModeNormal},
		{":d", "█a\nb\nc", ":2d<CR>", "a\n█c", ModeNormal},
		{":>", "█a", ":><CR>", "  █a", ModeNormal},
		{":j", "█a\nb", ":j<CR>", "a█ b", ModeNormal},
		{"count :", "█a\nb\nc", "2:d<CR>", "█c", ModeNormal},
	})
	h := newHarness(t, "█a")
	h.keys(":set nu<CR>")
	if !h.ed.number {
		t.Error(":set nu did not enable numbers")
	}
	h.keys(":set wrap=40<CR>")
	if h.ed.measure != 40 {
		t.Errorf("measure = %d", h.ed.measure)
	}
	h.keys(":noh<CR>")
	if h.ed.search.hl {
		t.Error(":noh left highlighting on")
	}
}

func TestRegisters(t *testing.T) {
	runCases(t, []keyCase{
		{"named", "█foo bar", "\"ayiww\"ap", "foo bfo█oar", ModeNormal},
		{"append", "█a b", "\"ayl w\"Ayl$\"ap", "a ba█b", ModeNormal},
		{"black hole", "█a b", "yiww\"_dwP", "a█a ", ModeNormal},
		{"yank register survives delete", "█a b", "yiwwdw\"0P", "a█a ", ModeNormal},
	})
	h := newHarness(t, "█line")
	h.keys("\"+yy")
	if len(h.clip) != 1 || h.clip[0] != "line\n" {
		t.Errorf("clipboard = %q", h.clip)
	}
	h.keys("\"+p")
	if h.text() != "line\nline" {
		t.Errorf("\"+p: %q", h.text())
	}
	if txt, lw := h.ed.Register('"'); txt != "line" || !lw {
		t.Errorf("unnamed = %q %v", txt, lw)
	}
}

func TestPaste(t *testing.T) {
	h := newHarness(t, "a█b")
	h.keys("i")
	h.ed.HandlePaste(pasteEvent("x\r\ny"))
	h.keys("<Esc>")
	if got := h.state(); got != "ax\n█yb" {
		t.Errorf("insert paste: %q", got)
	}
	h = newHarness(t, "a█b")
	h.ed.HandlePaste(pasteEvent("XY"))
	if got := h.state(); got != "aX█Yb" {
		t.Errorf("normal paste: %q", got)
	}
	h.keys("u")
	if got := h.text(); got != "ab" {
		t.Errorf("undo paste: %q", got)
	}
	h = newHarness(t, "█ab")
	h.keys("/")
	h.ed.HandlePaste(pasteEvent("b\nzzz"))
	h.keys("<CR>")
	if got := h.state(); got != "a█b" {
		t.Errorf("cmdline paste: %q", got)
	}
}

func TestEdgeCases(t *testing.T) {
	runCases(t, []keyCase{
		{"cw last char", "foo ba█r", "cwX<Esc>", "foo ba█X", ModeNormal},
		{"5dd at end", "a\n█b", "5dd", "█a", ModeNormal},
		{"3dd clamps", "a\n█b\nc", "3dd", "█a", ModeNormal},
		{"x empty line", "a\n█\nb", "x", "a\n█\nb", ModeNormal},
		{"p into empty buffer", "█", "p", "█", ModeNormal},
		{"dot p", "█a", "ylpp.", "aaa█a", ModeNormal},
		{"A empty line", "█", "Ax<Esc>", "█x", ModeNormal},
		{"o at end", "a\n█b", "oc<Esc>", "a\nb\n█c", ModeNormal},
		{"J at last line", "a\n█b", "J", "a\n█b", ModeNormal},
		{"J empty next", "█a\n\nb", "J", "█a\nb", ModeNormal},
		{"ciw on blanks", "a█   b", "ciwx<Esc>", "a█xb", ModeNormal},
		{"yy count past end", "a\n█b", "3yyP", "a\n█b\nb", ModeNormal},
		{"G count past end", "█a\nb", "9G", "a\n█b", ModeNormal},
		{"w over punctuation run", "█a == b", "w", "a █== b", ModeNormal},
		{"cc on list", "█- item", "ccx<Esc>", "█x", ModeNormal},
		{"d$ on empty", "a\n█\nb", "d$", "a\n█\nb", ModeNormal},
		{"V y p", "█a\nb", "Vyp", "a\n█a\nb", ModeNormal},
		{"v iw extends", "█foo bar", "viwd", "█ bar", ModeNormal},
		{"tilde count", "█abc", "5~", "AB█C", ModeNormal},
		{"esc cancels count", "█abc", "3<Esc>x", "█bc", ModeNormal},
		{"esc cancels operator", "█abc", "d<Esc>x", "█bc", ModeNormal},
		{"esc cancels register", "█abc", "\"<Esc>x", "█bc", ModeNormal},
	})
}
