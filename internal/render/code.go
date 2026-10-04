package render

import (
	"strings"

	"github.com/ZahakJ/astrolabe-cli/internal/md"
	"github.com/ZahakJ/astrolabe-cli/internal/text"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
)

// code renders a code block: a raised block w cells wide with one cell of
// padding, a faint language label on the top-right, highlighted lines
// clipped (not wrapped) with a faint ellipsis. Without a raised ground the
// block is framed by a faint bar instead.
func (r *renderer) code(b *md.CodeBlock, w int) []row {
	ground := r.codeGround()
	base := theme.Style{FG: r.t.Text, BG: ground.BG}
	label := cleanText(b.Lang)
	if label == "" && b.Info != "" {
		label = cleanText(strings.Fields(b.Info + " x")[0])
	}
	var lead, trail []sp
	if r.raised {
		lead = []sp{mk(" ", ground)}
		trail = []sp{mk(" ", ground)}
	} else {
		lead = []sp{mk(r.g.VLine+" ", r.faint())}
	}
	view := w - spansW(lead) - spansW(trail)
	if view < 4 {
		view = max(1, w-spansW(lead))
		trail = nil
	}
	ellSt := r.faint()
	ellSt.BG = ground.BG

	hl := newHighlighter(b.Lang, r.t)
	var rows []row
	labelRow := func(src int) row {
		lw := text.Width(label)
		var spans []sp
		spans = append(spans, lead...)
		if lw > 0 && lw <= view {
			spans = append(spans, mk(spaces(view-lw), ground))
			ls := r.faint()
			ls.BG = ground.BG
			ls.Attrs |= theme.Italic
			spans = append(spans, mk(label, ls))
		} else if r.raised {
			spans = append(spans, mk(spaces(view), ground))
		}
		spans = append(spans, trail...)
		return row{spans: trimIfPlain(spans, r.raised), src0: src, src1: src, kind: KindCode, blockID: b.ID}
	}
	// Top padding row carries the label; the bottom row balances it.
	if r.raised || label != "" {
		rows = append(rows, labelRow(b.StartLine))
	}
	for i, line := range b.Lines {
		src := b.FirstLine + i
		content := hl.line(cleanCode(line), base)
		cw := spansW(content)
		vis := toSpans(clipViewSp(content, cw, view, r.g.Ellipsis, ellSt))
		spans := append(append(append([]sp(nil), lead...), vis...), trail...)
		rows = append(rows, row{spans: trimIfPlain(spans, r.raised), src0: src, src1: src, kind: KindCode,
			blockID: b.ID, code: &codeRow{x: spansW(lead), view: view, content: content, width: cw}})
	}
	if len(b.Lines) == 0 && r.raised {
		rows = append(rows, labelRow(b.StartLine))
		rows[len(rows)-1].spans = trimIfPlain(append(append(append([]sp(nil), lead...), mk(spaces(view), ground)), trail...), true)
	}
	if r.raised {
		end := append(append(append([]sp(nil), lead...), mk(spaces(view), ground)), trail...)
		rows = append(rows, row{spans: end, src0: b.EndLine, src1: b.EndLine, kind: KindCode, blockID: b.ID})
	}
	if len(rows) == 0 {
		// An empty block without a ground: a lone faint bar.
		rows = append(rows, row{spans: trimIfPlain(append([]sp(nil), lead...), false), src0: b.StartLine,
			src1: b.EndLine, kind: KindCode, blockID: b.ID})
	}
	rows[0].src0 = b.StartLine
	rows[len(rows)-1].src1 = max(rows[len(rows)-1].src1, b.EndLine)
	return rows
}

// trimIfPlain drops trailing blank spans of unpainted code lines.
func trimIfPlain(spans []sp, raised bool) []sp {
	if raised {
		return spans
	}
	return trimSpaces(spans)
}

// clipViewSp clips internal spans for the initial (unscrolled) viewport,
// padding to the full view so the raised ground is a clean rectangle.
func clipViewSp(content []sp, width, view int, ell string, ellSt theme.Style) []Span {
	pub := make([]Span, len(content))
	for i, s := range content {
		pub[i] = Span{Text: s.Text, Style: s.Style.st}
	}
	if width <= view {
		out := pub
		if width < view {
			st := theme.Style{BG: ellSt.BG}
			out = append(out, Span{Text: spaces(view - width), Style: st})
		}
		return out
	}
	return clipView(pub, width, 0, view, ell, ellSt)
}

func toSpans(in []Span) []sp {
	out := make([]sp, len(in))
	for i, s := range in {
		out[i] = sp{Text: s.Text, Style: attr{st: s.Style, line: -1}}
	}
	return out
}

// ---------------------------------------------------------------------------
// Highlighter

// langSpec describes the lexical classes the small highlighter knows.
type langSpec struct {
	line      []string    // line comment openers
	block     [][2]string // block comment delimiters
	quotes    string      // single-line string quotes
	multi     []string    // multi-line string delimiters (opener = closer)
	keywords  map[string]bool
	fold      bool // keywords are case-insensitive
	hashWord  bool // '#' opens a comment only at a word start (sh, yaml)
	keyColon  bool // a word or string followed by ':' is a key (yaml, json)
	diff      bool
	backslash bool // backslash escapes inside strings
}

func words(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

var cKeywords = "auto break case char const continue default do double else enum extern float for goto if inline int long register restrict return short signed sizeof static struct switch typedef union unsigned void volatile while bool true false NULL nullptr class namespace template typename public private protected virtual override new delete this using try catch throw operator friend constexpr noexcept static_cast dynamic_cast reinterpret_cast const_cast std include define ifdef ifndef endif pragma"

var langs = map[string]*langSpec{
	"sh": {line: []string{"#"}, quotes: `"'`, hashWord: true, backslash: true,
		keywords: words("if then else elif fi for while until do done case esac in function return local export readonly set unset shift exit break continue select time trap source alias declare eval exec test echo printf cd read")},
	"python": {line: []string{"#"}, quotes: `"'`, multi: []string{`"""`, `'''`}, backslash: true,
		keywords: words("False None True and as assert async await break class continue def del elif else except finally for from global if import in is lambda nonlocal not or pass raise return try while with yield match case self print len range")},
	"go": {line: []string{"//"}, block: [][2]string{{"/*", "*/"}}, quotes: `"'`, multi: []string{"`"}, backslash: true,
		keywords: words("break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var nil true false iota bool byte error int int8 int16 int32 int64 uint uint8 uint16 uint32 uint64 uintptr float32 float64 string rune any make new len cap append copy delete panic recover")},
	"js": {line: []string{"//"}, block: [][2]string{{"/*", "*/"}}, quotes: `"'`, multi: []string{"`"}, backslash: true,
		keywords: words("break case catch class const continue debugger default delete do else export extends finally for function if import in instanceof let new return super switch this throw try typeof var void while with yield async await of static get set null undefined true false interface type enum implements private public protected readonly as from declare namespace abstract keyof number string boolean any unknown never")},
	"json": {quotes: `"`, keyColon: true, backslash: true, keywords: words("true false null")},
	"yaml": {line: []string{"#"}, quotes: `"'`, hashWord: true, keyColon: true,
		keywords: words("true false null yes no on off True False Null Yes No ~")},
	"c": {line: []string{"//"}, block: [][2]string{{"/*", "*/"}}, quotes: `"'`, backslash: true, keywords: words(cKeywords)},
	"rust": {line: []string{"//"}, block: [][2]string{{"/*", "*/"}}, quotes: `"`, backslash: true,
		keywords: words("as async await break const continue crate dyn else enum extern false fn for if impl in let loop match mod move mut pub ref return self Self static struct super trait true type unsafe use where while i8 i16 i32 i64 i128 isize u8 u16 u32 u64 u128 usize f32 f64 bool char str String Vec Option Some None Result Ok Err Box")},
	"lua": {line: []string{"--"}, block: [][2]string{{"--[[", "]]"}}, quotes: `"'`, multi: []string{}, backslash: true,
		keywords: words("and break do else elseif end false for function goto if in local nil not or repeat return then true until while self require")},
	"sql": {line: []string{"--"}, block: [][2]string{{"/*", "*/"}}, quotes: `'"`, fold: true,
		keywords: words("select from where and or not insert into values update set delete create table drop alter add index view join left right inner outer full cross on as group by order having limit offset distinct union all exists in is null like between case when then else end primary key foreign references default unique check constraint begin commit rollback transaction with recursive returning integer int text varchar char boolean date timestamp real float numeric count sum avg min max asc desc if replace")},
	"diff": {diff: true},
}

var langAliases = map[string]string{
	"sh": "sh", "bash": "sh", "zsh": "sh", "shell": "sh", "console": "sh", "ksh": "sh", "fish": "sh",
	"python": "python", "py": "python", "python3": "python",
	"go": "go", "golang": "go",
	"js": "js", "javascript": "js", "jsx": "js", "mjs": "js", "cjs": "js", "ts": "js", "typescript": "js", "tsx": "js",
	"json": "json", "jsonc": "json", "json5": "json",
	"yaml": "yaml", "yml": "yaml",
	"c": "c", "h": "c", "cpp": "c", "c++": "c", "cc": "c", "cxx": "c", "hpp": "c", "hh": "c", "objc": "c",
	"rust": "rust", "rs": "rust",
	"lua": "lua",
	"sql": "sql", "sqlite": "sql", "postgres": "sql", "postgresql": "sql", "mysql": "sql",
	"diff": "diff", "patch": "diff", "udiff": "diff",
}

// HighlightLanguages lists the canonical names of the languages the
// built-in highlighter understands (aliases such as "bash" or "ts" map onto
// them).
func HighlightLanguages() []string {
	return []string{"sh", "python", "go", "js", "json", "yaml", "c", "rust", "lua", "sql", "diff"}
}

// TokenClass is the lexical class of a highlighted token.
type TokenClass uint8

// Token classes. Diff lines are classified whole.
const (
	TokPlain TokenClass = iota
	TokComment
	TokString
	TokNumber
	TokKeyword
	TokHeader  // diff file header (+++ / ---)
	TokAdded   // diff added line
	TokRemoved // diff removed line
	TokHunk    // diff hunk header (@@)
	TokMeta    // diff metadata (diff …, index …)
)

var tokenClassNames = [...]string{"plain", "comment", "string", "number", "keyword", "header", "added", "removed", "hunk", "meta"}

// String returns the class name ("comment", "string", …), suitable as a
// CSS class.
func (c TokenClass) String() string {
	if int(c) < len(tokenClassNames) {
		return tokenClassNames[c]
	}
	return "plain"
}

// Token is a run of source text of one class.
type Token struct {
	Text  string
	Class TokenClass
}

// Highlight tokenises the lines of a code block in language lang with the
// built-in highlighter (HighlightLanguages; aliases accepted). Unknown
// languages yield one plain token per non-empty line. Block comments and
// multi-line strings carry over from line to line. Lines should be
// tab-expanded; they are not otherwise altered.
func Highlight(lang string, lines []string) [][]Token {
	h := newHighlighter(lang, theme.Theme{})
	out := make([][]Token, len(lines))
	for i, l := range lines {
		out[i] = h.tokens(l)
	}
	return out
}

// highlighter tokenises the lines of one code block, carrying block
// comment and multi-line string state from line to line.
type highlighter struct {
	spec *langSpec
	t    theme.Theme
	// open is the closer we are waiting for ("" when none) and inComment
	// tells whether it closes a comment or a string.
	open      string
	inComment bool
}

func newHighlighter(lang string, t theme.Theme) *highlighter {
	h := &highlighter{t: t}
	if name, ok := langAliases[strings.ToLower(lang)]; ok {
		h.spec = langs[name]
	}
	return h
}

func isIdent(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// line highlights one (tab-expanded, sanitised) line over base.
func (h *highlighter) line(s string, base theme.Style) []sp {
	toks := h.tokens(s)
	out := make([]sp, len(toks))
	for i, tk := range toks {
		out[i] = mk(tk.Text, h.style(tk.Class, base))
	}
	return out
}

// style maps a token class to the theme's code inks over base.
func (h *highlighter) style(c TokenClass, base theme.Style) theme.Style {
	t := h.t
	switch c {
	case TokComment:
		st := base.Fg(t.CodeComment)
		if t.CodeComment.IsDefault() {
			st = base.With(theme.Faint)
		}
		return st.With(theme.Italic)
	case TokString:
		return base.Fg(t.CodeString)
	case TokNumber:
		return base.Fg(t.CodeNumber)
	case TokKeyword:
		if t.CodeKeyword.IsDefault() {
			return base.With(theme.Bold)
		}
		return base.Fg(t.CodeKeyword)
	case TokHeader:
		return base.With(theme.Bold)
	case TokAdded:
		return base.Fg(t.Ok)
	case TokRemoved:
		return base.Fg(t.Danger)
	case TokHunk:
		return base.Fg(t.Accent)
	case TokMeta:
		if t.CodeComment.IsDefault() {
			return base.With(theme.Faint)
		}
		return base.Fg(t.CodeComment)
	}
	return base
}

// tokens splits one line into classified tokens.
func (h *highlighter) tokens(s string) []Token {
	if s == "" {
		return nil
	}
	if h.spec == nil {
		return []Token{{s, TokPlain}}
	}
	ls := h.spec
	const (
		base = TokPlain
		cmt  = TokComment
		str  = TokString
		num  = TokNumber
		kw   = TokKeyword
	)
	if ls.diff {
		switch {
		case strings.HasPrefix(s, "+++"), strings.HasPrefix(s, "---"):
			return []Token{{s, TokHeader}}
		case strings.HasPrefix(s, "+"):
			return []Token{{s, TokAdded}}
		case strings.HasPrefix(s, "-"):
			return []Token{{s, TokRemoved}}
		case strings.HasPrefix(s, "@@"):
			return []Token{{s, TokHunk}}
		case strings.HasPrefix(s, "diff "), strings.HasPrefix(s, "index "):
			return []Token{{s, TokMeta}}
		}
		return []Token{{s, TokPlain}}
	}

	var out []Token
	emit := func(t string, c TokenClass) {
		if t == "" {
			return
		}
		if n := len(out); n > 0 && out[n-1].Class == c {
			out[n-1].Text += t
			return
		}
		out = append(out, Token{t, c})
	}
	i := 0
	// Continue an open block comment or multi-line string.
	if h.open != "" {
		st := TokenClass(str)
		if h.inComment {
			st = cmt
		}
		j := strings.Index(s, h.open)
		if j < 0 {
			return []Token{{s, st}}
		}
		i = j + len(h.open)
		emit(s[:i], st)
		h.open = ""
	}
	start := i // start of pending plain text
	flush := func(to int) {
		if to > start {
			emit(s[start:to], base)
		}
	}
	atWordStart := func(i int) bool { return i == 0 || s[i-1] == ' ' || s[i-1] == '\t' }
	for i < len(s) {
		c := s[i]
		// Block comments.
		matched := false
		for _, bc := range ls.block {
			if strings.HasPrefix(s[i:], bc[0]) {
				flush(i)
				j := strings.Index(s[i+len(bc[0]):], bc[1])
				if j < 0 {
					emit(s[i:], cmt)
					h.open, h.inComment = bc[1], true
					return out
				}
				end := i + len(bc[0]) + j + len(bc[1])
				emit(s[i:end], cmt)
				i, start = end, end
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		// Line comments.
		for _, lc := range ls.line {
			if strings.HasPrefix(s[i:], lc) && (!ls.hashWord || atWordStart(i)) {
				flush(i)
				emit(s[i:], cmt)
				return out
			}
		}
		// Multi-line strings.
		for _, m := range ls.multi {
			if strings.HasPrefix(s[i:], m) {
				flush(i)
				j := strings.Index(s[i+len(m):], m)
				if j < 0 {
					emit(s[i:], str)
					h.open, h.inComment = m, false
					return out
				}
				end := i + len(m) + j + len(m)
				emit(s[i:end], str)
				i, start = end, end
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		switch {
		case strings.IndexByte(ls.quotes, c) >= 0 && !(c == '\'' && i > 0 && isIdent(s[i-1])):
			flush(i)
			j := i + 1
			for j < len(s) && s[j] != c {
				if ls.backslash && s[j] == '\\' {
					j++
				}
				j++
			}
			end := min(j+1, len(s))
			st := TokenClass(str)
			if ls.keyColon && isKeyAfter(s, end) {
				st = kw
			}
			emit(s[i:end], st)
			i, start = end, end
		case isDigit(c) && (i == 0 || !isIdent(s[i-1])):
			flush(i)
			j := i + 1
			for j < len(s) && (isIdent(s[j]) || s[j] == '.' && j+1 < len(s) && isDigit(s[j+1])) {
				j++
			}
			emit(s[i:j], num)
			i, start = j, j
		case isIdent(c) && (i == 0 || !isIdent(s[i-1])):
			j := i + 1
			for j < len(s) && (isIdent(s[j]) || (ls.keyColon && (s[j] == '-' || s[j] == '.'))) {
				j++
			}
			word := s[i:j]
			isKw := ls.keywords[word] || (ls.fold && ls.keywords[strings.ToLower(word)])
			if (ls.keyColon && isKeyAfter(s, j) && strings.TrimSpace(strings.TrimLeft(s[:i], " -")) == "") || isKw {
				flush(i)
				emit(word, kw)
				start = j
			}
			i = j
		default:
			i++
		}
	}
	flush(len(s))
	return out
}

// isKeyAfter reports whether s[i:] begins with optional blanks and ':'.
func isKeyAfter(s string, i int) bool {
	for i < len(s) && s[i] == ' ' {
		i++
	}
	return i < len(s) && s[i] == ':'
}
