package vault

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseQuery(t *testing.T) {
	q, err := ParseQuery(`alpha "two words" re:fo+ tag:#Proj/x path:Daily/ title:"big plan" Upper tag:`)
	if err != nil {
		t.Fatal(err)
	}
	var terms []string
	for _, tm := range q.Terms {
		terms = append(terms, fmt.Sprintf("%s/%v/%v", tm.Text, tm.Regexp != nil, tm.CaseSensitive))
	}
	if want := []string{"alpha/false/false", "two words/false/false", "fo+/true/false", "Upper/false/true"}; !reflect.DeepEqual(terms, want) {
		t.Errorf("terms %v", terms)
	}
	if !reflect.DeepEqual(q.Tags, []string{"proj/x"}) || q.Paths[0].Text != "Daily/" || !q.Paths[0].CaseSensitive || q.Titles[0].Text != "big plan" {
		t.Errorf("filters %+v %+v %+v", q.Tags, q.Paths, q.Titles)
	}
	if _, err := ParseQuery("re:("); err == nil {
		t.Error("bad regexp accepted")
	}
	if q, _ := ParseQuery("   "); !q.Empty() {
		t.Error("blank not empty")
	}
	if q, _ := ParseQuery(`re:"\S+ x"`); q.Terms[0].CaseSensitive || !q.Terms[0].Regexp.MatchString("AB X") {
		t.Error("regexp smart case")
	}
}

func searchVault(t *testing.T) *Vault {
	v := openScanned(t, map[string]string{
		"Garden.md":      "# Garden\n\nPlant tomatoes in spring.\n## Tomato varieties\nCherry tomatoes are sweet.\n",
		"notes/cook.md":  "---\ntitle: Cooking\ntags: [food]\n---\nTomato sauce needs garlic.\n```\n# tomato in code is body\n```\n",
		"notes/other.md": "Nothing here about veg.\nGarlic only.\n",
		"عربي.md":        "# ملاحظة\nنص عن الطماطم هنا\n",
		"daily/2026.md":  "#food diary: tomato soup and garlic bread\n",
		"Case.md":        "ÉCOLE and école and Straße\n",
		"crlf.md":        "\uFEFFtomato first\r\nsecond tomato\r\n",
	})
	// Deterministic recency: Garden newest.
	base := time.Now()
	for i, p := range []string{"Garden.md", "notes/cook.md", "daily/2026.md", "crlf.md", "notes/other.md", "عربي.md", "Case.md"} {
		mt := base.Add(-time.Duration(i) * time.Hour)
		os.Chtimes(filepath.Join(v.Root(), filepath.FromSlash(p)), mt, mt)
	}
	v.Rescan(context.Background())
	return v
}

func fmtResults(rs []Result) []string {
	var out []string
	for _, r := range rs {
		var ms []string
		for _, m := range r.Matches {
			ms = append(ms, r.Text[m[0]:m[1]])
		}
		out = append(out, fmt.Sprintf("%s %s:%d %q %v", r.Class, r.Path, r.Line, r.Text, ms))
	}
	return out
}

func TestSearch(t *testing.T) {
	v := searchVault(t)
	ctx := context.Background()
	rs, err := v.Search(ctx, "tomato", SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`heading Garden.md:4 "## Tomato varieties" [Tomato]`,
		`body Garden.md:3 "Plant tomatoes in spring." [tomato]`,
		`body Garden.md:5 "Cherry tomatoes are sweet." [tomato]`,
		`body notes/cook.md:5 "Tomato sauce needs garlic." [Tomato]`,
		`body notes/cook.md:7 "# tomato in code is body" [tomato]`,
		`body daily/2026.md:1 "#food diary: tomato soup and garlic bread" [tomato]`,
		`body crlf.md:1 "tomato first" [tomato]`,
		`body crlf.md:2 "second tomato" [tomato]`,
	}
	if got := fmtResults(rs); !reflect.DeepEqual(got, want) {
		t.Errorf("tomato:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// Title hits rank first; smart case.
	rs, _ = v.Search(ctx, "Garden", SearchOptions{})
	if got := fmtResults(rs); len(got) != 1 || got[0] != `title Garden.md:1 "Garden" [Garden]` {
		t.Errorf("Garden: %v", got)
	}
	if rs, _ := v.Search(ctx, "TOMATO", SearchOptions{}); len(rs) != 0 {
		t.Errorf("smart case: %v", fmtResults(rs))
	}
	// AND across the note; lines with all terms preferred.
	rs, _ = v.Search(ctx, "tomato garlic", SearchOptions{})
	want = []string{
		`body notes/cook.md:5 "Tomato sauce needs garlic." [Tomato garlic]`,
		`body daily/2026.md:1 "#food diary: tomato soup and garlic bread" [tomato garlic]`,
	}
	if got := fmtResults(rs); !reflect.DeepEqual(got, want) {
		t.Errorf("and: %v", got)
	}
	// Phrase, regexp, filters, title term matching title only.
	rs, _ = v.Search(ctx, `"cherry tomatoes"`, SearchOptions{})
	if len(rs) != 1 || rs[0].Line != 5 {
		t.Errorf("phrase: %v", fmtResults(rs))
	}
	rs, _ = v.Search(ctx, `re:to\w+es\b`, SearchOptions{})
	if len(rs) != 2 {
		t.Errorf("regexp: %v", fmtResults(rs))
	}
	rs, _ = v.Search(ctx, "tag:food garlic", SearchOptions{})
	if got := fmtResults(rs); len(got) != 2 || rs[0].Path != "notes/cook.md" || rs[1].Path != "daily/2026.md" {
		t.Errorf("tag filter: %v", got)
	}
	rs, _ = v.Search(ctx, "tag:food", SearchOptions{})
	if got := fmtResults(rs); len(got) != 2 || rs[0].Class != RankTitle || rs[0].Text != "Cooking" || rs[0].Line != 2 {
		t.Errorf("filter only: %v", got)
	}
	rs, _ = v.Search(ctx, "path:notes/ garlic", SearchOptions{})
	if len(rs) != 2 || rs[0].Path != "notes/cook.md" || rs[1].Path != "notes/other.md" {
		t.Errorf("path filter: %v", fmtResults(rs))
	}
	rs, _ = v.Search(ctx, "title:cook sauce", SearchOptions{})
	if len(rs) != 1 {
		t.Errorf("title filter: %v", fmtResults(rs))
	}
	rs, _ = v.Search(ctx, "cooking", SearchOptions{})
	if got := fmtResults(rs); len(got) != 1 || got[0] != `title notes/cook.md:2 "Cooking" [Cooking]` {
		t.Errorf("frontmatter title: %v", got)
	}
	// Unicode: Arabic substring and case folding that keeps offsets.
	rs, _ = v.Search(ctx, "الطماطم", SearchOptions{})
	if got := fmtResults(rs); len(got) != 1 || got[0] != `body عربي.md:2 "نص عن الطماطم هنا" [الطماطم]` {
		t.Errorf("arabic: %v", got)
	}
	rs, _ = v.Search(ctx, "école", SearchOptions{})
	if got := fmtResults(rs); len(got) != 1 || got[0] != `body Case.md:1 "ÉCOLE and école and Straße" [ÉCOLE école]` {
		t.Errorf("fold: %v", got)
	}
	// Limit.
	rs, _ = v.Search(ctx, "tomato", SearchOptions{Limit: 3})
	if len(rs) != 3 || rs[0].Class != RankHeading {
		t.Errorf("limit: %v", fmtResults(rs))
	}
	// Cancellation.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := v.Search(cctx, "tomato", SearchOptions{}); err == nil {
		t.Error("cancelled search returned no error")
	}
	if rs, _ := v.Search(ctx, "", SearchOptions{}); rs != nil {
		t.Error("empty query returned results")
	}
}

func TestSearchBeforeScan(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"a.md": "# A\nfind me", "b/c.md": "find me too #t"})
	v, _ := Open(root)
	rs, err := v.Search(context.Background(), "find tag:t", SearchOptions{})
	if err != nil || len(rs) != 1 || rs[0].Path != "b/c.md" {
		t.Errorf("%v %v", fmtResults(rs), err)
	}
}

// genCorpus writes n notes of roughly avg bytes each, with links, tags,
// tasks and headings, returning the root.
func genCorpus(tb testing.TB, n, avg int) string {
	tb.Helper()
	root := tb.TempDir()
	rng := rand.New(rand.NewSource(1))
	words := strings.Fields("the a of to and in is it you that was for on are with as his they be at one have this from or had by word but what some we can out other were all there when up use your how said an each she which do their time if will way about many then them write would like so these her long make thing see him two has look more day could go come did number sound no most people my over know water than call first who may down side been now find any new work part take get place made live where after back little only round man year came show every good me give our under name very through just form sentence great think say help low line differ turn cause much mean before move right boy old too same tell does set three want air well also play small end put home read hand port large spell add even land here must big high such follow act why ask men change went light kind off need house picture try us again animal point mother world near build self earth father")
	for i := 0; i < n; i++ {
		var b strings.Builder
		fmt.Fprintf(&b, "---\ntags: [t%d, area/a%d]\n---\n# Note %d\n\n", i%40, i%7, i)
		for b.Len() < avg {
			switch rng.Intn(12) {
			case 0:
				fmt.Fprintf(&b, "\n## Section %d\n\n", rng.Intn(100))
			case 1:
				fmt.Fprintf(&b, "- [ ] task %s due:2026-10-%02d\n", words[rng.Intn(len(words))], 1+rng.Intn(28))
			case 2:
				fmt.Fprintf(&b, "See [[Note %d]] and #tag%d.\n", rng.Intn(n), rng.Intn(50))
			default:
				for k := 0; k < 14; k++ {
					b.WriteString(words[rng.Intn(len(words))])
					b.WriteByte(' ')
				}
				b.WriteString("\n")
			}
		}
		dir := filepath.Join(root, fmt.Sprintf("d%02d", i%30))
		os.MkdirAll(dir, 0o755)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("Note %d.md", i)), []byte(b.String()), 0o644); err != nil {
			tb.Fatal(err)
		}
	}
	return root
}

// TestSearchSpeed is a coarse guard (generous bound so it holds under
// -race and on slow CI); BenchmarkSearch gives the real numbers.
func TestSearchSpeed(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	root := genCorpus(t, 1500, 6800)
	v, _ := Open(root)
	start := time.Now()
	if err := v.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	scan := time.Since(start)
	start = time.Now()
	rs, err := v.Search(context.Background(), "people water", SearchOptions{Limit: 200})
	if err != nil || len(rs) == 0 {
		t.Fatal(err)
	}
	took := time.Since(start)
	t.Logf("1500 notes: scan %v, search %v", scan, took)
	if took > ciSlack(2*time.Second) {
		t.Errorf("search took %v", took)
	}
}

func BenchmarkSearch(b *testing.B) {
	root := genCorpus(b, 1500, 6800)
	v, _ := Open(root)
	v.Scan(context.Background())
	for _, q := range []string{"water", "people water", "Picture", `re:mo\w+r`, "tag:area/a3 house", "zzzznotfound"} {
		b.Run(q, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := v.Search(context.Background(), q, SearchOptions{Limit: 200}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkScan(b *testing.B) {
	root := genCorpus(b, 1500, 6800)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v, _ := Open(root)
		if err := v.Scan(context.Background()); err != nil {
			b.Fatal(err)
		}
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
