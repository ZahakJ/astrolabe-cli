# Arabic and right-to-left text

Most terminals were built for text that runs left to right. An Arabic sentence printed into one of them usually comes out with its letters unjoined and its words in the wrong order. Astrolabe CLI fixes that itself, so Arabic notes read correctly even in terminals that do nothing for right-to-left text. This chapter explains what it does, how to check it in your terminal, and what is still imperfect. Persian and Hebrew go through the same path.

![An Arabic note in kitty: title, bullets, a quotation, a callout, a table and tasks, all aligned to the right, with the outline and backlinks panel on the side](shots/kitty-arabic.png)

*A real kitty window. Bullets, the quote bar and the fold chevron have moved to the right edge, because the paragraphs start with Arabic.*

## Two problems, from first principles

Take the word «قمر» (moon). In memory it is three letters stored in reading order: ق, then م, then ر. Two things must happen before it looks right on screen.

1. **Shaping.** Arabic letters change form depending on their neighbours: ق at the start of a word, م in the middle and ر at the end each have their own glyph. A program that prints the bare letters gets three isolated forms side by side, which reads like a word spelt out letter by letter.
2. **Ordering.** The word must be drawn from right to left, and a line that mixes Arabic with English or numbers needs the Unicode bidirectional algorithm to decide which run goes where. A terminal is a grid of cells filled from the left, so somebody has to work out which cell each letter goes in.

Some terminals do both jobs themselves, some do neither, and kitty does something in between. Astrolabe CLI has one behaviour for each kind.

## The three bidi modes

| Mode | For | What Astrolabe CLI does |
|---|---|---|
| `on` | terminals that do no bidi: foot, Alacritty, WezTerm, xterm, Windows Terminal and most others | shapes each word into its joined forms and reorders every line into visual order itself, so the terminal only has to put glyphs in cells |
| `runs` | kitty | the same as `on`, then writes each stretch of right-to-left letters pre-reversed, because kitty reverses such stretches once more while shaping; the two reversals cancel and the word reads correctly |
| `off` | terminals that implement bidi themselves: GNOME Terminal, Tilix and other VTE terminals, Konsole, Apple Terminal, mlterm | leaves the text in logical order and lets the terminal do the work |

The default is `auto`. It chooses `runs` in kitty (it looks at `TERM`, `KITTY_WINDOW_ID` and `TERM_PROGRAM`, and inside tmux it asks tmux which terminal is attached), `off` in the terminals listed above, and `on` everywhere else. To force one, use `--bidi on|off|runs`, the config key `bidi`, or `ASTROLABE_BIDI`.

Colours, highlights, the selection and the cursor stay on the right cells in every mode, because the reordering is applied last, on the finished screen rows.

## Check it in your terminal

```sh
astrolabe doctor
```

prints the bidi mode it chose and the reason, and the last line of its test card is an Arabic phrase. If it reads joined and from right to left, you are done. If it does not, your terminal was probably not recognised: try `astrolabe doctor --bidi runs`, then `--bidi on`, then `--bidi off`, and keep the one whose test line reads correctly by setting `bidi = …` in the config. A terminal that reverses right-to-left runs as kitty does, but is not detected, needs `runs`.

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

## What is still imperfect

- In `runs` mode, if your main font itself contains the Arabic letter forms, kitty folds punctuation that touches an Arabic word into the same run, and that punctuation can land on the other side of the word.
- Harakat are drawn by the terminal's font and can sit slightly off in some fonts; a vowel mark on a lam-alef ligature can be misplaced in kitty.
- Right-to-left text was checked by eye in real **kitty** and **foot** windows, directly and inside tmux. WezTerm, Alacritty, Ghostty, iTerm2, GNOME Terminal and Apple Terminal are untested.
- Konsole's own whole-line bidi moves text across the TUI's columns. The words read correctly with `bidi = off`, but Konsole is not recommended for right-to-left notes.

The same rules for the other surfaces are in [Terminals](terminals.md), and the full test record in [Verified on](verified.md).
