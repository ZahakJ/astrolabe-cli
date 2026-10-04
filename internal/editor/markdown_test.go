package editor

import (
	"strings"
	"testing"

	"github.com/ZahakJ/astrolabe-cli/internal/text"
)

func TestListContinuation(t *testing.T) {
	runCases(t, []keyCase{
		{"bullet", "█- item", "A<CR>next<Esc>", "- item\n- nex█t", ModeNormal},
		{"star", "█* a", "A<CR>b<Esc>", "* a\n* █b", ModeNormal},
		{"empty ends list", "█- item", "A<CR><CR>x<Esc>", "- item\n█x", ModeNormal},
		{"numbered", "█1. a", "A<CR>b<Esc>", "1. a\n2. █b", ModeNormal},
		{"paren numbered", "█9) a", "A<CR>b<Esc>", "9) a\n10) █b", ModeNormal},
		{"renumbers followers", "█1. a\n2. b\n3. c", "A<CR>x<Esc>", "1. a\n2. █x\n3. b\n4. c", ModeNormal},
		{"task", "█- [x] done", "A<CR>t<Esc>", "- [x] done\n- [ ] █t", ModeNormal},
		{"empty task ends", "█- [ ] a", "A<CR><CR>", "- [ ] a\n█", ModeInsert},
		{"quote", "█> q", "A<CR>r<Esc>", "> q\n> █r", ModeNormal},
		{"empty quote ends", "█> q", "A<CR><CR>", "> q\n█", ModeInsert},
		{"callout", "█> [!note] Title", "A<CR>body<Esc>", "> [!note] Title\n> bod█y", ModeNormal},
		{"list in quote", "█> - a", "A<CR>b<Esc>", "> - a\n> - █b", ModeNormal},
		{"list in quote ends to quote", "█> - a", "A<CR><CR>", "> - a\n> █", ModeInsert},
		{"nested empty outdents", "- a\n  - █b", "A<CR><CR>c<Esc>", "- a\n  - b\n- █c", ModeNormal},
		{"split item", "- foo █bar", "i<CR><Esc>", "- foo\n-█ bar", ModeNormal},
		{"cursor before marker", "█- a", "i<CR><Esc>", "\n█- a", ModeNormal},
		{"autoindent", "  █text", "A<CR>x<Esc>", "  text\n  █x", ModeNormal},
		{"blank indent removed", "  █text", "A<CR><CR>x<Esc>", "  text\n\n  █x", ModeNormal},
		{"not in code", "```\n█- a\n```", "A<CR>b<Esc>", "```\n- a\n█b\n```", ModeNormal},
		{"rule is not a list", "█- - -", "A<CR>x<Esc>", "- - -\n█x", ModeNormal},
		{"bold is not a list", "█**b**", "A<CR>x<Esc>", "**b**\n█x", ModeNormal},
		{"dot repeats continuation", "█- a", "A<CR>b<Esc>.", "- a\n- b\n- █b", ModeNormal},
		{"wrapped item continues", "- one\n  █two", "A<CR>x<Esc>", "- one\n  two\n- █x", ModeNormal},
		{"wrapped numbered item", "1. one\n   █two\n2. b", "A<CR>x<Esc>", "1. one\n   two\n2. █x\n3. b", ModeNormal},
		{"wrapped nested item", "- a\n  - b\n    █c", "A<CR>x<Esc>", "- a\n  - b\n    c\n  - █x", ModeNormal},
		{"after blank: plain indent", "- a\n\n  █para", "A<CR>x<Esc>", "- a\n\n  para\n  █x", ModeNormal},
		{"mid-line split keeps indent", "- a\n  b█c", "i<CR><Esc>", "- a\n  b\n █ c", ModeNormal},
	})
}

func TestListIndent(t *testing.T) {
	runCases(t, []keyCase{
		{"tab nests", "- a\n- █b", "i<Tab><Esc>", "- a\n  -█ b", ModeNormal},
		{"shift-tab outdents", "- a\n  - █b", "i<S-Tab><Esc>", "- a\n-█ b", ModeNormal},
		{"ordered nest restarts", "1. a\n2. █b", "A<Tab><Esc>", "1. a\n   1. █b", ModeNormal},
		{"ordered nest continues", "1. a\n   1. x\n2. █b", "A<Tab><Esc>", "1. a\n   1. x\n   2. █b", ModeNormal},
		{"ordered outdent renumbers", "1. a\n   1. █b", "A<S-Tab><Esc>", "1. a\n2. █b", ModeNormal},
		{"ordered nest renumbers rest", "1. a\n2. █b\n3. c", "A<Tab><Esc>", "1. a\n   1. █b\n2. c", ModeNormal},
		{"task nests", "- [ ] a\n- [ ] █b", "A<Tab><Esc>", "- [ ] a\n  - [ ] █b", ModeNormal},
		{"first item", "█- a", "A<Tab><Esc>", "  - █a", ModeNormal},
		{"tab elsewhere", "a█b", "i<Tab><Esc>", "a  █ b", ModeNormal},
		{"cursor keeps text", "- a\n- b█c", "i<Tab>x<Esc>", "- a\n  - b█xc", ModeNormal},
		{"tab indent file", "- a\n\t- b\n- █c", "A<Tab><Esc>", "- a\n\t- b\n\t- █c", ModeNormal},
	})
}

func TestTable(t *testing.T) {
	runCases(t, []keyCase{
		{"align and next cell",
			"| a | b |\n|---|---|\n| █longer | x |",
			"i<Tab>",
			"| a      | b   |\n| ------ | --- |\n| longer | x█   |", ModeInsert},
		{"tab to next row",
			"| a | b |\n|---|---|\n| █1 | 2 |\n| 3 | 4 |",
			"i<Tab><Tab>x",
			"| a   | b   |\n| --- | --- |\n| 1   | 2   |\n| 3x█   | 4   |", ModeInsert},
		{"header skips delimiter",
			"| a | █b |\n|---|---|\n| 1 | 2 |",
			"i<Tab>",
			"| a   | b   |\n| --- | --- |\n| 1█   | 2   |", ModeInsert},
		{"new row",
			"| a | b |\n|---|---|\n| 1 | █2 |",
			"i<Tab>z",
			"| a   | b   |\n| --- | --- |\n| 1   | 2   |\n| z█    |     |", ModeInsert},
		{"shift-tab",
			"| a | b |\n|---|---|\n| 1 | █2 |",
			"i<S-Tab>",
			"| a   | b   |\n| --- | --- |\n| 1█   | 2   |", ModeInsert},
		{"alignment",
			"| l | c | r |\n|:--|:-:|--:|\n| █x | y | z |",
			"i<Tab>",
			"| l    |   c   |    r |\n| :--- | :---: | ---: |\n| x    |   y█   |    z |", ModeInsert},
		{"escaped pipe and code",
			"| a | b |\n|---|---|\n| █`x|y` | c\\|d |",
			"i<Tab>",
			"| a     | b    |\n| ----- | ---- |\n| `x|y` | c\\|d█ |", ModeInsert},
		{"undo is one step",
			"| a | b |\n|---|---|\n| █longer | x |",
			"i<Tab><Esc>u",
			"| a | b |\n|---|---|\n| █longer | x |", ModeNormal},
	})
	// wide and Arabic characters align by display width
	h := newHarness(t, "| a | b |\n|---|---|\n| █日本 | سلام |")
	h.keys("i<Tab><Esc>")
	lines := strings.Split(h.text(), "\n")
	w := text.Width(lines[0])
	for _, l := range lines {
		if text.Width(l) != w {
			t.Errorf("row %q width %d, header %d", l, text.Width(l), w)
		}
	}
}

func TestCompleter(t *testing.T) {
	h := newHarness(t, "█")
	h.keys("isee [[no")
	if !h.ed.CompleterOpen() {
		t.Fatal("completer not open after [[")
	}
	if it := h.ed.completer.items; len(it) < 2 || it[0].Target != "Note" || it[1].Target != "Notebook" {
		t.Fatalf("items for 'no' = %+v", it)
	}
	h.keys("<Down><CR>")
	if got := h.state(); got != "see [[Notebook]]█" {
		t.Errorf("accept: %q", got)
	}
	if h.ed.CompleterOpen() {
		t.Error("completer still open")
	}

	h = newHarness(t, "█")
	h.keys("i[[pl<CR>")
	if got := h.state(); got != "[[projects/Plan]]█" {
		t.Errorf("path target: %q", got)
	}

	// Esc keeps the typed text and stays in Insert
	h = newHarness(t, "█")
	h.keys("i[[xy<Esc>")
	if got := h.state(); got != "[[xy█" || h.ed.Mode() != ModeInsert {
		t.Errorf("esc: %q %v", got, h.ed.Mode())
	}
	h.keys("<Esc>")
	if h.ed.Mode() != ModeNormal {
		t.Error("second Esc did not leave Insert")
	}

	// no match: Enter closes the link with the typed name
	h = newHarness(t, "█")
	h.keys("i[[Zeta<CR>")
	if got := h.state(); got != "[[Zeta]]█" {
		t.Errorf("no match: %q", got)
	}

	// an existing ]] is stepped over
	h = newHarness(t, "█]]")
	h.keys("i[[<CR>")
	if got := h.state(); got != "[[Note]]█" {
		t.Errorf("step over: %q", got)
	}

	// backspacing over [[ closes it
	h = newHarness(t, "█")
	h.keys("i[[<BS>")
	if h.ed.CompleterOpen() {
		t.Error("completer open after deleting [")
	}

	// dot-repeat replays the accepted completion, not the popup keys
	h = newHarness(t, "█\nx")
	h.keys("i[[pl<CR><Esc>j0.")
	if got := h.text(); got != "[[projects/Plan]]\n[[projects/Plan]]x" {
		t.Errorf("dot: %q", got)
	}

	// [[[ does not open; nor does an escaped bracket
	h = newHarness(t, "█")
	h.keys("i[[[")
	if h.ed.CompleterOpen() {
		t.Error("opened on [[[")
	}
}

func TestEncodingRoundTrip(t *testing.T) {
	for _, in := range []string{
		"",
		"a",
		"a\n",
		"a\nb",
		"a\r\nb\r\n",
		"a\r\nb",
		"\ufeffa\r\nb\r\n",
		"\ufeff# T\n\nbody\n",
		"mixed\r\nlf\n",
		"\n\n",
		"tab\there\n",
	} {
		e := New(Config{Text: []byte(in)})
		if got := string(e.Text()); got != in {
			t.Errorf("round trip %q → %q", in, got)
		}
		if e.Dirty() {
			t.Errorf("%q dirty after open", in)
		}
	}
	h := newHarness(t, "")
	h.ed = New(Config{Text: []byte("\ufeffab\r\ncd\r\n")})
	h.keys("x")
	if got := string(h.ed.Text()); got != "\ufeffb\r\ncd\r\n" {
		t.Errorf("CRLF edit: %q", got)
	}
	h.keys("ox<Esc>")
	if got := string(h.ed.Text()); got != "\ufeffb\r\nx\r\ncd\r\n" {
		t.Errorf("CRLF new line: %q", got)
	}
	h.ed = New(Config{Text: []byte("a\nb")})
	h.keys("x")
	if got := string(h.ed.Text()); got != "\nb" {
		t.Errorf("no final newline: %q", got)
	}
	h.ed = New(Config{Text: nil})
	h.keys("ihi<Esc>")
	if got := string(h.ed.Text()); got != "hi\n" {
		t.Errorf("new file: %q", got)
	}
	// a mixed file keeps its stray CR on untouched lines
	h.ed = New(Config{Text: []byte("a\r\nb\nc\n")})
	h.keys("Gx")
	if got := string(h.ed.Text()); got != "a\r\nb\n\n" {
		t.Errorf("mixed: %q", got)
	}
}

func TestGraphemeSafety(t *testing.T) {
	const eAcute = "é"
	const fatha = "بَ"
	const family = "👨‍👩‍👧"
	runCases(t, []keyCase{
		{"combining x", "█" + eAcute + "a", "x", "█a", ModeNormal},
		{"combining l", "█" + eAcute + "a", "l", eAcute + "█a", ModeNormal},
		{"combining h", eAcute + "█a", "h", "█" + eAcute + "a", ModeNormal},
		{"harakat x", "█" + fatha + "ت", "x", "█ت", ModeNormal},
		{"harakat l", "█" + fatha + "ت", "l", fatha + "█ت", ModeNormal},
		{"emoji x", "█" + family + "x", "x", "█x", ModeNormal},
		{"emoji l", "█" + family + "x", "l", family + "█x", ModeNormal},
		{"emoji $", "█a" + family, "$", "a█" + family, ModeNormal},
		{"backspace cluster", family + "█", "a<BS><BS><Esc>", "█", ModeNormal},
		{"r on cluster", "█" + family + "x", "ry", "█yx", ModeNormal},
		{"f base letter", "█xx" + fatha, "fب", "xx█" + fatha, ModeNormal},
		{"w over arabic", "█سلام علیکم", "w", "سلام █علیکم", ModeNormal},
		{"e over arabic", "█سَلام علیکم", "e", "سَلا█م علیکم", ModeNormal},
		{"diw arabic", "█سلام علیکم", "diw", "█ علیکم", ModeNormal},
		{"j keeps display column", "a" + family + "█b\nabcdef", "j", "a" + family + "b\nabc█def", ModeNormal},
		{"~ on cluster", "█" + eAcute, "~", "█É", ModeNormal},
		{"cjk words", "█日本 語", "w", "日本 █語", ModeNormal},
		{"tab column", "\t█a\nabcdef", "j", "\ta\nabcd█ef", ModeNormal},
	})
	// SetCursor never lands inside a cluster
	e := New(Config{Text: []byte("a" + eAcute + "b")})
	e.SetCursor(0, 2)
	if p := e.CursorPos(); p.Col != 1 {
		t.Errorf("SetCursor inside cluster → col %d", p.Col)
	}
}

func TestStatus(t *testing.T) {
	h := newHarness(t, "ab\nć█d")
	st := h.ed.Status()
	if st.Line != 2 || st.Col != 2 || st.Lines != 2 || st.Mode != ModeNormal {
		t.Errorf("status %+v", st)
	}
	h.keys("2d")
	if got := h.ed.Status().Pending; got != "2d" {
		t.Errorf("pending %q", got)
	}
	if ModeVisualLine.String() != "V-LINE" || ModeInsert.String() != "INSERT" {
		t.Error("mode labels")
	}
}

func TestContentStart(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int
	}{
		{"plain text", 0},
		{"  indented", 2},
		{"- item", 2},
		{"  - [ ] task", 8},
		{"> quoted", 2},
		{"> - [x] quoted task", 8},
		{"12. twelfth", 4},
		{"## Heading", 3},
		{"#hashtag", 0},
		{"", 0},
		{"- ", 2},
	} {
		if got := contentStart(c.in); got != c.want {
			t.Errorf("contentStart(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
