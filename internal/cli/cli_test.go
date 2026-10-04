package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/astrolabe-cli/internal/text"
	"github.com/ZahakJ/astrolabe-cli/internal/vault"
)

// fixedNow is Sunday 4 October 2026, 10:30 local time.
var fixedNow = time.Date(2026, 10, 4, 10, 30, 0, 0, time.Local)

// run is one in-process invocation of astrolabe.
type run struct {
	args      []string
	stdin     string
	stdinTTY  bool
	stdoutTTY bool
	stderrTTY bool
	env       map[string]string
	cwd       string
	hooks     Hooks
}

type result struct {
	code           int
	stdout, stderr string
}

// sandbox is a temporary HOME with XDG dirs and a vault.
type sandbox struct {
	t     *testing.T
	home  string
	vault string
	env   map[string]string
}

func newSandbox(t *testing.T, files map[string]string) *sandbox {
	t.Helper()
	home := t.TempDir()
	vault := filepath.Join(home, "vault")
	s := &sandbox{t: t, home: home, vault: vault, env: map[string]string{
		"HOME":            home,
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"),
		"XDG_STATE_HOME":  filepath.Join(home, ".state"),
		"LANG":            "en_US.UTF-8",
		"TERM":            "xterm-256color",
	}}
	if err := os.MkdirAll(filepath.Join(vault, ".astrolabe"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		s.write(name, body)
	}
	return s
}

func (s *sandbox) write(name, body string) {
	s.t.Helper()
	p := filepath.Join(s.vault, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		s.t.Fatal(err)
	}
}

func (s *sandbox) read(name string) string {
	s.t.Helper()
	b, err := os.ReadFile(filepath.Join(s.vault, filepath.FromSlash(name)))
	if err != nil {
		s.t.Fatal(err)
	}
	return string(b)
}

// age sets a file's mtime to now minus d.
func (s *sandbox) age(name string, d time.Duration) {
	s.t.Helper()
	tm := fixedNow.Add(-d)
	if err := os.Chtimes(filepath.Join(s.vault, filepath.FromSlash(name)), tm, tm); err != nil {
		s.t.Fatal(err)
	}
}

func (s *sandbox) run(r run) result {
	s.t.Helper()
	env := map[string]string{}
	for k, v := range s.env {
		env[k] = v
	}
	for k, v := range r.env {
		env[k] = v
	}
	cwd := r.cwd
	if cwd == "" {
		cwd = s.vault
	}
	var out, errb bytes.Buffer
	code := Run(Env{
		Args:      r.args,
		Stdin:     strings.NewReader(r.stdin),
		Stdout:    &out,
		Stderr:    &errb,
		StdinTTY:  r.stdinTTY,
		StdoutTTY: r.stdoutTTY,
		StderrTTY: r.stderrTTY,
		Getenv:    func(k string) string { return env[k] },
		Cwd:       cwd,
		Home:      s.home,
		Now:       func() time.Time { return fixedNow },
		Width:     100,
		Version:   "v0.0.0-test",
		Hooks:     r.hooks,
	})
	return result{code, out.String(), errb.String()}
}

// must runs and fails the test unless the exit code is want.
func (s *sandbox) must(want int, r run) result {
	s.t.Helper()
	res := s.run(r)
	if res.code != want {
		s.t.Fatalf("astrolabe %s: exit %d, want %d\nstdout:\n%s\nstderr:\n%s", strings.Join(r.args, " "), res.code, want, res.stdout, res.stderr)
	}
	return res
}

func args(a ...string) run { return run{args: a, stdinTTY: true} }

const lanternNote = `---
title: Lantern migration
aliases: [Lantern]
tags: [project/lantern]
---
# Lantern migration

Move the jobs to the new queue. The cutover is on Tuesday.

## Tasks

- [ ] Load test 📅 2026-10-06
- [ ] Renew certificate due:2026-10-02 !!
- [ ] Book the retro 📅 2026-10-04
- [ ] Buy a planisphere
- [x] Write the shim

See [[Cutover runbook]] and [[Missing note]] and [the site](https://example.org).
`

const runbookNote = `# Cutover runbook

Steps for the cutover of [[Lantern migration|Lantern]].

#runbook
`

func basicVault(t *testing.T) *sandbox {
	s := newSandbox(t, map[string]string{
		"projects/Lantern migration.md":  lanternNote,
		"runbooks/Cutover runbook.md":    runbookNote,
		"Inbox.md":                       "# Inbox\n\n- first thing\n\n## Reading\n\n- a book\n",
		"meetings/2026-09-28 Kickoff.md": "---\ntags: [meeting]\n---\n# Kickoff\n\nWe agreed on the [[Lantern]] plan.\n",
	})
	s.age("projects/Lantern migration.md", 3*time.Hour)
	s.age("runbooks/Cutover runbook.md", 2*time.Hour)
	s.age("Inbox.md", time.Hour)
	s.age("meetings/2026-09-28 Kickoff.md", 50*time.Hour)
	return s
}

func TestAddCapturesToToday(t *testing.T) {
	s := basicVault(t)
	res := s.must(0, args("add", "call", "the", "plumber"))
	want := "daily/2026-10-04.md:"
	if !strings.HasPrefix(res.stdout, want) {
		t.Fatalf("stdout %q, want prefix %q", res.stdout, want)
	}
	body := s.read("daily/2026-10-04.md")
	if !strings.HasPrefix(body, "# Sunday, 4 October 2026\n") {
		t.Errorf("daily note does not start with its heading:\n%s", body)
	}
	if !strings.HasSuffix(body, "- 10:30 call the plumber\n") {
		t.Errorf("capture line missing:\n%s", body)
	}
	// The printed line number points at the captured line.
	var line int
	if _, err := fmtSscan(strings.TrimPrefix(strings.TrimSpace(res.stdout), want), &line); err != nil {
		t.Fatal(err)
	}
	if got := strings.Split(body, "\n")[line-1]; got != "- 10:30 call the plumber" {
		t.Errorf("line %d is %q", line, got)
	}
	if res.stderr != "" {
		t.Errorf("unexpected stderr %q", res.stderr)
	}
}

func TestAddFromStdinAsTask(t *testing.T) {
	s := basicVault(t)
	s.must(0, run{args: []string{"add", "-t"}, stdin: "renew passport\n"})
	if body := s.read("daily/2026-10-04.md"); !strings.HasSuffix(body, "- [ ] renew passport\n") {
		t.Errorf("task capture missing:\n%s", body)
	}
	// Multi-line stdin becomes one item.
	s.must(0, run{args: []string{"add"}, stdin: "line one\n\nline two\n"})
	if body := s.read("daily/2026-10-04.md"); !strings.HasSuffix(body, "- 10:30 line one\n  line two\n") {
		t.Errorf("multi-line capture:\n%s", body)
	}
}

func TestAddNothingIsUsageError(t *testing.T) {
	s := basicVault(t)
	res := s.must(2, args("add"))
	if !strings.Contains(res.stderr, "nothing to capture") || !strings.Contains(res.stderr, "usage: astrolabe add") {
		t.Errorf("stderr: %q", res.stderr)
	}
	if res.stdout != "" {
		t.Errorf("stdout should be empty, got %q", res.stdout)
	}
	s.must(2, run{args: []string{"add"}, stdin: "   \n"})
	if _, err := os.Stat(filepath.Join(s.vault, "daily")); err == nil {
		t.Error("a failed capture created the daily folder")
	}
}

func TestAddToNoteAtHeading(t *testing.T) {
	s := basicVault(t)
	res := s.must(0, args("add", "-n", "Inbox", "--at", "Reading", "another book"))
	if !strings.HasPrefix(res.stdout, "Inbox.md:") {
		t.Errorf("stdout %q", res.stdout)
	}
	want := "# Inbox\n\n- first thing\n\n## Reading\n\n- a book\n- 10:30 another book\n"
	if got := s.read("Inbox.md"); got != want {
		t.Errorf("Inbox.md:\n%s\nwant:\n%s", got, want)
	}
	// A missing target is created at that path.
	s.must(0, args("add", "-n", "ideas/Later", "something"))
	if got := s.read("ideas/Later.md"); !strings.Contains(got, "- 10:30 something") {
		t.Errorf("new target: %q", got)
	}
}

func TestAddHumanOutput(t *testing.T) {
	s := basicVault(t)
	res := s.must(0, run{args: []string{"add", "hello"}, stdinTTY: true, stdoutTTY: true, env: map[string]string{"COLORTERM": "truecolor"}})
	if !strings.Contains(res.stdout, "Added to") || !strings.Contains(res.stdout, "\x1b[") {
		t.Errorf("human output: %q", res.stdout)
	}
	if !strings.Contains(stripANSI(res.stdout), "daily/2026-10-04.md:") {
		t.Errorf("human output lacks the location: %q", stripANSI(res.stdout))
	}
}

func TestNewNote(t *testing.T) {
	s := basicVault(t)
	res := s.must(0, args("new", "Lantern", "retro"))
	abs := filepath.Join(s.vault, "Lantern retro.md")
	if strings.TrimSpace(res.stdout) != abs {
		t.Errorf("stdout %q, want %q", res.stdout, abs)
	}
	if got := s.read("Lantern retro.md"); !strings.HasPrefix(got, "# Lantern retro\n") {
		t.Errorf("content %q", got)
	}
	// Never overwrites; stdin becomes the body; -d puts it in a folder.
	res = s.must(0, run{args: []string{"new", "-d", "meetings", "Lantern retro"}, stdin: "notes from stdin\n"})
	if !strings.HasSuffix(strings.TrimSpace(res.stdout), filepath.Join("meetings", "Lantern retro.md")) {
		t.Errorf("stdout %q", res.stdout)
	}
	if got := s.read("meetings/Lantern retro.md"); !strings.Contains(got, "notes from stdin") {
		t.Errorf("body missing: %q", got)
	}
	res = s.must(0, args("new", "Lantern retro"))
	if !strings.HasSuffix(strings.TrimSpace(res.stdout), "Lantern retro 2.md") {
		t.Errorf("duplicate title: %q", res.stdout)
	}
	s.must(2, args("new"))
	s.must(2, args("new", "-d", "../outside", "x"))
}

func TestNewWithEditor(t *testing.T) {
	s := basicVault(t)
	script := filepath.Join(s.home, "fake-editor")
	// The editor appends a line to the last argument (the file).
	if err := os.WriteFile(script, []byte("#!/bin/sh\nfor f; do :; done\necho 'edited' >> \"$f\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", script)
	t.Setenv("EDITOR", script)
	s.must(0, args("new", "-e", "Edited note"))
	if got := s.read("Edited note.md"); !strings.HasSuffix(got, "edited\n") {
		t.Errorf("editor did not run: %q", got)
	}
}

func TestTodayPrint(t *testing.T) {
	s := basicVault(t)
	res := s.must(0, args("today", "-p"))
	want := filepath.Join(s.vault, "daily", "2026-10-04.md")
	if strings.TrimSpace(res.stdout) != want {
		t.Errorf("stdout %q, want %q", res.stdout, want)
	}
	if got := s.read("daily/2026-10-04.md"); got != "# Sunday, 4 October 2026\n\n" {
		t.Errorf("daily note %q", got)
	}
	// daily_dir from the config file takes precedence.
	cfg := filepath.Join(s.home, ".config", "astrolabe-cli", "config")
	_ = os.MkdirAll(filepath.Dir(cfg), 0o755)
	_ = os.WriteFile(cfg, []byte("daily_dir = journal\n"), 0o644)
	res = s.must(0, args("today", "--print"))
	if !strings.HasSuffix(strings.TrimSpace(res.stdout), filepath.Join("journal", "2026-10-04.md")) {
		t.Errorf("config daily_dir ignored: %q", res.stdout)
	}
}

func TestFindPipe(t *testing.T) {
	s := basicVault(t)
	res := s.must(0, args("find", "cutover"))
	lines := strings.Split(strings.TrimSpace(res.stdout), "\n")
	if len(lines) < 3 {
		t.Fatalf("too few results:\n%s", res.stdout)
	}
	// Title hits first.
	if !strings.HasPrefix(lines[0], "runbooks/Cutover runbook.md:1:") {
		t.Errorf("first result %q", lines[0])
	}
	for _, l := range lines {
		parts := strings.SplitN(l, ":", 3)
		if len(parts) != 3 || parts[1] == "" {
			t.Errorf("not path:line:text: %q", l)
		}
		if strings.Contains(l, "\x1b") {
			t.Errorf("escape sequence in piped output: %q", l)
		}
	}
	if !strings.Contains(res.stdout, "projects/Lantern migration.md:8:Move the jobs to the new queue. The cutover is on Tuesday.") {
		t.Errorf("body hit missing:\n%s", res.stdout)
	}
}

func TestFindFilesAndJSON(t *testing.T) {
	s := basicVault(t)
	res := s.must(0, args("find", "-l", "cutover"))
	paths := strings.Split(strings.TrimSpace(res.stdout), "\n")
	seen := map[string]bool{}
	for _, p := range paths {
		if seen[p] {
			t.Errorf("duplicate path %q", p)
		}
		seen[p] = true
	}
	if !seen["projects/Lantern migration.md"] || !seen["runbooks/Cutover runbook.md"] {
		t.Errorf("paths: %v", paths)
	}

	res = s.must(0, args("find", "--json", "cutover"))
	var hits []map[string]any
	if err := json.Unmarshal([]byte(res.stdout), &hits); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, res.stdout)
	}
	for _, k := range []string{"path", "abs", "title", "line", "text", "matches", "kind"} {
		if _, ok := hits[0][k]; !ok {
			t.Errorf("JSON result lacks %q: %v", k, hits[0])
		}
	}
	if hits[0]["kind"] != "title" {
		t.Errorf("first kind %v", hits[0]["kind"])
	}
}

func TestFindNothing(t *testing.T) {
	s := basicVault(t)
	res := s.must(1, args("find", "zebra"))
	if res.stdout != "" || res.stderr != "" {
		t.Errorf("a miss in a pipe must be silent: %q %q", res.stdout, res.stderr)
	}
	res = s.must(1, run{args: []string{"find", "zebra"}, stderrTTY: true})
	if !strings.Contains(res.stderr, `no notes match "zebra"`) {
		t.Errorf("stderr %q", res.stderr)
	}
	s.must(2, args("find"))
	s.must(2, args("find", "re:("))
}

func TestFindHuman(t *testing.T) {
	s := basicVault(t)
	res := s.must(0, run{args: []string{"find", "cutover"}, stdoutTTY: true, env: map[string]string{"COLORTERM": "truecolor"}})
	if !strings.Contains(res.stdout, "\x1b[") {
		t.Fatal("no styling in a terminal")
	}
	plain := stripANSI(res.stdout)
	for _, want := range []string{"projects/Lantern migration.md", "matches in", "│"} {
		if !strings.Contains(plain, want) {
			t.Errorf("human find lacks %q:\n%s", want, plain)
		}
	}
	// NO_COLOR: no colour, attributes only.
	res = s.must(0, run{args: []string{"find", "cutover"}, stdoutTTY: true, env: map[string]string{"NO_COLOR": "1", "COLORTERM": "truecolor"}})
	if strings.Contains(res.stdout, "38;2;") || strings.Contains(res.stdout, "38;5;") {
		t.Errorf("colour despite NO_COLOR: %q", res.stdout)
	}
	// --color none in a terminal behaves the same.
	res = s.must(0, run{args: []string{"--color", "none", "find", "cutover"}, stdoutTTY: true})
	if strings.Contains(res.stdout, "38;") {
		t.Errorf("colour despite --color none")
	}
	// --ascii switches the glyphs.
	res = s.must(0, run{args: []string{"find", "cutover", "--ascii"}, stdoutTTY: true})
	if strings.Contains(res.stdout, "│") {
		t.Errorf("unicode glyph despite --ascii")
	}
}

func TestLs(t *testing.T) {
	s := basicVault(t)
	res := s.must(0, args("ls"))
	lines := strings.Split(strings.TrimSpace(res.stdout), "\n")
	want := []string{"Inbox.md", "runbooks/Cutover runbook.md", "projects/Lantern migration.md", "meetings/2026-09-28 Kickoff.md"}
	if len(lines) != len(want) {
		t.Fatalf("ls:\n%s", res.stdout)
	}
	for i, l := range lines {
		f := strings.Split(l, "\t")
		if len(f) != 3 || f[0] != want[i] {
			t.Errorf("line %d = %q, want path %q and 3 fields", i, l, want[i])
		}
	}
	if f := strings.Split(lines[2], "\t"); f[2] != "Lantern migration" {
		t.Errorf("title field %q", f[2])
	}
	res = s.must(0, args("ls", "--tag", "project", "-l"))
	if strings.TrimSpace(res.stdout) != "projects/Lantern migration.md" {
		t.Errorf("--tag (nested) -l: %q", res.stdout)
	}
	res = s.must(0, args("ls", "-l", "lantern"))
	if !strings.Contains(res.stdout, "projects/Lantern migration.md") || strings.Contains(res.stdout, "Inbox.md") {
		t.Errorf("query filter: %q", res.stdout)
	}
	res = s.must(0, args("ls", "--json", "-n", "1"))
	var notes []map[string]any
	if err := json.Unmarshal([]byte(res.stdout), &notes); err != nil || len(notes) != 1 {
		t.Fatalf("json: %v %s", err, res.stdout)
	}
	for _, k := range []string{"path", "abs", "title", "aliases", "tags", "modified", "words", "size"} {
		if _, ok := notes[0][k]; !ok {
			t.Errorf("ls JSON lacks %q", k)
		}
	}
	s.must(1, args("ls", "--tag", "nonexistent"))

	res = s.must(0, run{args: []string{"ls"}, stdoutTTY: true})
	plain := stripANSI(res.stdout)
	if !strings.Contains(plain, "1 h ago") || !strings.Contains(plain, "Lantern migration") || !strings.Contains(plain, "2 Oct") && !strings.Contains(plain, "Fri") {
		t.Errorf("human ls:\n%s", plain)
	}
}

func TestTasks(t *testing.T) {
	s := basicVault(t)
	res := s.must(0, args("tasks"))
	want := []string{
		"projects/Lantern migration.md:13:[ ] Renew certificate due:2026-10-02 !!",
		"projects/Lantern migration.md:14:[ ] Book the retro due:2026-10-04",
		"projects/Lantern migration.md:12:[ ] Load test due:2026-10-06",
		"projects/Lantern migration.md:15:[ ] Buy a planisphere",
	}
	if got := strings.Split(strings.TrimSpace(res.stdout), "\n"); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("tasks:\n%s\nwant:\n%s", res.stdout, strings.Join(want, "\n"))
	}
	res = s.must(0, args("tasks", "--json", "--all"))
	var ts []map[string]any
	if err := json.Unmarshal([]byte(res.stdout), &ts); err != nil {
		t.Fatal(err)
	}
	groups := map[string]int{}
	for _, x := range ts {
		groups[x["group"].(string)]++
	}
	if groups["overdue"] != 1 || groups["today"] != 1 || groups["upcoming"] != 1 || groups["undated"] != 2 {
		t.Errorf("groups %v", groups)
	}
	res = s.must(0, args("tasks", "--due"))
	if strings.Contains(res.stdout, "planisphere") {
		t.Errorf("--due kept an undated task")
	}
	res = s.must(0, run{args: []string{"tasks"}, stdoutTTY: true})
	plain := stripANSI(res.stdout)
	for _, w := range []string{"Overdue", "Today", "Upcoming", "Undated", "☐", "Tue", "4 open tasks"} {
		if !strings.Contains(plain, w) {
			t.Errorf("human tasks lacks %q:\n%s", w, plain)
		}
	}
	empty := newSandbox(t, map[string]string{"a.md": "# A\n"})
	empty.must(1, args("tasks"))
}

func TestTags(t *testing.T) {
	s := basicVault(t)
	res := s.must(0, args("tags"))
	want := "meeting\t1\nproject\t1\nproject/lantern\t1\nrunbook\t1\n"
	if res.stdout != want {
		t.Errorf("tags:\n%q\nwant\n%q", res.stdout, want)
	}
	res = s.must(0, run{args: []string{"tags"}, stdoutTTY: true})
	if plain := stripANSI(res.stdout); !strings.Contains(plain, "#project") || !strings.Contains(plain, "  /lantern") {
		t.Errorf("human tags:\n%s", plain)
	}
	res = s.must(0, args("tags", "--json"))
	var tags []map[string]any
	if err := json.Unmarshal([]byte(res.stdout), &tags); err != nil || len(tags) != 4 {
		t.Errorf("json tags %v %s", err, res.stdout)
	}
}

func TestLinksAndBacklinks(t *testing.T) {
	s := basicVault(t)
	res := s.must(0, args("links", "Lantern"))
	want := "18\twikilink\trunbooks/Cutover runbook.md\n18\tbroken\tMissing note\n18\turl\thttps://example.org\n"
	if res.stdout != want {
		t.Errorf("links:\n%q\nwant\n%q", res.stdout, want)
	}
	res = s.must(0, args("links", "-l", "Lantern"))
	if strings.TrimSpace(res.stdout) != "runbooks/Cutover runbook.md" {
		t.Errorf("links -l: %q", res.stdout)
	}
	res = s.must(0, args("backlinks", "projects/Lantern migration.md"))
	want = "meetings/2026-09-28 Kickoff.md:6:We agreed on the [[Lantern]] plan.\nrunbooks/Cutover runbook.md:3:Steps for the cutover of [[Lantern migration|Lantern]].\n"
	if res.stdout != want {
		t.Errorf("backlinks:\n%q\nwant\n%q", res.stdout, want)
	}
	res = s.must(0, run{args: []string{"backlinks", "Lantern"}, stdoutTTY: true})
	if plain := stripANSI(res.stdout); !strings.Contains(plain, "linked from 2 notes") {
		t.Errorf("human backlinks:\n%s", plain)
	}
	res = s.must(0, args("backlinks", "--json", "Lantern"))
	var bl []map[string]any
	if err := json.Unmarshal([]byte(res.stdout), &bl); err != nil || len(bl) != 2 || bl[0]["context"] == nil {
		t.Errorf("backlinks json: %v %s", err, res.stdout)
	}
	s.must(1, args("backlinks", "Inbox"))
	s.must(2, args("links"))
}

func TestPathAndRootResolution(t *testing.T) {
	s := basicVault(t)
	res := s.must(0, args("path"))
	if strings.TrimSpace(res.stdout) != s.vault {
		t.Errorf("path: %q", res.stdout)
	}
	res = s.must(0, args("path", "Inbox"))
	if strings.TrimSpace(res.stdout) != filepath.Join(s.vault, "Inbox.md") {
		t.Errorf("path Inbox: %q", res.stdout)
	}

	// From elsewhere: -C before or after the verb, ASTROLABE_DIR, flag beats env.
	other := t.TempDir()
	res = s.must(0, run{args: []string{"-C", s.vault, "path"}, cwd: other})
	if strings.TrimSpace(res.stdout) != s.vault {
		t.Errorf("-C before verb: %q", res.stdout)
	}
	res = s.must(0, run{args: []string{"path", "--dir=" + s.vault}, cwd: other})
	if strings.TrimSpace(res.stdout) != s.vault {
		t.Errorf("--dir= after verb: %q", res.stdout)
	}
	res = s.must(0, run{args: []string{"path"}, cwd: other, env: map[string]string{"ASTROLABE_DIR": s.vault}})
	if strings.TrimSpace(res.stdout) != s.vault {
		t.Errorf("ASTROLABE_DIR: %q", res.stdout)
	}
	// The former name's variable is a silent fallback; the new one wins.
	res = s.must(0, run{args: []string{"path"}, cwd: other, env: map[string]string{"FOLIO_DIR": s.vault}})
	if strings.TrimSpace(res.stdout) != s.vault {
		t.Errorf("FOLIO_DIR fallback: %q", res.stdout)
	}
	res = s.must(0, run{args: []string{"path"}, cwd: other, env: map[string]string{"FOLIO_DIR": other, "ASTROLABE_DIR": s.vault}})
	if strings.TrimSpace(res.stdout) != s.vault {
		t.Errorf("ASTROLABE_DIR must beat FOLIO_DIR: %q", res.stdout)
	}
	res = s.must(0, run{args: []string{"path", "-C", other}, cwd: other, env: map[string]string{"ASTROLABE_DIR": s.vault}})
	if strings.TrimSpace(res.stdout) != other {
		t.Errorf("-C must beat ASTROLABE_DIR: %q", res.stdout)
	}
	// Machine output from outside the vault uses absolute paths.
	res = s.must(0, run{args: []string{"ls", "-l"}, cwd: other, env: map[string]string{"ASTROLABE_DIR": s.vault}})
	if !strings.Contains(res.stdout, filepath.Join(s.vault, "Inbox.md")) {
		t.Errorf("ls from outside: %q", res.stdout)
	}
	// From a sub-folder, relative to it.
	res = s.must(0, run{args: []string{"ls", "-l"}, cwd: filepath.Join(s.vault, "projects")})
	if !strings.Contains(res.stdout, "../Inbox.md\n") || !strings.Contains(res.stdout, "Lantern migration.md\n") {
		t.Errorf("ls from a sub-folder: %q", res.stdout)
	}
	s.must(1, args("path", "nothing-like-this"))
	s.must(2, run{args: []string{"-C"}, cwd: other})
}

// fakeTUI records the requests it gets.
type fakeTUI struct {
	reqs []TUIRequest
	pick string
	err  error
}

func (f *fakeTUI) hooks() Hooks {
	return Hooks{RunTUI: func(_ context.Context, r TUIRequest) (TUIResult, error) {
		f.reqs = append(f.reqs, r)
		return TUIResult{Path: f.pick}, f.err
	}}
}

func TestReservedWordsAndNotes(t *testing.T) {
	s := basicVault(t)
	s.write("add.md", "# A note called add\n")
	f := &fakeTUI{}
	// "./add.md" opens the note; "add" alone is the verb.
	s.must(0, run{args: []string{"./add.md"}, hooks: f.hooks()})
	if len(f.reqs) != 1 || f.reqs[0].Path != "add.md" || f.reqs[0].Mode != ModeOpen {
		t.Fatalf("./add.md: %+v", f.reqs)
	}
	s.must(0, run{args: []string{"--", "add"}, hooks: f.hooks()})
	if len(f.reqs) != 2 || f.reqs[1].Path != "add.md" {
		t.Fatalf("-- add: %+v", f.reqs)
	}
	s.must(2, run{args: []string{"add"}, stdinTTY: true, hooks: f.hooks()})

	// Titles, aliases and unique fuzzy matches open notes.
	for arg, want := range map[string]string{
		"Lantern migration":          "projects/Lantern migration.md",
		"Lantern":                    "projects/Lantern migration.md",
		"projects/Lantern migration": "projects/Lantern migration.md",
		"kickoff":                    "meetings/2026-09-28 Kickoff.md",
		"Inbox.md:5":                 "Inbox.md",
	} {
		f.reqs = nil
		s.must(0, run{args: []string{arg}, hooks: f.hooks()})
		if len(f.reqs) != 1 || f.reqs[0].Path != want {
			t.Errorf("astrolabe %q opened %+v, want %s", arg, f.reqs, want)
		}
	}
	f.reqs = nil
	s.must(0, run{args: []string{"Inbox#Reading"}, hooks: f.hooks()})
	if f.reqs[0].Line != 5 {
		t.Errorf("heading anchor line = %d, want 5", f.reqs[0].Line)
	}
	// Unique verb prefixes are not verbs: "ad" opens the note add.md (a
	// unique fuzzy match), and a mistyped verb that matches no note gets
	// a suggestion and exit 1.
	f.reqs = nil
	s.must(0, run{args: []string{"ad"}, hooks: f.hooks()})
	if f.reqs[0].Path != "add.md" {
		t.Errorf("ad opened %q", f.reqs[0].Path)
	}
	res := s.must(1, run{args: []string{"tsks"}, hooks: f.hooks()})
	if !strings.HasPrefix(res.stderr, "astrolabe: did you mean `astrolabe tasks`?\n") {
		t.Errorf("stderr %q", res.stderr)
	}
	// The verb suggestion comes first, before note matches, and a typo of a
	// verb is not opened by a fuzzy match.
	s.write("Tasks backlog.md", "# Tasks backlog\n")
	res = s.must(1, run{args: []string{"tsks"}, hooks: f.hooks()})
	if !strings.HasPrefix(res.stderr, "astrolabe: did you mean `astrolabe tasks`?\n") || !strings.Contains(res.stderr, "Tasks backlog.md") {
		t.Errorf("stderr %q", res.stderr)
	}
	res = s.must(1, run{args: []string{"fnd", "rollback"}, hooks: f.hooks()})
	if !strings.HasPrefix(res.stderr, "astrolabe: did you mean `astrolabe find rollback`?\n") {
		t.Errorf("stderr %q", res.stderr)
	}
	// An exact note name always wins.
	f.reqs = nil
	s.must(0, run{args: []string{"Tasks backlog"}, hooks: f.hooks()})
	if f.reqs[0].Path != "Tasks backlog.md" {
		t.Errorf("opened %q", f.reqs[0].Path)
	}
	res = s.must(1, run{args: []string{"Lantern migratoin"}, hooks: f.hooks()})
	if !strings.Contains(res.stderr, "projects/Lantern migration.md") {
		t.Errorf("nearest match missing: %q", res.stderr)
	}
	// Ambiguous fuzzy matches are listed, not guessed.
	res = s.must(1, run{args: []string{"n"}, hooks: f.hooks()})
	if !strings.Contains(res.stderr, "be more specific") {
		t.Errorf("ambiguous: %q", res.stderr)
	}
	s.must(2, run{args: []string{"--bogus"}, hooks: f.hooks()})
}

func TestOpenOutsideVault(t *testing.T) {
	s := basicVault(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "loose.md")
	_ = os.WriteFile(p, []byte("# Loose\n"), 0o644)
	f := &fakeTUI{}
	s.must(0, run{args: []string{p}, hooks: f.hooks()})
	if f.reqs[0].Root.Dir != dir || f.reqs[0].Path != "loose.md" {
		t.Errorf("single-file mode: root %q path %q", f.reqs[0].Root.Dir, f.reqs[0].Path)
	}
}

func TestDefaultAndTodayTUI(t *testing.T) {
	s := basicVault(t)
	f := &fakeTUI{}
	s.must(0, run{hooks: f.hooks()})
	if f.reqs[0].Mode != ModeDefault || f.reqs[0].Path != "" || f.reqs[0].Vault == nil {
		t.Errorf("default with nothing recent: %+v", f.reqs[0])
	}
	s.must(0, run{args: []string{"today"}, hooks: f.hooks()})
	if f.reqs[1].Mode != ModeToday || f.reqs[1].Path != "daily/2026-10-04.md" {
		t.Errorf("today: %+v", f.reqs[1])
	}
	// Now today's note exists, plain astrolabe opens it.
	s.must(0, run{hooks: f.hooks()})
	if f.reqs[2].Path != "daily/2026-10-04.md" {
		t.Errorf("default with a daily note: %q", f.reqs[2].Path)
	}
	// stdin mode.
	s.must(0, run{args: []string{"-"}, stdin: "# piped\n", hooks: f.hooks()})
	if r := f.reqs[3]; r.Mode != ModeStdin || string(r.Source) != "# piped\n" {
		t.Errorf("stdin: %+v", r)
	}
	s.must(2, run{args: []string{"-"}, stdinTTY: true, hooks: f.hooks()})
}

func TestDefaultUsesRecent(t *testing.T) {
	s := basicVault(t)
	state := filepath.Join(s.home, ".state", "astrolabe-cli")
	_ = os.MkdirAll(state, 0o755)
	rec := "12\t" + filepath.Join(t.TempDir(), "elsewhere.md") + "\n7\t" + filepath.Join(s.vault, "Inbox.md") + "\n"
	_ = os.WriteFile(filepath.Join(state, "recent"), []byte(rec), 0o644)
	f := &fakeTUI{}
	s.must(0, run{hooks: f.hooks()})
	if f.reqs[0].Path != "Inbox.md" || f.reqs[0].Line != 7 {
		t.Errorf("recent: %q line %d", f.reqs[0].Path, f.reqs[0].Line)
	}
}

func TestNotBuiltHooks(t *testing.T) {
	s := basicVault(t)
	for _, a := range [][]string{{}, {"Inbox"}, {"today"}, {"pick"}, {"new", "-o", "X"}} {
		res := s.must(1, args(a...))
		if !strings.Contains(res.stderr, "not built") {
			t.Errorf("astrolabe %v: %q", a, res.stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(s.vault, "daily")); err == nil {
		t.Error("astrolabe today created a note although the TUI is missing")
	}
	if _, err := os.Stat(filepath.Join(s.vault, "X.md")); err == nil {
		t.Error("new -o created a note although the TUI is missing")
	}
	res := s.must(1, run{args: []string{"render", "-"}, stdin: "# x"})
	if !strings.Contains(res.stderr, "render is not built") {
		t.Errorf("render: %q", res.stderr)
	}
	s.must(1, args("export", "Inbox"))
}

func TestPick(t *testing.T) {
	s := basicVault(t)
	f := &fakeTUI{pick: "Inbox.md"}
	res := s.must(0, run{args: []string{"pick", "inb"}, hooks: f.hooks()})
	if strings.TrimSpace(res.stdout) != filepath.Join(s.vault, "Inbox.md") || f.reqs[0].Query != "inb" {
		t.Errorf("pick: %q %+v", res.stdout, f.reqs[0])
	}
	f = &fakeTUI{err: ErrCancelled}
	res = s.must(130, run{args: []string{"pick"}, hooks: f.hooks()})
	if res.stdout != "" || res.stderr != "" {
		t.Errorf("cancelled pick printed %q %q", res.stdout, res.stderr)
	}
}

func TestRenderAndExportHooks(t *testing.T) {
	s := basicVault(t)
	var got RenderRequest
	h := Hooks{
		Render: func(_ context.Context, r RenderRequest) error {
			got = r
			_, err := r.Out.Write([]byte("rendered\n"))
			return err
		},
		Export: func(_ context.Context, r ExportRequest) error {
			_, err := r.Out.Write([]byte("<html>" + r.Path + "</html>\n"))
			return err
		},
	}
	res := s.must(0, run{args: []string{"render", "-w", "60"}, stdin: "# hi\n", hooks: h})
	if res.stdout != "rendered\n" || string(got.Source) != "# hi\n" || got.Width != 60 || got.Styled || got.Name != "-" {
		t.Errorf("render stdin: %q %+v", res.stdout, got)
	}
	s.must(0, run{args: []string{"render", "--color=256", "Lantern"}, stdinTTY: true, hooks: h})
	if !got.Styled || got.Path != "projects/Lantern migration.md" || got.Vault == nil || got.Width != 100 {
		t.Errorf("render note forced colour: %+v", got)
	}
	res = s.must(0, run{args: []string{"export", "Inbox"}, stdinTTY: true, hooks: h})
	if res.stdout != "<html>Inbox.md</html>\n" {
		t.Errorf("export: %q", res.stdout)
	}
	out := filepath.Join(s.home, "out.html")
	s.must(0, run{args: []string{"export", "-o", out, "Inbox"}, stdinTTY: true, hooks: h})
	if b, _ := os.ReadFile(out); string(b) != "<html>Inbox.md</html>\n" {
		t.Errorf("export -o wrote %q", b)
	}
	s.must(2, run{args: []string{"render"}, stdinTTY: true, hooks: h})
	failing := Hooks{Render: func(context.Context, RenderRequest) error { return errors.New("boom") }}
	res = s.must(1, run{args: []string{"render", "Inbox"}, stdinTTY: true, hooks: failing})
	if !strings.Contains(res.stderr, "boom") {
		t.Errorf("render error: %q", res.stderr)
	}
}

func TestGlobalFlagErrors(t *testing.T) {
	s := basicVault(t)
	for _, a := range [][]string{
		{"--color", "rainbow", "ls"},
		{"ls", "--theme", "neon"},
		{"--bidi", "sideways", "ls"},
		{"ls", "--frobnicate"},
		{"find", "-z", "x"},
		{"tasks", "extra"},
	} {
		res := s.must(2, args(a...))
		if res.stdout != "" || !strings.HasPrefix(res.stderr, "astrolabe: ") {
			t.Errorf("astrolabe %v: stdout %q stderr %q", a, res.stdout, res.stderr)
		}
	}
	// Valid globals anywhere.
	s.must(0, args("ls", "--theme", "parchment", "--color=16", "--bidi", "off", "--ascii"))
	// "--" ends option parsing: the text is captured literally.
	s.must(0, args("add", "--", "-t", "is", "text"))
	if body := s.read("daily/2026-10-04.md"); !strings.Contains(body, "- 10:30 -t is text\n") {
		t.Errorf("-- not honoured:\n%s", body)
	}
}

func TestHelpAndVersion(t *testing.T) {
	s := basicVault(t)
	res := s.must(0, args("help"))
	for _, v := range Verbs() {
		if !strings.Contains(res.stdout, v) {
			t.Errorf("overview does not mention %q", v)
		}
	}
	for _, v := range Verbs() {
		d, ok := docs[v]
		if !ok || d.usage == "" || d.summary == "" {
			t.Errorf("verb %q has no help", v)
		}
		res := s.must(0, args("help", v))
		if !strings.HasPrefix(res.stdout, "astrolabe "+v) {
			t.Errorf("help %s starts %q", v, firstLine(res.stdout))
		}
		for i, l := range strings.Split(res.stdout, "\n") {
			if w := len([]rune(l)); w > 80 {
				t.Errorf("help %s line %d is %d columns: %q", v, i+1, w, l)
			}
		}
	}
	for i, l := range strings.Split(overview, "\n") {
		if w := len([]rune(l)); w > 80 {
			t.Errorf("overview line %d is %d columns", i+1, w)
		}
	}
	if res := s.must(0, args("find", "--help")); !strings.HasPrefix(res.stdout, "astrolabe find") {
		t.Errorf("find --help: %q", firstLine(res.stdout))
	}
	if res := s.must(0, args("-h")); !strings.Contains(res.stdout, "Global flags") {
		t.Errorf("-h: %q", firstLine(res.stdout))
	}
	res = s.must(2, args("help", "fnd"))
	if !strings.Contains(res.stderr, `did you mean "find"`) {
		t.Errorf("help fnd: %q", res.stderr)
	}
	res = s.must(0, args("version"))
	if !strings.HasPrefix(res.stdout, "astrolabe v0.0.0-test (") {
		t.Errorf("version: %q", res.stdout)
	}
	if res := s.must(0, args("--version")); !strings.HasPrefix(res.stdout, "astrolabe v0.0.0-test") {
		t.Errorf("--version: %q", res.stdout)
	}
	for _, v := range Verbs() {
		if !IsVerb(v) {
			t.Errorf("IsVerb(%q) = false", v)
		}
	}
	if IsVerb("ad") || IsVerb("Add") {
		t.Error("prefixes or other cases must not be verbs")
	}
}

func TestDoctor(t *testing.T) {
	s := basicVault(t)
	cfg := filepath.Join(s.home, ".config", "astrolabe-cli", "config")
	_ = os.MkdirAll(filepath.Dir(cfg), 0o755)
	_ = os.WriteFile(cfg, []byte("theme = mocha\ncolour = yes\n"), 0o644)
	res := s.must(0, args("doctor"))
	for _, w := range []string{"Terminal", "Vault", "Config", "Test card", s.vault, "4 notes", "1 broken link", "theme", "mocha", `unknown key "colour"`} {
		if !strings.Contains(res.stdout, w) {
			t.Errorf("doctor lacks %q:\n%s", w, res.stdout)
		}
	}
	if strings.Contains(res.stdout, "\x1b") {
		t.Error("doctor in a pipe printed escape sequences")
	}
	res = s.must(0, run{args: []string{"doctor"}, stdoutTTY: true, env: map[string]string{"COLORTERM": "truecolor"}})
	if !strings.Contains(res.stdout, "\x1b[") {
		t.Error("doctor in a terminal is unstyled")
	}
	if strings.Contains(res.stdout, "set-clipboard") {
		t.Error("tmux advice outside tmux")
	}
	res = s.must(0, run{args: []string{"doctor"}, env: map[string]string{"TMUX": "/tmp/tmux-0/default,1,0", "TERM": "tmux-256color"}})
	if !strings.Contains(res.stdout, "set -g set-clipboard on") {
		t.Errorf("doctor inside tmux lacks the set-clipboard note:\n%s", res.stdout)
	}
	if _, err := vault.WriteRecovered(filepath.Join(s.home, ".state", "astrolabe-cli", "recovered"), "Inbox.md", []byte("x"), fixedNow); err != nil {
		t.Fatal(err)
	}
	if res = s.must(0, args("doctor")); !strings.Contains(res.stdout, "1 unsaved buffer") {
		t.Errorf("doctor does not mention recovered buffers:\n%s", res.stdout)
	}
}

func TestFreshHome(t *testing.T) {
	// No vault anywhere: the default is ~/notes, created on first write.
	home := t.TempDir()
	s := &sandbox{t: t, home: home, vault: filepath.Join(home, "notes"), env: map[string]string{
		"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config"), "XDG_STATE_HOME": filepath.Join(home, ".state"),
	}}
	res := s.must(1, run{args: []string{"ls"}, cwd: home, stderrTTY: true})
	if !strings.Contains(res.stderr, "no vault at") {
		t.Errorf("ls without a vault: %q", res.stderr)
	}
	s.must(0, run{args: []string{"add", "first"}, cwd: home})
	if body := s.read("daily/2026-10-04.md"); !strings.Contains(body, "first") {
		t.Errorf("fresh capture: %q", body)
	}
	// The first interactive run writes Welcome.md into a fresh ~/notes.
	home2 := t.TempDir()
	s2 := &sandbox{t: t, home: home2, vault: filepath.Join(home2, "notes"), env: map[string]string{"HOME": home2}}
	f := &fakeTUI{}
	s2.must(0, run{cwd: home2, hooks: f.hooks()})
	if f.reqs[0].Path != "Welcome.md" {
		t.Errorf("welcome not opened: %+v", f.reqs[0].Path)
	}
}

func TestExampleVault(t *testing.T) {
	dir, err := filepath.Abs("../../examples/vault")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".obsidian", "daily-notes.json")); err != nil {
		t.Skip("examples/vault not present")
	}
	s := &sandbox{t: t, home: t.TempDir(), vault: dir, env: map[string]string{"LANG": "en_US.UTF-8"}}
	res := s.must(0, run{args: []string{"ls", "-l"}, cwd: filepath.Join(dir, "daily")})
	if n := len(strings.Split(strings.TrimSpace(res.stdout), "\n")); n < 14 {
		t.Errorf("example vault has %d notes", n)
	}
	s.must(0, run{args: []string{"path"}, cwd: filepath.Join(dir, "projects")})
	s.must(0, run{args: []string{"tasks", "--due"}, cwd: dir})
	s.must(0, run{args: []string{"find", "cutover"}, cwd: dir})
	s.must(0, run{args: []string{"backlinks", "Backpressure"}, cwd: dir})
	// The sample vault has exactly one deliberately broken link, so README
	// captures show no accidental ones.
	broken := 0
	for _, p := range strings.Split(strings.TrimSpace(s.must(0, run{args: []string{"ls", "-l"}, cwd: dir}).stdout), "\n") {
		for _, l := range strings.Split(s.must(0, run{args: []string{"links", "./" + p}, cwd: dir}).stdout, "\n") {
			if f := strings.Split(l, "\t"); len(f) == 3 && f[1] == "broken" {
				broken++
				if p != "Typography.md" || f[2] != "Missing note" {
					t.Errorf("unexpected broken link in %s: %s", p, l)
				}
			}
		}
	}
	if broken != 1 {
		t.Errorf("example vault has %d broken links, want exactly 1 ([[Missing note]])", broken)
	}
	res = s.must(0, run{args: []string{"tags"}, cwd: dir})
	if !strings.Contains(res.stdout, "فلك\t") {
		t.Errorf("Arabic tag missing:\n%s", res.stdout)
	}
	res = s.must(0, run{args: []string{"path", "Astrolabe"}, cwd: dir})
	if !strings.HasSuffix(strings.TrimSpace(res.stdout), "الأسطرلاب.md") {
		t.Errorf("alias of the Arabic note: %q", res.stdout)
	}
}

func TestFlagSet(t *testing.T) {
	fs := newFlagSet("x")
	b := fs.Bool("-b", "--bee")
	c := fs.Bool("-c", "")
	s := fs.String("-s", "--sea")
	n := fs.Int("-n", "--num", 3)
	pos, err := fs.Parse([]string{"one", "-bc", "-sVALUE", "--num=7", "two", "-", "--", "-b"})
	if err != nil {
		t.Fatal(err)
	}
	if !*b || !*c || *s != "VALUE" || *n != 7 || strings.Join(pos, ",") != "one,two,-,-b" {
		t.Errorf("parsed b=%v c=%v s=%q n=%d pos=%v", *b, *c, *s, *n, pos)
	}
	for _, bad := range [][]string{{"--num", "x"}, {"-s"}, {"--bee=1"}, {"-q"}, {"--sea"}} {
		fs := newFlagSet("x")
		fs.Bool("-b", "--bee")
		fs.String("-s", "--sea")
		fs.Int("-n", "--num", 0)
		if _, err := fs.Parse(bad); err == nil {
			t.Errorf("%v: no error", bad)
		}
	}
}

func TestHelpers(t *testing.T) {
	if d := levenshtein("kitten", "sitting"); d != 3 {
		t.Errorf("levenshtein = %d", d)
	}
	if v := nearestVerb("tsks"); v != "tasks" {
		t.Errorf("nearestVerb(tsks) = %q", v)
	}
	if v := nearestVerb("Lantern"); v != "" {
		t.Errorf("nearestVerb(Lantern) = %q", v)
	}
	if !segLess("a", "a/b") || !segLess("a/b", "a-c") {
		t.Error("segLess order")
	}
	now := fixedNow
	for d, want := range map[time.Duration]string{
		10 * time.Second: "just now", 5 * time.Minute: "5 min ago", 3 * time.Hour: "3 h ago",
		24 * time.Hour: "yesterday", 3 * 24 * time.Hour: "Thu", 20 * 24 * time.Hour: "14 Sep",
		400 * 24 * time.Hour: now.Add(-400 * 24 * time.Hour).Format("2 Jan 2006"),
	} {
		if got := ago(now.Add(-d), now); got != want {
			t.Errorf("ago(-%v) = %q, want %q", d, got, want)
		}
	}
	if commas(1234567) != "1,234,567" || commas(12) != "12" {
		t.Error("commas")
	}
	p := &painter{g: glyphsASCII(), width: 80}
	if got := p.highlight("\tsome text here", [][2]int{{6, 10}}, 80); got != "some text here" {
		t.Errorf("highlight off = %q", got)
	}
	long := strings.Repeat("word ", 40) + "needle" + strings.Repeat(" tail", 20)
	i := strings.Index(long, "needle")
	got := p.highlight(long, [][2]int{{i, i + 6}}, 40)
	if !strings.Contains(got, "needle") || !strings.HasPrefix(got, "...") || len([]rune(got)) > 40 {
		t.Errorf("windowed highlight = %q (%d)", got, len([]rune(got)))
	}
}

// TERM=dumb terminals print escape sequences as garbage: a terminal with
// TERM=dumb gets the human layout without any styling, and render is asked
// for plain text, unless --color forces a profile.
func TestDumbTerminalIsPlain(t *testing.T) {
	s := basicVault(t)
	dumb := map[string]string{"TERM": "dumb", "COLORTERM": "truecolor"}
	res := s.must(0, run{args: []string{"find", "cutover"}, stdoutTTY: true, env: dumb})
	if strings.Contains(res.stdout, "\x1b") {
		t.Errorf("escape sequences on TERM=dumb: %q", res.stdout)
	}
	if !strings.Contains(res.stdout, "matches in") {
		t.Errorf("human layout lost on TERM=dumb: %q", res.stdout)
	}
	res = s.must(0, run{args: []string{"doctor"}, stdoutTTY: true, env: dumb})
	if strings.Contains(res.stdout, "\x1b") {
		t.Errorf("doctor wrote escape sequences on TERM=dumb")
	}
	var got RenderRequest
	h := Hooks{Render: func(_ context.Context, r RenderRequest) error { got = r; return nil }}
	s.must(0, run{args: []string{"render", "Inbox"}, stdinTTY: true, stdoutTTY: true, env: dumb, hooks: h})
	if got.Styled {
		t.Error("render styled on TERM=dumb")
	}
	s.must(0, run{args: []string{"render", "--color=16", "Inbox"}, stdinTTY: true, stdoutTTY: true, env: dumb, hooks: h})
	if !got.Styled {
		t.Error("--color did not force styling on TERM=dumb")
	}
	s.must(0, run{args: []string{"render", "Inbox"}, stdinTTY: true, stdoutTTY: true, hooks: h})
	if !got.Styled {
		t.Error("render not styled on a normal terminal")
	}
}

// Human (terminal) output of find and tasks shows lines as the reader does,
// without Markdown syntax; piped output stays raw. Right-to-left lines are
// shaped and reordered when bidi is on.
func TestHumanOutputCleansMarkdown(t *testing.T) {
	s := newSandbox(t, map[string]string{
		"Plan.md": "# Plan\n\n- [ ] Ask **Priya** about the [[Retry budget|retry budget]] *today*\n\nThe *budget* note links [[Retry budget]].\n",
		"عربي.md": "# ملاحظة\n\nكلمة budget في سطر عربي\n",
	})
	tty := run{args: []string{"find", "budget"}, stdoutTTY: true, env: map[string]string{"NO_COLOR": "1"}}
	out := stripANSI(s.must(0, tty).stdout)
	if strings.Contains(out, "[[") || strings.Contains(out, "**") || strings.Contains(out, "*budget*") {
		t.Errorf("human find shows raw Markdown:\n%s", out)
	}
	if !strings.Contains(out, "Ask Priya about the retry budget today") {
		t.Errorf("human find lacks the cleaned line:\n%s", out)
	}
	if strings.Contains(out, "كلمة budget") || !strings.Contains(out, text.Visual("كلمة budget في سطر عربي", text.RTL)) {
		t.Errorf("RTL line not shaped and reordered for the terminal:\n%s", out)
	}
	if !strings.Contains(out, text.Visual("عربي", text.LTR)+".md") {
		t.Errorf("RTL file name not shaped, or not left to right:\n%s", out)
	}
	piped := s.must(0, run{args: []string{"find", "budget"}}).stdout
	if !strings.Contains(piped, "Plan.md:3:- [ ] Ask **Priya** about the [[Retry budget|retry budget]] *today*") ||
		!strings.Contains(piped, "كلمة budget في سطر عربي") {
		t.Errorf("piped find is not raw:\n%s", piped)
	}
	out = stripANSI(s.must(0, run{args: []string{"tasks"}, stdoutTTY: true, env: map[string]string{"NO_COLOR": "1"}}).stdout)
	if !strings.Contains(out, "Ask Priya about the retry budget today") || strings.Contains(out, "[[") {
		t.Errorf("human tasks shows raw Markdown:\n%s", out)
	}
	piped = s.must(0, run{args: []string{"tasks"}}).stdout
	if !strings.Contains(piped, "[ ] Ask **Priya** about the [[Retry budget|retry budget]] *today*") {
		t.Errorf("piped tasks is not raw:\n%s", piped)
	}
	// With bidi off the terminal does its own: logical order.
	out = stripANSI(s.must(0, run{args: []string{"--bidi", "off", "find", "budget"}, stdoutTTY: true, env: map[string]string{"NO_COLOR": "1"}}).stdout)
	if !strings.Contains(out, "كلمة budget في سطر عربي") {
		t.Errorf("bidi off reordered:\n%s", out)
	}
}

// Note paths contain spaces: help examples never pipe paths into a plain
// xargs (which splits on blanks), and use only portable flags.
func TestHelpExamplesSafeWithSpaces(t *testing.T) {
	for verb, d := range docs {
		for _, l := range strings.Split(d.body, "\n") {
			if strings.Contains(l, "xargs") && !strings.Contains(l, "xargs -0") {
				t.Errorf("help %s: unsafe xargs: %s", verb, strings.TrimSpace(l))
			}
			if strings.Contains(l, "xargs -d") {
				t.Errorf("help %s: GNU-only xargs -d: %s", verb, strings.TrimSpace(l))
			}
		}
	}
}

// On a terminal that reverses right-to-left runs itself (kitty), human
// output is emitted in visual order with each run's text pre-reversed, after
// styling; pipes stay raw.
func TestHumanOutputBidiRuns(t *testing.T) {
	s := newSandbox(t, map[string]string{
		"عربي.md": "# ملاحظة\n\nكلمة budget في سطر عربي\n",
	})
	line := "كلمة budget في سطر عربي"
	want := text.RunsString(text.Visual(line, text.RTL))
	for _, env := range []map[string]string{{"TERM": "xterm-kitty"}, {"KITTY_WINDOW_ID": "1", "NO_COLOR": "1"}} {
		out := stripANSI(s.must(0, run{args: []string{"find", "budget"}, stdoutTTY: true, env: env}).stdout)
		if !strings.Contains(out, want) {
			t.Errorf("env %v: kitty find lacks %q:\n%s", env, want, out)
		}
		if !strings.Contains(out, text.Shape("عربي")+".md") {
			t.Errorf("env %v: file name not emitted for run reversal:\n%s", env, out)
		}
	}
	out := stripANSI(s.must(0, run{args: []string{"--bidi", "runs", "doctor"}, stdoutTTY: true, env: map[string]string{"LANG": "en_US.UTF-8"}}).stdout)
	if !strings.Contains(out, "runs") || !strings.Contains(out, "set by --bidi") || !strings.Contains(out, text.Shape("الأسطرلاب")) {
		t.Errorf("doctor does not report bidi runs or its test line:\n%s", out)
	}
	piped := s.must(0, run{args: []string{"--bidi", "runs", "find", "budget"}}).stdout
	if !strings.Contains(piped, line) {
		t.Errorf("piped output is not raw:\n%s", piped)
	}
}
