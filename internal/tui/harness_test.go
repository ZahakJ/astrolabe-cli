package tui

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/astrolabe-cli/internal/cli"
	"github.com/ZahakJ/astrolabe-cli/internal/term"
	"github.com/ZahakJ/astrolabe-cli/internal/term/termtest"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
	"github.com/ZahakJ/astrolabe-cli/internal/vault"
)

// testNow is the fixed clock of the tests: the example vault's daily notes
// run up to the day before.
var testNow = time.Date(2026, 10, 4, 10, 0, 0, 0, time.Local)

// fakeHost records what the application asked of the terminal.
type fakeHost struct {
	copied    []string
	ran       []*exec.Cmd
	suspended int
	mouse     []bool
	run       func(cmd *exec.Cmd) error
}

func (h *fakeHost) Copy(s string) error { h.copied = append(h.copied, s); return nil }
func (h *fakeHost) RunExternal(cmd *exec.Cmd) error {
	h.ran = append(h.ran, cmd)
	if h.run != nil {
		return h.run(cmd)
	}
	return nil
}
func (h *fakeHost) Suspend() error   { h.suspended++; return nil }
func (h *fakeHost) SetMouse(on bool) { h.mouse = append(h.mouse, on) }

// harness is one application under test.
type harness struct {
	t     *testing.T
	a     *app
	vt    *termtest.VT
	scr   *term.Screen
	host  *fakeHost
	dir   string
	state string
}

type harnessOpt struct {
	w, h    int
	dir     string // vault directory (default: a copy of examples/vault)
	profile term.Profile
	glyphs  theme.Glyphs
	noScan  bool
	ext     bool
	home    string // home directory for the vault's display name
}

// copyVault copies examples/vault into a temporary directory.
func copyVault(t testing.TB) string {
	t.Helper()
	src := filepath.Join("..", "..", "examples", "vault")
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func newHarness(t *testing.T, o harnessOpt) *harness {
	t.Helper()
	if o.w == 0 {
		o.w, o.h = 100, 30
	}
	if o.dir == "" {
		o.dir = copyVault(t)
	}
	if o.profile == 0 {
		o.profile = term.ProfileTrueColor
	}
	if o.glyphs.Name == "" {
		o.glyphs = theme.UnicodeGlyphs
	}
	v, err := vault.Open(o.dir)
	if err != nil {
		t.Fatal(err)
	}
	if !o.noScan {
		if err := v.Scan(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	vt := termtest.New(o.w, o.h)
	scr := term.NewScreen(vt, o.w, o.h, term.Encoder{Profile: o.profile})
	host := &fakeHost{}
	state := t.TempDir()
	a := newApp(config{
		Vault:        v,
		Screen:       scr,
		Host:         host,
		Profile:      o.profile,
		Theme:        theme.IronGall,
		Glyphs:       o.glyphs,
		Ground:       true,
		Bidi:         true,
		Measure:      78,
		External:     o.ext,
		ConfigFile:   filepath.Join(state, "config"),
		RecentFile:   filepath.Join(state, "recent"),
		RecoveredDir: filepath.Join(state, "recovered"),
		Cwd:          state,
		Home:         o.home,
		Now:          func() time.Time { return testNow },
	})
	return &harness{t: t, a: a, vt: vt, scr: scr, host: host, dir: o.dir, state: state}
}

// open starts the application as `astrolabe NOTE` would.
func (h *harness) start(mode cli.TUIMode, path string, line int) *harness {
	h.a.start(cli.TUIRequest{Mode: mode, Path: path, Line: line, Vault: h.a.v})
	h.frame()
	return h
}

func (h *harness) open(path string) *harness { return h.start(cli.ModeOpen, path, 0) }

// keyEvent parses one key name: "enter", "ctrl+p", "shift+tab", "space", a
// single character.
func keyEvent(k string) term.KeyEvent {
	named := map[string]term.Key{
		"enter": term.KeyEnter, "esc": term.KeyEscape, "tab": term.KeyTab,
		"backspace": term.KeyBackspace, "up": term.KeyUp, "down": term.KeyDown,
		"left": term.KeyLeft, "right": term.KeyRight, "pgdown": term.KeyPageDown,
		"pgup": term.KeyPageUp, "home": term.KeyHome, "end": term.KeyEnd,
		"delete": term.KeyDelete,
	}
	if k == "space" {
		return term.KeyEvent{Key: term.KeyRune, Rune: ' '}
	}
	if k == "shift+tab" {
		return term.KeyEvent{Key: term.KeyTab, Mod: term.ModShift}
	}
	if strings.HasPrefix(k, "ctrl+") {
		r := []rune(strings.TrimPrefix(k, "ctrl+"))
		return term.KeyEvent{Key: term.KeyRune, Rune: r[0], Mod: term.ModCtrl}
	}
	if n, ok := named[k]; ok {
		return term.KeyEvent{Key: n}
	}
	r := []rune(k)
	return term.KeyEvent{Key: term.KeyRune, Rune: r[0]}
}

// keys sends space-separated key names, each as its own event, settling
// timers and background work after each (like a person typing slowly).
func (h *harness) keys(seq string) *harness {
	h.t.Helper()
	for _, k := range strings.Fields(seq) {
		h.a.handle(keyEvent(k))
		h.settle()
	}
	h.frame()
	return h
}

// fast sends keys without letting timers fire in between (a quick typist).
func (h *harness) fast(seq string) *harness {
	for _, k := range strings.Fields(seq) {
		h.a.handle(keyEvent(k))
	}
	h.frame()
	return h
}

// typ types literal text, one rune per key event.
func (h *harness) typ(s string) *harness {
	for _, r := range s {
		h.a.handle(term.KeyEvent{Key: term.KeyRune, Rune: r})
	}
	h.settle()
	h.frame()
	return h
}

// settle fires due-able timers early and waits for background jobs.
func (h *harness) settle() {
	for i := 0; i < 20; i++ {
		busy := false
		for _, o := range h.a.overlays {
			if tk, ok := o.(ticker); ok && !tk.deadline().IsZero() {
				tk.tick(h.a, tk.deadline())
				busy = true
			}
		}
		for h.a.jobs > 0 {
			select {
			case fn := <-h.a.async:
				fn()
			case <-time.After(5 * time.Second):
				h.t.Fatal("background job did not finish")
			}
			busy = true
		}
		if !busy {
			return
		}
	}
}

// wait lets the leader-palette timer fire.
func (h *harness) waitLeader() *harness {
	if !h.a.keys.leaderAt.IsZero() {
		h.a.tick(h.a.keys.leaderAt)
	}
	h.frame()
	return h
}

// frame draws and flushes one frame and returns the screen text.
func (h *harness) frame() string {
	h.a.draw()
	if err := h.scr.Flush(); err != nil {
		h.t.Fatal(err)
	}
	return h.vt.String()
}

func (h *harness) screen() string { return h.vt.String() }

func (h *harness) contains(want ...string) *harness {
	h.t.Helper()
	s := h.screen()
	for _, w := range want {
		if !strings.Contains(s, w) {
			h.t.Fatalf("screen lacks %q:\n%s", w, s)
		}
	}
	return h
}

func (h *harness) lacks(bad ...string) *harness {
	h.t.Helper()
	s := h.screen()
	for _, b := range bad {
		if strings.Contains(s, b) {
			h.t.Fatalf("screen unexpectedly has %q:\n%s", b, s)
		}
	}
	return h
}

// statusLine returns the bottom row.
func (h *harness) statusLine() string {
	_, rows := h.vt.Size()
	return h.vt.Row(rows - 1)
}

// cursorText returns the text of the reader's cursor line.
func (h *harness) cursorText() string {
	d := h.a.doc
	if d == nil || d.page == nil {
		return ""
	}
	return strings.TrimSpace(d.page.Lines[d.cur].Text())
}

func (h *harness) read(rel string) string {
	b, err := os.ReadFile(filepath.Join(h.dir, filepath.FromSlash(rel)))
	if err != nil {
		h.t.Fatal(err)
	}
	return string(b)
}

// gotoText moves the reader's cursor to the first line containing s.
func (h *harness) gotoText(s string) *harness {
	h.t.Helper()
	d := h.a.doc
	for i, l := range d.page.Lines {
		if strings.Contains(l.Text(), s) {
			d.cur = i
			h.a.clampView(d)
			h.frame()
			return h
		}
	}
	h.t.Fatalf("no line contains %q", s)
	return h
}
