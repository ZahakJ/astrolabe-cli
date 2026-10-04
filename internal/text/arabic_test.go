package text

import (
	"reflect"
	"testing"
)

func TestShape(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"marhaba", "مرحبا", "ﻣﺮﺣﺒﺎ"},
		{"kitab: final isolated after alef", "كتاب", "ﻛﺘﺎﺏ"},
		{"salam: lam-alef final ligature", "سلام", "ﺳﻼﻡ"},
		{"la: isolated lam-alef", "لا", "ﻻ"},
		{"lam-alef with hamza above", "لأن", "ﻷﻥ"},
		{"lam-alef madda", "لآ", "ﻵ"},
		{"lam-alef hamza below", "إلإ", "ﺇﻹ"},
		{"allah: no ligature between lams", "الله", "ﺍﻟﻠﻪ"},
		{"basmala with harakat", "بِسْمِ", "ﺑِﺴْﻢِ"},
		{"lam fatha alef keeps mark", "لَا", "ﻻَ"},
		{"shadda on medial", "محمّد", "ﻣﺤﻤّﺪ"},
		{"single letter isolated", "ب", "ﺏ"},
		{"hamza is non-joining", "ماء", "ﻣﺎء"},
		{"tatweel joins after", "بـ", "ﺑـ"},
		{"tatweel joins before", "ـب", "ـﺐ"},
		{"tatweel between", "بـب", "ﺑـﺐ"},
		{"zwj forces join", "ب‍", "ﺑ‍"},
		{"zwnj breaks join", "ب‌ب", "ﺏ‌ﺏ"},
		{"two words", "اهلا وسهلا", "ﺍﻫﻼ ﻭﺳﻬﻼ"},
		{"taa marbuta final", "مدرسة", "ﻣﺪﺭﺳﺔ"},
		{"alef maksura", "على", "ﻋﻠﻰ"},
		// Persian and Urdu letters (Presentation Forms-A).
		{"persian pedar", "پدر", "ﭘﺪﺭ"},
		{"persian ketab-ha with zwnj", "کتاب‌ها", "ﮐﺘﺎﺏ‌ﻫﺎ"},
		{"persian gol", "گل", "ﮔﻞ"},
		{"persian zhaleh", "ژاله", "ﮊﺍﻟﻪ"},
		{"persian yek", "یک", "ﯾﮏ"},
		{"persian chay", "چای", "ﭼﺎﯼ"},
		{"persian yeh medial", "میز", "ﻣﯿﺰ"},
		{"urdu tteh", "ٹوپی", "ﭨﻮﭘﯽ"},
		{"non-arabic untouched", "hello world 123", "hello world 123"},
		{"mixed latin and arabic", "Go لغة", "Go ﻟﻐﺔ"},
		{"arabic digits untouched", "عام ٢٠٢٤", "ﻋﺎﻡ ٢٠٢٤"},
		{"punctuation breaks join", "ب،ب", "ﺏ،ﺏ"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Shape(tt.in)
			if got != tt.want {
				t.Errorf("Shape(%q) = %q (%U), want %q (%U)", tt.in, got, []rune(got), tt.want, []rune(tt.want))
			}
			if again := Shape(got); again != got {
				t.Errorf("Shape not idempotent: %q → %q", got, again)
			}
		})
	}
}

func TestShapeWidthNeverGrows(t *testing.T) {
	for _, s := range []string{"بِسْمِ اللَّهِ الرَّحْمَٰنِ الرَّحِيمِ", "سلام علیکم", "لا إله إلا الله", "پژوهش‌های تازه"} {
		if Width(Shape(s)) > Width(s) {
			t.Errorf("shaping widened %q: %d → %d", s, Width(s), Width(Shape(s)))
		}
	}
}

func TestShapeClusters(t *testing.T) {
	got := ShapeClusters([]string{"س", "ل", "ا", "م"})
	want := []string{"ﺳ", "ﻼ", "", "ﻡ"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ShapeClusters = %q, want %q", got, want)
	}
	// Marks on the fused alef move into the ligature's cluster.
	got = ShapeClusters([]string{"ل", "أَ", "ن"})
	want = []string{"ﻷَ", "", "ﻥ"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ShapeClusters marks = %q, want %q", got, want)
	}
	in := []string{"a", "b"}
	if got := ShapeClusters(in); &got[0] != &in[0] {
		t.Error("non-Arabic input should be returned as is")
	}
}

func TestVisualArabic(t *testing.T) {
	tests := []struct {
		in   string
		base Direction
		want string
	}{
		// Shaped, then reversed: the first letter ends up rightmost.
		{"مرحبا", Neutral, "ﺎﺒﺣﺮﻣ"},
		{"سلام", Neutral, "ﻡﻼﺳ"},
		// Digits keep their order; the RTL paragraph puts them at the left.
		{"العدد 42", Neutral, "42 ﺩﺪﻌﻟﺍ"},
		// Latin words inside Arabic keep LTR order.
		{"لغة Go جميلة", Neutral, "ﺔﻠﻴﻤﺟ Go ﺔﻐﻟ"},
		// LTR paragraph with an Arabic word.
		{"word كلمة end", LTR, "word ﺔﻤﻠﻛ end"},
		// Brackets mirrored inside the RTL run.
		{"(نص)", RTL, "(ﺺﻧ)"},
		{"plain latin", LTR, "plain latin"},
	}
	for _, tt := range tests {
		if got := Visual(tt.in, tt.base); got != tt.want {
			t.Errorf("Visual(%q) = %q (%U), want %q (%U)", tt.in, got, []rune(got), tt.want, []rune(tt.want))
		}
	}
}

func TestShapeThenReorderClusters(t *testing.T) {
	// The editor's pipeline: clusters → shape (aligned) → reorder → map.
	cl := clustersOf("لا بأس")
	sh := ShapeClusters(cl)
	if len(sh) != len(cl) {
		t.Fatalf("length changed")
	}
	bl := Reorder(sh, Neutral)
	var vis string
	for _, l := range bl.VisualToLogical {
		vis += sh[l]
	}
	if vis != "ﺱﺄﺑ ﻻ" {
		t.Errorf("visual = %q (%U)", vis, []rune(vis))
	}
	// The cursor on the fused alef (logical 1) maps next to the ligature.
	if d := bl.LogicalToVisual[1] - bl.LogicalToVisual[0]; d != -1 && d != 1 {
		t.Errorf("alef slot not adjacent to lam: %v", bl.LogicalToVisual)
	}
}
