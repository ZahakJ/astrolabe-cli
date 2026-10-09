package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZahakJ/astrolabe-cli/internal/vault"
)

// homeSandbox is a HOME with XDG dirs and no vault; mkVault adds plain
// folders of notes (no marker) under it.
func homeSandbox(t *testing.T) *sandbox {
	t.Helper()
	home := t.TempDir()
	return &sandbox{t: t, home: home, vault: filepath.Join(home, "notes"), env: map[string]string{
		"HOME":            home,
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"),
		"XDG_STATE_HOME":  filepath.Join(home, ".state"),
		"LANG":            "en_US.UTF-8",
		"TERM":            "xterm-256color",
	}}
}

func (s *sandbox) mkVault(rel string, files ...string) string {
	s.t.Helper()
	dir := filepath.Join(s.home, filepath.FromSlash(rel))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.t.Fatal(err)
	}
	for _, f := range files {
		p := filepath.Join(dir, filepath.FromSlash(f))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte("# "+strings.TrimSuffix(filepath.Base(f), ".md")+"\n"), 0o644); err != nil {
			s.t.Fatal(err)
		}
	}
	return dir
}

func (s *sandbox) configFile() string {
	return filepath.Join(s.home, ".config", "astrolabe-cli", "config")
}

// remembered is config "dir" as written, "" when absent.
func (s *sandbox) remembered() string {
	s.t.Helper()
	c, err := vault.LoadConfig(s.configFile(), nil)
	if err != nil {
		s.t.Fatal(err)
	}
	v, _ := c.FileValue("dir")
	return v
}

// elsewhere is an unrelated working directory (like /tmp).
func (s *sandbox) elsewhere() string {
	return s.mkVault("elsewhere/deep")
}

const usingLine = "Using ~/my-notes from now on; `astrolabe vault` to change.\n"

func TestRememberFlagThenPlainRun(t *testing.T) {
	s := homeSandbox(t)
	mine := s.mkVault("my-notes", "Alpha.md", "runbooks/Beta.md")
	res := s.must(0, run{args: []string{"-C", mine, "ls", "-l"}, cwd: s.elsewhere(), stderrTTY: true})
	if res.stderr != usingLine {
		t.Fatalf("first -C run: stderr %q, want %q", res.stderr, usingLine)
	}
	if got := s.remembered(); got != mine {
		t.Fatalf("config dir = %q, want %q", got, mine)
	}
	// The same -C again: nothing new to say.
	if res := s.must(0, run{args: []string{"-C", "~/my-notes", "ls"}, cwd: s.elsewhere(), stderrTTY: true}); res.stderr != "" {
		t.Fatalf("second -C run: stderr %q", res.stderr)
	}
	// A plain run from anywhere uses it, and says nothing.
	res = s.must(0, run{args: []string{"ls", "-l"}, cwd: s.elsewhere(), stderrTTY: true})
	if res.stderr != "" || !strings.Contains(res.stdout, filepath.Join(mine, "Alpha.md")) {
		t.Fatalf("plain run: %q %q", res.stdout, res.stderr)
	}
	if out := s.must(0, run{args: []string{"path"}, cwd: s.elsewhere()}).stdout; out != mine+"\n" {
		t.Fatalf("path = %q", out)
	}
	// A different -C replaces it.
	other := s.mkVault("other", "Gamma.md")
	res = s.must(0, run{args: []string{"-C", other, "path"}, cwd: s.elsewhere(), stderrTTY: true})
	if res.stderr != "Using ~/other from now on; `astrolabe vault` to change.\n" || s.remembered() != other {
		t.Fatalf("replacing: %q, config %q", res.stderr, s.remembered())
	}
	// In a pipe (stderr not a terminal) it is remembered quietly.
	s.must(0, run{args: []string{"-C", mine, "ls"}, cwd: s.elsewhere()})
	if s.remembered() != mine {
		t.Fatalf("quiet remember: %q", s.remembered())
	}
}

func TestRememberRules(t *testing.T) {
	t.Run("marker above cwd beats remembered and does not replace it", func(t *testing.T) {
		s := homeSandbox(t)
		mine := s.mkVault("my-notes", "Alpha.md")
		project := s.mkVault("code/project", "README.md")
		os.MkdirAll(filepath.Join(project, ".obsidian"), 0o755)
		os.MkdirAll(filepath.Join(project, "src"), 0o755)
		vault.SetConfigValue(s.configFile(), "dir", mine)
		res := s.must(0, run{args: []string{"path"}, cwd: filepath.Join(project, "src"), stderrTTY: true})
		if res.stdout != project+"\n" || res.stderr != "" {
			t.Fatalf("path in project: %q %q", res.stdout, res.stderr)
		}
		if s.remembered() != mine {
			t.Fatalf("project stole the remembered vault: %q", s.remembered())
		}
		doc := s.must(0, run{args: []string{"doctor"}, cwd: filepath.Join(project, "src")}).stdout
		if !strings.Contains(doc, "you are inside it") || !strings.Contains(doc, "~/my-notes") || !strings.Contains(doc, "not used here") {
			t.Fatalf("doctor does not explain the marker win:\n%s", doc)
		}
	})
	t.Run("marker remembered when nothing is", func(t *testing.T) {
		s := homeSandbox(t)
		project := s.mkVault("my-notes", "README.md")
		os.MkdirAll(filepath.Join(project, ".astrolabe"), 0o755)
		res := s.must(0, run{args: []string{"ls"}, cwd: project, stderrTTY: true})
		if res.stderr != usingLine || s.remembered() != project {
			t.Fatalf("marker not remembered: %q %q", res.stderr, s.remembered())
		}
	})
	t.Run("remembered beats ~/notes and cwd with md", func(t *testing.T) {
		s := homeSandbox(t)
		mine := s.mkVault("my-notes", "Alpha.md")
		s.mkVault("notes", "Home.md")
		cwd := s.mkVault("loose", "stray.md")
		vault.SetConfigValue(s.configFile(), "dir", mine)
		if out := s.must(0, run{args: []string{"path"}, cwd: cwd}).stdout; out != mine+"\n" {
			t.Fatalf("path = %q", out)
		}
	})
	t.Run("env wins and never writes config", func(t *testing.T) {
		s := homeSandbox(t)
		mine := s.mkVault("my-notes", "Alpha.md")
		res := s.must(0, run{args: []string{"ls", "-l"}, cwd: s.elsewhere(), env: map[string]string{"ASTROLABE_DIR": mine}, stderrTTY: true})
		if !strings.Contains(res.stdout, "Alpha.md") || res.stderr != "" {
			t.Fatalf("env run: %q %q", res.stdout, res.stderr)
		}
		if _, err := os.Stat(s.configFile()); err == nil {
			t.Fatalf("ASTROLABE_DIR wrote the config")
		}
	})
	t.Run("stale remembered skipped with a warning", func(t *testing.T) {
		s := homeSandbox(t)
		s.mkVault("notes", "Home.md")
		vault.SetConfigValue(s.configFile(), "dir", filepath.Join(s.home, "gone"))
		res := s.must(0, run{args: []string{"path"}, cwd: s.elsewhere()})
		if res.stdout != filepath.Join(s.home, "notes")+"\n" {
			t.Fatalf("path = %q", res.stdout)
		}
		if n := strings.Count(res.stderr, "\n"); n != 1 || !strings.Contains(res.stderr, "~/gone no longer exists") {
			t.Fatalf("stale warning: %q", res.stderr)
		}
		doc := s.must(0, run{args: []string{"doctor"}, cwd: s.elsewhere()})
		if !strings.Contains(doc.stdout, "~/gone no longer exists") || doc.stderr != "" {
			t.Fatalf("doctor on stale: %q\n%s", doc.stderr, doc.stdout)
		}
	})
	t.Run("never remembered: missing -C, single file, doctor", func(t *testing.T) {
		s := homeSandbox(t)
		s.must(1, run{args: []string{"-C", filepath.Join(s.home, "missing"), "ls"}, cwd: s.elsewhere()})
		loose := s.mkVault("loose", "stray.md")
		f := &fakeTUI{}
		s.must(0, run{args: []string{filepath.Join(loose, "stray.md")}, cwd: s.elsewhere(), hooks: f.hooks()})
		mine := s.mkVault("my-notes", "Alpha.md")
		s.must(0, run{args: []string{"-C", mine, "doctor"}, cwd: s.elsewhere()})
		if _, err := os.Stat(s.configFile()); err == nil {
			t.Fatalf("config written: %q", s.remembered())
		}
	})
}

func TestTutorialGate(t *testing.T) {
	t.Run("opens once in a fresh ~/notes", func(t *testing.T) {
		s := homeSandbox(t)
		f := &fakeTUI{}
		s.must(0, run{cwd: s.home, hooks: f.hooks()})
		if f.reqs[0].Path != vault.WelcomeName {
			t.Fatalf("welcome not opened: %q", f.reqs[0].Path)
		}
		// The TUI records the note it showed; the untouched tutorial is
		// still not resumed by the next plain run.
		welcome := filepath.Join(s.home, "notes", vault.WelcomeName)
		rf := vault.LoadRecent(filepath.Join(s.home, ".state", "astrolabe-cli", "recent"))
		rf.Touch(welcome, 1)
		if err := rf.Save(); err != nil {
			t.Fatal(err)
		}
		s.must(0, run{cwd: s.home, hooks: f.hooks()})
		if f.reqs[1].Path == vault.WelcomeName {
			t.Fatal("welcome opened twice")
		}
		// Once edited, it is an ordinary note and is resumed like one.
		os.WriteFile(welcome, []byte("# Welcome, mine now\n"), 0o644)
		s.must(0, run{cwd: s.home, hooks: f.hooks()})
		if f.reqs[2].Path != vault.WelcomeName {
			t.Fatalf("edited welcome not resumed: %q", f.reqs[2].Path)
		}
		f.reqs = f.reqs[:2]
		// Even with ~/notes removed it does not come back by itself.
		os.RemoveAll(filepath.Join(s.home, "notes"))
		s.must(0, run{cwd: s.home, hooks: f.hooks()})
		if f.reqs[2].Path == vault.WelcomeName || fileExists(filepath.Join(s.home, "notes", vault.WelcomeName)) {
			t.Fatal("welcome came back after ~/notes was removed")
		}
	})
	t.Run("never after a -C run", func(t *testing.T) {
		s := homeSandbox(t)
		mine := s.mkVault("my-notes", "Alpha.md")
		s.must(0, run{args: []string{"-C", mine, "ls"}, cwd: s.elsewhere()})
		f := &fakeTUI{}
		s.must(0, run{cwd: s.elsewhere(), hooks: f.hooks()})
		r := f.reqs[0]
		if r.Path == vault.WelcomeName || r.Root.Dir != mine || r.Root.Source != vault.RootFromConfig {
			t.Fatalf("plain run after -C: path %q root %+v", r.Path, r.Root)
		}
		if fileExists(filepath.Join(s.home, "notes", vault.WelcomeName)) || fileExists(filepath.Join(mine, vault.WelcomeName)) {
			t.Fatal("Welcome.md written")
		}
	})
	t.Run("never when a vault is remembered", func(t *testing.T) {
		s := homeSandbox(t)
		mine := s.mkVault("my-notes")
		vault.SetConfigValue(s.configFile(), "dir", mine)
		f := &fakeTUI{}
		s.must(0, run{cwd: s.elsewhere(), hooks: f.hooks()})
		if f.reqs[0].Path != "" || f.reqs[0].Root.Dir != mine {
			t.Fatalf("remembered empty vault: %+v", f.reqs[0])
		}
		// A remembered vault that vanished: still no tutorial.
		os.RemoveAll(mine)
		s.must(0, run{cwd: s.elsewhere(), hooks: f.hooks()})
		if f.reqs[1].Path == vault.WelcomeName {
			t.Fatal("welcome opened with a (stale) remembered vault")
		}
	})
	t.Run("learn opens it on purpose", func(t *testing.T) {
		s := homeSandbox(t)
		mine := s.mkVault("my-notes", "Alpha.md")
		vault.SetConfigValue(s.configFile(), "dir", mine)
		f := &fakeTUI{}
		s.must(0, run{args: []string{"learn"}, cwd: s.elsewhere(), hooks: f.hooks()})
		if f.reqs[0].Path != vault.WelcomeName || f.reqs[0].Mode != ModeOpen {
			t.Fatalf("learn: %+v", f.reqs[0])
		}
		if b, _ := os.ReadFile(filepath.Join(mine, vault.WelcomeName)); string(b) != vault.WelcomeNote {
			t.Fatal("learn did not write Welcome.md")
		}
		os.WriteFile(filepath.Join(mine, vault.WelcomeName), []byte("# My welcome\n"), 0o644)
		s.must(0, run{args: []string{"learn"}, cwd: s.elsewhere(), hooks: f.hooks()})
		if b, _ := os.ReadFile(filepath.Join(mine, vault.WelcomeName)); string(b) != "# My welcome\n" || f.reqs[1].Path != vault.WelcomeName {
			t.Fatal("learn overwrote an existing Welcome.md")
		}
		if out := s.must(0, run{args: []string{"learn", "--print"}, cwd: s.elsewhere()}).stdout; out != vault.WelcomeNote {
			t.Fatalf("learn --print: %q", out)
		}
		s.must(2, run{args: []string{"learn", "x"}, cwd: s.elsewhere()})
	})
}

func TestVaultVerb(t *testing.T) {
	s := homeSandbox(t)
	mine := s.mkVault("my-notes", "Alpha.md")
	os.MkdirAll(filepath.Dir(s.configFile()), 0o755)
	os.WriteFile(s.configFile(), []byte("# mine\ntheme = mocha\n"), 0o644)

	// Nothing remembered: the new default.
	if out := s.must(0, run{args: []string{"vault"}, cwd: s.elsewhere()}).stdout; out != filepath.Join(s.home, "notes")+"\n" {
		t.Fatalf("vault (nothing) = %q", out)
	}
	// Set.
	res := s.must(0, run{args: []string{"vault", "~/my-notes"}, cwd: s.elsewhere()})
	if res.stderr != usingLine || s.remembered() != mine {
		t.Fatalf("vault DIR: %q, config %q", res.stderr, s.remembered())
	}
	if b, _ := os.ReadFile(s.configFile()); string(b) != "# mine\ntheme = mocha\ndir = "+mine+"\n" {
		t.Fatalf("config after set:\n%s", b)
	}
	// Show: the path alone in a pipe, the story on a terminal.
	if out := s.must(0, run{args: []string{"vault"}, cwd: s.elsewhere()}).stdout; out != mine+"\n" {
		t.Fatalf("vault (piped) = %q", out)
	}
	human := stripANSI(s.must(0, run{args: []string{"vault"}, cwd: s.elsewhere(), stdoutTTY: true}).stdout)
	for _, want := range []string{"~/my-notes", "chosen by", "remembered vault", "remembered"} {
		if !strings.Contains(human, want) {
			t.Fatalf("vault (tty) lacks %q:\n%s", want, human)
		}
	}
	// Invalid.
	if res := s.must(1, run{args: []string{"vault", "~/nope"}, cwd: s.elsewhere()}); !strings.Contains(res.stderr, "no such directory") {
		t.Fatalf("missing DIR: %q", res.stderr)
	}
	s.must(1, run{args: []string{"vault", filepath.Join(mine, "Alpha.md")}, cwd: s.elsewhere()})
	s.must(2, run{args: []string{"vault", "a", "b"}, cwd: s.elsewhere()})
	s.must(2, run{args: []string{"vault", "--forget", mine}, cwd: s.elsewhere()})
	s.must(2, run{args: []string{"vault", "--bogus"}, cwd: s.elsewhere()})
	if s.remembered() != mine {
		t.Fatal("a failed vault command changed the config")
	}
	// Forget.
	res = s.must(0, run{args: []string{"vault", "--forget"}, cwd: s.elsewhere()})
	if !strings.Contains(res.stderr, "Forgot ~/my-notes") || s.remembered() != "" {
		t.Fatalf("forget: %q, config %q", res.stderr, s.remembered())
	}
	if b, _ := os.ReadFile(s.configFile()); string(b) != "# mine\ntheme = mocha\n" {
		t.Fatalf("config after forget:\n%s", b)
	}
	if res := s.must(0, run{args: []string{"vault", "--forget"}, cwd: s.elsewhere()}); !strings.Contains(res.stderr, "No vault is remembered") {
		t.Fatalf("forget twice: %q", res.stderr)
	}
	// `astrolabe vault` never remembers by itself.
	s.must(0, run{args: []string{"-C", mine, "vault"}, cwd: s.elsewhere()})
	if s.remembered() != "" {
		t.Fatal("`astrolabe -C DIR vault` remembered")
	}
}
