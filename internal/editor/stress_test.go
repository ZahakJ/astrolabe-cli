package editor

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/folio/internal/term"
)

const sampleNote = "---\ntitle: Sample\ntags: [a, b]\n---\n# Heading\n\nSome *text* with **bold**, `code` and [[Link|label]].\n" +
	"- item one\n  - nested [ ] two\n1. first\n2. second\n- [ ] task\n> quote line\n> [!tip] Callout\n\n" +
	"| a | b |\n|---|---|\n| 1 | 2 |\n\n```go\nfunc main() {}\n```\nسلام علیکم é 👨‍👩‍👧 日本語\n\tTabbed\tline\n"

var stormKeys = []string{
	"h", "j", "k", "l", "w", "b", "e", "W", "B", "E", "0", "^", "$", "gg", "G", "{", "}", "%", "H", "M", "L",
	"x", "X", "dd", "dw", "cw", "ciw", "daw", "yy", "p", "P", "u", "<C-r>", ".", "J", "~", ">>", "<<",
	"i", "a", "A", "I", "o", "O", "<Esc>", "<Esc>", "v", "V", "d", "y", "c", "fx", "t ", ";", ",",
	"r!", "s", "S", "C", "D", "Y", "*", "n", "N", "di(", "ci\"", "dip", "yap", "3", "2", "<CR>", "<BS>",
	"<Tab>", "<S-Tab>", "[[", "]", "<C-t>", "<C-w>", "<C-u>", "<Up>", "<Down>", "<Left>", "<Right>",
	"<C-d>", "<C-u>", "<C-f>", "<C-b>", "zz", "-", " ", "foo ", "سل", "é", "|", "# ", "- ",
	"/a<CR>", "?e<CR>", ":s/a/b/g<CR>", ":%s/e/E/<CR>", "gJ", "g~w", "gv", "o<Esc>", "<Del>",
}

func TestKeyStorm(t *testing.T) {
	seeds := 60
	steps := 400
	if testing.Short() {
		seeds = 10
	}
	for seed := 0; seed < seeds; seed++ {
		r := rand.New(rand.NewSource(int64(seed)))
		e := New(Config{
			Text:      []byte(sampleNote),
			Width:     30 + r.Intn(60),
			Height:    5 + r.Intn(25),
			Measure:   20 + r.Intn(60),
			Bidi:      r.Intn(2) == 0,
			Number:    r.Intn(3) == 0,
			Completer: FuzzyCompleter([]Candidate{{Target: "Link", Label: "Link"}}),
			Now:       func() time.Time { return time.Unix(0, 0) },
		})
		s := newScreen(100, 40)
		var log []string
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("seed %d panicked after %q: %v", seed, log, p)
				}
			}()
			for i := 0; i < steps; i++ {
				k := stormKeys[r.Intn(len(stormKeys))]
				log = append(log, k)
				if r.Intn(40) == 0 {
					e.HandlePaste(term.PasteEvent{Text: "pasted\nlines"})
				} else {
					for _, ev := range parseKeys(k) {
						e.HandleKey(ev)
					}
				}
				if r.Intn(10) == 0 {
					e.Draw(s, term.Rect{X: r.Intn(5), Y: r.Intn(3), W: e.width, H: e.height})
				}
				checkInvariants(t, e, seed, log)
			}
			// undo everything: back to the original text
			for _, ev := range parseKeys("<Esc><Esc>") {
				e.HandleKey(ev)
			}
			if e.cmd.active {
				e.HandleKey(term.KeyEvent{Key: term.KeyEscape})
			}
			for i := 0; i < steps*2 && len(e.buf.undo) > 0; i++ {
				e.HandleKey(term.KeyEvent{Key: term.KeyRune, Rune: 'u'})
			}
			if got := string(e.Text()); got != sampleNote {
				t.Fatalf("seed %d: undo all did not restore the text:\n%q\nkeys %q", seed, got, log)
			}
		}()
	}
}

func checkInvariants(t *testing.T, e *Editor, seed int, log []string) {
	t.Helper()
	b := e.buf
	if len(b.lines) == 0 {
		t.Fatalf("seed %d: no lines after %q", seed, log)
	}
	p := e.cur
	if p.Line < 0 || p.Line >= len(b.lines) {
		t.Fatalf("seed %d: cursor line %d of %d after %q", seed, p.Line, len(b.lines), log)
	}
	l := b.lines[p.Line]
	if p.Col < 0 || p.Col > len(l) {
		t.Fatalf("seed %d: cursor col %d of %d after %q", seed, p.Col, len(l), log)
	}
	if snapCol(l, p.Col) != p.Col {
		t.Fatalf("seed %d: cursor inside a cluster (%d in %q) after %q", seed, p.Col, l, log)
	}
	if e.mode == ModeNormal && !e.cmd.active && p.Col > 0 && p.Col >= len(l) {
		t.Fatalf("seed %d: Normal cursor past end (%d in %q) after %q", seed, p.Col, l, log)
	}
	for _, s := range b.lines {
		if strings.Contains(s, "\n") {
			t.Fatalf("seed %d: newline inside a line after %q", seed, log)
		}
	}
}

func bigNote(n int) string {
	var sb strings.Builder
	for i := 0; sb.Len() < n; i++ {
		fmt.Fprintf(&sb, "## Section %d\n\nParagraph %d with *emphasis*, a [[Link %d]] and some longer prose that wraps at the measure of the page, #tag%d.\n- item a\n- item b\n\n", i, i, i, i)
	}
	return sb.String()
}

func TestLargeBufferSpeed(t *testing.T) {
	src := bigNote(150 << 10)
	start := time.Now()
	e := New(Config{Text: []byte(src), Width: 100, Height: 40})
	s := newScreen(100, 40)
	e.Draw(s, term.Rect{W: 100, H: 40})
	for _, k := range parseKeys("G") {
		e.HandleKey(k)
	}
	e.Draw(s, term.Rect{W: 100, H: 40})
	e.SetCursor(e.LineCount()/2, 0)
	for _, k := range parseKeys("o") {
		e.HandleKey(k)
	}
	for i := 0; i < 2000; i++ {
		e.HandleKey(term.KeyEvent{Key: term.KeyRune, Rune: 'a' + rune(i%26)})
		if i%50 == 0 {
			e.Draw(s, term.Rect{W: 100, H: 40})
		}
	}
	e.HandleKey(term.KeyEvent{Key: term.KeyEscape})
	for _, k := range parseKeys(":%s/Section/Part/g<CR>u") {
		e.HandleKey(k)
	}
	for _, k := range parseKeys("u") {
		e.HandleKey(k)
	}
	e.Draw(s, term.Rect{W: 100, H: 40})
	if string(e.Text()) != src {
		t.Fatal("text not restored after undo")
	}
	if d := time.Since(start); d > ciSlack(3*time.Second) {
		t.Errorf("large buffer workout took %v", d)
	}
}

func BenchmarkTypeAndDraw(b *testing.B) {
	src := bigNote(100 << 10)
	e := New(Config{Text: []byte(src), Width: 100, Height: 40})
	s := newScreen(100, 40)
	e.SetCursor(e.LineCount()/2, 0)
	e.HandleKey(term.KeyEvent{Key: term.KeyRune, Rune: 'o'})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.HandleKey(term.KeyEvent{Key: term.KeyRune, Rune: 'x'})
		e.Draw(s, term.Rect{W: 100, H: 40})
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
