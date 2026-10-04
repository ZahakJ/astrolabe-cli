package md

import (
	"strings"
)

// Frontmatter is the leading "---" YAML block of a note. Only what folio
// needs is interpreted; everything is also kept as ordered raw fields so it
// can be displayed. The block is never re-serialised.
type Frontmatter struct {
	// StartLine and EndLine are the lines of the opening and closing
	// delimiters ("---" and "---" or "...").
	StartLine, EndLine int
	// Fields are the top-level "key: value" entries in source order.
	Fields []Field
	// Title is the unquoted "title" value.
	Title string
	// Tags come from "tags" (or "tag"): a YAML list, an inline [a, b] list,
	// or a comma/space separated string; leading '#' removed, empties
	// dropped, duplicates kept out.
	Tags []string
	// Aliases come from "aliases" (or "alias"): list, inline list, or a
	// comma separated string.
	Aliases []string
	// Date and Created are the raw unquoted "date" and "created" values.
	Date, Created string
}

// Field is one top-level frontmatter entry.
type Field struct {
	Key string
	// Value is the text after "key:" with surrounding blanks removed and
	// quotes left as written ("" for a key whose value is on following
	// lines).
	Value string
	// Lines are the following indented or "- item" lines that belong to
	// this key, raw.
	Lines []string
	// Line is the 0-based source line of the key.
	Line int
}

// Get returns the first field with the given key (compared
// case-insensitively).
func (f *Frontmatter) Get(key string) (Field, bool) {
	if f == nil {
		return Field{}, false
	}
	for _, fl := range f.Fields {
		if strings.EqualFold(fl.Key, key) {
			return fl, true
		}
	}
	return Field{}, false
}

// List interprets the field as a list: a YAML block list ("- a"), an
// inline list ("[a, b]"), or a scalar split on commas. Items are unquoted.
func (fl Field) List() []string {
	return fieldList(fl, false)
}

// Scalar returns the unquoted single-line value.
func (fl Field) Scalar() string { return unquoteYAML(fl.Value) }

// isFenceLine reports whether s is a frontmatter delimiter made of the
// given marker, allowing trailing blanks.
func isFenceLine(s, marker string) bool {
	return strings.TrimRight(s, " \t") == marker
}

// parseFrontmatter recognises frontmatter at the start of the document.
// It returns nil when line 0 is not "---" or no closing line exists.
func parseFrontmatter(src string, starts, ends []int) *Frontmatter {
	if len(starts) < 2 || !isFenceLine(src[starts[0]:ends[0]], "---") {
		return nil
	}
	closeLine := -1
	for i := 1; i < len(starts); i++ {
		l := src[starts[i]:ends[i]]
		if isFenceLine(l, "---") || isFenceLine(l, "...") {
			closeLine = i
			break
		}
	}
	if closeLine < 0 {
		return nil
	}
	fm := &Frontmatter{StartLine: 0, EndLine: closeLine}
	var cur *Field
	for i := 1; i < closeLine; i++ {
		l := src[starts[i]:ends[i]]
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || l[0] == '#' {
			continue // blank line or top-level YAML comment
		}
		indented := l[0] == ' ' || l[0] == '\t'
		if !indented && !strings.HasPrefix(l, "- ") && l != "-" {
			if k, v, ok := splitKey(l); ok {
				fm.Fields = append(fm.Fields, Field{Key: k, Value: v, Line: i})
				cur = &fm.Fields[len(fm.Fields)-1]
				continue
			}
		}
		if cur != nil {
			cur.Lines = append(cur.Lines, l)
		}
	}
	for _, fl := range fm.Fields {
		switch strings.ToLower(fl.Key) {
		case "title":
			if fm.Title == "" {
				fm.Title = strings.TrimSpace(unquoteYAML(fl.Value))
			}
		case "tags", "tag":
			for _, t := range fieldList(fl, true) {
				t = strings.TrimLeft(strings.TrimSpace(t), "#")
				if t != "" && !containsString(fm.Tags, t) {
					fm.Tags = append(fm.Tags, t)
				}
			}
		case "aliases", "alias":
			for _, a := range fieldList(fl, false) {
				if a = strings.TrimSpace(a); a != "" && !containsString(fm.Aliases, a) {
					fm.Aliases = append(fm.Aliases, a)
				}
			}
		case "date":
			if fm.Date == "" {
				fm.Date = strings.TrimSpace(unquoteYAML(fl.Value))
			}
		case "created":
			if fm.Created == "" {
				fm.Created = strings.TrimSpace(unquoteYAML(fl.Value))
			}
		}
	}
	return fm
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// splitKey splits "key: value". The key may be quoted; a colon inside a
// quoted key does not end it. "key:value" (no space) is accepted only when
// the value is empty or the key is a plain word, to avoid splitting URLs.
func splitKey(l string) (key, val string, ok bool) {
	if l == "" {
		return "", "", false
	}
	if l[0] == '"' || l[0] == '\'' {
		q := l[0]
		end := strings.IndexByte(l[1:], q)
		if end < 0 {
			return "", "", false
		}
		key = l[1 : 1+end]
		rest := strings.TrimLeft(l[2+end:], " \t")
		if !strings.HasPrefix(rest, ":") {
			return "", "", false
		}
		return key, strings.TrimSpace(rest[1:]), true
	}
	for i := 0; i < len(l); i++ {
		if l[i] == ':' && (i+1 == len(l) || l[i+1] == ' ' || l[i+1] == '\t') {
			key = strings.TrimRight(l[:i], " \t")
			if key == "" {
				return "", "", false
			}
			return key, strings.TrimSpace(l[i+1:]), true
		}
	}
	return "", "", false
}

// unquoteYAML removes matching single or double quotes from a scalar and
// decodes the common escapes. Unquoted values are returned trimmed.
func unquoteYAML(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 {
		switch {
		case v[0] == '"' && v[len(v)-1] == '"':
			inner := v[1 : len(v)-1]
			if !strings.Contains(inner, `\`) {
				return inner
			}
			var b strings.Builder
			for i := 0; i < len(inner); i++ {
				c := inner[i]
				if c == '\\' && i+1 < len(inner) {
					i++
					switch inner[i] {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					default:
						b.WriteByte(inner[i])
					}
					continue
				}
				b.WriteByte(c)
			}
			return b.String()
		case v[0] == '\'' && v[len(v)-1] == '\'':
			return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
		}
	}
	return v
}

// fieldList interprets a field as a list of strings. When words is true a
// scalar is split on commas and blanks (tags); otherwise on commas only.
func fieldList(fl Field, words bool) []string {
	v := strings.TrimSpace(fl.Value)
	var out []string
	switch {
	case strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]"):
		for _, it := range splitFlow(v[1 : len(v)-1]) {
			if it = unquoteYAML(it); it != "" {
				out = append(out, it)
			}
		}
	case v == "" || v == "|" || v == ">":
		for _, l := range fl.Lines {
			t := strings.TrimSpace(l)
			if strings.HasPrefix(t, "-") {
				t = strings.TrimSpace(t[1:])
				if it := unquoteYAML(t); it != "" {
					out = append(out, it)
				}
			} else if t != "" && v != "" {
				out = append(out, splitScalar(t, words)...)
			}
		}
	default:
		out = splitScalar(unquoteYAML(v), words)
	}
	return out
}

func splitScalar(s string, words bool) []string {
	f := func(r rune) bool { return r == ',' || words && (r == ' ' || r == '\t') }
	parts := strings.FieldsFunc(s, f)
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// splitFlow splits the inside of a YAML flow sequence on commas that are
// not inside quotes.
func splitFlow(s string) []string {
	var out []string
	var q byte
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case c == ',':
			out = append(out, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	out = append(out, strings.TrimSpace(s[start:]))
	return out
}
