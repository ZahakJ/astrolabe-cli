# Design

What it is built on and what it leaves out. Implementation notes are in [internals.md](internals.md).

- **One static binary** of about 5.5 MB: the Go standard library plus `golang.org/x/sys`, `x/term`, `x/text` and `github.com/rivo/uniseg`. The terminal layer, Markdown parser, renderer, highlighter and editor are its own.
- **Plain files.** Notes stay Markdown files. No database, no index on disk, nothing written to `.obsidian/`. Each run scans the vault in memory, and search reads the files on every query. Files over 2 MB are indexed by title only.
- **The note first.** The requested note is drawn before the vault is indexed. `ASTROLABE_DEBUG=1` prints the first-frame time.
- **A typeset page.** Hierarchy comes from weight, colour, spacing and rules, at a measure of 78 columns. The same page works in 16 colours, in ASCII and in a pipe.
- **Reader and editor.** The rendered page is for reading, the source for editing, and switching keeps your place. The built-in editor knows Markdown lists, tables and `[[` links; `E`, or `editor = external`, opens `$EDITOR` instead.
- **Its own background.** At 256 colours and above the theme paints its ground; `ground = off` keeps the terminal's. At 16 colours no background is painted.
- **Bidi.** Right-to-left text is shaped and reordered by Astrolabe CLI (`on`), left to terminals that do bidi themselves (`off`), or pre-reversed for kitty (`runs`). Editing is always in logical order.
- **Search folds Arabic.** Every search ignores harakat and tatweel and unifies letter variants, Persian letters, presentation forms and Arabic-Indic digits. `re:` patterns stay literal.

## Out of scope

Graph view, image display, sync, plugins or scripting, Org-style clocking and recurring tasks, a table or spreadsheet editor, publishing, LaTeX rendering, editor splits, and Vim macros and marks.
