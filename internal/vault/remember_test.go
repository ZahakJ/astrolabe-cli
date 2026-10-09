package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveRootOrder is the resolution order of DESIGN.md §3: flag, env,
// the vault you stand in, the remembered vault, ~/notes, cwd with .md,
// else a new ~/notes.
func TestResolveRootOrder(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	mk := func(p ...string) string {
		d := filepath.Join(p...)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		return d
	}
	mk(home)
	project := mk(base, "work", "project")
	mk(project, ".astrolabe")
	inProject := mk(project, "sub")
	remembered := mk(home, "my-notes")
	plain := mk(base, "plain")
	os.WriteFile(filepath.Join(plain, "x.md"), nil, 0o644)
	empty := mk(base, "empty")
	gone := filepath.Join(home, "gone")
	file := filepath.Join(plain, "x.md")
	notes := filepath.Join(home, "notes")

	type want struct {
		dir    string
		src    RootSource
		stale  bool
		exists bool
	}
	cases := []struct {
		name  string
		in    RootInputs
		notes bool // ~/notes exists
		want  want
	}{
		{"flag beats all", RootInputs{Flag: plain, Env: empty, Config: remembered, Cwd: inProject}, false, want{plain, RootFromFlag, false, true}},
		{"env beats marker and remembered", RootInputs{Env: empty, Config: remembered, Cwd: inProject}, false, want{empty, RootFromEnv, false, true}},
		{"marker above cwd beats remembered", RootInputs{Config: remembered, Cwd: inProject}, true, want{project, RootFromMarker, false, true}},
		{"remembered from anywhere", RootInputs{Config: remembered, Cwd: empty}, false, want{remembered, RootFromConfig, false, true}},
		{"remembered beats ~/notes", RootInputs{Config: remembered, Cwd: empty}, true, want{remembered, RootFromConfig, false, true}},
		{"remembered beats cwd with md", RootInputs{Config: remembered, Cwd: plain}, false, want{remembered, RootFromConfig, false, true}},
		{"remembered beats the new default", RootInputs{Config: "~/my-notes", Cwd: empty}, false, want{remembered, RootFromConfig, false, true}},
		{"stale remembered skipped to ~/notes", RootInputs{Config: gone, Cwd: plain}, true, want{notes, RootFromHome, true, true}},
		{"stale remembered skipped to cwd", RootInputs{Config: gone, Cwd: plain}, false, want{plain, RootFromCwd, true, true}},
		{"remembered file skipped", RootInputs{Config: file, Cwd: empty}, false, want{notes, RootFromDefault, true, false}},
		{"~/notes beats cwd md", RootInputs{Cwd: plain}, true, want{notes, RootFromHome, false, true}},
		{"cwd md", RootInputs{Cwd: plain}, false, want{plain, RootFromCwd, false, true}},
		{"new default", RootInputs{Cwd: empty}, false, want{notes, RootFromDefault, false, false}},
	}
	for _, c := range cases {
		os.RemoveAll(notes)
		if c.notes {
			mk(notes)
		}
		c.in.Home = home
		r, err := ResolveRoot(c.in)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if r.Dir != c.want.dir || r.Source != c.want.src || (r.Stale != "") != c.want.stale || r.Exists != c.want.exists {
			t.Errorf("%s: got %s %v exists=%v stale=%q, want %s %v exists=%v stale=%v",
				c.name, r.Dir, r.Source, r.Exists, r.Stale, c.want.dir, c.want.src, c.want.exists, c.want.stale)
		}
		if c.in.Config != "" && r.Remembered == "" {
			t.Errorf("%s: Remembered not reported", c.name)
		}
	}
	r, _ := ResolveRoot(RootInputs{Config: gone, Cwd: empty, Home: home})
	if !strings.Contains(r.Stale, "no longer exists") || r.Remembered != gone {
		t.Errorf("stale reason %q, remembered %q", r.Stale, r.Remembered)
	}
}

func TestDisplayPathAndShortName(t *testing.T) {
	home := "/home/u"
	for _, c := range []struct{ dir, disp, short string }{
		{"/home/u/notes", "~/notes", "~/notes"},
		{"/home/u", "~", "~"},
		{"/home/u/Documents/a-long-notes-folder", "~/Documents/a-long-notes-folder", "a-long-notes-folder"},
		{"/srv/notes", "/srv/notes", "/srv/notes"},
		{"/home/user2/n", "/home/user2/n", "/home/user2/n"},
	} {
		if d := DisplayPath(c.dir, home); d != c.disp {
			t.Errorf("DisplayPath(%s) = %q, want %q", c.dir, d, c.disp)
		}
		if s := ShortName(c.dir, home); s != c.short {
			t.Errorf("ShortName(%s) = %q, want %q", c.dir, s, c.short)
		}
	}
}

// TestConfigRememberRoundTrip: writing and clearing "dir" keeps every other
// line, comment and blank line as it was.
func TestConfigRememberRoundTrip(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config")
	orig := "# my settings\ntheme = mocha   # dark\n\n# where\nmeasure = 72\n"
	os.WriteFile(file, []byte(orig), 0o644)
	if err := SetConfigValue(file, "dir", "/home/u/my notes"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(file)
	if string(b) != orig+"dir = /home/u/my notes\n" {
		t.Fatalf("after set:\n%s", b)
	}
	if err := SetConfigValue(file, "dir", "/home/u/other"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(file)
	if string(b) != orig+"dir = /home/u/other\n" {
		t.Fatalf("after replace:\n%s", b)
	}
	c, _ := LoadConfig(file, nil)
	if v, _ := c.FileValue("dir"); v != "/home/u/other" {
		t.Fatalf("FileValue = %q", v)
	}
	if v, _ := c.Get("theme"); v != "mocha" {
		t.Fatalf("theme = %q", v)
	}
	// A value that would read as a comment is quoted.
	SetConfigValue(file, "dir", "/srv/a #b")
	c, _ = LoadConfig(file, nil)
	if v, _ := c.FileValue("dir"); v != "/srv/a #b" {
		t.Fatalf("quoted value read back as %q", v)
	}
	// The environment override does not leak into FileValue.
	c, _ = LoadConfig(file, func(k string) string {
		if k == "ASTROLABE_DIR" {
			return "/env"
		}
		return ""
	})
	if v, _ := c.Get("dir"); v != "/env" {
		t.Fatalf("Get = %q", v)
	}
	if v, _ := c.FileValue("dir"); v != "/srv/a #b" {
		t.Fatalf("FileValue with env = %q", v)
	}
	removed, err := UnsetConfigValue(file, "dir")
	if err != nil || !removed {
		t.Fatal(removed, err)
	}
	b, _ = os.ReadFile(file)
	if string(b) != orig {
		t.Fatalf("after unset:\n%q\nwant\n%q", b, orig)
	}
	if removed, err := UnsetConfigValue(file, "dir"); err != nil || removed {
		t.Fatal("second unset", removed, err)
	}
	if removed, err := UnsetConfigValue(filepath.Join(dir, "none"), "dir"); err != nil || removed {
		t.Fatal("missing file", removed, err)
	}
	// Created with its directory when absent.
	nf := filepath.Join(dir, "new", "astrolabe-cli", "config")
	if err := ConfigWritable(nf); err != nil {
		t.Fatal(err)
	}
	if err := SetConfigValue(nf, "dir", "/x"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(nf); string(b) != "dir = /x\n" {
		t.Fatalf("new file: %q", b)
	}
	if entries, _ := os.ReadDir(filepath.Dir(nf)); len(entries) != 1 {
		t.Fatalf("probe or temp files left: %v", entries)
	}
}

func TestUsedAndWriteWelcome(t *testing.T) {
	state := t.TempDir()
	getenv := func(k string) string {
		if k == "XDG_STATE_HOME" {
			return state
		}
		return ""
	}
	if Used(getenv) {
		t.Fatal("used before marking")
	}
	if err := MarkUsed(getenv); err != nil || !Used(getenv) {
		t.Fatal("MarkUsed", err)
	}
	dir := filepath.Join(t.TempDir(), "v")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "other.md"), []byte("# x\n"), 0o644)
	if ok, err := WriteWelcome(dir); !ok || err != nil {
		t.Fatal("WriteWelcome into a non-empty vault", ok, err)
	}
	os.WriteFile(filepath.Join(dir, WelcomeName), []byte("mine\n"), 0o644)
	if ok, _ := WriteWelcome(dir); ok {
		t.Fatal("overwrote an existing Welcome.md")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, WelcomeName)); string(b) != "mine\n" {
		t.Fatal("Welcome.md changed")
	}
}
