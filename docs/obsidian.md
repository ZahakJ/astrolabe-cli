# Astrolabe CLI and Obsidian vaults

Astrolabe CLI opens an Obsidian vault as it is. It needs no import and no configuration, and it never writes to `.obsidian/`. This page lists what Astrolabe CLI understands, how it resolves links, and what it ignores.

## Finding the vault

If you run Astrolabe CLI anywhere inside a folder that has `.obsidian/` above it, that folder is the vault. `.astrolabe/` works the same way for vaults that have never seen Obsidian. Settings that come earlier in the order take precedence: `-C DIR`, `$ASTROLABE_DIR` and the config key `dir`. The full order is in the README under [How notes are stored](../README.md#how-notes-are-stored), and `astrolabe doctor` shows which rule applied.

The scan skips:

- dot-directories, which includes `.obsidian/`, `.trash/` and `.git/`
- `node_modules`
- anything matched by a top-level `.astrolabeignore`

`.astrolabeignore` works like `.gitignore`. A pattern without `/` matches at any depth, a trailing `/` matches directories only, and `**` is supported. Symlinks to files are followed; symlinks to directories are not. Files over 2 MB are indexed by title only.

## Notes and titles

A note is any `.md` file, identified by its path relative to the vault. Other files (images, PDFs, SVGs) are attachments; they can be linked and embedded, and `astrolabe doctor` counts them.

A note's title is the first of these that exists:

1. the frontmatter `title`
2. the first H1
3. the file name without `.md`

## Frontmatter

Astrolabe CLI reads a leading `---` YAML block and interprets only these keys:

| Key | Use |
|---|---|
| `title` | the note's title |
| `tags` (or `tag`) | tags; either a YAML list or a comma- or space-separated string |
| `aliases` (or `alias`) | other names the note answers to, in links, the finder and `astrolabe NOTE` |
| `date`, `created` | shown in the title block |

Every other key is kept as written. In the reader, the extra keys are folded into a `▸ properties · N` line, which `za` opens. Astrolabe CLI never re-serialises the frontmatter. A task toggle or capture never touches it, and the editor saves it byte for byte.

## Links

| Syntax | Understood |
|---|---|
| `[[Note]]`, `[[Note\|label]]` | yes |
| `[[folder/Note]]`, `[[2026/plan]]` (folder suffix) | yes |
| `[[Note#Heading]]`, `[[Note#Heading#Subheading]]` | yes; the last heading is used |
| `[[Note#^block-id]]` and `^block-id` at the end of a block | yes |
| `[[#Heading]]` (same note) | yes |
| `![[Note]]`, `![[Note#Heading]]` | embedded: the first 12 lines in a quote bar, with the note's title |
| `![[image.png]]`, `![alt](image.png)` | shown as `▣ alt` with the file name; no image display |
| `[text](relative/path.md)`, `[text](path.md#heading)` | yes; percent-encoded paths are decoded |
| `[text](https://…)`, `<https://…>`, bare `https://…` and `www.…` | external links |
| `[text][ref]` with `[ref]: …` | rendered as a link, but not counted for backlinks or `astrolabe links` |

### Resolution

Astrolabe CLI follows Obsidian's resolution order:

1. **Path match.** For a wikilink, Astrolabe CLI first tries an exact relative path from the vault root, then from the linking note's folder. A Markdown link tries the note's folder first.
2. **Basename match.** Next comes the shortest path whose base name matches. The match ignores case, and `.md` is optional. An exact case match wins over a case-insensitive one. Remaining ties go to fewer folders, then the shorter path, then alphabetical order.
3. **Aliases.**

Attachments must be linked with their extension (`[[diagram.svg]]`). Heading anchors match while ignoring case and punctuation.

### Broken links

A link that resolves to nothing is broken:

- **In the reader** it is drawn in the danger colour with a dotted or curly underline where the terminal supports one, and a plain underline otherwise.
- **Following it** offers to create the note. A broken link to an attachment (any target with a file extension other than `.md`, such as `![[chart.png]]` or `[[minutes.pdf]]`) only reports "missing attachment": Astrolabe CLI never creates `chart.png.md`.
- **Listings.** `astrolabe links` lists it with the kind `broken`, and `astrolabe doctor` counts broken links across the vault.

### Backlinks

Each backlink comes with its context line, shown in:

- the context panel (`Space b`)
- the title block (`N backlinks`)
- `astrolabe backlinks NOTE`

Links from a note to its own headings are not counted.

## Tags

Astrolabe CLI reads tags from two places: `#tag` and `#nested/tag` in the text, and the frontmatter `tags`. Nested tags roll up, so a note tagged `#project/lantern` also counts towards `#project`.

- Tags are case-insensitive.
- Letters from any script are allowed: `#فلك` is a tag.
- Purely numeric tags (`#2026`) are not tags.

A `#` is not a tag in these places:

- inside code
- in a URL or link
- in a heading marker
- in a `%% comment %%` or `<!-- comment -->`
- when the character before it is not a space or the start of a line

## Tasks

| Written | State | Shown |
|---|---|---|
| `- [ ] text` | open | `☐` |
| `- [x] text`, `- [X] text` | done | `☑`, text faint and struck through |
| `- [/] text` | in progress | `◐` |
| `- [-] text` | cancelled | `☒` |
| `- [?] text` (any single character) | custom, counted as open | |

**Due dates** can be written three ways: `📅 2026-10-05` (as the Obsidian Tasks plugin writes it), `due:2026-10-05` or `@due(2026-10-05)`. The reader shows the date as a chip at the end of the line, such as `due 5 Oct`, `due today`, or `overdue 1 Oct` in the danger colour.

**Priorities** are `!`, `!!`, `!!!` and the Tasks plugin's `🔺 ⏫ 🔼 🔽 ⏬`. The agenda and `astrolabe tasks` sort by due date with undated tasks last, then by priority, path and line.

**Toggling** with `x` rewrites exactly the one character between the brackets:

- `[ ]` → `[x]`
- `[x]` → `[ ]`
- `[/]` → `[x]`
- `[-]` and custom states → `[ ]`

Astrolabe CLI refuses a toggle in these cases:

- the line inside the brackets has changed on disk since the note was read
- the line is no longer a task
- the line is inside a code fence

## Callouts

`> [!kind] Title` callouts render with a bar, a label line in the callout's colour, and a glyph. The fold markers work: `> [!tip]-` starts folded and `> [!tip]+` starts open. Toggle either with `za`.

| Kind and aliases | Glyph |
|---|---|
| `note`, `info` | `¶` |
| `abstract`, `summary`, `tldr` | `✎` |
| `todo` | `☐` |
| `tip`, `hint`, `important` | `☞` |
| `success`, `check`, `done` | `✓` |
| `question`, `help`, `faq` | `?` |
| `warning`, `caution`, `attention` | `!` |
| `failure`, `fail`, `missing`, `danger`, `error` | `✗` |
| `bug` | `✱` |
| `example` | `≡` |
| `quote`, `cite` | `❝` |

Unknown kinds use the `note` style. The design called for `ℹ` and `※`, but neither glyph is in DejaVu Sans Mono, so Astrolabe CLI uses `¶` and `✱` instead.

## Other Markdown

| Syntax | In Astrolabe CLI |
|---|---|
| `==highlight==` | highlighted ground |
| `~~strike~~` | faint and struck through |
| `%% comment %%`, `<!-- comment -->` | hidden in the reader and in exports |
| footnotes `[^1]` with `[^1]: …` | `[1]` markers; definitions gathered at the end; `Enter` on a marker jumps to it |
| `$inline$`, `$$display$$` math | shown as source, in the math colour (no LaTeX rendering) |
| tables | box-drawing hairlines, alignment from the delimiter row; stacked records when too narrow |
| fenced code | raised block with a language label; highlighting for sh, python, go, js/ts, json, yaml, c/c++, rust, lua, sql and diff |
| HTML blocks | shown as source, faint; `<br>` breaks lines |

## Daily notes

Astrolabe CLI reads `folder` and `format` from `.obsidian/daily-notes.json`. Without that file it uses `daily/YYYY-MM-DD.md`. The config keys `daily_dir` and `daily_format` override both. The supported Moment tokens are `YYYY YY MMMM MMM MM M DD D dddd ddd`, and text in `[brackets]` is literal. Month and weekday names are English.

A daily note that Astrolabe CLI creates starts with a heading such as `# Sunday, 4 October 2026`. Captures (`astrolabe add`, `Space c`) append one line each:

- `- HH:MM text` for a note
- `- [ ] text` for a task

With `--at HEADING`, the line goes at the end of that section, and Astrolabe CLI adds the heading first if it is missing.

## What Astrolabe CLI ignores

- **Plugins.** Dataview queries, Templater syntax and other plugin blocks show as ordinary text or code.
- **Canvas.** `.canvas` files and the graph view.
- **Images.** They are not displayed (Astrolabe CLI has no terminal image protocols in version 1).
- **Some syntax.** Inline footnotes `^[text]`, and image sizes such as `![[image.png|100]]`; the size stays in the alt text.
- **Parts of the daily-notes settings.** The `template` setting, and Moment week tokens, which are copied literally.
- **Workspace and appearance settings.** Everything else under `.obsidian/`.

## Using Astrolabe CLI and Obsidian on the same vault

This works without special care:

- Astrolabe CLI edits files in place with atomic writes and changes only the bytes you changed.
- Astrolabe CLI adds nothing to the vault apart from the notes and lines you create.
- If Obsidian (or a sync tool) changes a note while it is open in Astrolabe CLI's editor, saving does not overwrite it. Astrolabe CLI asks whether to reload, overwrite or cancel.
- In the reader, Astrolabe CLI reloads a note when you run `:e`, after `E`, and after saving. A note changed elsewhere while you are only reading it is not reloaded automatically.
