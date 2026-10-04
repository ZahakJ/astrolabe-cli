package text

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/text/unicode/bidi"
)

func TestBaseDirection(t *testing.T) {
	tests := []struct {
		in   string
		want Direction
	}{
		{"", Neutral},
		{"123 !?", Neutral},
		{"hello", LTR},
		{"  42. Hello", LTR},
		{"שלום", RTL},
		{"مرحبا world", RTL},
		{"world مرحبا", LTR},
		{"١٢٣ مرحبا", RTL}, // Arabic digits are weak; the letter decides
		{"«مرحبا»", RTL},
		{"- [ ] مهمة", RTL},
		{"日本語", LTR},
	}
	for _, tt := range tests {
		if got := BaseDirection(tt.in); got != tt.want {
			t.Errorf("BaseDirection(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
	if !HasRTL("abc ש") || HasRTL("abc def 123") || !HasRTL("١٢٣") {
		t.Error("HasRTL")
	}
}

func TestReorderString(t *testing.T) {
	tests := []struct {
		name string
		in   string
		base Direction
		want string
	}{
		{"latin untouched", "abc DEF", LTR, "abc DEF"},
		{"hebrew auto", "שלום world", Neutral, "world םולש"},
		{"hebrew with number", "שלום 123 עולם", Neutral, "םלוע 123 םולש"},
		{"rtl run inside ltr", "abc שלום 123 עולם def", LTR, "abc םלוע 123 םולש def"},
		{"mirrored brackets", "(שלום)", RTL, "(םולש)"},
		{"arabic brackets and digits", "مرحبا (بالعالم) 2024", Neutral, "2024 (ملاعلاب) ابحرم"},
		{"arabic-indic digits", "قيمة ١٢٣ هنا", Neutral, "انه ١٢٣ ةميق"},
		{"number after arabic in ltr", "see: كتاب 12.5% ok", LTR, "see: 12.5 باتك% ok"},
		{"forced ltr on rtl text", "שלום", LTR, "םולש"},
		{"forced rtl on latin text", "abc def", RTL, "abc def"},
		{"rtl paragraph with latin words", "אני אוהב Go ו-Rust", RTL, "Rust-ו Go בהוא ינא"},
		{"guillemets mirror", "«שלום»", RTL, "«םולש»"},
		{"trailing space keeps paragraph side", "שלום ", LTR, "םולש "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ReorderString(tt.in, tt.base); got != tt.want {
				t.Errorf("ReorderString(%q, %v) = %q, want %q", tt.in, tt.base, got, tt.want)
			}
		})
	}
}

func clustersOf(s string) []string {
	var cl []string
	for _, g := range Graphemes(s) {
		cl = append(cl, g.Text)
	}
	return cl
}

func TestReorderMaps(t *testing.T) {
	cl := clustersOf("abc שלום")
	bl := Reorder(cl, LTR)
	wantV2L := []int{0, 1, 2, 3, 7, 6, 5, 4}
	if !reflect.DeepEqual(bl.VisualToLogical, wantV2L) {
		t.Errorf("VisualToLogical = %v, want %v", bl.VisualToLogical, wantV2L)
	}
	for v, l := range bl.VisualToLogical {
		if bl.LogicalToVisual[l] != v {
			t.Fatalf("maps are not inverse: %v / %v", bl.VisualToLogical, bl.LogicalToVisual)
		}
	}
	wantLevels := []uint8{0, 0, 0, 0, 1, 1, 1, 1}
	if !reflect.DeepEqual(bl.Levels, wantLevels) {
		t.Errorf("Levels = %v", bl.Levels)
	}
	if !bl.IsRTL(5) || bl.IsRTL(1) {
		t.Error("IsRTL")
	}

	// Numbers inside an RTL run in an LTR paragraph are level 2 and keep
	// their own left-to-right order within the reversed run.
	cl = clustersOf("ab אב 12 גד")
	bl = Reorder(cl, LTR)
	wantLevels = []uint8{0, 0, 0, 1, 1, 1, 2, 2, 1, 1, 1}
	if !reflect.DeepEqual(bl.Levels, wantLevels) {
		t.Errorf("Levels = %v, want %v", bl.Levels, wantLevels)
	}
	got := ""
	for _, l := range bl.VisualToLogical {
		got += cl[l]
	}
	if got != "ab דג 12 בא" {
		t.Errorf("visual = %q", got)
	}

	// Combining marks travel with their base cluster.
	cl = clustersOf("שָׁלוֹם")
	bl = Reorder(cl, Neutral)
	if len(bl.VisualToLogical) != len(cl) || bl.VisualToLogical[0] != len(cl)-1 {
		t.Errorf("marked hebrew order = %v", bl.VisualToLogical)
	}

	if bl := Reorder(nil, LTR); len(bl.VisualToLogical) != 0 {
		t.Error("empty line")
	}
}

type cell struct {
	g   string
	idx int
}

func TestVisualOrderGeneric(t *testing.T) {
	var cells []cell
	for i, g := range clustersOf("x (אב) y") {
		cells = append(cells, cell{g, i})
	}
	vis, bl := VisualOrder(cells, func(c cell) string { return c.g }, LTR)
	var b strings.Builder
	for _, c := range vis {
		g := c.g
		if bl.IsRTL(c.idx) {
			g = Mirror(g)
		}
		b.WriteString(g)
	}
	if b.String() != "x (בא) y" {
		t.Errorf("VisualOrder = %q", b.String())
	}
}

func TestMirror(t *testing.T) {
	for in, want := range map[string]string{"(": ")", ")": "(", "[": "]", "«": "»", "≤": "≥", "a": "a", "≮": "≯", "": ""} {
		if got := Mirror(in); got != want {
			t.Errorf("Mirror(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestReorderParityMatchesXText cross-checks the recovered levels against
// x/text's run directions on random mixed strings.
func TestReorderParityMatchesXText(t *testing.T) {
	alphabet := []string{"a", "b", "Z", " ", "1", "2", ".", ",", "%", "$", "-", "+", "(", ")", "[", "]",
		"א", "ב", "ש", "ا", "ب", "ل", "م", "١", "٢", "!", "?", "َ", "/", ":"}
	rng := rand.New(rand.NewSource(1))
	for iter := 0; iter < 3000; iter++ {
		n := 1 + rng.Intn(14)
		var parts []string
		for i := 0; i < n; i++ {
			parts = append(parts, alphabet[rng.Intn(len(alphabet))])
		}
		s := strings.Join(parts, "")
		cl := clustersOf(s)
		for _, base := range []Direction{LTR, RTL} {
			bl := Reorder(cl, base)
			mark := "‎"
			if base == RTL {
				mark = "‏"
			}
			var p bidi.Paragraph
			p.SetString(mark + s)
			o, err := p.Order()
			if err != nil {
				t.Fatal(err)
			}
			parity := make([]int, len([]rune(mark+s)))
			for i := 0; i < o.NumRuns(); i++ {
				r := o.Run(i)
				a, b := r.Pos()
				for j := a; j <= b; j++ {
					if r.Direction() == bidi.RightToLeft {
						parity[j] = 1
					}
				}
			}
			ri := 1
			for ci, c := range cl {
				if int(bl.Levels[ci]%2) != parity[ri] {
					t.Fatalf("%q base %v: cluster %d level %d disagrees with x/text parity", s, base, ci, bl.Levels[ci])
				}
				if bl.Levels[ci] > 2 {
					t.Fatalf("%q: level %d > 2", s, bl.Levels[ci])
				}
				ri += len([]rune(c))
			}
			// The permutation is a permutation.
			seen := make([]bool, len(cl))
			for _, l := range bl.VisualToLogical {
				if seen[l] {
					t.Fatalf("%q: duplicate index in permutation", s)
				}
				seen[l] = true
			}
		}
	}
}

func BenchmarkVisual(b *testing.B) {
	s := "النص العربي مع English words و 123 أرقام في سطر واحد طويل"
	for i := 0; i < b.N; i++ {
		Visual(s, Neutral)
	}
}

func TestVisualRanges(t *testing.T) {
	s := "كلمة budget في سطر"
	i := strings.Index(s, "budget")
	vis, rs := VisualRanges(s, [][2]int{{i, i + len("budget")}})
	if vis != Visual(s, BaseDirection(s)) {
		t.Fatalf("VisualRanges text %q differs from Visual %q", vis, Visual(s, BaseDirection(s)))
	}
	if len(rs) != 1 || vis[rs[0][0]:rs[0][1]] != "budget" {
		t.Fatalf("ranges %v over %q", rs, vis)
	}
	// LTR text passes through untouched.
	if v, r := VisualRanges("plain budget", [][2]int{{6, 12}}); v != "plain budget" || r[0] != [2]int{6, 12} {
		t.Fatalf("LTR: %q %v", v, r)
	}
}

func TestVisualPath(t *testing.T) {
	if got, want := VisualPath("يوميات/2026-10-03"), Visual("يوميات", LTR)+"/2026-10-03"; got != want {
		t.Errorf("VisualPath = %q, want %q", got, want)
	}
	if VisualPath("a/b.md") != "a/b.md" {
		t.Error("LTR path changed")
	}
}
