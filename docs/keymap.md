# Astrolabe CLI keymap

This page lists every key in Astrolabe CLI, for printing or keeping in a second window. The reader keys come from the keymap table in `internal/tui/keymap.go`. The in-app help (`?`) and the leader palette (press `Space` and wait) read the same table.

A count typed before a key repeats it or gives it a target: `5j` moves five lines down, and `12G` goes to source line 12. `Esc` always closes the topmost overlay.

## Reader

The reader shows an accent-coloured `▎` cursor in the left margin. Actions apply to the block under it.

### Move

| Keys | Action |
|---|---|
| `j`, `↓` | cursor down (counts work: `5j`) |
| `k`, `↑` | cursor up |
| `Ctrl-d` | half page down |
| `Ctrl-u` | half page up |
| `Ctrl-f`, `PgDn` | page down |
| `Ctrl-b`, `PgUp` | page up |
| `gg`, `Home` | top (`{N}gg`: source line N) |
| `G`, `End` | bottom (`{N}G`: source line N) |
| `]]`, `}` | next heading |
| `[[`, `{` | previous heading |
| `l`, `→` | scroll a code block right (4 columns) |
| `h`, `←` | scroll a code block left |

The cursor skips blank spacing lines.

### Links and history

| Keys | Action |
|---|---|
| `Tab` | next link (highlights it) |
| `Shift-Tab` | previous link |
| `Enter`, `gd`, `Ctrl-]` | follow the highlighted link, or the first link on the cursor line |
| `Backspace`, `Ctrl-o`, `H` | back in history |
| `L` | forward in history |

What `Enter` does depends on what is under the cursor:

- **A note link** opens the note, and a link to a heading or block jumps there.
- **A broken link** asks whether to create the note (`y` / `n`). A broken link to an attachment (`![[chart.png]]`) only says it is missing.
- **A URL** is shown in the status bar and copied to the clipboard (OSC 52). Astrolabe CLI never opens it.
- **A heading with no link** has its fold toggled.
- **A footnote mark** jumps to the footnote's definition.

### Folds, tasks, editing

| Keys | Action |
|---|---|
| `za` | fold or unfold the section under the cursor |
| `zc` | fold the section |
| `zo` | unfold the section |
| `zM` | fold every section |
| `zR` | unfold everything |
| `x` | toggle the task under the cursor (rewrites one character in the file) |
| `i` | edit this block in the built-in editor, in Insert mode at the start of the source line's text (after any list, task, quote or heading marker) |
| `a` | edit this block in the built-in editor, in Insert mode at the end of the source line |
| `e` | edit this block in the built-in editor, in Normal mode on the source line |
| `E` | open `$VISUAL` / `$EDITOR` at this line, then reload the note. With `editor = external`, `i`, `a` and `e` do this too |

Task states toggle like this:

- `[ ]` → `[x]`
- `[x]` → `[ ]`
- `[/]` (in progress) → `[x]`
- `[-]` (cancelled) and any other custom state → `[ ]`

### Search

| Keys | Action |
|---|---|
| `/` | search inside the note (incremental; `Enter` keeps it, `Esc` restores the view) |
| `n` | next match |
| `N` | previous match |
| `Ctrl-p` | find a note |

### Space (leader)

| Keys | Palette word | Action |
|---|---|---|
| `Space f` | find | find a note (title, path, alias) |
| `Space /`, `Space s` | search | search the vault's text |
| `Space d` | today | today's daily note |
| `Space c` | capture | capture a line to today's note |
| `Space n` | new | new note |
| `Space t` | agenda | open tasks by due date |
| `Space #` | tags | tags, then notes with a tag |
| `Space b` | backlinks | focus backlinks in the context panel (again: close it) |
| `Space o` | outline | focus the outline in the context panel (again: close it) |
| `Space e` | tree | toggle the file tree |
| `Space r` | recent | recent notes |
| `Space T` | theme | cycle the theme (saved to the config file) |
| `Space y` | yank | copy this note's wikilink |
| `Space q` | quit | quit |

### Other

| Keys | Action |
|---|---|
| `q`, `ZZ` | quit (asks if there are unsaved edits) |
| `?` | help |
| `:` | command line |
| `Esc` | clear the highlighted link and search |
| `Ctrl-l` | redraw the screen |
| `Ctrl-z` | suspend (`fg` to return) |
| `Ctrl-c` | shows "type q to quit"; it does not quit |

### Reader commands

Type these after `:`. `Tab` and `Shift-Tab` complete command names, note names after `:e`, and theme names after `:theme`.

| Command | Action |
|---|---|
| `:e NOTE`, `:edit`, `:o`, `:open` | open a note (name, path or unique fuzzy match) |
| `:e` | reload the current note from disk |
| `:new [TITLE]`, `:enew` | create a note and open it in the editor (asks for a title if none is given) |
| `:today`, `:daily` | today's daily note |
| `:theme [NAME]`, `:colo NAME` | switch theme and save it; with no name, show the current one |
| `:export [FILE]` | write the note as HTML; the default is `<name>.html` in the current directory |
| `:set wrap=N` (`measure`, `tw`, `textwidth`) | page width in columns, 20 or more |
| `:set nu`, `:set nonu`, `:set nu!` | line numbers in the editor |
| `:set` | show the width and theme |
| `:N` | go to source line N |
| `:tags` | tags overlay |
| `:agenda`, `:tasks` | agenda overlay |
| `:noh` | clear the search highlight |
| `:help`, `:h` | help overlay |
| `:q`, `:quit`, `:wq`, `:x`, `:qa` | quit |
| `:w` | does nothing in the reader, which never changes the file; use `i` to edit |

## Overlays and panels

### Finder (`Ctrl-p`, `Space f`, `Space r`) and vault search (`Space /`)

| Keys | Action |
|---|---|
| typing | filter (the finder) or search (vault search; runs as you type) |
| `↓`, `Ctrl-n`, `Ctrl-j` | next result |
| `↑`, `Ctrl-p`, `Ctrl-k` | previous result |
| `PgDn`, `Ctrl-d`, `Ctrl-f` | page down |
| `PgUp`, `Ctrl-u`, `Ctrl-b` | page up |
| `Enter` | open the selected note (vault search jumps to the line) |
| `Ctrl-w`, `Alt-Backspace` | delete a word |
| `Ctrl-a`, `Ctrl-e`, `←`, `→` | move in the input |
| `Esc` | close |

With an empty query the finder lists recent notes. The note you are reading is listed last, so `Ctrl-p Enter` returns to the previous note.

### Agenda (`Space t`)

| Keys | Action |
|---|---|
| `j` `k`, `↓` `↑` | move |
| `g`, `G` | first / last |
| `x`, `Space` | toggle the task |
| `Enter`, `l` | open the note at the task |
| `q`, `Esc` | close |

Below 70 columns the agenda drops its note column so the task text gets the whole row; the selected task's note is named in the header instead.

### Tags (`Space #`)

| Keys | Action |
|---|---|
| typing | filter tags |
| `↓` `↑` | move |
| `Enter` | list the notes with that tag |

In the list of notes: `Enter` or `l` opens the selected note, `Backspace` or `h` goes back to the tags, and `j`/`k` move.

### Capture (`Space c`) and new note (`Space n`)

| Keys | Action |
|---|---|
| typing | the text |
| `Tab` | (capture) switch between a note line and a task; typing `[ ] ` first also makes a task |
| `Enter` | save |
| `Esc` | cancel |

### Prompts

| Prompt | Keys |
|---|---|
| Create a note for a broken link | `y` or `Enter` yes · `n` or `q` no |
| File changed on disk while editing | `r` reload theirs · `o` overwrite with mine · `c` cancel |

### Context panel (`Space o`, `Space b`) and file tree (`Space e`)

| Keys | Action |
|---|---|
| `j` `k`, `↓` `↑` | move |
| `g`, `G` | first / last |
| `Enter`, `l` | jump to the heading, open the backlink or note, or open a folder (tree) |
| `h`, `←` | close a folder (tree) |
| `Ctrl-d`, `Ctrl-u` | half page (tree) |
| `Esc`, `Tab` | return focus to the page |

At 124 columns or wider, the context panel (outline above backlinks) opens by default.

### Help (`?`)

`j` `k` scroll, `Ctrl-d` `Ctrl-u` scroll by half a page, `g` `G` go to the top and bottom, and `q`, `?` or `Esc` close it.

### Home screen

`j` `k` move over the recent notes, and `Enter` opens one. All the reader's `Space` keys and `Ctrl-p` work here.

## Mouse

| Where | Action |
|---|---|
| reader | the wheel scrolls 3 lines. A click places the cursor, follows a link, or toggles a fold chevron |
| editor | a click places the cursor; the wheel scrolls |
| overlays | the wheel scrolls; clicks are ignored |

`mouse = off` in the config hands the mouse back to the terminal.

## Editor

The editor implements a deliberate subset of Vim, and everything in it behaves as it does in Vim. Lines that soft-wrap move by display line with `j`/`k`. A count, or an operator, makes them move by source line.

### Modes and leaving

| Keys | Action |
|---|---|
| `i` `a` `I` `A` | Insert before / after the cursor, at the line's first non-blank / end |
| `o` `O` | open a line below / above (on a list item, continues the list) |
| `gI` | Insert at column 0 |
| `v`, `V` | Visual, Visual-line |
| `gv` | reselect the last Visual area |
| `Esc` | Insert/Visual → Normal. In Normal with nothing pending: save if changed, back to the reader |
| `:w`, `Ctrl-s` | save |
| `:q` | leave (refuses with unsaved changes, as Vim does) |
| `:q!`, `ZQ` | leave, discarding changes |
| `:wq`, `:x`, `ZZ` | save if changed and leave |

In a `astrolabe new -o` session, leaving the editor exits Astrolabe CLI. Everywhere else it returns to the reader. Other ex commands (`:e NOTE`, `:theme NAME`, `:today`, `:new`, `:export`) are passed to the reader.

### Motions

| Keys | Motion |
|---|---|
| `h` `j` `k` `l`, arrows, `Backspace`, `Space` | character / line |
| `w` `b` `e` `ge`, `W` `B` `E` `gE` | words, WORDS |
| `0` `^` `$` `g_` `{N}\|` | line start, first non-blank, end, last non-blank, column N |
| `gg` `G` `{N}G` `{N}%` | first / last / Nth line, N percent |
| `{` `}` | paragraph back / forward |
| `%` | matching bracket |
| `f` `F` `t` `T` `;` `,` | find a character on the line, repeat, repeat backwards |
| `H` `M` `L` | top / middle / bottom of the window |
| `+` `-` `Enter` | next / previous line, first non-blank |
| `Ctrl-d` `Ctrl-u` `Ctrl-f` `Ctrl-b` | half / full page |
| `Ctrl-e` `Ctrl-y` | scroll one line |
| `zz` `zt` `zb` | cursor line to the middle / top / bottom |

### Operators and text objects

| Keys | Action |
|---|---|
| `d` `c` `y` | delete, change, yank |
| `>` `<` | indent, outdent |
| `g~` `gu` `gU` | toggle case, lower, upper |
| `iw` `aw` `iW` `aW` | word, WORD |
| `ip` `ap` | paragraph |
| `i"` `a"` `i'` `a'` `` i` `` `` a` `` | quoted text |
| `i(` `a(` `ib` · `i[` `a[` · `i{` `a{` `iB` · `i<` `a<` | brackets (a count selects enclosing levels) |

### Changes

| Keys | Action |
|---|---|
| `dd` `cc` `yy` `>>` `<<` | the whole line |
| `D` `C` `Y` | to the end of the line (`Y` as in Neovim) |
| `x` `X` | delete a character after / before |
| `s` `S` | substitute a character / line |
| `r{c}` | replace a character |
| `J` `gJ` | join lines with / without a space |
| `~` | toggle case |
| `p` `P` | put after / before |
| `u` `Ctrl-r` | undo, redo (an insert session is one step) |
| `.` | repeat the last change (with a new count if given) |
| `&` | repeat the last `:s` |

### Registers

The editor supports the unnamed register, `"0` (last yank), `"a`–`"z` (capitals append), `"_` (black hole), and `"+` / `"*`, which copy to the system clipboard through the terminal (OSC 52). Text pasted from the terminal arrives as a bracketed paste.

### Search and substitute

| Keys | Action |
|---|---|
| `/pat` `?pat` | search forward / backward (incremental, smart-case, Vim "magic" syntax) |
| `n` `N` | next / previous match |
| `*` `#` | the word under the cursor, forward / backward |
| `:s/a/b/[flags]` | substitute on the current line |
| `:%s/a/b/g` | substitute on every line |
| `:N,Ms/…`, `:'<,'>s/…` | ranges: `N`, `.`, `$`, `%`, `'<,'>`, `±N` |

The flags are `g`, `i`, `I` and `&`. In the replacement, `&`, `\0`–`\9`, `\r`, `\n` and `\t` work. Other ex commands: `:N`, `:d`, `:y`, `:j`, `:>`, `:<`, `:u`, `:red`, `:noh`. `:set` accepts `nu`, `nonu`, `nu!`, `wrap=N`, `hls`, `ic` and `scs`.

### Markdown help in Insert mode

| Keys | Action |
|---|---|
| `Enter` | continue a bullet, numbered item (renumbering the rest), task (new `[ ]`), quote or callout. On an empty item it ends the list, or outdents a nested item |
| `Tab` / `Shift-Tab` on a list item | indent / outdent it (ordered lists renumber) |
| `Tab` / `Shift-Tab` in a table | next / previous cell, re-aligning the whole table. `Tab` in the last cell adds a row |
| `Tab` elsewhere | spaces to the next multiple of 4, or a tab in files indented with tabs |
| `[[` | note completer: `↓` `↑` or `Ctrl-n` `Ctrl-p` move, `Enter` or `Tab` insert, `Esc` keeps the typed text |
| `Ctrl-t` | insert `YYYY-MM-DD HH:MM` |

### Not in the editor

These Vim features are missing:

- numbered registers `"1`–`"9` and `"-`
- `Ctrl-r {reg}` in Insert mode, and `gi`
- sentence objects `is` / `as`
- macros and marks
- `:set nowrap`: the editor always soft-wraps
- splits
- back-references inside a search pattern (`\1`); they do work in `:s` replacements
- the `c` (confirm) flag of `:s`
