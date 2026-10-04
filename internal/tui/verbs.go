package tui

import (
	"bufio"
	"context"
	"path"
	"strings"
	"time"

	"github.com/ZahakJ/folio/internal/cli"
	"github.com/ZahakJ/folio/internal/export"
	"github.com/ZahakJ/folio/internal/md"
	"github.com/ZahakJ/folio/internal/render"
	"github.com/ZahakJ/folio/internal/term"
	"github.com/ZahakJ/folio/internal/theme"
	"github.com/ZahakJ/folio/internal/vault"
)

// waitScan waits for the vault's scan so links resolve, giving up after a
// generous bound (a huge vault on a slow disk still renders, with links
// that were not indexed yet shown as broken).
func waitScan(ctx context.Context, v *vault.Vault) {
	if v == nil || v.Scanned() {
		return
	}
	t := time.NewTimer(10 * time.Second)
	defer t.Stop()
	select {
	case <-v.Done():
	case <-ctx.Done():
	case <-t.C:
	}
}

// titleOf derives the fallback title from a file name.
func titleOf(name, rel string) string {
	if rel != "" {
		return noteName(rel)
	}
	if name == "" || name == "-" {
		return ""
	}
	return strings.TrimSuffix(path.Base(name), ".md")
}

// Render is the cli.Hooks.Render entry point (`folio render`): ANSI for a
// terminal (or forced colour), plain text into a pipe. Piped output is never
// reordered or shaped (DESIGN.md §6).
func Render(ctx context.Context, req cli.RenderRequest) error {
	waitScan(ctx, req.Vault)
	doc := md.ParseBytes(req.Source)
	d := req.Display
	gl := d.Glyphs
	if gl.Name == "" {
		gl = theme.UnicodeGlyphs
	}
	opt := render.Options{
		Width:    req.Width,
		Measure:  d.Measure,
		Glyphs:   gl,
		Resolver: &resolver{v: req.Vault, from: req.Path},
		Title:    titleOf(req.Name, req.Path),
	}
	if opt.Width <= 0 {
		opt.Width = 80
	}
	if req.Path != "" && req.Vault != nil {
		opt.Backlinks = req.Vault.BacklinkCount(req.Path)
	}
	w := bufio.NewWriterSize(req.Out, 64<<10)
	if !req.Styled {
		opt.Theme = d.Theme.Mono()
		opt.Bidi = false
		page := render.Render(doc, opt)
		if _, err := w.WriteString(page.Plain()); err != nil {
			return err
		}
		return w.Flush()
	}
	profile := d.Caps.Profile
	th := term.ThemeFor(d.Theme, profile)
	if !d.Ground {
		th = th.WithoutGround()
	}
	opt.Theme = th
	opt.Bidi = d.Caps.Bidi && d.Caps.TTY
	// Paint the theme's room only where the profile can show it, so the
	// ink always sits on its own ground (ivory on a light terminal would
	// otherwise vanish).
	opt.PaintGround = !th.Ground.IsDefault() && profile >= term.Profile256
	page := render.Render(doc, opt)
	if _, err := w.WriteString(page.ANSI(d.Caps.Encoder())); err != nil {
		return err
	}
	return w.Flush()
}

// Export is the cli.Hooks.Export entry point (`folio export`): one
// standalone HTML document.
func Export(ctx context.Context, req cli.ExportRequest) error {
	waitScan(ctx, req.Vault)
	doc := md.ParseBytes(req.Source)
	opt := export.Options{
		Path:      req.Path,
		Title:     titleOf(req.Name, req.Path),
		Resolver:  &resolver{v: req.Vault, from: req.Path},
		Generator: "folio",
	}
	if req.Path != "" && req.Vault != nil {
		opt.Backlinks = req.Vault.BacklinkCount(req.Path)
	}
	w := bufio.NewWriterSize(req.Out, 64<<10)
	if err := export.Write(w, doc, opt); err != nil {
		return err
	}
	return w.Flush()
}
