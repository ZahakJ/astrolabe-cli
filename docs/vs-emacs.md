# Astrolabe CLI and Emacs

This page compares Astrolabe CLI with an Emacs notes setup (Org-mode, org-roam, org-agenda, org-capture, Denote) and lists neighbouring tools. Back to the [README](../README.md).

If you already have an Emacs notes setup with Org-mode, org-roam, org-agenda, org-capture or Denote, this section is a fair account of what you would gain and lose.

| | Astrolabe CLI | Emacs + Org / org-roam / Denote |
|---|---|---|
| Setup | none; one binary | an Emacs install plus packages and configuration, usually worth it once done |
| Start | a first frame in milliseconds | seconds for a large config, unless you keep a daemon and use `emacsclient` |
| Locked-down machine | download one file, `chmod +x` | needs Emacs installed, and packages fetched or vendored |
| Reading view | a typeset page: folds, tables, callouts, chips | the source buffer with font-lock; `org-modern` and similar packages narrow the gap |
| File format | plain Markdown, Obsidian-compatible | Org (Denote also supports Markdown and plain text) |
| Shell composition | every verb pipes: `path:line:text`, `--json`, exit codes | possible through `emacsclient --eval` or batch mode, not the usual way to work |
| Capture | `astrolabe add`, `Space c`, tmux popup | `org-capture` templates: far more flexible |
| Agenda | open tasks by due date; one date and a priority per task | scheduled and deadline dates, repeaters, habits, clocking, custom agenda views |
| Links and graph | wikilinks, backlinks with context, tags | org-roam's database, queries, backlink buffer and graph UIs |
| Literate code | none | Babel: run code blocks, tangle files |
| Export | standalone HTML, ANSI and plain text | HTML, LaTeX/PDF, ODT, Markdown and more through `ox` |
| Refile and archive | none; move files yourself | `org-refile`, `org-archive` |
| Tables | rendered; the editor re-aligns Markdown tables | Org tables with formulas, column view |
| Right-to-left text | reorders and shapes Arabic itself in terminals with no bidi | Emacs has its own bidi engine; shaping in a text terminal depends on the terminal |
| Extensibility | none: no plugins, no scripting | unlimited, in Lisp |
| Editor | a Vim subset, plus `$EDITOR` one key away | a complete editor, with Evil for Vim keys |

**Where Astrolabe CLI is better:**

- There is nothing to configure, and it starts at once.
- It runs on a machine where you cannot install anything.
- The reading view is designed to be read.
- The notes are plain Markdown that Obsidian, GitHub and every other Markdown tool also read.
- Every verb composes with the shell.
- Arabic is readable in terminals that do no bidi.

**What Astrolabe CLI does not do:**

- No Org-depth agenda: scheduling, repeaters, clocking and custom views.
- No Babel.
- No export to LaTeX, PDF or ODT.
- No refile or archive workflows.
- No org-roam graph or database queries.
- No column view or spreadsheet tables.
- No extension language.
- It is not a full editor.

**Stay on Emacs** if your agenda carries your week (scheduled items, repeaters, clocked time), if you rely on Babel or LaTeX export, or if your setup is your own Lisp. Astrolabe CLI cannot replace any of that, and it does not try to. Astrolabe CLI fits people who want their notes readable and searchable from any terminal in seconds, and whose notes are, or could be, Markdown.

**Neighbours.**

- [Obsidian](https://obsidian.md) is the graphical application whose vault format Astrolabe CLI follows. Many people will use both on the same folder.
- [glow](https://github.com/charmbracelet/glow) renders Markdown in the terminal, but it has no vault, links, tasks or editing.
- [nb](https://github.com/xwmx/nb) is a broad shell notebook with git-backed sync, bookmarks and encryption. Astrolabe CLI has no sync and a narrower scope.
- [zk](https://github.com/zk-org/zk) is a Zettelkasten CLI with a SQLite index and an LSP server for your editor. Astrolabe CLI keeps no index on disk and brings its own reader.
- Neovim plugins such as obsidian.nvim and render-markdown.nvim may be all you need if you never leave Neovim.

Astrolabe CLI sits between these: a reader and capture tool that works in any terminal, over the same files your other tools use.
