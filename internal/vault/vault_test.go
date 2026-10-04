package vault

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"
)

// writeTree creates files (slash paths → content) under root.
func writeTree(t testing.TB, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func openScanned(t testing.TB, files map[string]string) *Vault {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, files)
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	return v
}

func paths(ns []*Note) []string {
	var out []string
	for _, n := range ns {
		out = append(out, n.Path)
	}
	return out
}

func TestScanRules(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"a.md":                   "# A",
		"sub/b.md":               "# B",
		"sub/deep/c.MD":          "# C",
		".hidden/x.md":           "x",
		".dot.md":                "x",
		"node_modules/m.md":      "x",
		"build/out.md":           "x",
		"drafts/tmp/skip.md":     "x",
		"sub/scratch.md":         "x",
		"keep/scratch-not.md":    "# kept",
		"img/pic.png":            "PNG",
		"عربي/ملاحظة.md":         "# ملاحظة",
		".astrolabeignore":       "# comment\nbuild/\n/drafts/**\nscratch.md\n",
		"target/linked.md":       "# Linked",
		"outside/elsewhere.md":   "# Else",
		".obsidian/workspace.md": "x",
		// Control characters would break the one-result-per-line output.
		"bad\nname.md":   "x",
		"tab\tdir/x.md":  "x",
		"esc\x1b[31m.md": "x",
	})
	// File symlink followed, directory symlink not.
	if err := os.Symlink(filepath.Join(root, "target", "linked.md"), filepath.Join(root, "link.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(root, "dirlink")); err != nil {
		t.Fatal(err)
	}
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := paths(v.Notes())
	want := []string{"a.md", "keep/scratch-not.md", "link.md", "outside/elsewhere.md", "sub/b.md", "sub/deep/c.MD", "target/linked.md", "عربي/ملاحظة.md"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("notes\n got %v\nwant %v", got, want)
	}
	if n, _ := v.Note("link.md"); n == nil || n.Title != "Linked" {
		t.Errorf("symlinked note %+v", n)
	}
	if !reflect.DeepEqual(v.Files(), []string{"img/pic.png"}) {
		t.Errorf("files %v", v.Files())
	}
	if !v.Scanned() {
		t.Error("not marked scanned")
	}
}

func TestMatchIgnore(t *testing.T) {
	cases := []struct {
		pat, rel string
		dir, ok  bool
	}{
		{"*.tmp.md", "a/b/x.tmp.md", false, true},
		{"build/", "build", true, true},
		{"build/", "build", false, false},
		{"a/*/c.md", "a/b/c.md", false, true},
		{"a/*/c.md", "a/b/d/c.md", false, false},
		{"a/**/c.md", "a/b/d/c.md", false, true},
		{"a/**/c.md", "a/c.md", false, true},
		{"/top.md", "top.md", false, true},
		{"/top.md", "sub/top.md", false, false},
		{"**/x", "p/q/x", true, true},
	}
	for _, c := range cases {
		if got := matchIgnore(c.pat, c.rel, c.dir); got != c.ok {
			t.Errorf("matchIgnore(%q, %q, %v) = %v", c.pat, c.rel, c.dir, got)
		}
	}
}

func TestOpenBeforeScanAndRefresh(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"one.md": "# One\n[[two]]", "two.md": "# Two"})
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if v.Len() != 0 {
		t.Fatal("Open must not scan")
	}
	n, err := v.Refresh("one.md")
	if err != nil || n.Title != "One" {
		t.Fatalf("refresh: %v %+v", err, n)
	}
	if v.Len() != 1 {
		t.Fatalf("len %d", v.Len())
	}
	// The link does not resolve yet, then does after the scan.
	if _, ok := v.ResolveLink("one.md", n.Links[0]); ok {
		t.Error("resolved before two.md was known")
	}
	<-v.StartScan(context.Background())
	if _, ok := v.ResolveLink("one.md", n.Links[0]); !ok {
		t.Error("not resolved after scan")
	}
	// Refresh after deletion removes the note.
	os.Remove(filepath.Join(root, "two.md"))
	if _, err := v.Refresh("two.md"); !os.IsNotExist(err) {
		t.Errorf("refresh deleted: %v", err)
	}
	if _, ok := v.Note("two.md"); ok {
		t.Error("deleted note still indexed")
	}
	if _, err := v.Refresh("../escape.md"); err == nil {
		t.Error("escape accepted")
	}
}

func TestRescanOnlyChanged(t *testing.T) {
	v := openScanned(t, map[string]string{"a.md": "# A", "b.md": "# B", "c.md": "# C"})
	before, _ := v.Note("a.md")
	// Modify b (new size), delete c, add d.
	writeTree(t, v.Root(), map[string]string{"b.md": "# B changed", "d.md": "# D"})
	os.Remove(filepath.Join(v.Root(), "c.md"))
	changed, err := v.Rescan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if changed != 3 {
		t.Errorf("changed %d, want 3", changed)
	}
	after, _ := v.Note("a.md")
	if before != after {
		t.Error("unchanged note was re-read")
	}
	if n, _ := v.Note("b.md"); n.Title != "B changed" {
		t.Errorf("b title %q", n.Title)
	}
	if !reflect.DeepEqual(paths(v.Notes()), []string{"a.md", "b.md", "d.md"}) {
		t.Errorf("notes %v", paths(v.Notes()))
	}
}

func TestRecentNotesOrder(t *testing.T) {
	v := openScanned(t, map[string]string{"old.md": "o", "new.md": "n", "mid.md": "m"})
	now := time.Now()
	for i, p := range []string{"old.md", "mid.md", "new.md"} {
		mt := now.Add(time.Duration(i-3) * time.Hour)
		os.Chtimes(filepath.Join(v.Root(), p), mt, mt)
	}
	v.Rescan(context.Background())
	if got := paths(v.RecentNotes()); !reflect.DeepEqual(got, []string{"new.md", "mid.md", "old.md"}) {
		t.Errorf("recent %v", got)
	}
}

// TestConcurrentScanAndQuery exercises the index under -race: a scan, a
// rescan, refreshes, writes and every query run at once.
func TestConcurrentScanAndQuery(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{}
	for i := 0; i < 300; i++ {
		files[filepathName(i)] = "# Note " + itoa(i) + "\n#tag" + itoa(i%7) + " [[" + "n" + itoa((i+1)%300) + "]]\n- [ ] task " + itoa(i) + " due:2026-10-0" + itoa(1+i%9) + "\n"
	}
	writeTree(t, root, files)
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	done := v.StartScan(ctx)
	var wg sync.WaitGroup
	for g := 0; g < 6; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for k := 0; k < 30; k++ {
				switch (g + k) % 6 {
				case 0:
					v.Backlinks("n1.md")
					v.Unresolved()
				case 1:
					v.Tags()
					v.NotesWithTag("tag3")
				case 2:
					GroupTasks(v.Tasks(false), Date{2026, 10, 5})
				case 3:
					v.Search(ctx, "note", SearchOptions{Limit: 10})
				case 4:
					v.Refresh("n" + itoa(k) + ".md")
					v.ResolveName("n" + itoa(k+1))
				case 5:
					v.Capture("hello", CaptureOptions{Target: "inbox.md"})
					v.Rescan(ctx)
				}
			}
		}(g)
	}
	wg.Wait()
	<-done
	if err := v.ScanErr(); err != nil {
		t.Fatal(err)
	}
	if v.Len() != 301 {
		t.Errorf("len %d", v.Len())
	}
	bl := v.Backlinks("n1.md")
	if len(bl) != 1 || bl[0].From != "n0.md" {
		t.Errorf("backlinks %+v", bl)
	}
}

func filepathName(i int) string { return "n" + itoa(i) + ".md" }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestTagsRollUp(t *testing.T) {
	v := openScanned(t, map[string]string{
		"a.md": "#project/alpha #Project",
		"b.md": "---\ntags: [project/beta]\n---\n",
		"c.md": "#other #project/alpha/x",
		"d.md": "no tags",
	})
	got := v.Tags()
	want := []TagCount{{"other", 1}, {"project", 3}, {"project/alpha", 2}, {"project/alpha/x", 1}, {"project/beta", 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tags\n got %v\nwant %v", got, want)
	}
	ps := paths(v.NotesWithTag("#project/alpha"))
	sort.Strings(ps)
	if !reflect.DeepEqual(ps, []string{"a.md", "c.md"}) {
		t.Errorf("with tag %v", ps)
	}
	if len(v.NotesWithTag("proj")) != 0 {
		t.Error("prefix of a tag name matched")
	}
}

func TestRelAndCancel(t *testing.T) {
	v := openScanned(t, map[string]string{"a/b.md": "x"})
	if rel, err := v.Rel(filepath.Join(v.Root(), "a", "b.md")); err != nil || rel != "a/b.md" {
		t.Errorf("rel %q %v", rel, err)
	}
	if _, err := v.Rel(filepath.Dir(v.Root())); err == nil {
		t.Error("outside path accepted")
	}
	if v.Abs("a/b.md") != filepath.Join(v.Root(), "a", "b.md") {
		t.Error("abs")
	}
	root := t.TempDir()
	writeTree(t, root, map[string]string{"x.md": "x"})
	v2, _ := Open(root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	<-v2.StartScan(ctx)
	if v2.ScanErr() == nil {
		t.Error("cancelled scan reported success")
	}
	if _, err := v2.Rescan(context.Background()); err != nil || v2.Len() != 1 {
		t.Errorf("rescan after cancel: %v %d", err, v2.Len())
	}
	// A missing root is an empty vault, not an error.
	v3, err := Open(filepath.Join(root, "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if err := v3.Scan(context.Background()); err != nil || v3.Len() != 0 {
		t.Errorf("missing root: %v", err)
	}
}
