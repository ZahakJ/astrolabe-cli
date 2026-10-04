package text

import (
	"reflect"
	"testing"
)

func TestWidth(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"hello", 5},
		{"héllo", 5},
		{"é", 1},     // e + combining acute
		{"日本語", 6},    // wide
		{"a日b", 4},    // mixed
		{"👍", 2},      // emoji
		{"👍🏽", 2},     // emoji + skin tone modifier: one cluster
		{"👨‍👩‍👧", 2},  // ZWJ family
		{"🇫🇷", 2},     // flag
		{"✦", 1},      // dingbat, text presentation
		{"\t", 0},     // control
		{"a\x1bb", 2}, // control ignored
		{"سلام", 4},   // Arabic letters
		{"بِسْمِ", 3}, // harakat are zero width
		{"ﻻ", 1},      // lam-alef ligature
		{"한국어", 6},    // Hangul syllables
		{"가", 2},     // conjoining jamo: one wide cluster
		{"x‍y", 2},    // ZWJ between letters
		{"①", 1},      // ambiguous width counts 1
		{"ｱ", 1},      // halfwidth katakana
		{"Ａ", 2},      // fullwidth latin
		{" ", 1},      // NBSP
		{"ạ̈", 1},    // stacked marks
	}
	for _, tt := range tests {
		if got := Width(tt.in); got != tt.want {
			t.Errorf("Width(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestGraphemes(t *testing.T) {
	gs := Graphemes("aé日👍🏽")
	var texts []string
	var widths, offs []int
	for _, g := range gs {
		texts = append(texts, g.Text)
		widths = append(widths, g.Width)
		offs = append(offs, g.Offset)
	}
	if !reflect.DeepEqual(texts, []string{"a", "é", "日", "👍🏽"}) {
		t.Errorf("texts = %q", texts)
	}
	if !reflect.DeepEqual(widths, []int{1, 1, 2, 2}) {
		t.Errorf("widths = %v", widths)
	}
	if !reflect.DeepEqual(offs, []int{0, 1, 4, 7}) {
		t.Errorf("offsets = %v", offs)
	}
	if GraphemeWidth("日") != 2 || GraphemeWidth("") != 0 || GraphemeWidth("\n") != 0 {
		t.Error("GraphemeWidth")
	}
	n := 0
	EachGrapheme("abc", func(string, int) bool { n++; return n < 2 })
	if n != 2 {
		t.Error("EachGrapheme must stop when fn returns false")
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		in       string
		w        int
		ellipsis string
		want     string
	}{
		{"hello world", 20, "…", "hello world"},
		{"hello world", 11, "…", "hello world"},
		{"hello world", 8, "…", "hello w…"},
		{"hello world", 8, "...", "hello..."},
		{"hello", 1, "…", "…"},
		{"hello", 2, "...", "he"}, // ellipsis does not fit: hard cut
		{"hello", 0, "…", ""},
		{"日本語テキスト", 7, "…", "日本語…"},
		{"日本語テキスト", 6, "…", "日本…"}, // a wide char never straddles
		{"éééé", 3, "…", "éé…"},
		{"👍🏽👍🏽👍🏽", 5, "…", "👍🏽👍🏽…"},
	}
	for _, tt := range tests {
		got := Truncate(tt.in, tt.w, tt.ellipsis)
		if got != tt.want {
			t.Errorf("Truncate(%q, %d, %q) = %q, want %q", tt.in, tt.w, tt.ellipsis, got, tt.want)
		}
		if Width(got) > tt.w {
			t.Errorf("Truncate(%q, %d) too wide: %d", tt.in, tt.w, Width(got))
		}
	}
}

func TestTruncateLeft(t *testing.T) {
	tests := []struct {
		in   string
		w    int
		want string
	}{
		{"projects/astrolabe/notes.md", 30, "projects/astrolabe/notes.md"},
		{"projects/astrolabe/notes.md", 12, "…be/notes.md"},
		{"abcdefgh", 5, "…efgh"},
		{"日本語テキスト", 7, "…キスト"},
		{"日本語テキスト", 6, "…スト"}, // a wide char never straddles
		{"abc", 1, "…"},
		{"abc", 0, ""},
	}
	for _, tt := range tests {
		if got := TruncateLeft(tt.in, tt.w, "…"); got != tt.want {
			t.Errorf("TruncateLeft(%q, %d) = %q, want %q", tt.in, tt.w, got, tt.want)
		}
	}
}

func TestPadAlign(t *testing.T) {
	tests := []struct {
		got, want string
	}{
		{PadRight("ab", 5), "ab   "},
		{PadRight("abcdef", 3), "abcdef"},
		{PadLeft("ab", 5), "   ab"},
		{PadRight("日本", 6), "日本  "},
		{Center("ab", 7), "  ab   "},
		{Center("ab", 6), "  ab  "},
		{Align("x", 3, AlignRight), "  x"},
		{Align("x", 3, AlignCenter), " x "},
		{Align("x", 3, AlignLeft), "x  "},
		{Fit("hello world", 8, AlignLeft, "…"), "hello w…"},
		{Fit("hi", 4, AlignRight, "…"), "  hi"},
		{Fit("日本語", 5, AlignLeft, "…"), "日本…"},
		{Fit("日本語", 4, AlignLeft, "…"), "日… "}, // padded where a wide char cannot fit
	}
	for i, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("case %d: got %q, want %q", i, tt.got, tt.want)
		}
	}
}

func TestExpandTabs(t *testing.T) {
	tests := []struct {
		in   string
		tw   int
		want string
	}{
		{"a\tb", 4, "a   b"},
		{"\tx", 4, "    x"},
		{"abcd\te", 4, "abcd    e"},
		{"日\tx", 4, "日  x"},
		{"a\nb\tc", 2, "a\nb c"},
		{"none", 4, "none"},
	}
	for _, tt := range tests {
		if got := ExpandTabs(tt.in, tt.tw); got != tt.want {
			t.Errorf("ExpandTabs(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSlice(t *testing.T) {
	tests := []struct {
		in       string
		from, to int
		want     string
	}{
		{"hello", 1, 4, "ell"},
		{"hello", 3, 10, "lo"},
		{"日本語", 1, 5, " 本 "}, // split wide chars become spaces
		{"日本語", 2, 4, "本"},
		{"abc", 2, 2, ""},
	}
	for _, tt := range tests {
		if got := Slice(tt.in, tt.from, tt.to); got != tt.want {
			t.Errorf("Slice(%q, %d, %d) = %q, want %q", tt.in, tt.from, tt.to, got, tt.want)
		}
	}
}
