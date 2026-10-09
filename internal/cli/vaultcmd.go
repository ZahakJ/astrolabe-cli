package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZahakJ/astrolabe-cli/internal/vault"
)

// afterRun records what this run learned about the vault (DESIGN.md §3):
// a vault in ~/notes marks the state file "used" (so the first-run Welcome
// note never opens by itself again), and a vault chosen by -C, or by a
// marker above the working directory while nothing is remembered yet, is
// remembered in config "dir". Only the vault ResolveRoot chose counts, once
// it opened and exists; failures are soft (doctor reports an unwritable
// config).
func (a *app) afterRun() {
	if !a.rooted || a.v == nil || !dirExists(a.root.Dir) {
		return
	}
	r := a.root
	if (r.Source == vault.RootFromHome || r.Source == vault.RootFromDefault) && filepath.IsAbs(vault.UsedFile(a.getenv)) {
		_ = vault.MarkUsed(a.getenv)
	}
	if a.noRemember {
		return
	}
	remembered := r.Remembered != "" && r.Stale == ""
	switch r.Source {
	case vault.RootFromFlag:
		if remembered && vault.SamePath(r.Remembered, r.Dir) {
			return
		}
	case vault.RootFromMarker:
		// Standing in a project vault must not steal the remembered one.
		if remembered {
			return
		}
	default:
		return
	}
	if err := a.remember(r.Dir, false); err != nil {
		a.infof("could not remember the vault: %v", err)
	}
}

// remember writes dir as config "dir" and prints the confirmation line on
// stderr when it is a terminal (or always, when the user asked: `astrolabe
// vault DIR`), so pipelines and scripts stay quiet.
func (a *app) remember(dir string, always bool) error {
	file := vault.ConfigFile(a.getenv)
	if !filepath.IsAbs(file) {
		// No $HOME and no $XDG_CONFIG_HOME: never write relative to cwd.
		return fmt.Errorf("no config directory (HOME is not set)")
	}
	if err := vault.SetConfigValue(file, "dir", dir); err != nil {
		return err
	}
	if !always && !a.env.StderrTTY {
		return nil
	}
	fmt.Fprintf(a.env.Stderr, "Using %s from now on; `astrolabe vault` to change.\n", vault.DisplayPath(dir, a.env.Home))
	return nil
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// cmdVault is `astrolabe vault [DIR | --forget]`: show, set or forget the
// remembered vault.
func (a *app) cmdVault(args []string) error {
	fs := newFlagSet("vault")
	forget := fs.Bool("", "--forget")
	pos, err := fs.Parse(args)
	if err != nil {
		return err
	}
	a.noRemember = true
	switch {
	case len(pos) > 1:
		return usagef("vault", "one directory at a time (got %d)", len(pos))
	case *forget && len(pos) > 0:
		return usagef("vault", "--forget takes no directory")
	case *forget:
		return a.forgetVault()
	case len(pos) == 1:
		return a.setVault(pos[0])
	}
	return a.showVault()
}

// showVault prints the vault a plain run would use now: the path alone in
// a pipe; the path, the rule that chose it and the remembered vault on a
// terminal.
func (a *app) showVault() error {
	a.quietStale = true
	r, err := a.resolveRoot()
	if err != nil {
		return err
	}
	if !a.human() {
		if r.Stale != "" {
			a.errorf("the remembered vault %s; skipping it", a.staleShown(r))
		}
		a.raw("%s\n", r.Dir)
		return nil
	}
	p := a.painter()
	home := a.env.Home
	row := func(k, v string) { a.out("  %s%s\n", p.muted(fmt.Sprintf("%-12s", k)), v) }
	name := vault.DisplayPath(r.Dir, home)
	if !r.Exists {
		name += "  " + p.faint("(not created yet)")
	}
	a.out("%s %s\n", p.accent(p.g.Brand), p.heading(name))
	row("chosen by", rootWhy(r.Source))
	switch {
	case r.Stale != "":
		row("remembered", p.danger(a.staleShown(r))+"  "+p.faint("skipped"))
	case r.Remembered == "":
		row("remembered", p.faint("nothing yet: `astrolabe -C DIR` once, or `astrolabe vault DIR`"))
	case vault.SamePath(r.Remembered, r.Dir):
		row("remembered", vault.DisplayPath(r.Remembered, home))
	default:
		row("remembered", vault.DisplayPath(r.Remembered, home)+"  "+p.faint(notUsedWhy(r.Source)))
	}
	return nil
}

// setVault is `astrolabe vault DIR`.
func (a *app) setVault(arg string) error {
	dir := arg
	if dir == "~" {
		dir = a.env.Home
	} else if strings.HasPrefix(dir, "~/") && a.env.Home != "" {
		dir = filepath.Join(a.env.Home, dir[2:])
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(a.env.Cwd, dir)
	}
	dir = filepath.Clean(dir)
	fi, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return &notFound{msg: fmt.Sprintf("no such directory: %s", arg)}
	case err != nil:
		return err
	case !fi.IsDir():
		return &notFound{msg: fmt.Sprintf("%s is not a directory", arg)}
	}
	if err := a.remember(dir, true); err != nil {
		return fmt.Errorf("could not remember the vault: %w", err)
	}
	if a.envDir() != "" {
		a.infof("$ASTROLABE_DIR is set, so it still wins in this environment")
	}
	return nil
}

// forgetVault is `astrolabe vault --forget`.
func (a *app) forgetVault() error {
	file := vault.ConfigFile(a.getenv)
	if !filepath.IsAbs(file) {
		return fmt.Errorf("no config directory (HOME is not set)")
	}
	old, _ := a.config().FileValue("dir")
	removed, err := vault.UnsetConfigValue(file, "dir")
	if err != nil {
		return fmt.Errorf("could not forget the vault: %w", err)
	}
	if !removed {
		fmt.Fprintf(a.env.Stderr, "No vault is remembered.\n")
		return nil
	}
	fmt.Fprintf(a.env.Stderr, "Forgot %s; the next `astrolabe -C DIR` is remembered.\n", vault.DisplayPath(old, a.env.Home))
	return nil
}

// cmdLearn is `astrolabe learn`: open the Welcome note on purpose, writing
// it into the current vault as Welcome.md when no such file exists;
// --print writes the tutorial to stdout instead.
func (a *app) cmdLearn(args []string) error {
	fs := newFlagSet("learn")
	print := fs.Bool("-p", "--print")
	pos, err := fs.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("learn", "unexpected argument %q", pos[0])
	}
	if *print {
		a.raw("%s", vault.WelcomeNote)
		return nil
	}
	if a.env.Hooks.RunTUI == nil {
		return tuiMissing()
	}
	v, err := a.vault()
	if err != nil {
		return err
	}
	if _, err := vault.WriteWelcome(v.Root()); err != nil {
		return fmt.Errorf("could not write %s: %w", vault.WelcomeName, err)
	}
	if _, err := v.Refresh(vault.WelcomeName); err != nil {
		return err
	}
	req := a.tuiRequest(ModeOpen)
	req.Path = vault.WelcomeName
	return a.runTUI(req)
}
