package vault

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestResolveRoot(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	os.MkdirAll(home, 0o755)
	vault := filepath.Join(base, "work", "vault")
	os.MkdirAll(filepath.Join(vault, ".obsidian"), 0o755)
	deep := filepath.Join(vault, "a", "b")
	os.MkdirAll(deep, 0o755)
	plain := filepath.Join(base, "plain")
	os.MkdirAll(plain, 0o755)
	os.WriteFile(filepath.Join(plain, "x.md"), nil, 0o644)
	empty := filepath.Join(base, "empty")
	os.MkdirAll(empty, 0o755)

	check := func(name string, in RootInputs, dir string, src RootSource, exists bool) {
		t.Helper()
		r, err := ResolveRoot(in)
		if err != nil || r.Dir != dir || r.Source != src || r.Exists != exists {
			t.Errorf("%s: got %+v %v, want %s %v %v", name, r, err, dir, src, exists)
		}
	}
	check("flag", RootInputs{Flag: "rel", Env: "/e", Cwd: plain, Home: home}, filepath.Join(plain, "rel"), RootFromFlag, false)
	check("env", RootInputs{Env: "~/n", Config: "/c", Cwd: plain, Home: home}, filepath.Join(home, "n"), RootFromEnv, false)
	check("config", RootInputs{Config: vault, Cwd: plain, Home: home}, vault, RootFromConfig, true)
	check("marker", RootInputs{Cwd: deep, Home: home}, vault, RootFromMarker, true)
	check("cwd md", RootInputs{Cwd: plain, Home: home}, plain, RootFromCwd, true)
	check("default", RootInputs{Cwd: empty, Home: home}, filepath.Join(home, "notes"), RootFromDefault, false)
	os.MkdirAll(filepath.Join(home, "notes"), 0o755)
	check("home notes beats cwd md", RootInputs{Cwd: plain, Home: home}, filepath.Join(home, "notes"), RootFromHome, true)
	check("marker beats home notes", RootInputs{Cwd: deep, Home: home}, vault, RootFromMarker, true)
	if _, err := ResolveRoot(RootInputs{Flag: filepath.Join(plain, "x.md"), Home: home}); err == nil {
		t.Error("file accepted as root")
	}

	root, rel, err := ResolveFileRoot("a/b/n.md", vault)
	if err != nil || root != vault || rel != "a/b/n.md" {
		t.Errorf("file in vault: %q %q %v", root, rel, err)
	}
	root, rel, _ = ResolveFileRoot(filepath.Join(plain, "x.md"), "/")
	if root != plain || rel != "x.md" {
		t.Errorf("loose file: %q %q", root, rel)
	}
}

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestRecent(t *testing.T) {
	dir := t.TempDir()
	getenv := env(map[string]string{"XDG_STATE_HOME": dir, "HOME": "/nonexistent"})
	file := RecentFile(getenv)
	if file != filepath.Join(dir, "astrolabe-cli", "recent") {
		t.Fatalf("file %q", file)
	}
	if RecentFile(env(map[string]string{"HOME": "/h"})) != "/h/.local/state/astrolabe-cli/recent" {
		t.Error("default state path")
	}
	r := LoadRecent(file) // missing: empty
	if len(r.Entries()) != 0 {
		t.Fatal("not empty")
	}
	r.Touch("/v/a.md", 10)
	r.Touch("/v/b.md", 3)
	r.Touch("/v/a.md", 12)
	r.Touch("bad\npath", 1)
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	r = LoadRecent(file)
	if want := []RecentEntry{{"/v/a.md", 12}, {"/v/b.md", 3}}; !reflect.DeepEqual(r.Entries(), want) {
		t.Errorf("entries %v", r.Entries())
	}
	if l, ok := r.Line("/v/b.md"); !ok || l != 3 {
		t.Errorf("line %d %v", l, ok)
	}
	r.Rename("/v/b.md", "/v/c.md")
	r.Remove("/v/a.md")
	if want := []RecentEntry{{"/v/c.md", 3}}; !reflect.DeepEqual(r.Entries(), want) {
		t.Errorf("after rename/remove %v", r.Entries())
	}
	// Corruption tolerated.
	os.WriteFile(file, []byte("garbage\n\x00\x01\nx\t/abs.md\n5\trelative.md\n7\t/ok.md\n7\t/ok.md\n-1\t/neg.md\n"), 0o600)
	r = LoadRecent(file)
	if want := []RecentEntry{{"/ok.md", 7}}; !reflect.DeepEqual(r.Entries(), want) {
		t.Errorf("corrupt %v", r.Entries())
	}
	// Cap.
	for i := 0; i < MaxRecent+50; i++ {
		r.Touch("/n/"+strconv.Itoa(i)+".md", i)
	}
	r.Save()
	r = LoadRecent(file)
	if e := r.Entries(); len(e) != MaxRecent || e[0].Path != "/n/249.md" {
		t.Errorf("cap: %d %v", len(e), e[0])
	}
}

func TestConfig(t *testing.T) {
	dir := t.TempDir()
	getenv := env(map[string]string{"XDG_CONFIG_HOME": dir, "ASTROLABE_MEASURE": "64"})
	file := ConfigFile(getenv)
	c, err := LoadConfig(file, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if c.Int("measure", 78) != 64 || c.String("theme", "iron-gall") != "iron-gall" {
		t.Error("missing file / env")
	}
	os.MkdirAll(filepath.Dir(file), 0o755)
	os.WriteFile(file, []byte("# my config\r\ntheme = parchment\r\nmeasure=70\nground = off # trailing\nbidi = \"auto\"\nfancy = yes\nnot a pair\n"), 0o644)
	c, _ = LoadConfig(file, getenv)
	if c.String("theme", "") != "parchment" || c.Int("measure", 0) != 64 || c.Bool("ground", true) || c.String("bidi", "") != "auto" {
		t.Errorf("values: %q %d %v %q", c.String("theme", ""), c.Int("measure", 0), c.Bool("ground", true), c.String("bidi", ""))
	}
	if !reflect.DeepEqual(c.Unknown, []string{"fancy"}) || len(c.Bad) != 1 {
		t.Errorf("unknown %v bad %v", c.Unknown, c.Bad)
	}
	if err := SetConfigValue(file, "theme", "mocha"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(file)
	want := "# my config\r\ntheme = mocha\r\nmeasure=70\nground = off # trailing\nbidi = \"auto\"\nfancy = yes\nnot a pair\n"
	if string(b) != want {
		t.Errorf("persist:\n%q\nwant\n%q", b, want)
	}
	nf := filepath.Join(dir, "new", "astrolabe-cli", "config")
	if err := SetConfigValue(nf, "theme", "graphite"); err != nil {
		t.Fatal(err)
	}
	SetConfigValue(nf, "measure", "72")
	if b, _ := os.ReadFile(nf); string(b) != "theme = graphite\nmeasure = 72\n" {
		t.Errorf("new file %q", b)
	}
}

func TestWelcome(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "notes")
	ok, err := EnsureWelcome(dir)
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	if ok, _ := EnsureWelcome(dir); ok {
		t.Error("written twice")
	}
	n := parseNote(WelcomeName, []byte(WelcomeNote), time.Time{}, int64(len(WelcomeNote)))
	if n.Title != "Welcome to astrolabe" || len(n.Tasks) != 3 || len(n.Headings) < 4 {
		t.Errorf("welcome parse: %q %d tasks %d headings", n.Title, len(n.Tasks), len(n.Headings))
	}
	for _, s := range []string{"> [!tip]", "| Keys |", "```sh", "[[Ideas]]", "Space f", "Esc Esc"} {
		if !strings.Contains(WelcomeNote, s) {
			t.Errorf("welcome lacks %q", s)
		}
	}
	other := t.TempDir()
	os.WriteFile(filepath.Join(other, "mine.md"), []byte("x"), 0o644)
	if ok, _ := EnsureWelcome(other); ok {
		t.Error("welcome written into a non-empty vault")
	}
}

func TestRecoveredBuffers(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "astrolabe", "recovered")
	if got := UnseenRecovered(dir); got != nil {
		t.Fatalf("empty dir: %v", got)
	}
	now := time.Date(2026, 10, 4, 15, 30, 12, 0, time.UTC)
	p, err := WriteRecovered(dir, "projects/Lantern migration.md", []byte("mine\n"), now)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "20261004-153012-Lantern migration.md" {
		t.Fatalf("name %q", p)
	}
	if b, _ := os.ReadFile(p); string(b) != "mine\n" {
		t.Fatalf("content %q", b)
	}
	if info, _ := os.Stat(p); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode())
	}
	// Same second, same note: a second file, never an overwrite.
	p2, err := WriteRecovered(dir, "Lantern migration.md", []byte("again\n"), now)
	if err != nil || p2 == p {
		t.Fatalf("second write %q %v", p2, err)
	}
	if got := UnseenRecovered(dir); len(got) != 2 {
		t.Fatalf("unseen %v", got)
	}
	if got := UnseenRecovered(dir); len(got) != 0 {
		t.Fatalf("announced twice: %v", got)
	}
	if got := RecoveredFiles(dir); len(got) != 2 {
		t.Fatalf("files %v", got)
	}
}

// The tool's former name: its config and state directories are read when
// the new ones are absent, its marker and ignore file still count, and
// nothing is moved.
func TestFormerNameFallback(t *testing.T) {
	home := t.TempDir()
	getenv := env(map[string]string{"HOME": home})
	if got := ConfigDir(getenv); got != filepath.Join(home, ".config", "astrolabe-cli") {
		t.Errorf("fresh ConfigDir = %q", got)
	}
	old := filepath.Join(home, ".config", "folio")
	oldState := filepath.Join(home, ".local", "state", "folio")
	for _, d := range []string{old, oldState} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got := ConfigDir(getenv); got != old {
		t.Errorf("ConfigDir with only the old dir = %q", got)
	}
	if got := StateDir(getenv); got != oldState {
		t.Errorf("StateDir with only the old dir = %q", got)
	}
	if err := os.MkdirAll(filepath.Join(home, ".config", "astrolabe-cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ConfigDir(getenv); got != filepath.Join(home, ".config", "astrolabe-cli") {
		t.Errorf("ConfigDir with both = %q", got)
	}
	if _, err := os.Stat(old); err != nil {
		t.Error("old dir was touched")
	}
	// .folio/ still marks a vault; .folioignore is read when alone.
	v := filepath.Join(home, "v")
	os.MkdirAll(filepath.Join(v, ".folio"), 0o755)
	os.MkdirAll(filepath.Join(v, "sub"), 0o755)
	if dir, ok := findMarker(filepath.Join(v, "sub")); !ok || dir != v {
		t.Errorf("findMarker = %q %v", dir, ok)
	}
	os.WriteFile(filepath.Join(v, ".folioignore"), []byte("drafts/\n"), 0o644)
	if p := loadIgnore(v); len(p) != 1 || p[0] != "drafts/" {
		t.Errorf("loadIgnore = %v", p)
	}
}
