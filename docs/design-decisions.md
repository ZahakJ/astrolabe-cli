# Design decisions

This page covers the goals behind Astrolabe CLI, the choices made to meet them, and what is out of scope. The normative design contract is [DESIGN.md](../DESIGN.md). Back to the [README](../README.md).

## Goals

**Light.** Astrolabe CLI is a single static binary of about 5.5 MB with no runtime dependencies: no libc, no interpreter, no Nerd Font. It runs on old Linux machines, on macOS, over SSH and inside tmux. Your notes stay ordinary Markdown files in an ordinary folder. Astrolabe CLI keeps no database and no index on disk, and it adds no files to your notes folder. It opens an existing Obsidian vault as it is, and it never writes to `.obsidian/`.

**Quick.** Astrolabe CLI draws the note you asked for first and indexes the vault in the background. On a generated vault of 1,500 notes (7.9 MB), opening a 424-line note drew the first frame in 6–10 ms. The team measured 4–26 ms on a real vault of 1,525 notes. A whole `astrolabe find` run, including process start and reading every file, took 30–43 ms on the same 1,500 notes, and a capture with `astrolabe add` takes about 2 ms. To see the first-frame time on your own machine, run Astrolabe CLI with `ASTROLABE_DEBUG=1`.

**A page worth reading.** Terminals have only one font size, so the reader builds hierarchy from weight, colour, spacing and rules. Lines are kept to a readable width (78 columns by default) and centred. Headings have fold chevrons, and tables use hairline borders and re-flow to fit the width. Tasks show due-date chips and callouts get their own colours. Arabic is shaped and laid out right to left, even in terminals that cannot do this themselves. The same page also works with 16 colours, with plain ASCII, and in a pipe.

## Decisions

These are the choices behind Astrolabe CLI, for anyone evaluating it or working on it.

- **Go, with four dependencies.** Astrolabe CLI uses the standard library plus `golang.org/x/sys`, `x/term`, `x/text` and `github.com/rivo/uniseg`. There is no TUI framework, no Markdown library and no syntax-highlighting library. The terminal layer, the Markdown parser, the renderer, the highlighter and the editor are written for Astrolabe CLI. That keeps the binary small and static, and it gives full control over every cell on the page.
- **A built-in editor and `$EDITOR`.** A locked-down machine may have only `vi`, and the built-in editor understands Markdown lists, tables and `[[` links and keeps your place in the page. Your own editor is one key away (`E`), or always used with `editor = external`.
- **Separate reader and editor modes.** The rendered page is for reading and navigating; the source is for editing. Mixing the two makes both worse. Switching keeps your place: the editor opens on the source line of the block under the cursor, and leaving it returns to the same block.
- **No index on disk.** The files are the only state. Each run scans the vault in memory, which takes tens of milliseconds for 1,500 notes, so there is nothing to go stale, to rebuild, or to conflict in a sync tool. Search reads the files on every query. Files over 2 MB are indexed by title only.
- **The theme paints its own background.** In truecolour and 256 colours the page is a room with its own ground, so contrast is controlled. If you prefer your terminal's background, set `ground = off`. At 16 colours Astrolabe CLI never paints a background.
- **Bidi in Astrolabe CLI.** Astrolabe CLI reorders and shapes right-to-left text itself because most terminals do not. It steps aside for the terminals that implement bidi (`off`), and it compensates for kitty, which reverses each right-to-left run while shaping (`runs`). The `runs` transform is applied last, on the finished screen rows, so overlays, truncation and highlights need no special cases. Editing always happens in logical order; only the display is reordered.
- **Search folds Arabic.** Every search (finder, `astrolabe pick`, vault search, in-note `/`, completion, `astrolabe find`, `astrolabe ls`) compares a folded form of the query and the text: harakat, tatweel and invisible controls dropped, alef forms, alef maqsura, teh marbuta, hamza carriers and Persian letters unified, presentation forms and Arabic-Indic digits mapped to letters and ASCII digits. A query word also matches after the article and the attached particles (ال، وال، بال، و، ب، ل، ف، ك) as a word start. `re:` patterns stay literal.
- **Left out of version 1:** graph view, image display, sync, plugins or scripting, Org-style clocking and recurring tasks, a real table or spreadsheet editor, publishing, LaTeX rendering, multiple windows or splits in the editor, and Vim macros and marks.
