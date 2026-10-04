// Command astrolabe is a terminal Markdown notes tool: a reader, a small modal
// editor and a set of shell verbs over a folder of plain .md files.
//
// This file only wires the process to internal/cli (DESIGN.md §2: "dispatch
// only"). The interactive application, the renderer and the HTML exporter
// are connected through cli.Hooks.
package main

import (
	"os"

	"golang.org/x/term"

	"github.com/ZahakJ/astrolabe-cli/internal/cli"
	"github.com/ZahakJ/astrolabe-cli/internal/tui"
)

// version is set at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	os.Exit(cli.Run(cli.Env{
		Args:      os.Args[1:],
		Stdin:     os.Stdin,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		StdinTTY:  term.IsTerminal(int(os.Stdin.Fd())),
		StdoutTTY: term.IsTerminal(int(os.Stdout.Fd())),
		StderrTTY: term.IsTerminal(int(os.Stderr.Fd())),
		Version:   version,
		Hooks:     hooks(),
	}))
}

// hooks connects the verbs implemented outside internal/cli: the
// interactive application (astrolabe, astrolabe NOTE, astrolabe -, today, pick, new -o)
// and the render/export verbs, which internal/tui implements on top of
// internal/render and internal/export.
func hooks() cli.Hooks {
	return cli.Hooks{
		RunTUI: tui.Run,
		Render: tui.Render,
		Export: tui.Export,
	}
}
