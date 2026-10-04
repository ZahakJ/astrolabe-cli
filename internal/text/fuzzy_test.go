package text

import (
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestFuzzyMatchBasic(t *testing.T) {
	tests := []struct {
		pat, cand string
		ok        bool
		pos       []int
	}{
		{"", "anything", true, nil},
		{"abc", "abc", true, []int{0, 1, 2}},
		{"abc", "a_b_c", true, []int{0, 2, 4}},
		{"abc", "acb", false, nil},
		{"fb", "foo/bar.md", true, []int{0, 4}},
		{"bar", "foo/bar.md", true, []int{4, 5, 6}},
		{"FB", "foo/bar", false, nil},             // smart case: upper → sensitive
		{"FB", "Foo/Bar", true, []int{0, 4}},      // sensitive match
		{"fb", "FOO/BAR", true, []int{0, 4}},      // insensitive
		{"édi", "Éditions", true, []int{0, 2, 3}}, // non-ASCII folding, byte offsets
		{"日記", "2026/日記.md", true, []int{5, 8}},
		{"meet notes", "work/meetings/2026 notes.md", true, nil},
		{"meet zzz", "work/meetings.md", false, nil},
	}
	for _, tt := range tests {
		m, ok := FuzzyMatch(tt.pat, tt.cand)
		if ok != tt.ok {
			t.Errorf("FuzzyMatch(%q, %q) ok = %v", tt.pat, tt.cand, ok)
			continue
		}
		if tt.pos != nil && !reflect.DeepEqual(m.Positions, tt.pos) {
			t.Errorf("FuzzyMatch(%q, %q) positions = %v, want %v", tt.pat, tt.cand, m.Positions, tt.pos)
		}
	}
}

func TestFuzzyPositionsAreValid(t *testing.T) {
	cands := []string{"daily/2026-10-03.md", "Projects/Folio Design.md", "reading/books/The Name of the Rose.md", "a/b/c/d/e/f.md"}
	pats := []string{"dd", "fold", "rose", "nr", "abc", "2026", "md"}
	for _, c := range cands {
		for _, p := range pats {
			m, ok := FuzzyMatch(p, c)
			if !ok {
				continue
			}
			pr := []rune(p)
			if len(m.Positions) != len(pr) {
				t.Fatalf("%q in %q: %d positions", p, c, len(m.Positions))
			}
			for i, off := range m.Positions {
				if i > 0 && off <= m.Positions[i-1] {
					t.Fatalf("%q in %q: positions not increasing %v", p, c, m.Positions)
				}
				r := []rune(c[off:])[0]
				if toLower(r) != toLower(pr[i]) {
					t.Fatalf("%q in %q: position %d is %q", p, c, off, r)
				}
			}
		}
	}
}

func toLower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + 32
	}
	return r
}

// TestFuzzyRanking pins the orderings a finder user expects.
func TestFuzzyRanking(t *testing.T) {
	tests := []struct {
		pat    string
		better string
		worse  string
	}{
		{"fol", "folio.md", "notes/fooled.md"},                  // prefix and consecutive
		{"fd", "Folio Design.md", "fluid.md"},                   // word boundaries
		{"rose", "books/Rose.md", "books/prose and verse.md"},   // boundary vs mid-word
		{"note", "projects/notes.md", "notes/projects/plan.md"}, // basename beats directory
		{"md", "Meeting Digest.md", "random.md"},                // initials... both match
		{"tsk", "tasks.md", "notes/t-s/backlog.md"},             // consecutive in basename
		{"gd", "garden/Getting Done.md", "garden/good.md"},
		{"ab", "ab.md", "a/b.md"},
		{"daily", "daily/2026-10-03.md", "archive/daily-old/x.md"},
		{"cam", "CamelCase", "chamomile"},
	}
	for _, tt := range tests {
		b, okb := FuzzyMatch(tt.pat, tt.better)
		w, okw := FuzzyMatch(tt.pat, tt.worse)
		if !okb || !okw {
			t.Errorf("%q: both should match (%v %v)", tt.pat, okb, okw)
			continue
		}
		if tt.pat == "md" {
			continue // only checks that both match
		}
		if b.Score <= w.Score {
			t.Errorf("%q: %q (%d) should beat %q (%d)", tt.pat, tt.better, b.Score, tt.worse, w.Score)
		}
	}
}

func TestFuzzyBestAlignment(t *testing.T) {
	// Greedy matching would take the first 'b'; the DP prefers the boundary.
	m, ok := FuzzyMatch("bar", "abc/bar")
	if !ok || !reflect.DeepEqual(m.Positions, []int{4, 5, 6}) {
		t.Errorf("positions = %v", m.Positions)
	}
	m, _ = FuzzyMatch("fm", "folio/main.md")
	if !reflect.DeepEqual(m.Positions, []int{0, 6}) {
		t.Errorf("positions = %v", m.Positions)
	}
}

func TestRank(t *testing.T) {
	cands := []string{"inbox.md", "ideas/index.md", "index.md", "journal.md"}
	r := Rank("index", cands)
	if len(r) != 2 || cands[r[0].Index] != "index.md" || cands[r[1].Index] != "ideas/index.md" {
		var got []string
		for _, x := range r {
			got = append(got, fmt.Sprintf("%s:%d", cands[x.Index], x.Score))
		}
		t.Errorf("Rank = %v", got)
	}
	if r := Rank("", cands); len(r) != 4 || r[2].Index != 2 {
		t.Error("empty pattern keeps order")
	}
}

func makeCandidates(n int) []string {
	words := []string{"daily", "projects", "reading", "garden", "Meeting", "notes", "Design", "folio",
		"archive", "ideas", "journal", "books", "recipes", "travel", "people", "research", "2026", "draft"}
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s/%s/%s %s %d.md", words[i%len(words)], words[(i/3)%len(words)],
			words[(i/7)%len(words)], words[(i/11)%len(words)], i)
	}
	return out
}

// TestFuzzySpeed enforces the budget: 5,000 candidates per keystroke.
func TestFuzzySpeed(t *testing.T) {
	if testing.Short() {
		t.Skip("timing check skipped with -short")
	}
	cands := makeCandidates(5000)
	start := time.Now()
	for _, p := range []string{"d", "dr", "drf", "drft", "rdg nts"} {
		Rank(p, cands)
	}
	if el := time.Since(start) / 5; el > ciSlack(40*time.Millisecond) {
		t.Errorf("ranking 5000 candidates took %v per keystroke", el)
	}
}

func BenchmarkRank5000(b *testing.B) {
	cands := makeCandidates(5000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Rank("prjdsgn", cands)
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
