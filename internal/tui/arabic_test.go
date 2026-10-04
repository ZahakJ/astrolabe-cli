package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZahakJ/astrolabe-cli/internal/term"
	"github.com/ZahakJ/astrolabe-cli/internal/text"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
)

func arabicHarness(t *testing.T) *harness {
	t.Helper()
	dir := copyVault(t)
	note := "# الأسطرلاب\n\nالأَسْطُرْلابُ آلَةٌ فَلَكِيَّةٌ، وفي المكتبة كِتابٌ عن Atlas 2026.\n"
	if err := os.WriteFile(filepath.Join(dir, "الأسطرلاب.md"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "مكتبة.md"), []byte("# مكتبة\n\nنص.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return newHarness(t, harnessOpt{dir: dir})
}

// cellsText reads the cells [x0, x1) of screen row y.
func cellsText(h *harness, y, x0, x1 int) string {
	var b strings.Builder
	for x := x0; x < x1; x++ {
		b.WriteString(h.vt.Cell(x, y).Text)
	}
	return b.String()
}

func TestInNoteSearchArabic(t *testing.T) {
	for _, q := range []string{"الاسطرلاب", "كتاب", "مكتبه", "atlas"} {
		h := arabicHarness(t).open("الأسطرلاب.md")
		h.keys("/")
		h.typ(q)
		h.keys("enter")
		d := h.a.doc
		if len(d.matches) == 0 {
			t.Fatalf("/%s: no match", q)
		}
		m := d.matches[d.mi]
		h.frame()
		// The highlighted cells hold the matched word (visual order: read
		// them right to left for Arabic).
		y := 1 + m.line - d.top
		got := cellsText(h, y, m.x0+h.a.layout().page.X, m.x1+h.a.layout().page.X)
		rs := []rune(got)
		if text.HasRTL(got) {
			for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
				rs[i], rs[j] = rs[j], rs[i]
			}
		}
		if f := text.Fold(string(rs)); !strings.Contains(f, text.Fold(q)) {
			t.Errorf("/%s highlights %q (fold %q)", q, got, f)
		}
	}
}

func TestFinderArabic(t *testing.T) {
	h := arabicHarness(t).open("Home.md")
	h.keys("ctrl+p")
	h.typ("اسطرلاب")
	h.settle()
	h.frame()
	var f *finder
	for _, o := range h.a.overlays {
		if ff, ok := o.(*finder); ok {
			f = ff
		}
	}
	if f == nil || len(f.results) == 0 {
		t.Fatal("no finder results")
	}
	if p := f.results[0].c.note.Path; p != "الأسطرلاب.md" {
		t.Errorf("first result %q", p)
	}
	h.contains(text.Visual("الأسطرلاب", text.RTL))
}

func TestLineInputVisualCaret(t *testing.T) {
	scr := term.NewScreen(&strings.Builder{}, 20, 1, term.Encoder{})
	in := lineInput{}
	in.set("كتاب")
	// Caret at the end of right-to-left text: the visual left edge.
	if x := in.draw(scr, 2, 0, 15, theme.Style{}, true); x != 2 {
		t.Errorf("end caret at %d, want 2", x)
	}
	if got := scr.Cell(2, 0).Text + scr.Cell(3, 0).Text + scr.Cell(4, 0).Text + scr.Cell(5, 0).Text; got != text.Visual("كتاب", text.RTL) {
		t.Errorf("field shows %q", got)
	}
	in.pos = 0 // before the first letter: right edge of the word
	if x := in.draw(scr, 2, 0, 15, theme.Style{}, true); x != 6 {
		t.Errorf("start caret at %d, want 6", x)
	}
	in.pos = len("ك") // between the first and second letters
	if x := in.draw(scr, 2, 0, 15, theme.Style{}, true); x != 5 {
		t.Errorf("caret after one letter at %d, want 5", x)
	}
	in.set("ab كتاب")
	if x := in.draw(scr, 0, 0, 15, theme.Style{}, true); x != 3 {
		t.Errorf("mixed line end caret at %d, want 3", x)
	}
}
