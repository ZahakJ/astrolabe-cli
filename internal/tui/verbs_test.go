package tui

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ZahakJ/astrolabe-cli/internal/cli"
	"github.com/ZahakJ/astrolabe-cli/internal/render"
	"github.com/ZahakJ/astrolabe-cli/internal/term"
	"github.com/ZahakJ/astrolabe-cli/internal/text"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
	"github.com/ZahakJ/astrolabe-cli/internal/vault"
)

func openVault(t *testing.T) (*vault.Vault, string) {
	t.Helper()
	dir := copyVault(t)
	v, err := vault.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	v.StartScan(context.Background())
	return v, dir
}

func display(p term.Profile) cli.Display {
	return cli.Display{
		ThemeName: theme.IronGall.Name, Theme: theme.IronGall, Glyphs: theme.UnicodeGlyphs,
		Ground: true, Measure: 78, Caps: term.Caps{Profile: p, TTY: true, Bidi: true},
	}
}

func TestRenderHookPlainAndStyled(t *testing.T) {
	v, dir := openVault(t)
	src, _ := os.ReadFile(filepath.Join(dir, "projects", "Lantern migration.md"))
	var plain bytes.Buffer
	err := Render(context.Background(), cli.RenderRequest{
		Source: src, Name: "Lantern migration", Path: "projects/Lantern migration.md",
		Vault: v, Width: 60, Display: display(term.ProfileTrueColor), Out: &plain,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := plain.String()
	if strings.Contains(out, "\x1b") {
		t.Fatal("plain render contains escape sequences")
	}
	for _, want := range []string{"Lantern migration", "backlinks", "Why now"} {
		if !strings.Contains(out, want) {
			t.Fatalf("plain render lacks %q:\n%s", want, out)
		}
	}
	for _, l := range strings.Split(out, "\n") {
		if w := len([]rune(l)); w > 60 {
			t.Fatalf("line wider than -w 60 (%d): %q", w, l)
		}
	}
	var styled bytes.Buffer
	err = Render(context.Background(), cli.RenderRequest{
		Source: src, Name: "x.md", Vault: v, Width: 60, Styled: true,
		Display: display(term.ProfileTrueColor), Out: &styled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(styled.String(), "\x1b[") || !strings.Contains(styled.String(), "48;2;") {
		t.Fatal("styled render has no colour / painted ground")
	}
	var s16 bytes.Buffer
	Render(context.Background(), cli.RenderRequest{Source: src, Vault: v, Width: 60, Styled: true, Display: display(term.Profile16), Out: &s16})
	if strings.Contains(s16.String(), "48;") {
		t.Fatal("16-colour render paints a ground")
	}
}

func TestExportHook(t *testing.T) {
	v, dir := openVault(t)
	src, _ := os.ReadFile(filepath.Join(dir, "Home.md"))
	var buf bytes.Buffer
	if err := Export(context.Background(), cli.ExportRequest{Source: src, Name: "Home", Path: "Home.md", Vault: v, Out: &buf}); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if !strings.HasPrefix(html, "<!doctype html>") || !strings.Contains(html, "projects/Lantern%20migration.html") && !strings.Contains(html, "projects/Lantern migration.html") {
		t.Fatalf("export:\n%.600s", html)
	}
}

func TestExportCommand(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Home.md")
	h.keys(":")
	h.typ("export")
	h.keys("enter")
	b, err := os.ReadFile(filepath.Join(h.state, "Home.html"))
	if err != nil {
		t.Fatalf("no export written: %v (status %q)", err, h.statusLine())
	}
	if !strings.Contains(string(b), "<html") {
		t.Fatal("export is not HTML")
	}
	if _, err := os.Stat(filepath.Join(h.dir, "Home.html")); err == nil {
		t.Fatal(":export wrote into the vault")
	}
}

func TestResolverAdapter(t *testing.T) {
	v, _ := openVault(t)
	<-v.Done()
	r := &resolver{v: v, from: "Home.md"}
	if p, ok := r.Resolve(renderQuery("Tidewater", true)); !ok || p != "projects/Tidewater.md" {
		t.Fatalf("Resolve = %q %v", p, ok)
	}
	if _, ok := r.Resolve(renderQuery("No such note", true)); ok {
		t.Fatal("a missing note resolved")
	}
	if title, src, ok := r.Embed("projects/Tidewater.md"); !ok || title == "" || !strings.Contains(src, "Tidewater") {
		t.Fatalf("Embed = %q %v", title, ok)
	}
}

func renderQuery(target string, wiki bool) render.LinkQuery {
	return render.LinkQuery{Target: target, Wiki: wiki}
}

// `astrolabe render` on a run-reversing terminal (kitty): each Arabic word is
// emitted in logical letter order at its visual position.
func TestRenderHookBidiRuns(t *testing.T) {
	src := []byte("# عنوان\n\nكتاب جميل جدا.\n")
	d := display(term.ProfileTrueColor)
	d.Caps.BidiMode = term.BidiRuns
	var out bytes.Buffer
	if err := Render(context.Background(), cli.RenderRequest{Source: src, Width: 40, Styled: true, Display: d, Out: &out}); err != nil {
		t.Fatal(err)
	}
	got := regexp.MustCompile("\x1b\\[[0-9;:]*m").ReplaceAllString(out.String(), "")
	want := "." + text.Shape("جدا") + " " + text.Shape("جميل") + " " + text.Shape("كتاب")
	if !strings.Contains(got, want) {
		t.Errorf("render for kitty lacks %q:\n%s", want, got)
	}
}
