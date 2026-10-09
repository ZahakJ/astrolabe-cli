package vault

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// WelcomeName is the file name of the welcome note.
const WelcomeName = "Welcome.md"

// WelcomeNote is written into a fresh ~/notes on the first interactive run.
// It teaches astrolabe in a minute using the keymap of DESIGN.md §4.3 and
// exercises the renderer (callout, tasks, table, code, wikilinks).
const WelcomeNote = `---
tags: [astrolabe]
---
# Welcome to astrolabe

Your notes are plain Markdown files in this folder. astrolabe reads them
beautifully, edits them in place and never keeps anything else here.

> [!tip] The one key to remember
> Press ` + "`Space`" + ` and wait: a palette lists every command. ` + "`?`" + ` shows them all.

## Read

Move with ` + "`j`" + ` and ` + "`k`" + `, jump between headings with ` + "`]]`" + ` and ` + "`[[`" + `.
` + "`Tab`" + ` picks the next link and ` + "`Enter`" + ` follows it; ` + "`Backspace`" + ` comes back.
Try it on [[#Write|the next section]], or on [[Ideas]], a note that does
not exist yet: following it offers to create it.

## Write

Press ` + "`i`" + ` and type: the line under the cursor opens in the built-in Vim-style
editor, already in Insert mode. ` + "`Esc Esc`" + ` saves and returns here. ` + "`a`" + ` types at
the end of the line, ` + "`e`" + ` opens the editor in Normal mode, ` + "`E`" + ` opens ` + "`$EDITOR`" + `.

- [x] Open astrolabe
- [ ] Toggle this task with ` + "`x`" + ` (it rewrites one character of the file)
- [ ] Capture a thought with ` + "`Space c`" + ` — it lands in today's note

## Find

| Keys | Does |
|:--|:--|
| ` + "`Space f`" + ` | find a note by name |
| ` + "`Space /`" + ` | search every note's text |
| ` + "`Space d`" + ` | open today's daily note |
| ` + "`Space t`" + ` | open tasks, by due date |
| ` + "`Space n`" + ` | start a new note |

## From the shell

` + "```sh" + `
astrolabe add "call the plumber"   # one line into today's note
astrolabe add -t "renew passport"  # …as a task
astrolabe find invoice             # path:line:text, like grep
nvim "$(astrolabe pick)"           # choose a note, edit it elsewhere
` + "```" + `

Delete this note whenever you like; ` + "`q`" + ` quits.
`

// EnsureWelcome writes WelcomeNote into dir if dir does not exist yet or
// contains no Markdown file at its top level, creating dir as needed. It
// never overwrites an existing Welcome.md. It reports whether the note was
// written.
func EnsureWelcome(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	for _, e := range entries {
		if !e.IsDir() && isMarkdown(e.Name()) {
			return false, nil
		}
	}
	err = createExclusive(filepath.Join(dir, WelcomeName), []byte(WelcomeNote))
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	return err == nil, err
}

// WriteWelcome writes WelcomeNote as dir/Welcome.md when no file of that
// name exists (for `astrolabe learn`), creating dir as needed. It reports
// whether the note was written; an existing Welcome.md is left alone.
func WriteWelcome(dir string) (bool, error) {
	err := createExclusive(filepath.Join(dir, WelcomeName), []byte(WelcomeNote))
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	return err == nil, err
}
