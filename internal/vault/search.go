package vault

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// RankClass orders search results: title hits, then heading hits, then
// body hits (DESIGN.md §3).
type RankClass int

const (
	RankTitle   RankClass = iota // the note's title (or, for filter-only queries, the note)
	RankHeading                  // a heading line
	RankBody                     // any other line
)

// String returns "title", "heading" or "body".
func (c RankClass) String() string {
	switch c {
	case RankTitle:
		return "title"
	case RankHeading:
		return "heading"
	}
	return "body"
}

// Result is one search hit.
type Result struct {
	Path  string // vault-relative note path
	Title string // note title
	// Line is the 1-based line of the hit. For a RankTitle result it is the
	// line the title comes from (H1 or frontmatter title), or 0 when the
	// title is the file name.
	Line int
	// Text is the line's text without its terminator; for a RankTitle
	// result it is the title itself.
	Text string
	// Matches are the [start, end) byte ranges in Text that matched, sorted
	// and non-overlapping.
	Matches [][2]int
	Class   RankClass
	ModTime time.Time
}

// Term is one search term.
type Term struct {
	// Text is the term as written (without quotes or prefix).
	Text string
	// Regexp is set for "re:" terms.
	Regexp *regexp.Regexp
	// CaseSensitive follows smart case: a term with an upper-case letter
	// matches case-sensitively.
	CaseSensitive bool

	needle []byte         // folded needle for substring terms
	whole  *regexp.Regexp // multi-line variant used to pre-filter files
}

// Query is a parsed search query. All terms and filters are ANDed.
type Query struct {
	Terms  []Term   // text terms (substring or regexp)
	Tags   []string // tag: filters (lower-case, without '#')
	Paths  []Term   // path: filters (substring of the vault-relative path)
	Titles []Term   // title: filters (substring of the title or an alias)
}

// Empty reports whether the query has no terms and no filters.
func (q *Query) Empty() bool {
	return len(q.Terms) == 0 && len(q.Tags) == 0 && len(q.Paths) == 0 && len(q.Titles) == 0
}

// ParseQuery parses a search query: whitespace-separated terms, "quoted
// phrases", re:PATTERN (a Go regexp; quote it to include spaces), and the
// filters tag:x, path:x, title:x (values may be quoted). Matching is
// smart-case. An invalid regexp is an error.
func ParseQuery(s string) (*Query, error) {
	q := &Query{}
	for _, tok := range tokenizeQuery(s) {
		key, val := "", tok.text
		if !tok.quoted {
			if i := strings.IndexByte(tok.text, ':'); i > 0 {
				switch k := strings.ToLower(tok.text[:i]); k {
				case "re", "tag", "path", "title":
					key, val = k, tok.text[i+1:]
				}
			}
		}
		if tok.valQuoted {
			val = tok.val
		}
		if val == "" {
			continue // "tag:" while typing
		}
		switch key {
		case "":
			q.Terms = append(q.Terms, newTerm(val))
		case "re":
			t, err := newRegexpTerm(val)
			if err != nil {
				return nil, err
			}
			q.Terms = append(q.Terms, t)
		case "tag":
			if t := strings.ToLower(strings.Trim(strings.TrimPrefix(val, "#"), "/")); t != "" {
				q.Tags = append(q.Tags, t)
			}
		case "path":
			q.Paths = append(q.Paths, newTerm(val))
		case "title":
			q.Titles = append(q.Titles, newTerm(val))
		}
	}
	return q, nil
}

type qtoken struct {
	text      string // whole token (for unquoted) or phrase (for quoted)
	quoted    bool   // the whole token was a quoted phrase
	val       string // the quoted value after "key:"
	valQuoted bool
}

// tokenizeQuery splits on whitespace, honouring "phrases" and key:"values".
func tokenizeQuery(s string) []qtoken {
	var out []qtoken
	i := 0
	for i < len(s) {
		r, w := utf8.DecodeRuneInString(s[i:])
		if unicode.IsSpace(r) {
			i += w
			continue
		}
		if s[i] == '"' {
			j := strings.IndexByte(s[i+1:], '"')
			var phrase string
			if j < 0 {
				phrase, i = s[i+1:], len(s)
			} else {
				phrase, i = s[i+1:i+1+j], i+j+2
			}
			if phrase != "" {
				out = append(out, qtoken{text: phrase, quoted: true})
			}
			continue
		}
		start := i
		for i < len(s) {
			r, w := utf8.DecodeRuneInString(s[i:])
			if unicode.IsSpace(r) {
				break
			}
			if s[i] == '"' && i > start && s[i-1] == ':' {
				// key:"quoted value"
				j := strings.IndexByte(s[i+1:], '"')
				var val string
				if j < 0 {
					val, i = s[i+1:], len(s)
				} else {
					val, i = s[i+1:i+1+j], i+j+2
				}
				out = append(out, qtoken{text: s[start:i], val: val, valQuoted: true})
				start = -1
				break
			}
			i += w
		}
		if start >= 0 {
			out = append(out, qtoken{text: s[start:i]})
		}
	}
	return out
}

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

func newTerm(s string) Term {
	cs := hasUpper(s)
	t := Term{Text: s, CaseSensitive: cs}
	if cs {
		t.needle = []byte(s)
	} else {
		t.needle = foldBytes(nil, []byte(s))
	}
	return t
}

// regexpHasUpper is smart case for patterns: escapes such as \S or \W do
// not count as upper-case letters.
func regexpHasUpper(p string) bool {
	for i := 0; i < len(p); i++ {
		if p[i] == '\\' {
			i++
			continue
		}
		r, w := utf8.DecodeRuneInString(p[i:])
		if unicode.IsUpper(r) {
			return true
		}
		i += w - 1
	}
	return false
}

func newRegexpTerm(p string) (Term, error) {
	cs := regexpHasUpper(p)
	flags := ""
	if !cs {
		flags = "(?i)"
	}
	re, err := regexp.Compile(flags + p)
	if err != nil {
		return Term{}, fmt.Errorf("bad regexp %q: %w", p, err)
	}
	whole, err := regexp.Compile("(?m)" + flags + p)
	if err != nil {
		return Term{}, err
	}
	return Term{Text: p, Regexp: re, CaseSensitive: cs, whole: whole}, nil
}

// foldBytes appends a lower-cased copy of src to dst in which every rune
// keeps its UTF-8 length, so byte offsets in the folded text are offsets
// in the original. (The rare runes whose lower case has a different
// length are left as they are.)
func foldBytes(dst, src []byte) []byte {
	start := len(dst)
	dst = append(dst, src...)
	out := dst[start:]
	for i := 0; i < len(out); {
		c := out[i]
		if c < utf8.RuneSelf {
			if 'A' <= c && c <= 'Z' {
				out[i] = c + 32
			}
			i++
			continue
		}
		r, w := utf8.DecodeRune(out[i:])
		if lr := unicode.ToLower(r); lr != r && utf8.RuneLen(lr) == w {
			utf8.EncodeRune(out[i:], lr)
		}
		i += w
	}
	return dst
}

// match reports whether the term occurs in hay (raw) / foldedHay.
func (t *Term) match(raw, folded []byte) bool {
	if t.Regexp != nil {
		return t.whole.Match(raw)
	}
	if t.CaseSensitive {
		return bytes.Contains(raw, t.needle)
	}
	return bytes.Contains(folded, t.needle)
}

func (t *Term) matchString(s string) bool {
	if t.Regexp != nil {
		return t.Regexp.MatchString(s)
	}
	if t.CaseSensitive {
		return strings.Contains(s, string(t.needle))
	}
	return bytes.Contains(foldBytes(nil, []byte(s)), t.needle)
}

// ranges appends the match ranges of t in one line.
func (t *Term) ranges(dst [][2]int, raw, folded []byte) [][2]int {
	if t.Regexp != nil {
		for _, m := range t.Regexp.FindAllIndex(raw, -1) {
			if m[1] > m[0] {
				dst = append(dst, [2]int{m[0], m[1]})
			}
		}
		return dst
	}
	hay := folded
	if t.CaseSensitive {
		hay = raw
	}
	if len(t.needle) == 0 {
		return dst
	}
	for off := 0; off < len(hay); {
		i := bytes.Index(hay[off:], t.needle)
		if i < 0 {
			break
		}
		dst = append(dst, [2]int{off + i, off + i + len(t.needle)})
		off += i + len(t.needle)
	}
	return dst
}

// SearchOptions configure Search.
type SearchOptions struct {
	// Limit caps the number of results after ranking (0 = no limit).
	Limit int
}

// Search parses query and runs it; see SearchQuery.
func (v *Vault) Search(ctx context.Context, query string, opts SearchOptions) ([]Result, error) {
	q, err := ParseQuery(query)
	if err != nil {
		return nil, err
	}
	return v.SearchQuery(ctx, q, opts)
}

type searchFile struct {
	rel  string
	abs  string
	mod  time.Time
	note *Note // indexed metadata if current, else nil
}

// SearchQuery scans the vault's files concurrently and returns the hits,
// ranked title > heading > body, then most recently modified first, then
// path and line. A note qualifies when it passes every filter and contains
// every term (in its text or title). Within a qualifying note, lines that
// contain all terms are returned; if no single line does, lines containing
// any term are. A title that contains every term yields one RankTitle
// result; a query of filters only yields one RankTitle result per note.
// Cancelling ctx (search-as-you-type) stops the scan and returns ctx.Err().
func (v *Vault) SearchQuery(ctx context.Context, q *Query, opts SearchOptions) ([]Result, error) {
	if q.Empty() {
		return nil, nil
	}
	files, err := v.searchFiles(ctx)
	if err != nil {
		return nil, err
	}
	jobs := make(chan searchFile)
	var (
		mu  sync.Mutex
		all []Result
		wg  sync.WaitGroup
	)
	workers := runtime.GOMAXPROCS(0)
	if workers > 16 {
		workers = 16
	}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var local []Result
			var fold []byte
			for f := range jobs {
				if ctx.Err() != nil {
					continue
				}
				local, fold = searchOne(q, f, local, fold)
			}
			mu.Lock()
			all = append(all, local...)
			mu.Unlock()
		}()
	}
feed:
	for _, f := range files {
		select {
		case jobs <- f:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if a.Class != b.Class {
			return a.Class < b.Class
		}
		if !a.ModTime.Equal(b.ModTime) {
			return a.ModTime.After(b.ModTime)
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Line < b.Line
	})
	if opts.Limit > 0 && len(all) > opts.Limit {
		all = all[:opts.Limit]
	}
	return all, nil
}

// searchFiles lists the notes to search: the index once the scan is done,
// else a fresh walk (so search works before the scan completes).
func (v *Vault) searchFiles(ctx context.Context) ([]searchFile, error) {
	var out []searchFile
	if v.Scanned() {
		for _, n := range v.Notes() {
			out = append(out, searchFile{rel: n.Path, abs: v.Abs(n.Path), mod: n.ModTime, note: n})
		}
		return out, nil
	}
	ch := make(chan walkEntry, 256)
	errc := make(chan error, 1)
	go func() { errc <- v.walk(ctx, ch) }()
	for e := range ch {
		if !e.md {
			continue
		}
		f := searchFile{rel: e.rel, abs: e.abs, mod: e.info.ModTime()}
		if n, ok := v.Note(e.rel); ok && n.ModTime.Equal(f.mod) {
			f.note = n
		}
		out = append(out, f)
	}
	if err := <-errc; err != nil {
		return nil, err
	}
	return out, nil
}

// searchOne searches one file, appending its results.
func searchOne(q *Query, f searchFile, out []Result, fold []byte) ([]Result, []byte) {
	raw, err := os.ReadFile(f.abs)
	if err != nil {
		return out, fold
	}
	note := f.note
	if note != nil && int64(len(raw)) != note.Size {
		note = nil // changed since indexed
	}
	if note == nil {
		if fi, err := os.Stat(f.abs); err == nil {
			f.mod = fi.ModTime()
		}
		note = parseNote(f.rel, raw, f.mod, int64(len(raw)))
	}
	// Filters.
	for _, t := range q.Tags {
		if !note.HasTag(t) {
			return out, fold
		}
	}
	for i := range q.Paths {
		if !q.Paths[i].matchString(f.rel) {
			return out, fold
		}
	}
	for i := range q.Titles {
		ok := q.Titles[i].matchString(note.Title)
		for _, a := range note.Aliases {
			ok = ok || q.Titles[i].matchString(a)
		}
		if !ok {
			return out, fold
		}
	}
	title := note.Title
	if len(q.Terms) == 0 {
		return append(out, titleResult(note, f, nil)), fold
	}
	needFold := false
	for _, t := range q.Terms {
		if t.Regexp == nil && !t.CaseSensitive {
			needFold = true
		}
	}
	var folded []byte
	if needFold {
		fold = foldBytes(fold[:0], raw)
		folded = fold
	}
	// Note-level AND: every term in the text or the title.
	titleAll := true
	for i := range q.Terms {
		t := &q.Terms[i]
		inTitle := t.matchString(title)
		titleAll = titleAll && inTitle
		if !inTitle && !t.match(raw, folded) {
			return out, fold
		}
	}
	titleLine := 0
	if titleAll {
		var rs [][2]int
		tb := []byte(title)
		ft := foldBytes(nil, tb)
		for i := range q.Terms {
			rs = q.Terms[i].ranges(rs, tb, ft)
		}
		r := titleResult(note, f, mergeRanges(rs))
		titleLine = r.Line
		out = append(out, r)
	}
	// Line scan.
	type hit struct {
		line   int
		text   string
		ranges [][2]int
		class  RankClass
		all    bool
	}
	var hits []hit
	anyAll := false
	inFence := false
	var fenceCh byte
	var fenceLen int
	ln := 0
	off := 0
	for off < len(raw) {
		end := bytes.IndexByte(raw[off:], '\n')
		next := len(raw)
		if end >= 0 {
			end += off
			next = end + 1
		} else {
			end = len(raw)
		}
		ln++
		lineRaw := raw[off:end]
		if len(lineRaw) > 0 && lineRaw[len(lineRaw)-1] == '\r' {
			lineRaw = lineRaw[:len(lineRaw)-1]
		}
		var lineFold []byte
		if folded != nil {
			lineFold = folded[off : off+len(lineRaw)]
		}
		off = next
		isFence := false
		if len(lineRaw) >= 3 && bytes.IndexAny(lineRaw, "`~") >= 0 {
			if ch, n, rest, ok := fenceInfo(string(lineRaw)); ok {
				isFence = true
				if inFence {
					if ch == fenceCh && n >= fenceLen && strings.TrimSpace(rest) == "" {
						inFence = false
					}
				} else {
					inFence, fenceCh, fenceLen = true, ch, n
				}
			}
		}
		if ln == titleLine {
			continue
		}
		var rs [][2]int
		matched := 0
		for i := range q.Terms {
			before := len(rs)
			rs = q.Terms[i].ranges(rs, lineRaw, lineFold)
			if len(rs) > before {
				matched++
			}
		}
		if matched == 0 {
			continue
		}
		class := RankBody
		if !inFence && !isFence && len(lineRaw) > 0 && (lineRaw[0] == '#' || lineRaw[0] == ' ') {
			if _, _, ok := atxHeading(string(lineRaw)); ok {
				class = RankHeading
			}
		}
		textLine := string(lineRaw)
		if ln == 1 {
			// Drop a BOM from the displayed text, shifting ranges.
			if strings.HasPrefix(textLine, "\uFEFF") {
				textLine = textLine[3:]
				for k := range rs {
					rs[k][0] = max(rs[k][0]-3, 0)
					rs[k][1] = max(rs[k][1]-3, 0)
				}
			}
		}
		all := matched == len(q.Terms)
		anyAll = anyAll || all
		hits = append(hits, hit{ln, textLine, mergeRanges(rs), class, all})
	}
	for _, h := range hits {
		if anyAll && !h.all {
			continue
		}
		out = append(out, Result{Path: f.rel, Title: title, Line: h.line, Text: h.text,
			Matches: h.ranges, Class: h.class, ModTime: f.mod})
	}
	return out, fold
}

// titleResult builds the RankTitle result for a note.
func titleResult(n *Note, f searchFile, ranges [][2]int) Result {
	return Result{Path: f.rel, Title: n.Title, Line: n.TitleLine, Text: n.Title,
		Class: RankTitle, ModTime: f.mod, Matches: ranges}
}

// mergeRanges sorts ranges and merges overlapping or touching ones.
func mergeRanges(rs [][2]int) [][2]int {
	if len(rs) < 2 {
		return rs
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i][0] < rs[j][0] })
	out := rs[:1]
	for _, r := range rs[1:] {
		last := &out[len(out)-1]
		if r[0] <= last[1] {
			if r[1] > last[1] {
				last[1] = r[1]
			}
			continue
		}
		out = append(out, r)
	}
	return out
}
