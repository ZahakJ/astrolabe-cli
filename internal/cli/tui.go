package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZahakJ/folio/internal/vault"
)

// tuiRequest fills the parts of a TUIRequest common to every mode. The vault
// must already be open; its background scan is started here.
func (a *app) tuiRequest(mode TUIMode) TUIRequest {
	if a.v != nil && !a.scanned {
		a.v.StartScan(context.Background())
	}
	return TUIRequest{
		Mode:       mode,
		Vault:      a.v,
		Root:       a.root,
		Config:     a.config(),
		RecentFile: vault.RecentFile(a.getenv),
		StdoutTTY:  a.env.StdoutTTY,
		Display:    *a.display(),
	}
}

// runTUI hands over to the interactive application.
func (a *app) runTUI(req TUIRequest) error {
	if a.env.Hooks.RunTUI == nil {
		return tuiMissing()
	}
	_, err := a.env.Hooks.RunTUI(context.Background(), req)
	return err
}

func tuiMissing() error {
	return fmt.Errorf("the interactive reader is %w (the shell verbs work: see 'folio help')", ErrNotBuilt)
}

// openDefault is plain `folio`: today's daily note if it exists, else the
// last note of this vault, else the home screen (DESIGN.md §4.1). The first
// interactive run in a fresh ~/notes writes Welcome.md (DESIGN.md §3).
func (a *app) openDefault() error {
	if a.env.Hooks.RunTUI == nil {
		return tuiMissing()
	}
	v, err := a.vault()
	if err != nil {
		return err
	}
	req := a.tuiRequest(ModeDefault)
	if daily := v.DailyPath(a.now()); fileExists(v.Abs(daily)) {
		req.Path = daily
	} else if rel, line, ok := a.lastNote(v); ok {
		req.Path, req.Line = rel, line
	}
	if req.Path == "" && (a.root.Source == vault.RootFromHome || a.root.Source == vault.RootFromDefault) {
		if wrote, err := vault.EnsureWelcome(v.Root()); err != nil {
			a.errorf("could not write %s: %v", vault.WelcomeName, err)
		} else if wrote {
			if _, err := v.Refresh(vault.WelcomeName); err == nil {
				req.Path = vault.WelcomeName
			}
		}
	}
	return a.runTUI(req)
}

// lastNote returns the most recent note of the recent file that lies in
// this vault and still exists.
func (a *app) lastNote(v *vault.Vault) (string, int, bool) {
	r := vault.LoadRecent(vault.RecentFile(a.getenv))
	for _, e := range r.Entries() {
		if !within(v.Root(), e.Path) || !fileExists(e.Path) {
			continue
		}
		rel, err := filepath.Rel(v.Root(), e.Path)
		if err != nil {
			continue
		}
		return filepath.ToSlash(rel), e.Line, true
	}
	return "", 0, false
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// openNote is `folio NOTE`.
func (a *app) openNote(args []string) error {
	a.firstArg = true // a mistyped verb gets a suggestion first
	ref, err := a.resolveNote(strings.Join(args, " "))
	a.firstArg = false
	if err != nil {
		return err
	}
	if a.env.Hooks.RunTUI == nil {
		return tuiMissing()
	}
	req := a.tuiRequest(ModeOpen)
	req.Path, req.Line = ref.Rel, ref.Line
	return a.runTUI(req)
}

// openStdin is `folio -`.
func (a *app) openStdin() error {
	b, ok, err := a.readStdin()
	if err != nil {
		return err
	}
	if !ok {
		return usagef("", "'folio -' pages Markdown from stdin, but stdin is a terminal; pipe something in")
	}
	if a.env.Hooks.RunTUI == nil {
		return tuiMissing()
	}
	if _, err := a.vault(); err != nil {
		return err
	}
	req := a.tuiRequest(ModeStdin)
	req.Source, req.Name = b, "-"
	return a.runTUI(req)
}

// cmdPick is `folio pick [QUERY]`.
func (a *app) cmdPick(args []string) error {
	fs := newFlagSet("pick")
	pos, err := fs.Parse(args)
	if err != nil {
		return err
	}
	if a.env.Hooks.RunTUI == nil {
		return tuiMissing()
	}
	v, err := a.vault()
	if err != nil {
		return err
	}
	req := a.tuiRequest(ModePick)
	req.Query = strings.Join(pos, " ")
	res, err := a.env.Hooks.RunTUI(context.Background(), req)
	if err != nil {
		return err
	}
	if res.Path == "" {
		return ErrCancelled
	}
	a.out("%s\n", v.Abs(res.Path))
	return nil
}

// source reads the document named by a render/export argument: "-" (or no
// argument with piped stdin) is stdin; an existing file is read as is; else
// the argument is a note name.
func (a *app) source(verb string, pos []string) (src []byte, name, rel string, err error) {
	if len(pos) > 1 {
		return nil, "", "", usagef(verb, "one document at a time (got %d)", len(pos))
	}
	if len(pos) == 0 || pos[0] == "-" {
		b, ok, err := a.readStdin()
		if err != nil {
			return nil, "", "", err
		}
		if !ok {
			return nil, "", "", usagef(verb, "name a file or note, or pipe Markdown on stdin")
		}
		if _, err := a.vault(); err != nil {
			return nil, "", "", err
		}
		return b, "-", "", nil
	}
	ref, err := a.resolveNote(pos[0])
	if err != nil {
		return nil, "", "", err
	}
	b, _, err := vault.ReadFile(a.v.Abs(ref.Rel))
	if err != nil {
		return nil, "", "", err
	}
	return b, pos[0], ref.Rel, nil
}

// cmdRender is `folio render [FILE|-]`.
func (a *app) cmdRender(args []string) error {
	fs := newFlagSet("render")
	width := fs.Int("-w", "--width", 0)
	pos, err := fs.Parse(args)
	if err != nil {
		return err
	}
	if a.env.Hooks.Render == nil {
		return fmt.Errorf("render is %w", ErrNotBuilt)
	}
	src, name, rel, err := a.source("render", pos)
	if err != nil {
		return err
	}
	d := *a.display()
	w := *width
	if w == 0 {
		w = d.Caps.Width
	}
	a.v.StartScan(context.Background())
	return a.env.Hooks.Render(context.Background(), RenderRequest{
		Source:  src,
		Name:    name,
		Path:    rel,
		Vault:   a.v,
		Width:   w,
		Styled:  a.styledStdout(),
		Display: d,
		Out:     a.env.Stdout,
	})
}

// cmdExport is `folio export NOTE [-o FILE]`.
func (a *app) cmdExport(args []string) error {
	fs := newFlagSet("export")
	outFile := fs.String("-o", "--output")
	pos, err := fs.Parse(args)
	if err != nil {
		return err
	}
	if a.env.Hooks.Export == nil {
		return fmt.Errorf("export is %w", ErrNotBuilt)
	}
	src, name, rel, err := a.source("export", pos)
	if err != nil {
		return err
	}
	a.v.StartScan(context.Background())
	req := ExportRequest{Source: src, Name: name, Path: rel, Vault: a.v, Display: *a.display(), Out: a.env.Stdout}
	if *outFile == "" {
		return a.env.Hooks.Export(context.Background(), req)
	}
	var buf bytes.Buffer
	req.Out = &buf
	if err := a.env.Hooks.Export(context.Background(), req); err != nil {
		return err
	}
	dst := *outFile
	if !filepath.IsAbs(dst) {
		dst = filepath.Join(a.env.Cwd, dst)
	}
	if _, err := vault.WriteFile(dst, buf.Bytes(), nil); err != nil {
		return fmt.Errorf("writing %s: %w", *outFile, err)
	}
	a.infof("wrote %s", *outFile)
	return nil
}
