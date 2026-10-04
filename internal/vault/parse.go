package vault

import (
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// This file is the light line scanner of DESIGN.md §3: it extracts the link
// graph, tags, tasks and outline from a note without building an AST. It
// understands frontmatter, fenced code blocks (``` and ~~~, also indented or
// inside block quotes), inline code spans and %% / <!-- --> comments, which
// is what is needed to avoid false links and tags.

// parseNote indexes the content of the note at vault-relative path rel.
func parseNote(rel string, data []byte, mod time.Time, size int64) *Note {
	n := &Note{Path: rel, ModTime: mod, Size: size}
	if size > LargeFileSize || int64(len(data)) > LargeFileSize {
		head := data
		if len(head) > 64<<10 {
			head = head[:64<<10]
			if i := lastIndexByte(head, '\n'); i >= 0 {
				head = head[:i+1]
			}
		}
		p := parseNote(rel, head, mod, int64(len(head)))
		n.Title, n.TitleLine = p.Title, p.TitleLine
		n.Large = true
		return n
	}
	s := string(data)
	s = strings.TrimPrefix(s, "\uFEFF")
	sc := scanner{note: n, seenTag: map[string]bool{}}
	sc.run(s)
	if n.Title == "" {
		n.Title = titleFromPath(rel)
		n.TitleLine = 0
	}
	return n
}

func lastIndexByte(b []byte, c byte) int {
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] == c {
			return i
		}
	}
	return -1
}

// titleFromPath is the file name without directory and ".md".
func titleFromPath(rel string) string {
	base := path.Base(rel)
	if strings.EqualFold(path.Ext(base), ".md") {
		base = base[:len(base)-3]
	}
	return base
}

type scanner struct {
	note    *Note
	seenTag map[string]bool
	h1      string
	h1Line  int
	fmTitle bool

	// fence state
	inFence  bool
	fenceCh  byte
	fenceLen int
	// comment state across lines
	mask maskState
	// setext tracking: the previous line if it could be a paragraph
	prevPara     string
	prevParaLine int
	lastNonBlank int
}

func (sc *scanner) run(s string) {
	n := sc.note
	lineNo := 0
	bodyStart := 0
	// Frontmatter.
	if nl := strings.IndexByte(s, '\n'); nl >= 0 && strings.HasPrefix(s, "---") {
		first := strings.TrimRight(s[:nl], " \t\r")
		if first == "---" {
			if end, consumed, ok := sc.frontmatter(s[nl+1:]); ok {
				n.FrontmatterEnd = end
				lineNo = end
				bodyStart = nl + 1 + consumed
			}
		}
	}
	body := s[bodyStart:]
	for len(body) > 0 {
		var line string
		line, body, _ = cutLine(body)
		lineNo++
		sc.line(line, lineNo)
	}
	n.Lines = lineNo
	if !sc.fmTitle && sc.h1 != "" {
		n.Title, n.TitleLine = sc.h1, sc.h1Line
	}
}

// cutLine splits s at the first '\n', dropping a trailing '\r' from the
// line. found is false when s has no newline (the last line).
func cutLine(s string) (line, rest string, found bool) {
	i := strings.IndexByte(s, '\n')
	if i < 0 {
		line, rest = s, ""
	} else {
		line, rest, found = s[:i], s[i+1:], true
	}
	if strings.HasSuffix(line, "\r") {
		line = line[:len(line)-1]
	}
	return line, rest, found
}

// frontmatter parses the YAML block that follows the opening "---" line.
// It returns the 1-based line number (counting the opening line as 1) of the
// closing delimiter and the number of bytes of rest consumed through it.
func (sc *scanner) frontmatter(rest string) (end, consumed int, ok bool) {
	type item struct {
		line int
		text string
	}
	var lines []item
	off := 0
	ln := 1
	for off < len(rest) {
		line, _, found := cutLine(rest[off:])
		adv := len(line)
		if found {
			adv = strings.IndexByte(rest[off:], '\n') + 1
		}
		off += adv
		ln++
		t := strings.TrimRight(line, " \t")
		if t == "---" || t == "..." {
			sc.applyFrontmatter(func(yield func(int, string)) {
				for _, it := range lines {
					yield(it.line, it.text)
				}
			})
			return ln, off, true
		}
		lines = append(lines, item{ln, line})
		if !found {
			break
		}
	}
	return 0, 0, false
}

func (sc *scanner) applyFrontmatter(each func(func(int, string))) {
	n := sc.note
	type kv struct {
		key   string
		value string
		list  []string
		line  int
		isLst bool
	}
	var kvs []*kv
	var cur *kv
	each(func(ln int, line string) {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			return
		}
		if line[0] == ' ' || line[0] == '\t' || line[0] == '-' {
			t := strings.TrimSpace(line)
			if cur != nil && strings.HasPrefix(t, "-") {
				cur.list = append(cur.list, unquote(strings.TrimSpace(t[1:])))
				cur.isLst = true
			} else if cur != nil && !cur.isLst {
				// folded/continued scalar
				if cur.value == "" || cur.value == "|" || cur.value == ">" || cur.value == "|-" || cur.value == ">-" {
					cur.value = t
				} else {
					cur.value += " " + t
				}
			}
			return
		}
		i := strings.IndexByte(line, ':')
		if i <= 0 {
			cur = nil
			return
		}
		key := strings.TrimSpace(line[:i])
		val := strings.TrimSpace(line[i+1:])
		cur = &kv{key: key, value: val, line: ln}
		if strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]") {
			cur.isLst = true
			for _, p := range strings.Split(val[1:len(val)-1], ",") {
				if p = unquote(strings.TrimSpace(p)); p != "" {
					cur.list = append(cur.list, p)
				}
			}
			cur.value = ""
		}
		kvs = append(kvs, cur)
	})
	for _, e := range kvs {
		val := unquote(e.value)
		display := val
		if e.isLst {
			display = strings.Join(e.list, ", ")
		}
		n.Properties = append(n.Properties, Property{Key: e.key, Value: display, Line: e.line})
		switch strings.ToLower(e.key) {
		case "title":
			if val != "" && !sc.fmTitle {
				n.Title, n.TitleLine, sc.fmTitle = val, e.line, true
			}
		case "tags", "tag":
			items := e.list
			if !e.isLst {
				items = strings.FieldsFunc(val, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
			}
			for _, t := range items {
				for _, f := range strings.FieldsFunc(t, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
					sc.addTag(strings.TrimPrefix(f, "#"))
				}
			}
		case "aliases", "alias":
			items := e.list
			if !e.isLst && val != "" {
				items = strings.Split(val, ",")
			}
			for _, a := range items {
				if a = strings.TrimSpace(a); a != "" {
					n.Aliases = append(n.Aliases, a)
				}
			}
		case "date":
			if val != "" {
				n.DateRaw = val
				n.Date = parseFrontDate(val)
			}
		case "created":
			if n.DateRaw == "" && val != "" {
				n.DateRaw = val
				n.Date = parseFrontDate(val)
			}
		}
	}
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

var frontDateLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04",
	"2006-01-02 15:04",
	"2006-01-02",
}

func parseFrontDate(s string) time.Time {
	for _, l := range frontDateLayouts {
		if t, err := time.ParseInLocation(l, s, time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (sc *scanner) addTag(t string) {
	t = strings.Trim(t, "/")
	if t == "" || allDigits(t) {
		return
	}
	k := strings.ToLower(t)
	if sc.seenTag[k] {
		return
	}
	sc.seenTag[k] = true
	sc.note.Tags = append(sc.note.Tags, t)
}

func allDigits(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) && r != '/' {
			return false
		}
	}
	return true
}

// line processes one body line.
func (sc *scanner) line(line string, ln int) {
	n := sc.note
	n.Words += countWords(line)

	// Fences.
	if ch, cnt, rest, ok := fenceInfo(line); ok {
		if sc.inFence {
			if ch == sc.fenceCh && cnt >= sc.fenceLen && strings.TrimSpace(rest) == "" {
				sc.inFence = false
			}
		} else {
			sc.inFence, sc.fenceCh, sc.fenceLen = true, ch, cnt
		}
		sc.prevPara = ""
		sc.lastNonBlank = ln
		return
	}
	if sc.inFence {
		return
	}

	masked := maskLine(line, &sc.mask)
	trimmed := strings.TrimSpace(masked)
	if trimmed == "" {
		sc.prevPara = ""
		return
	}

	// Block anchors.
	if strings.IndexByte(masked, '^') >= 0 {
		if m := blockIDRe.FindStringSubmatchIndex(masked); m != nil {
			id := masked[m[2]:m[3]]
			target := ln
			if strings.TrimSpace(masked[:m[0]]) == "" && sc.lastNonBlank > 0 {
				target = sc.lastNonBlank // a lone "^id" line names the block above
			}
			n.Blocks = append(n.Blocks, Block{ID: id, Line: target})
		}
	}

	// Headings.
	isHeading := false
	if lvl, text, ok := atxHeading(masked); ok {
		if _, t, ok2 := atxHeading(line); ok2 {
			text = t
		}
		otext := stripBlockID(text)
		n.Headings = append(n.Headings, Heading{Level: lvl, Text: otext, Line: ln})
		if lvl == 1 && sc.h1 == "" && otext != "" {
			sc.h1, sc.h1Line = otext, ln
		}
		isHeading = true
		sc.prevPara = ""
	} else if lvl := setextUnderline(masked); lvl > 0 && sc.prevPara != "" {
		text := stripBlockID(strings.TrimSpace(sc.prevPara))
		n.Headings = append(n.Headings, Heading{Level: lvl, Text: text, Line: sc.prevParaLine})
		if lvl == 1 && sc.h1 == "" {
			sc.h1, sc.h1Line = text, sc.prevParaLine
		}
		sc.prevPara = ""
		sc.lastNonBlank = ln
		return
	}

	// Tasks.
	if !isHeading {
		if col, w, ok := taskLine(masked); ok {
			raw := strings.TrimSpace(line[col+w+1:])
			st, _ := utf8.DecodeRuneInString(line[col:])
			t := Task{State: st, Raw: raw, Line: ln, Col: col}
			t.Text, t.Due, t.Priority = taskMeta(raw)
			n.Tasks = append(n.Tasks, t)
		}
	}

	// Links, then tags on what remains.
	rest := extractLinks(masked, ln, line, func(l Link) { n.Links = append(n.Links, l) })
	extractTags(rest, sc.addTag)

	if !isHeading && isParagraphLine(masked) {
		sc.prevPara, sc.prevParaLine = line, ln
	} else {
		sc.prevPara = ""
	}
	sc.lastNonBlank = ln
}

var blockIDRe = regexp.MustCompile(`(?:^|\s)\^([A-Za-z0-9-]+)\s*$`)

func stripBlockID(s string) string {
	if m := blockIDRe.FindStringIndex(s); m != nil {
		return strings.TrimSpace(s[:m[0]])
	}
	return s
}

// fenceInfo reports whether line is a code fence line (opening or closing),
// returning the fence character, its run length and the text after it.
// Indentation and block-quote markers are allowed before the fence so that
// fences nested in list items and quotes are honoured.
func fenceInfo(line string) (ch byte, n int, rest string, ok bool) {
	t := strings.TrimLeft(line, " \t")
	for strings.HasPrefix(t, ">") {
		t = strings.TrimLeft(t[1:], " \t")
	}
	if len(t) < 3 || (t[0] != '`' && t[0] != '~') {
		return 0, 0, "", false
	}
	ch = t[0]
	for n < len(t) && t[n] == ch {
		n++
	}
	if n < 3 {
		return 0, 0, "", false
	}
	rest = t[n:]
	if ch == '`' && strings.IndexByte(rest, '`') >= 0 {
		return 0, 0, "", false
	}
	return ch, n, rest, true
}

// maskState carries multi-line comment state between lines.
type maskState struct {
	pct  bool // inside %% ... %%
	html bool // inside <!-- ... -->
}

// maskLine returns line with inline code spans and comments replaced by
// spaces (byte offsets are preserved), updating st for comments that span
// lines.
func maskLine(line string, st *maskState) string {
	var b []byte // lazily allocated copy
	blank := func(from, to int) {
		if b == nil {
			b = []byte(line)
		}
		for i := from; i < to; i++ {
			b[i] = ' '
		}
	}
	i := 0
	for i < len(line) {
		if st.pct {
			j := strings.Index(line[i:], "%%")
			if j < 0 {
				blank(i, len(line))
				i = len(line)
				break
			}
			blank(i, i+j+2)
			i += j + 2
			st.pct = false
			continue
		}
		if st.html {
			j := strings.Index(line[i:], "-->")
			if j < 0 {
				blank(i, len(line))
				i = len(line)
				break
			}
			blank(i, i+j+3)
			i += j + 3
			st.html = false
			continue
		}
		c := line[i]
		switch {
		case c == '\\' && i+1 < len(line):
			i += 2
			continue
		case c == '`':
			n := 0
			for i+n < len(line) && line[i+n] == '`' {
				n++
			}
			if end := findBacktickRun(line, i+n, n); end >= 0 {
				blank(i, end+n)
				i = end + n
			} else {
				i += n
			}
			continue
		case c == '%' && strings.HasPrefix(line[i:], "%%"):
			st.pct = true
			blank(i, i+2)
			i += 2
			continue
		case c == '<' && strings.HasPrefix(line[i:], "<!--"):
			st.html = true
			blank(i, i+4)
			i += 4
			continue
		}
		i++
	}
	if b == nil {
		return line
	}
	return string(b)
}

// findBacktickRun finds the start of the next run of exactly n backticks in
// s at or after from, or -1.
func findBacktickRun(s string, from, n int) int {
	for i := from; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		j := i
		for j < len(s) && s[j] == '`' {
			j++
		}
		if j-i == n {
			return i
		}
		i = j
	}
	return -1
}

// atxHeading parses "# text" headings (up to three spaces of indentation).
func atxHeading(line string) (level int, text string, ok bool) {
	i := 0
	for i < len(line) && line[i] == ' ' {
		i++
	}
	if i > 3 {
		return 0, "", false
	}
	n := 0
	for i+n < len(line) && line[i+n] == '#' {
		n++
	}
	if n == 0 || n > 6 {
		return 0, "", false
	}
	rest := line[i+n:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return 0, "", false
	}
	text = strings.TrimSpace(rest)
	if j := strings.TrimRight(text, "#"); j != text {
		if j == "" {
			text = ""
		} else if strings.HasSuffix(j, " ") || strings.HasSuffix(j, "\t") {
			text = strings.TrimSpace(j)
		}
	}
	return n, text, true
}

// setextUnderline returns 1 for a "===" line, 2 for a "---" line, else 0.
func setextUnderline(line string) int {
	t := strings.TrimRight(line, " \t")
	lead := len(t) - len(strings.TrimLeft(t, " "))
	if lead > 3 {
		return 0
	}
	t = t[lead:]
	if t == "" {
		return 0
	}
	if strings.Trim(t, "=") == "" {
		return 1
	}
	if len(t) >= 2 && strings.Trim(t, "-") == "" {
		return 2
	}
	return 0
}

// isParagraphLine reports whether a (masked) line can be the text of a
// setext heading: plain text, not a list item, quote, table row or
// indented code.
func isParagraphLine(line string) bool {
	if strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
		return false
	}
	t := strings.TrimLeft(line, " ")
	if t == "" {
		return false
	}
	switch t[0] {
	case '>', '|', '#', '<':
		return false
	case '-', '*', '+':
		if len(t) == 1 || t[1] == ' ' || t[1] == '\t' {
			return false
		}
	}
	if listNumberLen(t) > 0 {
		return false
	}
	return true
}

// listNumberLen returns the length of an ordered list marker ("12. ",
// "3) ") at the start of t, or 0.
func listNumberLen(t string) int {
	i := 0
	for i < len(t) && i < 9 && t[i] >= '0' && t[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(t) || (t[i] != '.' && t[i] != ')') {
		return 0
	}
	if i+1 < len(t) && t[i+1] != ' ' && t[i+1] != '\t' {
		return 0
	}
	return i + 1
}

// taskLine reports whether line is a task list item and returns the byte
// offset and width of the state character.
func taskLine(line string) (col, width int, ok bool) {
	i := 0
	skip := func() {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
	}
	skip()
	for i < len(line) && line[i] == '>' {
		i++
		skip()
	}
	if i >= len(line) {
		return 0, 0, false
	}
	switch line[i] {
	case '-', '*', '+':
		i++
	default:
		n := listNumberLen(line[i:])
		if n == 0 {
			return 0, 0, false
		}
		i += n
	}
	if i >= len(line) || (line[i] != ' ' && line[i] != '\t') {
		return 0, 0, false
	}
	skip()
	if i+2 >= len(line) || line[i] != '[' {
		return 0, 0, false
	}
	r, w := utf8.DecodeRuneInString(line[i+1:])
	if r == ']' || r == utf8.RuneError || i+1+w >= len(line) || line[i+1+w] != ']' {
		return 0, 0, false
	}
	after := i + 2 + w
	if after < len(line) && line[after] != ' ' && line[after] != '\t' {
		return 0, 0, false
	}
	return i + 1, w, true
}

var dueRe = regexp.MustCompile(`📅\s*(\d{4}-\d{2}-\d{2})|\bdue:(\d{4}-\d{2}-\d{2})|@due\((\d{4}-\d{2}-\d{2})\)`)

// taskMeta extracts the due date and priority from a task's raw text and
// returns the cleaned text.
func taskMeta(raw string) (text string, due Date, prio int) {
	s := raw
	if m := dueRe.FindStringSubmatchIndex(s); m != nil {
		for g := 1; g <= 3; g++ {
			if m[2*g] >= 0 {
				due, _ = ParseDate(s[m[2*g]:m[2*g+1]])
				break
			}
		}
		s = s[:m[0]] + " " + s[m[1]:]
	}
	s = stripBlockID(s)
	var out []string
	for _, f := range strings.Fields(s) {
		switch f {
		case "!", "!!", "!!!":
			prio = maxPrio(prio, len(f))
			continue
		}
		kept := strings.Map(func(r rune) rune {
			switch r {
			case '🔺':
				prio = maxPrio(prio, 3)
			case '⏫':
				prio = maxPrio(prio, 2)
			case '🔼':
				prio = maxPrio(prio, 1)
			case '🔽':
				prio = minPrio(prio, -1)
			case '⏬':
				prio = minPrio(prio, -2)
			case '\uFE0F':
			default:
				return r
			}
			return -1
		}, f)
		if kept != "" {
			out = append(out, kept)
		}
	}
	return strings.Join(out, " "), due, prio
}

func maxPrio(a, b int) int {
	if a < 0 || b > a {
		return b
	}
	return a
}

func minPrio(a, b int) int {
	if a > 0 {
		return a // an explicit high priority wins over a low marker
	}
	if b < a {
		return b
	}
	return a
}

// countWords counts whitespace-separated tokens containing a letter or
// digit.
func countWords(s string) int {
	n := 0
	inWord, hasAlnum := false, false
	for i := 0; i < len(s); {
		c := s[i]
		var r rune
		w := 1
		if c < utf8.RuneSelf {
			r = rune(c)
		} else {
			r, w = utf8.DecodeRuneInString(s[i:])
		}
		i += w
		if r == ' ' || r == '\t' || (r >= utf8.RuneSelf && unicode.IsSpace(r)) {
			if inWord && hasAlnum {
				n++
			}
			inWord, hasAlnum = false, false
			continue
		}
		inWord = true
		if !hasAlnum {
			if r < utf8.RuneSelf {
				hasAlnum = (r|0x20 >= 'a' && r|0x20 <= 'z') || (r >= '0' && r <= '9')
			} else {
				hasAlnum = unicode.IsLetter(r) || unicode.IsDigit(r)
			}
		}
	}
	if inWord && hasAlnum {
		n++
	}
	return n
}

// extractLinks finds wikilinks, embeds, Markdown links and bare URLs in a
// masked line, reports each through add, and returns the line with the
// links blanked (so tags inside them are not counted).
func extractLinks(masked string, ln int, orig string, add func(Link)) string {
	if strings.IndexByte(masked, '[') < 0 && !strings.Contains(masked, "://") && strings.IndexByte(masked, '<') < 0 {
		return masked
	}
	b := []byte(masked)
	blank := func(from, to int) {
		for i := from; i < to; i++ {
			b[i] = ' '
		}
	}
	ctx := strings.TrimSpace(orig)
	// Collect, then report in column order (the passes below find
	// different kinds at different times).
	var found []Link
	emit := add
	add = func(l Link) { found = append(found, l) }
	defer func() {
		sort.SliceStable(found, func(i, j int) bool { return found[i].Col < found[j].Col })
		for _, l := range found {
			emit(l)
		}
	}()

	// Wikilinks and embeds.
	for i := 0; i+1 < len(b); i++ {
		if b[i] != '[' || b[i+1] != '[' {
			continue
		}
		end := strings.Index(string(b[i+2:]), "]]")
		if end < 0 {
			break
		}
		inner := orig[i+2 : i+2+end]
		if k := strings.LastIndex(string(b[i+2:i+2+end]), "[["); k >= 0 {
			// "[[a [[b]]": restart at the inner opener
			i += 1 + k
			continue
		}
		start := i
		embed := i > 0 && b[i-1] == '!'
		if embed {
			start = i - 1
		}
		closeAt := i + 2 + end + 2
		if strings.TrimSpace(inner) != "" {
			l := parseWikilink(inner)
			l.Embed, l.Line, l.Col, l.Context = embed, ln, start, ctx
			if embed {
				l.Kind = KindEmbed
			}
			add(l)
		}
		blank(start, closeAt)
		i = closeAt - 1
	}

	// Markdown links and images.
	s := string(b)
	for i := 0; i < len(s); i++ {
		if s[i] != '[' || (i > 0 && s[i-1] == '\\') {
			continue
		}
		depth, j := 0, i
		for ; j < len(s); j++ {
			switch s[j] {
			case '\\':
				j++
				continue
			case '[':
				depth++
			case ']':
				depth--
			}
			if depth == 0 {
				break
			}
		}
		if j+1 >= len(s) || s[j] != ']' || s[j+1] != '(' {
			continue
		}
		dest, closeAt, ok := parseLinkDest(s, j+2)
		if !ok {
			continue
		}
		embed := i > 0 && s[i-1] == '!'
		start := i
		if embed {
			start = i - 1
		}
		l := Link{Label: strings.TrimSpace(orig[i+1 : j]), Embed: embed, Line: ln, Col: start, Context: ctx}
		classifyDest(&l, dest)
		add(l)
		blank(start, closeAt+1)
		s = string(b)
		i = closeAt
	}

	// Autolinks <scheme:...> and bare http(s) URLs.
	for i := 0; i < len(s); i++ {
		if s[i] == '<' {
			j := strings.IndexByte(s[i+1:], '>')
			if j > 0 {
				inner := s[i+1 : i+1+j]
				if hasScheme(inner) && !strings.ContainsAny(inner, " \t<") {
					add(Link{Kind: KindURL, Target: inner, Line: ln, Col: i, Context: ctx})
					blank(i, i+j+2)
					s = string(b)
					i += j + 1
				}
			}
			continue
		}
		if s[i] != 'h' || (!strings.HasPrefix(s[i:], "http://") && !strings.HasPrefix(s[i:], "https://")) {
			continue
		}
		if i > 0 {
			if r, _ := utf8.DecodeLastRuneInString(s[:i]); unicode.IsLetter(r) || unicode.IsDigit(r) {
				continue
			}
		}
		j := i
		for j < len(s) && s[j] != ' ' && s[j] != '\t' && s[j] != '<' && s[j] != '>' {
			j++
		}
		u := trimURL(s[i:j])
		add(Link{Kind: KindURL, Target: u, Line: ln, Col: i, Context: ctx})
		blank(i, i+len(u))
		s = string(b)
		i += len(u) - 1
	}
	return s
}

// parseWikilink splits the inside of [[...]].
func parseWikilink(inner string) Link {
	l := Link{Kind: KindWikilink, Wiki: true}
	target := inner
	if k := strings.Index(inner, "|"); k >= 0 {
		target, l.Label = inner[:k], strings.TrimSpace(inner[k+1:])
		target = strings.TrimSuffix(target, "\\") // table-escaped pipe
	}
	target = strings.TrimSpace(target)
	if k := strings.IndexByte(target, '#'); k >= 0 {
		frag := target[k+1:]
		target = strings.TrimSpace(target[:k])
		if strings.HasPrefix(frag, "^") {
			l.Block = strings.TrimSpace(frag[1:])
		} else if bi := strings.Index(frag, "#^"); bi >= 0 {
			l.Block = strings.TrimSpace(frag[bi+2:])
		} else {
			l.Heading = strings.TrimSpace(frag)
		}
	}
	l.Target = target
	return l
}

// parseLinkDest parses a Markdown link destination starting at i (just
// after "("). It returns the destination and the index of the closing ')'.
func parseLinkDest(s string, i int) (dest string, closeAt int, ok bool) {
	for i < len(s) && s[i] == ' ' {
		i++
	}
	if i >= len(s) {
		return "", 0, false
	}
	if s[i] == '<' {
		j := strings.IndexByte(s[i+1:], '>')
		if j < 0 {
			return "", 0, false
		}
		dest = s[i+1 : i+1+j]
		i += j + 2
	} else {
		depth, j := 0, i
		for ; j < len(s); j++ {
			c := s[j]
			if c == '\\' {
				j++
				continue
			}
			if c == ' ' || c == '\t' {
				break
			}
			if c == '(' {
				depth++
			} else if c == ')' {
				if depth == 0 {
					break
				}
				depth--
			}
		}
		if j > len(s) {
			j = len(s)
		}
		dest = s[i:j]
		i = j
	}
	for i < len(s) && s[i] == ' ' {
		i++
	}
	if i < len(s) && (s[i] == '"' || s[i] == '\'' || s[i] == '(') {
		q := s[i]
		if q == '(' {
			q = ')'
		}
		j := strings.IndexByte(s[i+1:], q)
		if j < 0 {
			return "", 0, false
		}
		i += j + 2
		for i < len(s) && s[i] == ' ' {
			i++
		}
	}
	if i >= len(s) || s[i] != ')' {
		return "", 0, false
	}
	return dest, i, true
}

// classifyDest fills Kind/Target/Heading/Block from a Markdown destination.
func classifyDest(l *Link, dest string) {
	if hasScheme(dest) || strings.HasPrefix(dest, "www.") {
		l.Kind, l.Target = KindURL, dest
		return
	}
	l.Kind = KindMarkdown
	if l.Embed {
		l.Kind = KindEmbed
	}
	frag := ""
	if k := strings.IndexByte(dest, '#'); k >= 0 {
		dest, frag = dest[:k], dest[k+1:]
	}
	if u, err := url.PathUnescape(dest); err == nil {
		dest = u
	}
	if f, err := url.PathUnescape(frag); err == nil {
		frag = f
	}
	l.Target = dest
	if strings.HasPrefix(frag, "^") {
		l.Block = frag[1:]
	} else if frag != "" {
		l.Heading = frag
	}
}

// hasScheme reports whether s starts with a URL scheme ("https:",
// "mailto:", "obsidian:"); single letters (Windows drives) do not count.
func hasScheme(s string) bool {
	i := 0
	for i < len(s) {
		c := s[i]
		if c|0x20 >= 'a' && c|0x20 <= 'z' || (i > 0 && (c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.')) {
			i++
			continue
		}
		break
	}
	return i >= 2 && i < len(s) && s[i] == ':'
}

// trimURL drops trailing punctuation and unbalanced closing brackets.
func trimURL(u string) string {
	for len(u) > 0 {
		c := u[len(u)-1]
		switch c {
		case '.', ',', ':', ';', '!', '?', '"', '\'', '*', '_', '~':
			u = u[:len(u)-1]
			continue
		case ')':
			if strings.Count(u, "(") < strings.Count(u, ")") {
				u = u[:len(u)-1]
				continue
			}
		case ']':
			if strings.Count(u, "[") < strings.Count(u, "]") {
				u = u[:len(u)-1]
				continue
			}
		}
		break
	}
	return u
}

// extractTags reports each #tag in s. A tag starts at a '#' preceded by
// whitespace or the line start and consists of letters, digits, marks, '_',
// '-' and '/'; a purely numeric tag is not a tag (issue numbers).
func extractTags(s string, add func(string)) {
	for i := 0; i < len(s); i++ {
		if s[i] != '#' {
			continue
		}
		if i > 0 {
			r, _ := utf8.DecodeLastRuneInString(s[:i])
			if !unicode.IsSpace(r) {
				continue
			}
		}
		j := i + 1
		for j < len(s) {
			r, w := utf8.DecodeRuneInString(s[j:])
			if !isTagRune(r) {
				break
			}
			j += w
		}
		tag := strings.TrimRight(s[i+1:j], "/")
		if tag != "" && tag[0] != '/' && !allDigits(tag) {
			add(tag)
		}
		if j > i+1 {
			i = j - 1
		}
	}
}

func isTagRune(r rune) bool {
	return r == '_' || r == '-' || r == '/' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r)
}
