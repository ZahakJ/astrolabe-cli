# Arabic and right-to-left text

Astrolabe CLI shapes Arabic into joined letter forms and lays it out right to left itself, so Arabic notes read correctly in terminals that do nothing for right-to-left text. Persian and Hebrew go through the same path.

![An Arabic note in kitty: title, bullets, a quotation, a callout, a table and tasks, all aligned to the right, with the outline and backlinks panel on the side](shots/kitty-arabic.png)

*kitty. Bullets, the quote bar and the fold chevron sit on the right edge.*

## The three bidi modes

Most terminals do no bidi, some do their own, and kitty reverses right-to-left runs while shaping. Each gets a mode.

| Mode | For | What Astrolabe CLI does |
|---|---|---|
| `on` | terminals that do no bidi: foot, Alacritty, WezTerm, xterm, Windows Terminal and most others | shapes each word into its joined forms and reorders every line into visual order |
| `runs` | kitty | the same as `on`, then writes each stretch of right-to-left letters pre-reversed for kitty to reverse back |
| `off` | terminals that implement bidi themselves: GNOME Terminal, Tilix and other VTE terminals, Konsole, Apple Terminal, mlterm | leaves the text in logical order and lets the terminal do the work |

The default is `auto`. It chooses `runs` in kitty (it looks at `TERM`, `KITTY_WINDOW_ID` and `TERM_PROGRAM`, and inside tmux it asks tmux which terminal is attached), `off` in the terminals listed above, and `on` everywhere else. To force one, use `--bidi on|off|runs`, the config key `bidi`, or `ASTROLABE_BIDI`.

Colours, highlights, the selection and the cursor stay on the right cells in every mode.

## Check it in your terminal

```sh
astrolabe doctor
```

prints the bidi mode it chose and the reason, and the last line of its test card is an Arabic phrase, which should read joined and right to left. If it does not, try `astrolabe doctor --bidi runs`, then `--bidi on`, then `--bidi off`, and keep the one whose test line reads correctly by setting `bidi = …` in the config. A terminal that reverses right-to-left runs as kitty does, but is not detected, needs `runs`.

## Where Arabic works

Everywhere text is shown: the reader, the start page, the finder, vault search and in-note `/`, tags, the agenda, the file tree, the outline and backlinks panel, completion menus, status messages, and the CLI's own human output (`astrolabe render`, `ls`, `tasks`, `find`). A paragraph whose first strong character is Arabic or Hebrew is aligned right, with its bullets and bars on the right edge. Right-to-left titles that are too long are cut at their logical end, with the ellipsis on the visual left.

Typing works too. In the finder, search, prompts and the `:` and `/` lines, Arabic appears shaped and in visual order as you type, with the caret on the right cell. In the editor's Insert mode you type Arabic as usual.

Output sent to a pipe is never reordered or shaped: `astrolabe find … | grep` sees the letters as they are in the file.

## Search that understands Arabic

Every search in Astrolabe CLI (finder, `astrolabe pick`, vault search, in-note `/`, the tag filter, `[[` and `:e` completion, `astrolabe find` and `ls`, and note resolution on the command line) folds Arabic the same way, so you can type the plain form of a word and find every spelling of it:

- harakat, tatweel and invisible control characters are ignored, so «النُّجوم» finds «النجوم»;
- أ إ آ ٱ match ا, ى matches ي, ة matches ه, and ؤ and ئ match و and ي;
- Persian letters match their Arabic counterparts, presentation forms match plain letters, and Arabic-Indic digits match 0 to 9.

Highlights land on the original letters, not on the folded ones. A search written `re:…` is a literal regular expression and is not folded. The fuzzy finder treats the word after the article and attached particles (ال، وال، بال، لل، و، ب، ل، ف، ك) as a word start, so «اسطرلاب» ranks «الأسطرلاب» high.
