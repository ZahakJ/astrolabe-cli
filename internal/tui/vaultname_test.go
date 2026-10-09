package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZahakJ/astrolabe-cli/internal/cli"
)

// vaultInHome moves a copy of the example vault to HOME/name.
func vaultInHome(t *testing.T, name string) (home, dir string) {
	t.Helper()
	home = t.TempDir()
	dir = filepath.Join(home, name)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(copyVault(t), dir); err != nil {
		t.Fatal(err)
	}
	return home, dir
}

// TestStatusBarNamesVault: the status bar says which vault you are in
// (DESIGN.md §4.2): "✦ ~/my-notes › runbooks › Lantern cutover".
func TestStatusBarNamesVault(t *testing.T) {
	home, dir := vaultInHome(t, "my-notes")
	h := newHarness(t, harnessOpt{dir: dir, home: home}).open("runbooks/Lantern cutover.md")
	st := h.statusLine()
	if !strings.Contains(st, "✦ ~/my-notes › runbooks › Lantern cutover") {
		t.Fatalf("status bar does not name the vault: %q", st)
	}
	// The home screen names it by its home-relative path too.
	h2 := newHarness(t, harnessOpt{dir: dir, home: home}).start(cli.ModeDefault, "", 0)
	h2.contains("~/my-notes")
	if st := h2.statusLine(); !strings.Contains(st, "✦ ~/my-notes") {
		t.Fatalf("home status bar: %q", st)
	}
	// Narrow: only the note name and the pill.
	h3 := newHarness(t, harnessOpt{dir: dir, home: home, w: 50, h: 20}).open("runbooks/Lantern cutover.md")
	if st := h3.statusLine(); strings.Contains(st, "my-notes") || strings.Contains(st, "runbooks") || !strings.Contains(st, "READ") {
		t.Fatalf("narrow status bar: %q", st)
	}
	// A long path outside the short form falls back to the folder name.
	home2, dir2 := vaultInHome(t, filepath.Join("a-rather-long-folder", "notes-of-mine"))
	h4 := newHarness(t, harnessOpt{dir: dir2, home: home2}).open("Home.md")
	if st := h4.statusLine(); !strings.Contains(st, "✦ notes-of-mine › Home") {
		t.Fatalf("long vault path: %q", st)
	}
}
