# Astrolabe CLI

Astrolabe CLI is a reader, a small Vim-style editor and a set of shell verbs for a folder of Markdown notes. It is the terminal companion to [Astrolabe](https://github.com/ZahakJ/astrolabe), the notes app for the web, desktop and phone, and it opens the same vault; it also works on its own, on any folder of `.md` files or on an Obsidian vault exactly as it is. The command is `astrolabe`, and the installer adds the short alias `ast`.

![The reader in kitty: an English note on the left and an Arabic note on the right, side by side](shots/kitty-hero.png)

*Two kitty windows on the same vault. The Arabic note on the right is shaped and laid out right to left by Astrolabe CLI itself; kitty does no bidi of its own.*

## What it is for

- **Reading notes in a terminal as a page, not as source.** Headings get weight and colour instead of `#` marks, paragraphs keep a readable measure, tables re-flow to the window, callouts, tasks and due dates are drawn as such, and sections fold.
- **Capturing from the shell without opening anything.** `astrolabe add "call the plumber"` appends one line to today's daily note, and every list verb prints plain lines in a pipe.
- **Living inside an existing vault.** Wikilinks, aliases, tags, tasks, callouts and daily notes are read the way Obsidian reads them, and nothing is added to your notes folder: no database, no index on disk.

## What it is not

It does not run Obsidian plugins (no Dataview, Templater or Canvas), draws no graph and shows no images. It never touches the network at runtime. The [design decisions](design-decisions.md) chapter lists the rest of what is out of scope, and why.

## How to read this manual

[Install](install.md) and [First run](tour.md) take five minutes and leave you able to use it. [Keymap](keymap.md) and the [shell reference](cli.md) are the references to keep open. [Vaults and your files](obsidian.md) says how the vault is chosen and what is promised about your files; read its *Data safety* section before pointing it at notes you care about. [Arabic and right-to-left text](arabic.md) is for anyone who writes in Arabic, Persian or Hebrew.

Astrolabe CLI is open source and free, under the MIT licence. The source is at [github.com/ZahakJ/astrolabe-cli](https://github.com/ZahakJ/astrolabe-cli).
