package text

import (
	"reflect"
	"strings"
	"testing"
)

func TestWrap(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		width int
		want  []string
	}{
		{"empty", "", 10, []string{""}},
		{"fits", "hello world", 20, []string{"hello world"}},
		{"exact", "hello world", 11, []string{"hello world"}},
		{"simple", "the quick brown fox jumps over the lazy dog", 10,
			[]string{"the quick", "brown fox", "jumps over", "the lazy", "dog"}},
		{"multiple spaces kept inside", "a  b   c", 20, []string{"a  b   c"}},
		{"spaces at break dropped", "aaaa    bbbb", 6, []string{"aaaa", "bbbb"}},
		{"leading spaces kept", "  indented text here", 12, []string{"  indented", "text here"}},
		{"long word broken", "abcdefghijklmnop", 5, []string{"abcde", "fghij", "klmno", "p"}},
		{"long word after short", "hi abcdefghijkl", 5, []string{"hi", "abcde", "fghij", "kl"}},
		{"hard breaks", "one\ntwo three\n\nfour", 20, []string{"one", "two three", "", "four"}},
		{"crlf", "a\r\nb", 5, []string{"a", "b"}},
		{"trailing newline", "a\n", 5, []string{"a"}},
		{"hyphen break", "well-known fact", 6, []string{"well-", "known", "fact"}},
		{"cjk breaks between characters", "日本語のテキストです", 6, []string{"日本語", "のテキ", "ストで", "す"}},
		{"cjk mixed", "hello 世界 test", 8, []string{"hello 世", "界 test"}},
		{"cjk wide char never split", "日本語", 3, []string{"日", "本", "語"}},
		{"too narrow for wide", "日本", 1, []string{"日", "本"}},
		{"combining marks kept", "café café café", 9, []string{"café café", "café"}},
		{"emoji cluster", "a 👨‍👩‍👧 b", 3, []string{"a", "👨‍👩‍👧", "b"}},
		{"nbsp does not break", "10 km away", 6, []string{"10 km", "away"}},
		{"arabic", "السلام عليكم ورحمة الله", 12, []string{"السلام عليكم", "ورحمة الله"}},
		{"only spaces collapse", "    ", 10, []string{""}},
		{"width zero treated as one", "ab", 0, []string{"a", "b"}},
		{"url breaks after slash", "see https://example.org/a/very/long/path", 20,
			[]string{"see https://", "example.org/a/very/", "long/path"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Wrap(tt.in, tt.width)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Wrap(%q, %d) =\n%q\nwant\n%q", tt.in, tt.width, got, tt.want)
			}
			w := max(tt.width, 1)
			for _, l := range got {
				if Width(l) > w && GraphemeCount(l) > 1 {
					t.Errorf("line %q exceeds width %d", l, w)
				}
			}
		})
	}
}

// GraphemeCount is a test helper.
func GraphemeCount(s string) int { return len(Graphemes(s)) }

func TestWrapHangingIndent(t *testing.T) {
	got := WrapIndent("Buy bread, milk and a small jar of honey from the market", 24, "• ", "  ")
	want := []string{
		"• Buy bread, milk and a",
		"  small jar of honey",
		"  from the market",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("WrapIndent =\n%q\nwant\n%q", got, want)
	}
	got = WrapWidths("aaa bbb ccc ddd", 3, 7)
	want = []string{"aaa", "bbb ccc", "ddd"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("WrapWidths = %q", got)
	}
	// Hard breaks continue at the rest width.
	got = WrapIndent("one two\nthree four", 9, "10. ", "    ")
	want = []string{"10. one", "    two", "    three", "    four"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("WrapIndent hard = %q", got)
	}
}

func TestWrapRanges(t *testing.T) {
	s := "alpha beta gamma"
	rs := WrapRanges(s, 10, 10)
	want := []Range{{0, 10}, {11, 16}}
	if !reflect.DeepEqual(rs, want) {
		t.Errorf("WrapRanges = %v, want %v", rs, want)
	}
	// Ranges never overlap and never include a hard break.
	s = "one two three\nfour five six seven\n\neight"
	rs = WrapRanges(s, 7, 7)
	prev := -1
	for _, r := range rs {
		if r.Start < prev || r.End < r.Start {
			t.Fatalf("bad ranges %v", rs)
		}
		if strings.Contains(s[r.Start:r.End], "\n") {
			t.Errorf("range %v contains newline", r)
		}
		prev = r.End
	}
}

// TestWrapPreservesText checks that wrapping only removes spaces at breaks
// and newlines: joining the lines back recovers the words.
func TestWrapPreservesText(t *testing.T) {
	inputs := []string{
		"Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor.",
		"日本語とEnglishが混ざった文章を折り返すテストです。",
		"مرحبا بالعالم هذا نص عربي طويل للاختبار والتجربة",
		"supercalifragilisticexpialidocious and more",
	}
	for _, in := range inputs {
		for w := 1; w <= 30; w++ {
			lines := Wrap(in, w)
			joined := strings.Join(lines, "")
			if strip(joined) != strip(in) {
				t.Errorf("w=%d: text changed:\n%q\n%q", w, joined, in)
			}
			for _, l := range lines {
				if Width(l) > w && GraphemeCount(l) > 1 {
					t.Errorf("w=%d: line %q too wide", w, l)
				}
			}
		}
	}
}

func strip(s string) string { return strings.ReplaceAll(s, " ", "") }

type sty struct{ id int }

func TestWrapSpans(t *testing.T) {
	spans := []Span[sty]{
		{"Read the ", sty{0}},
		{"design document", sty{1}},
		{" before starting the work.", sty{0}},
	}
	got := WrapSpans(spans, 16, 16)
	want := [][]Span[sty]{
		{{"Read the ", sty{0}}, {"design", sty{1}}},
		{{"document", sty{1}}, {" before", sty{0}}},
		{{"starting the", sty{0}}},
		{{"work.", sty{0}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("WrapSpans =\n%v\nwant\n%v", got, want)
	}
	// Break opportunity inside a word spanning two styles is not created.
	spans = []Span[sty]{{"un", sty{0}}, {"breakable", sty{1}}, {" word", sty{0}}}
	got = WrapSpans(spans, 11, 11)
	want = [][]Span[sty]{
		{{"un", sty{0}}, {"breakable", sty{1}}},
		{{"word", sty{0}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("WrapSpans word =\n%v\nwant\n%v", got, want)
	}
	// Hard break and empty spans.
	spans = []Span[sty]{{"", sty{9}}, {"a\n", sty{0}}, {"\nb", sty{1}}}
	got = WrapSpans(spans, 5, 5)
	want = [][]Span[sty]{{{"a", sty{0}}}, {}, {{"b", sty{1}}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("WrapSpans hard =\n%v\nwant\n%v", got, want)
	}
	if SpansWidth(spans) != 2 || SpansText(spans) != "a\n\nb" {
		t.Error("SpansWidth/SpansText")
	}
}

func TestTruncateSpans(t *testing.T) {
	spans := []Span[sty]{{"hello ", sty{0}}, {"world", sty{1}}}
	got := TruncateSpans(spans, 8, "…")
	want := []Span[sty]{{"hello ", sty{0}}, {"w", sty{1}}, {"…", sty{1}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TruncateSpans = %v", got)
	}
	if got := TruncateSpans(spans, 20, "…"); !reflect.DeepEqual(got, spans) {
		t.Errorf("TruncateSpans fit = %v", got)
	}
	got = TruncateSpans(spans, 6, "…")
	want = []Span[sty]{{"hello", sty{0}}, {"…", sty{0}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TruncateSpans boundary = %v", got)
	}
}

func BenchmarkWrap(b *testing.B) {
	s := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 20)
	for i := 0; i < b.N; i++ {
		Wrap(s, 72)
	}
}
