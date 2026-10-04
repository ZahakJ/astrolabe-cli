package text

import (
	"strings"
	"testing"
)

func TestFold(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Hello World", "hello world"},
		{"الأسطرلاب", "الاسطرلاب"},
		{"إسلام آمن ٱلله", "اسلام امن الله"},
		{"كِتابٌ", "كتاب"},
		{"مكتبة", "مكتبه"},
		{"على", "علي"},
		{"مسؤول", "مسوول"},
		{"رئيس", "رييس"},
		{"کتاب یک", "كتاب يك"},
		{"سنة ١٤٤٥ و۲۰۲۴", "سنه 1445 و2024"},
		{"جـمـيـل", "جميل"}, // tatweel
		{"a‏b‫c⁩", "abc"},   // bidi controls
		{"ﺍﻟﺴﻼﻡ", "السلام"}, // presentation forms, lam-alef ligature
		{"ﻣﺎﺀ", "ماء"},
		{"Café", "café"},
		{"Café", "café"}, // decomposed → NFC
		{"ÉCOLE", "école"},
	}
	for _, tt := range tests {
		if got := Fold(tt.in); got != tt.want {
			t.Errorf("Fold(%q) = %q (%U), want %q", tt.in, got, []rune(got), tt.want)
		}
	}
	if FoldExact("ABC أ") != "ABC ا" {
		t.Errorf("FoldExact = %q", FoldExact("ABC أ"))
	}
}

func TestFoldOffsets(t *testing.T) {
	src := "قال: الأَسْطُرْلابُ ﻻ Café!"
	f, offs := AppendFoldString(nil, nil, src, true)
	if len(offs) != len(f) {
		t.Fatalf("offset map length %d for %d folded bytes", len(offs), len(f))
	}
	for i := 1; i < len(offs); i++ {
		if offs[i] < offs[i-1] || int(offs[i]) >= len(src) {
			t.Fatalf("offsets not monotonic/in range at %d: %v", i, offs)
		}
	}
	for _, tc := range []struct{ needle, want string }{
		{"الاسطرلاب", "الأَسْطُرْلابُ"},
		{"سطر", "سْطُرْ"},
		{"لا", "ﻻ"},
		{"café", "Café"},
		{"قال", "قال"},
	} {
		rs := FoldIndexAll(src, Fold(tc.needle), true)
		if len(rs) == 0 {
			t.Errorf("%q not found", tc.needle)
			continue
		}
		var got []string
		for _, r := range rs {
			got = append(got, src[r[0]:r[1]])
		}
		if !contains(got, tc.want) {
			t.Errorf("%q matched %q, want %q", tc.needle, got, tc.want)
		}
	}
	// A range ending inside a ligature covers the whole ligature.
	if rs := FoldIndexAll("ﻻ", "ل", true); len(rs) != 1 || rs[0] != [2]int{0, len("ﻻ")} {
		t.Errorf("ligature range = %v", rs)
	}
}

func contains(a []string, s string) bool {
	for _, x := range a {
		if strings.TrimSpace(x) == s {
			return true
		}
	}
	return false
}

func TestMatcherArabic(t *testing.T) {
	tests := []struct {
		pattern, cand string
		ok            bool
	}{
		{"الاسطرلاب", "الأسطرلاب", true},
		{"كتاب", "كِتابٌ قديم", true},
		{"مكتبه", "المكتبة", true},
		{"علي", "على الطاولة", true},
		{"١٢", "ملاحظات 12", true},
		{"cafe", "Café", false}, // NFC only: accents still count
		{"café", "Café noir", true},
		{"جبل رحلة", "رحلة إلى الجبل", true}, // terms in any order
		{"جبل بحر", "رحلة إلى الجبل", false},
	}
	for _, tt := range tests {
		mt, ok := FuzzyMatch(tt.pattern, tt.cand)
		if ok != tt.ok {
			t.Errorf("FuzzyMatch(%q, %q) ok = %v", tt.pattern, tt.cand, ok)
			continue
		}
		for _, p := range mt.Positions {
			if p < 0 || p >= len(tt.cand) {
				t.Errorf("%q in %q: position %d out of range", tt.pattern, tt.cand, p)
			}
		}
	}
	// Positions point at the original letters, harakat skipped.
	mt, _ := FuzzyMatch("كتاب", "كِتابٌ")
	if want := []int{0, 4, 6, 8}; !equalInts(mt.Positions, want) {
		t.Errorf("positions %v, want %v", mt.Positions, want)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRankArabicProclitics(t *testing.T) {
	cands := []string{"مسطرة الرسم", "الأسطرلاب", "واسطة"}
	r := Rank("اسطرلاب", cands)
	if len(r) == 0 || cands[r[0].Index] != "الأسطرلاب" {
		t.Errorf("Rank: %v", r)
	}
	// A word after the article ranks above the same letters mid-word.
	cands = []string{"تكتبون", "الكتب"}
	r = Rank("كتب", cands)
	if len(r) != 2 || cands[r[0].Index] != "الكتب" {
		t.Errorf("article bonus: %+v", r)
	}
	cands = []string{"مكتبة", "وكتب"}
	r = Rank("كتب", cands)
	if len(r) != 2 || cands[r[0].Index] != "وكتب" {
		t.Errorf("particle bonus: %+v", r)
	}
}
