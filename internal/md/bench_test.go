package md

import (
	"os"
	"strings"
	"testing"
	"time"
)

// benchNote builds a realistic note of at least size bytes mixing every
// common construct.
func benchNote(size int) string {
	section := `## Section heading with *emphasis*

A paragraph with **bold**, _italic_, ` + "`code`" + `, a [[Wiki Link|label]], a
[markdown link](notes/Other%20Note.md#part) and https://example.org/path?q=1.
Tags like #project/alpha and #idea sit inline; math $e^{i\pi}+1=0$ too. ^b1

- [ ] a task 📅 2026-10-05 with [[Target]]
- [x] a finished task
  - nested bullet with ==highlight== and ~~strike~~
  1. ordered child
  2. another child

> [!note]+ A callout title
> Callout body text that wraps over
> a couple of lines with a footnote[^f].

| Column | Other | Third |
|:-------|:-----:|------:|
| [[a|b]] | ` + "`x|y`" + ` | 42 |
| plain | *em* | 7 |

` + "```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```" + `

%% a hidden comment %%

`
	var b strings.Builder
	b.WriteString("---\ntitle: Benchmark\ntags: [a, b]\n---\n# Benchmark\n\n")
	for b.Len() < size {
		b.WriteString(section)
	}
	b.WriteString("[^f]: The footnote.\n")
	return b.String()
}

func BenchmarkParse100K(b *testing.B) {
	src := benchNote(100 << 10)
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Parse(src)
	}
}

func BenchmarkParse1M(b *testing.B) {
	src := benchNote(1 << 20)
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Parse(src)
	}
}

func BenchmarkHelpers100K(b *testing.B) {
	doc := Parse(benchNote(100 << 10))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Outline(doc)
		Links(doc)
		WordCount(doc)
	}
}

// TestParseSpeed guards against accidental quadratic behaviour on a
// realistic large note (generous bound so slow CI machines pass).
func TestParseSpeed(t *testing.T) {
	src := benchNote(100 << 10)
	best := time.Hour
	for i := 0; i < 5; i++ {
		st := time.Now()
		Parse(src)
		if d := time.Since(st); d < best {
			best = d
		}
	}
	if best > ciSlack(100*time.Millisecond) {
		t.Fatalf("parsing a %d-byte note took %v", len(src), best)
	}
	checkInvariants(t, src, Parse(src))
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
