# Shell reference

This page covers every shell verb and global flag, the search syntax, exit codes, sample output, and aliases and snippets for your shell. Back to the [README](../README.md).

## Verbs

The verbs follow these rules:

- Results go to stdout and messages to stderr.
- When stdout is not a terminal, the output is plain and stable: `path:line:text` or tab-separated columns, and every list verb also accepts `--json`.
- In a pipe, paths are relative to the current directory when you are inside the vault, and absolute otherwise, so they always open as printed.

| Verb | What it does | Output in a pipe |
|---|---|---|
| `astrolabe` | open the reader: today's daily note, else the last note, else home | |
| `astrolabe NOTE` | open a note by path, title, alias, `NOTE:LINE`, `Note#Heading` or unique fuzzy match | |
| `astrolabe -` | page Markdown from stdin (read-only) | |
| `astrolabe add TEXT…` | append `- HH:MM text` to today's daily note. Reads stdin if no TEXT is given. `-t` adds a task, `-n NOTE` targets another note, `--at HEADING` adds to the end of that section | `path:line` |
| `astrolabe new TITLE…` | create a note and print its path. Piped stdin becomes the body. `-d DIR` picks the folder, `-e` opens it in `$EDITOR`, `-o` in the built-in editor | absolute path |
| `astrolabe today` | open today's daily note (created if missing). `-p` prints its path instead | absolute path |
| `astrolabe find QUERY…` | full-text search. `-l` prints paths only, `-n N` limits results, `--json` | `path:line:text` |
| `astrolabe ls [QUERY]` | notes, most recently modified first. `--tag T`, `-l`, `-n N`, `--json` | `path⇥YYYY-MM-DD HH:MM⇥title` |
| `astrolabe pick [QUERY]` | interactive fuzzy picker that prints the chosen absolute path | absolute path |
| `astrolabe tasks` | open tasks grouped by due date. `--all`, `--due`, `--json` | `path:line:[ ] text due:DATE !!` |
| `astrolabe tags` | tags with note counts. `-c` sorts by count | `tag⇥count` |
| `astrolabe links NOTE` | outgoing links, resolved. `-l`, `--json` | `line⇥kind⇥destination` |
| `astrolabe backlinks NOTE` | incoming links, each with the line that links | `path:line:context` |
| `astrolabe render [FILE\|NOTE\|-]` | typeset Markdown: ANSI on a terminal, plain text in a pipe. `-w COLS` sets the width | text |
| `astrolabe export NOTE` | one standalone HTML file. `-o FILE` writes to a file | HTML |
| `astrolabe path [NOTE]` | the vault root, or a note's absolute path | absolute path |
| `astrolabe doctor` | detected terminal capabilities, vault, config, warnings and a test card | |
| `astrolabe help [VERB]`, `astrolabe version` | the manual for one verb; the version | |

## Global flags

These global flags go before or after the verb: `-C DIR` (or `--dir`), `--theme NAME`, `--color auto|truecolor|256|16|none`, `--ascii` and `--bidi auto|on|off|runs`. Astrolabe CLI honours `NO_COLOR`. The verb names are reserved, so to open a note named after a verb, give its path: `astrolabe ./add.md`. A first argument that names no note but is close to a verb gets a suggestion before any note matches: `astrolabe tsks` prints ``did you mean `astrolabe tasks`?``.

## Search syntax

Terms are ANDed. Matching is by substring and smart-case: case is ignored unless a term contains a capital letter. Quote a phrase to match it as one term. Filters:

- `re:PATTERN` matches a Go regular expression.
- `tag:x` keeps notes tagged x.
- `path:x` keeps notes whose path contains x.
- `title:x` keeps notes whose title or an alias contains x.

## Exit status

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | nothing found (`find`, `ls`, `tasks`), or no note matches the name |
| 2 | usage error |
| 130 | cancelled (`Esc` in `astrolabe pick`) |

## Sample output

Some real output, from the sample vault:

```console
$ astrolabe find cutover | head -3
runbooks/Lantern cutover.md:2:Lantern cutover
runbooks/Lantern cutover.md:7:# Lantern cutover
projects/Lantern migration.md:33:3. **Cutover** — flip consumers queue by queue, smallest first. The

$ astrolabe links "Lantern migration" | head -3
16	embed	attachments/lantern-topology.svg
34	wikilink	runbooks/Lantern cutover.md
41	wikilink	notes/Idempotency keys.md

$ astrolabe links Typography | grep broken
25	broken	Missing note

$ astrolabe tasks | head -3
Typography.md:83:[ ] Transcribe the colophon of the Leiden copy due:2026-02-01
Inbox.md:11:[ ] Renew the staging TLS certificate due:2026-10-02 !!
daily/2026-10-02.md:5:[ ] Renew the staging TLS certificate due:2026-10-02 !!

$ astrolabe find zzz; echo $?
1
```

In a terminal the same verbs print grouped, aligned and lightly coloured output instead. Lines are shown as the reader shows them, without `[[…]]`, `**` or list markers, and Arabic is shaped and reordered when bidi is on; only the piped form above is raw:

```console
$ astrolabe tasks --due
  Overdue · 5
    ☐  Transcribe the colophon of the Leiden copy             1 Feb        Typography
    ☐  Renew the staging TLS certificate !!                   2 Oct        Inbox
    …
  Today · 1
    ☐  Compare plate latitudes with the gazetteer             today        Typography
```

## Aliases and snippets

Aliases and snippets for your shell rc:

```sh
alias n='astrolabe add'                   # n "idea"
alias nt='astrolabe add -t'               # nt "reply to the vendor due:2026-10-09"
alias nf='astrolabe find'
np() { f=$(astrolabe pick "$@") && nvim "$f"; }     # pick a note, edit it in Neovim

nvim -q <(astrolabe find deploy)          # every hit in the quickfix list
pbpaste | astrolabe add                   # capture the clipboard (macOS)
git log --oneline -20 | astrolabe new "Release notes draft"
astrolabe ls -l --tag meeting | while IFS= read -r f; do grep -l rollback "$f"; done
```

To log every commit to your daily note, save this as `.git/hooks/post-commit` and make it executable:

```sh
#!/bin/sh
astrolabe add "$(basename "$(git rev-parse --show-toplevel)"): $(git log -1 --format=%s)" >/dev/null
```

This writes a line such as `- 14:02 astrolabe: Fix the retry budget`. A hook runs inside the repository, so set `ASTROLABE_DIR` or the config key `dir`. Otherwise a repository that contains `.md` files would be taken as the vault (see [Vault root](obsidian.md#vault-root)).

Note paths can contain spaces, so use `while IFS= read -r f` or `tr '\n' '\0' | xargs -0` rather than plain `xargs` (`xargs -d '\n'` is GNU only; macOS lacks it).

More pipelines, cron jobs, tmux bindings and Neovim mappings are in [Shell, tmux and Neovim](integration.md).
