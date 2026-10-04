package export

import (
	"regexp"
	"strings"

	"github.com/ZahakJ/folio/internal/theme"
)

// Due date and priority markers, as the reader recognises them.
var (
	dueRe  = regexp.MustCompile(`\s*(?:📅\x{FE0F}?\s*|\bdue:\s*|@due\(\s*)(\d{4}-\d{2}-\d{2})\)?`)
	prioRe = regexp.MustCompile(`\s*(🔺|⏫|🔼|🔽|⏬)\x{FE0F}?`)
)

// palette writes the CSS custom properties for a theme.
func palette(t theme.Theme) string {
	c := func(x theme.Color) string { return x.String() }
	v := []struct{ k, v string }{
		{"ground", c(t.Ground)}, {"raised", c(t.Raised)}, {"hover", c(t.Hover)},
		{"text", c(t.Text)}, {"muted", c(t.Muted)}, {"faint", c(t.Faint)},
		{"heading", c(t.Heading)}, {"accent", c(t.Accent)}, {"accent-soft", c(t.AccentSoft)},
		{"border", c(t.Border)}, {"danger", c(t.Danger)}, {"ok", c(t.Ok)}, {"link", c(t.Link)},
		{"math", c(t.Math)}, {"code-comment", c(t.CodeComment)}, {"code-string", c(t.CodeString)},
		{"code-number", c(t.CodeNumber)}, {"code-keyword", c(t.CodeKeyword)},
	}
	var b strings.Builder
	for _, kv := range v {
		b.WriteString("  --" + kv.k + ": " + kv.v + ";\n")
	}
	for _, k := range theme.CalloutKinds() {
		b.WriteString("  --callout-" + k + ": " + t.CalloutColor(k).String() + ";\n")
	}
	return b.String()
}

// stylesheet is the page's inline CSS: the parchment theme by day, the
// iron-gall theme by night, a plain high-contrast page in print.
func stylesheet() string {
	var b strings.Builder
	b.WriteString(":root {\n  color-scheme: light dark;\n")
	b.WriteString(palette(theme.Parchment))
	b.WriteString("}\n@media (prefers-color-scheme: dark) {\n:root {\n")
	b.WriteString(palette(theme.IronGall))
	b.WriteString("}\n}\n")
	var callouts strings.Builder
	for _, k := range theme.CalloutKinds() {
		callouts.WriteString("aside.callout.callout-" + k + " { --hue: var(--callout-" + k + "); }\n")
	}
	b.WriteString(callouts.String())
	b.WriteString(baseCSS)
	return b.String()
}

const baseCSS = `* { box-sizing: border-box; }
html { background: var(--ground); }
body {
  margin: 0; padding: 3.5rem 1.25rem 5rem;
  background: var(--ground); color: var(--text);
  font-family: "Iowan Old Style", "Palatino Linotype", Palatino, "Book Antiqua", Charter, "Bitstream Charter", Georgia, "Noto Serif", serif;
  font-size: 1.125rem; line-height: 1.6;
  text-rendering: optimizeLegibility; font-kerning: normal;
  -webkit-text-size-adjust: 100%;
}
.page { max-width: 40rem; margin: 0 auto; }
p, ul, ol, blockquote, aside, figure, .table, pre, .math.display, details { margin: 0 0 1.1em; }
a { color: var(--link); text-decoration: none; border-bottom: 1px solid color-mix(in srgb, var(--link) 35%, transparent); }
a:hover { border-bottom-color: var(--link); }
a.external::after { content: "\2009\2197"; color: var(--faint); font-size: .85em; }
.link.broken { color: var(--danger); text-decoration: underline wavy color-mix(in srgb, var(--danger) 60%, transparent); text-underline-offset: .2em; }

.title-block { margin-bottom: 1.6rem; border-bottom: 1px solid var(--border); padding-bottom: .9rem; }
h1.title { border: 0; padding: 0; font-size: 2.1rem; line-height: 1.2; margin: 0 0 .4rem; color: var(--heading); font-weight: 600; letter-spacing: .005em; }
.chips { margin: 0; color: var(--muted); font-size: .95rem; font-variant-numeric: oldstyle-nums; }
.chips .dot { color: var(--faint); padding: 0 .3em; }
.tag { color: var(--muted); white-space: nowrap; }
.tag .hash { color: var(--accent); }
details.properties { color: var(--muted); font-size: .92rem; margin-bottom: 1.8rem; }
details.properties summary { color: var(--faint); cursor: pointer; list-style: none; }
details.properties summary::before { content: "\25B8\00a0"; }
details.properties[open] summary::before { content: "\25BE\00a0"; }
details.properties dl { display: grid; grid-template-columns: max-content 1fr; gap: .15rem 1.2rem; margin: .5rem 0 0 1.1rem; }
details.properties dt { color: var(--faint); }
details.properties dd { margin: 0; }

h1, h2, h3, h4, h5, h6 { line-height: 1.3; margin: 2rem 0 .7rem; font-weight: 600; }
h1 { font-size: 1.6rem; color: var(--heading); padding-bottom: .3rem; border-bottom: 1px solid var(--border); margin-top: 2.6rem; }
h2 { font-size: 1.3rem; color: var(--heading); margin-top: 2.6rem; }
h3 { font-size: 1.1rem; color: var(--text); }
h4, h5, h6 { font-size: 1rem; color: var(--muted); font-style: italic; font-weight: 400; }

strong { font-weight: 650; }
del { color: var(--faint); }
mark { background: var(--accent-soft); color: inherit; padding: 0 .15em; border-radius: 2px; }
code, pre, kbd, samp { font-family: "JetBrains Mono", "DejaVu Sans Mono", Menlo, Consolas, "Liberation Mono", monospace; font-size: .84em; }
:not(pre) > code { background: var(--raised); padding: .1em .35em; border-radius: 3px; }
kbd { border: 1px solid var(--border); border-bottom-width: 2px; border-radius: 3px; padding: 0 .3em; }
.math { color: var(--math); font-family: "DejaVu Sans Mono", Menlo, Consolas, monospace; font-size: .9em; }
.math.display { display: block; text-align: center; white-space: pre-wrap; padding: .4rem 0; }
.raw { color: var(--faint); font-family: monospace; font-size: .85em; }
pre.html { color: var(--faint); white-space: pre-wrap; }
sup.fnref a { border: 0; font-size: .75em; color: var(--accent); }

ul, ol { padding: 0; padding-inline-start: 1.6em; }
li { margin: .15em 0; }
li > p { margin: .3em 0; }
li > ul, li > ol, li > blockquote, li > figure { margin: .15em 0 .3em; }
ul { list-style: none; }
ul > li { position: relative; }
ul > li::before { content: "\2022"; color: var(--accent); position: absolute; inset-inline-start: -1.1em; }
ul ul > li::before { content: "\25E6"; }
ul ul ul > li::before { content: "\25AA"; }
ol > li::marker { color: var(--accent); font-variant-numeric: tabular-nums; }
ul.tasks > li::before, ul.tasks > li.task::before { content: none; }
ol.tasks { list-style: none; }
li.task { list-style: none; position: relative; }
li.task .box { color: var(--accent); position: absolute; inset-inline-start: -1.4em; font-family: "DejaVu Sans", sans-serif; }
li.task.done .box, li.task.cancelled .box { color: var(--faint); }
li.task.done > .text, li.task.cancelled > .text { color: var(--faint); text-decoration: line-through; }
.due { color: var(--muted); font-size: .9em; margin-inline-start: .4em; white-space: nowrap; }
.due.overdue { color: var(--danger); font-weight: 600; }
.priority { color: var(--danger); font-weight: 700; margin-inline-start: .3em; }

blockquote { margin-inline: 0; padding: 0; padding-inline-start: 1rem; border-inline-start: 3px solid var(--accent); color: var(--muted); font-style: italic; }
blockquote blockquote { margin-top: .6em; }
blockquote.embed { font-style: normal; color: var(--text); border-inline-start-color: var(--border); }
.embed-title { font-weight: 600; margin-bottom: .4em; }
.embed-title a, a.embed { color: var(--heading); }
.glyph { color: var(--accent); }

aside.callout { --hue: var(--callout-note); border-inline-start: 3px solid var(--hue); padding: .1rem 0; padding-inline-start: 1rem; }
aside.callout > :last-child, aside.callout details > :last-child { margin-bottom: 0; }
.callout-title { color: var(--hue); font-weight: 650; margin: 0 0 .35em; }
.callout-title .glyph { color: inherit; font-family: "DejaVu Sans", sans-serif; font-weight: 400; margin-inline-end: .2em; }
aside.callout details > summary { cursor: pointer; list-style: none; }
aside.callout details > summary::after { content: "\00a0\25B8"; color: var(--faint); font-weight: 400; }
aside.callout details[open] > summary::after { content: "\00a0\25BE"; }
aside.callout summary::-webkit-details-marker, details.properties summary::-webkit-details-marker { display: none; }

figure.code { direction: ltr; position: relative; background: var(--raised); border-radius: 4px; margin-left: 0; margin-right: 0; }
figure.code figcaption { position: absolute; top: .35rem; right: .7rem; direction: ltr; color: var(--faint); font-style: italic; font-size: .75rem; font-family: monospace; }
figure.code pre { margin: 0; padding: 1.4rem 1rem 1rem; overflow-x: auto; line-height: 1.45; color: var(--text); }
.tk-comment { color: var(--code-comment); font-style: italic; }
.tk-string { color: var(--code-string); }
.tk-number { color: var(--code-number); }
.tk-keyword { color: var(--code-keyword); }
.tk-header { font-weight: 700; }
.tk-added { color: var(--ok); }
.tk-removed { color: var(--danger); }
.tk-hunk { color: var(--accent); }
.tk-meta { color: var(--code-comment); }

.table { overflow-x: auto; }
table { border-collapse: collapse; font-size: .95em; font-variant-numeric: tabular-nums; }
th, td { padding: .3rem .9rem; vertical-align: top; text-align: start; }
th:first-child, td:first-child { padding-inline-start: 0; }
th { font-weight: 650; border-bottom: 1px solid var(--border); }
th + th, td + td { border-inline-start: 1px solid var(--border); }
.l { text-align: left; } .c { text-align: center; } .r { text-align: right; }

.ornament { text-align: center; color: var(--faint); margin: 2rem 0; letter-spacing: .6em; }
.ornament .star { color: var(--accent); }
img { max-width: 100%; height: auto; }

.footnotes { margin-top: 3rem; font-size: .92rem; color: var(--muted); }
.footnotes::before { content: ""; display: block; width: 6rem; border-top: 1px solid var(--border); margin-bottom: 1rem; }
.footnotes ol { padding-inline-start: 1.4em; }
.footnotes li::marker { color: var(--accent); }
.footnotes .back { border: 0; color: var(--faint); }

@media print {
  :root { --ground: #fff; --raised: #f4f1ea; --text: #111; --muted: #444; --faint: #777; --heading: #111; --link: #111; --border: #bbb; }
  body { padding: 0; font-size: 11pt; }
  .page { max-width: none; }
  a { border-bottom: 0; }
  a.external[href]::after { content: " (" attr(href) ")"; font-size: .8em; }
  figure.code pre { white-space: pre-wrap; overflow: visible; }
  h1, h2, h3, h4 { break-after: avoid; }
  figure, table, aside, blockquote { break-inside: avoid; }
  details.properties { display: none; }
}
`
