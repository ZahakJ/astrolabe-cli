package render

import (
	"strings"

	"github.com/ZahakJ/astrolabe-cli/internal/md"
	"github.com/ZahakJ/astrolabe-cli/internal/term"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
)

// ANSI serialises the page for a terminal: each line's spans as SGR
// sequences for enc's colour profile (OSC 8 hyperlinks when
// enc.Hyperlinks), one "\n"-terminated line per page line. Lines are not
// padded beyond what the page holds (see Options.PaintGround).
func (p *Page) ANSI(enc term.Encoder) string {
	var b []byte
	for _, l := range p.Lines {
		spans := l.Spans
		if !l.hasGround() {
			spans = trimRight(spans)
		}
		b = enc.AppendSpans(b, spans)
		b = append(b, '\n')
	}
	return string(b)
}

// Plain serialises the page as plain text: no escapes, trailing blanks
// removed, glyphs as rendered (render with theme.ASCIIGlyphs for ASCII
// output). Render with Options.Bidi off for pipes: plain output is never
// reordered or shaped.
func (p *Page) Plain() string {
	var b strings.Builder
	for _, l := range p.Lines {
		b.WriteString(strings.TrimRight(l.Text(), " "))
		b.WriteByte('\n')
	}
	return b.String()
}

// hasGround reports whether the line paints a background anywhere (its
// trailing blanks then matter).
func (l Line) hasGround() bool {
	for _, s := range l.Spans {
		if !s.Style.BG.IsDefault() {
			return true
		}
	}
	return false
}

// trimRight drops trailing unstyled blanks so piped ANSI output carries no
// invisible padding.
func trimRight(spans []Span) []Span {
	for len(spans) > 0 {
		last := spans[len(spans)-1]
		t := strings.TrimRight(last.Text, " ")
		if t == last.Text {
			return spans
		}
		if last.Style.Attrs.AnyUnderline() || last.Style.Attrs&(theme.Reverse|theme.Strike) != 0 {
			return spans
		}
		spans = spans[: len(spans)-1 : len(spans)-1]
		if t != "" {
			return append(spans, Span{Text: t, Style: last.Style})
		}
	}
	return spans
}

// ANSI parses src, renders it with opt and serialises it for enc: the
// one-call form behind `astrolabe render` on a terminal.
func ANSI(src string, opt Options, enc term.Encoder) string {
	return Render(md.Parse(src), opt).ANSI(enc)
}

// Plain parses src, renders it with opt (bidi forced off) and returns
// plain text: the form behind `astrolabe render` into a pipe.
func Plain(src string, opt Options) string {
	opt.Bidi = false
	opt.PaintGround = false
	opt.Theme = opt.Theme.Mono()
	return Render(md.Parse(src), opt).Plain()
}
