package vault

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func readStr(t *testing.T, v *Vault, rel string) string {
	t.Helper()
	b, err := os.ReadFile(v.Abs(rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTasksGrouping(t *testing.T) {
	v := openScanned(t, map[string]string{
		"b.md": "- [ ] later 📅 2026-10-10\n- [ ] today low due:2026-10-03 🔽\n- [x] done due:2026-10-01\n- [ ] undated\n",
		"a.md": "- [ ] overdue @due(2026-09-30)\n- [ ] today high due:2026-10-03 !!!\n- [/] in progress\n- [-] cancelled due:2026-09-01\n- [ ] today none due:2026-10-03\n",
	})
	g := GroupTasks(v.Tasks(false), Date{2026, 10, 3})
	texts := func(ts []TaskRef) []string {
		var out []string
		for _, t := range ts {
			out = append(out, t.Path+":"+t.Task.Text)
		}
		return out
	}
	if got := texts(g.Overdue); !reflect.DeepEqual(got, []string{"a.md:overdue"}) {
		t.Errorf("overdue %v", got)
	}
	if got := texts(g.Today); !reflect.DeepEqual(got, []string{"a.md:today high", "a.md:today none", "b.md:today low"}) {
		t.Errorf("today %v", got)
	}
	if got := texts(g.Upcoming); !reflect.DeepEqual(got, []string{"b.md:later"}) {
		t.Errorf("upcoming %v", got)
	}
	if got := texts(g.Undated); !reflect.DeepEqual(got, []string{"a.md:in progress", "b.md:undated"}) {
		t.Errorf("undated %v", got)
	}
	if len(v.Tasks(true)) != 9 {
		t.Errorf("all tasks %d", len(v.Tasks(true)))
	}
}

func TestToggleTask(t *testing.T) {
	src := "\uFEFF- [ ] first\r\n```\r\n- [ ] in code\r\n```\r\n  - [x] second ⏫\r\n- [/] third\r\nno newline at end - [ ] x"
	v := openScanned(t, map[string]string{"t.md": src})
	task, err := v.ToggleTask("t.md", 1, "first")
	if err != nil || task.State != 'x' {
		t.Fatalf("toggle 1: %v %+v", err, task)
	}
	want := strings.Replace(src, "- [ ] first", "- [x] first", 1)
	if got := readStr(t, v, "t.md"); got != want {
		t.Fatalf("after toggle 1:\n%q\nwant\n%q", got, want)
	}
	if _, err := v.ToggleTask("t.md", 5, ""); err != nil {
		t.Fatal(err)
	}
	want = strings.Replace(want, "  - [x] second", "  - [ ] second", 1)
	if _, err := v.ToggleTask("t.md", 6, ""); err != nil {
		t.Fatal(err)
	}
	want = strings.Replace(want, "- [/] third", "- [x] third", 1)
	if got := readStr(t, v, "t.md"); got != want {
		t.Fatalf("after toggles:\n%q\nwant\n%q", got, want)
	}
	// Not a task / inside a fence / text changed.
	var tc *TaskChangedError
	for _, line := range []int{2, 3, 7, 99} {
		if _, err := v.ToggleTask("t.md", line, ""); !errors.As(err, &tc) {
			t.Errorf("line %d: %v", line, err)
		}
	}
	if _, err := v.ToggleTask("t.md", 1, "something else"); !errors.As(err, &tc) {
		t.Errorf("changed text: %v", err)
	}
	if got := readStr(t, v, "t.md"); got != want {
		t.Error("failed toggles modified the file")
	}
	// The index was refreshed.
	n, _ := v.Note("t.md")
	if n.Tasks[0].State != 'x' {
		t.Errorf("index not refreshed: %+v", n.Tasks[0])
	}
}

func TestDailyConfigAndPath(t *testing.T) {
	root := t.TempDir()
	v, _ := Open(root)
	day := time.Date(2026, 3, 7, 9, 5, 0, 0, time.Local)
	if got := v.DailyPath(day); got != "daily/2026-03-07.md" {
		t.Errorf("default path %q", got)
	}
	writeTree(t, root, map[string]string{".obsidian/daily-notes.json": `{"folder":"Journal/","format":"YYYY/MM-MMM/dddd D [of] MMMM YY"}`})
	v, _ = Open(root)
	if got := v.DailyPath(day); got != "Journal/2026/03-Mar/Saturday 7 of March 26.md" {
		t.Errorf("obsidian path %q", got)
	}
	writeTree(t, root, map[string]string{".obsidian/daily-notes.json": `{"folder":"","format":""}`})
	v, _ = Open(root)
	if got := v.DailyPath(day); got != "2026-03-07.md" {
		t.Errorf("root path %q", got)
	}
	v.SetDailyConfig(DailyConfig{Folder: "days", Format: "D-M-YYYY ddd"})
	if got := v.DailyPath(day); got != "days/7-3-2026 Sat.md" {
		t.Errorf("override path %q", got)
	}
	if got := DailyHeading(day); got != "# Saturday, 7 March 2026" {
		t.Errorf("heading %q", got)
	}
	rel, created, err := v.EnsureDaily(day)
	if err != nil || !created || readStr(t, v, rel) != "# Saturday, 7 March 2026\n\n" {
		t.Fatalf("ensure: %q %v %v", rel, created, err)
	}
	if _, created, _ := v.EnsureDaily(day); created {
		t.Error("created twice")
	}
	if _, ok := v.Note(rel); !ok {
		t.Error("daily note not indexed")
	}
}

func TestCapture(t *testing.T) {
	v := openScanned(t, map[string]string{})
	now := time.Date(2026, 10, 3, 14, 7, 0, 0, time.Local)
	r, err := v.Capture("first thought", CaptureOptions{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if r.Path != "daily/2026-10-03.md" || r.Line != 3 {
		t.Errorf("result %+v", r)
	}
	if _, err := v.Capture("buy milk", CaptureOptions{Now: now, Task: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Capture("line one\n\n  line two  \n", CaptureOptions{Now: now}); err != nil {
		t.Fatal(err)
	}
	want := "# Saturday, 3 October 2026\n\n- 14:07 first thought\n- [ ] buy milk\n- 14:07 line one\n  line two\n"
	if got := readStr(t, v, r.Path); got != want {
		t.Errorf("daily:\n%q\nwant\n%q", got, want)
	}
	// Existing file without final newline, CRLF: one terminator added, style kept.
	writeTree(t, v.Root(), map[string]string{"inbox.md": "# Inbox\r\n\r\n- old"})
	r, err = v.Capture("new", CaptureOptions{Target: "inbox.md", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if got := readStr(t, v, "inbox.md"); got != "# Inbox\r\n\r\n- old\r\n- 14:07 new\r\n" || r.Line != 4 {
		t.Errorf("crlf inbox %q line %d", got, r.Line)
	}
	// Missing target is created; folders too.
	if _, err := v.Capture("x", CaptureOptions{Target: "new/dir/note.md", Now: now, Task: true}); err != nil {
		t.Fatal(err)
	}
	if got := readStr(t, v, "new/dir/note.md"); got != "- [ ] x\n" {
		t.Errorf("new target %q", got)
	}
	if _, err := v.Capture("  ", CaptureOptions{}); err == nil {
		t.Error("empty capture accepted")
	}
}

func TestCaptureUnderHeading(t *testing.T) {
	v := openScanned(t, map[string]string{
		"p.md": "# Project\n\n## Ideas\n- one\n\n### Sub\n- deep\n\n## Log\n- entry\n",
		"e.md": "# E\n## Empty\n## Next\n",
	})
	now := time.Date(2026, 10, 3, 8, 0, 0, 0, time.Local)
	r, err := v.Capture("two", CaptureOptions{Target: "p.md", Heading: "ideas", Now: now, Task: true})
	if err != nil {
		t.Fatal(err)
	}
	want := "# Project\n\n## Ideas\n- one\n\n### Sub\n- deep\n- [ ] two\n\n## Log\n- entry\n"
	if got := readStr(t, v, "p.md"); got != want || r.Line != 8 {
		t.Errorf("under heading:\n%q line %d\nwant\n%q", got, r.Line, want)
	}
	if _, err := v.Capture("x", CaptureOptions{Target: "e.md", Heading: "## Empty", Now: now, Task: true}); err != nil {
		t.Fatal(err)
	}
	if got := readStr(t, v, "e.md"); got != "# E\n## Empty\n\n- [ ] x\n\n## Next\n" {
		t.Errorf("empty section %q", got)
	}
	if _, err := v.Capture("y", CaptureOptions{Target: "e.md", Heading: "Missing", Now: now, Task: true}); err != nil {
		t.Fatal(err)
	}
	if got := readStr(t, v, "e.md"); got != "# E\n## Empty\n\n- [ ] x\n\n## Next\n\n## Missing\n\n- [ ] y\n" {
		t.Errorf("missing heading %q", got)
	}
}

func TestSafeFilename(t *testing.T) {
	cases := map[string]string{
		"Meeting: notes / ideas?": "Meeting notes ideas",
		"  .hidden.  ":            "hidden",
		"ملاحظات عن الكتاب":       "ملاحظات عن الكتاب",
		"a#b^c[d]e|f":             "a b c d e f",
		"tab\there\nnew":          "tab here new",
		"":                        "Untitled",
		"///":                     "Untitled",
		"con":                     "con_",
		"Café — déjà vu":          "Café — déjà vu",
		strings.Repeat("é", 150):  strings.Repeat("é", 100),
		"x\x00y":                  "xy",
	}
	for in, want := range cases {
		if got := SafeFilename(in); got != want {
			t.Errorf("SafeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCreateNote(t *testing.T) {
	v := openScanned(t, map[string]string{"Existing.md": "x"})
	rel, err := v.CreateNote("Existing", NewNoteOptions{})
	if err != nil || rel != "Existing 2.md" {
		t.Fatalf("%q %v", rel, err)
	}
	rel, err = v.CreateNote("existing", NewNoteOptions{})
	if err != nil || rel != "existing 3.md" {
		t.Fatalf("case-insensitive collision: %q %v", rel, err)
	}
	rel, err = v.CreateNote("رحلة: الصيف", NewNoteOptions{Dir: "ملاحظات/سفر", Body: []byte("نص")})
	if err != nil || rel != "ملاحظات/سفر/رحلة الصيف.md" {
		t.Fatalf("%q %v", rel, err)
	}
	if got := readStr(t, v, rel); got != "# رحلة: الصيف\n\nنص\n" {
		t.Errorf("content %q", got)
	}
	if n, ok := v.Note(rel); !ok || n.Title != "رحلة: الصيف" {
		t.Errorf("index %+v", n)
	}
	if got := readStr(t, v, "Existing 2.md"); got != "# Existing\n\n" {
		t.Errorf("empty body %q", got)
	}
	if _, err := v.CreateNote("x", NewNoteOptions{Dir: "../out"}); err == nil {
		t.Error("escaping dir accepted")
	}
	if _, err := v.CreateNote("  ", NewNoteOptions{}); err == nil {
		t.Error("empty title accepted")
	}
}

func TestWriteFileAtomicAndConflict(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "n.md")
	os.WriteFile(p, []byte("one\r\n"), 0o600)
	data, tok, err := ReadFile(p)
	if err != nil || string(data) != "one\r\n" || !tok.Exists {
		t.Fatal(err)
	}
	// Someone else edits the file underneath us.
	os.WriteFile(p, []byte("theirs\r\n"), 0o600)
	_, err = WriteFile(p, []byte("mine"), &tok)
	var ce *ConflictError
	if !errors.As(err, &ce) || !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "theirs\r\n" {
		t.Fatal("conflicting write clobbered the file")
	}
	// Touched but unchanged content is not a conflict.
	_, tok, _ = ReadFile(p)
	later := time.Now().Add(time.Hour)
	os.Chtimes(p, later, later)
	tok2, err := WriteFile(p, []byte("mine"), &tok)
	if err != nil {
		t.Fatalf("touch-only: %v", err)
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v not preserved", fi.Mode())
	}
	// Deleted underneath.
	os.Remove(p)
	if _, err := WriteFile(p, []byte("x"), &tok2); !errors.As(err, &ce) {
		t.Errorf("deleted: %v", err)
	}
	// Expect-absent but created.
	os.WriteFile(p, []byte("new"), 0o644)
	if _, err := WriteFile(p, []byte("x"), &StatToken{}); !errors.As(err, &ce) {
		t.Errorf("created: %v", err)
	}
	// nil token overwrites; no temp files left behind.
	if _, err := WriteFile(p, []byte("forced"), nil); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("leftover files: %v", entries)
	}
}

func TestWriteThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real", "note.md")
	os.MkdirAll(filepath.Dir(target), 0o755)
	os.WriteFile(target, []byte("old"), 0o640)
	link := filepath.Join(dir, "link.md")
	os.Symlink(target, link)
	_, tok, _ := ReadFile(link)
	if _, err := WriteFile(link, []byte("new"), &tok); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Lstat(link)
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink replaced by a regular file")
	}
	if b, _ := os.ReadFile(target); string(b) != "new" {
		t.Errorf("target %q", b)
	}
	if fi, _ := os.Stat(target); fi.Mode().Perm() != 0o640 {
		t.Errorf("target mode %v", fi.Mode())
	}
}

func TestVaultWriteNoteRefreshes(t *testing.T) {
	v := openScanned(t, map[string]string{"n.md": "# Old"})
	_, tok, _ := v.ReadNote("n.md")
	if _, err := v.WriteNote("n.md", []byte("# New\n[[x]]"), &tok); err != nil {
		t.Fatal(err)
	}
	if n, _ := v.Note("n.md"); n.Title != "New" || len(n.Links) != 1 {
		t.Errorf("not refreshed: %+v", n)
	}
	if _, err := v.WriteNote("n.md", []byte("again"), &tok); !errors.Is(err, ErrConflict) {
		t.Errorf("stale token accepted: %v", err)
	}
	if _, err := v.Rescan(context.Background()); err != nil {
		t.Fatal(err)
	}
}
