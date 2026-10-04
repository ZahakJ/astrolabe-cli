package editor

import (
	"strings"

	"github.com/ZahakJ/folio/internal/text"
)

// maxCandidates caps the completer's list.
const maxCandidates = 50

type completerState struct {
	open    bool
	anchor  Pos // just after the "[["
	items   []Candidate
	sel     int
	query   string
	queried bool
}

// FuzzyCompleter returns a Completer that ranks a fixed candidate list
// with internal/text's fuzzy matcher on "Label Detail" (an empty query
// keeps the list order, e.g. most recent first).
func FuzzyCompleter(cands []Candidate) Completer {
	keys := make([]string, len(cands))
	for i, c := range cands {
		keys[i] = c.Label
		if c.Detail != "" && c.Detail != c.Label {
			keys[i] += " " + c.Detail
		}
	}
	return func(q string) []Candidate {
		if strings.TrimSpace(q) == "" {
			return cands[:min(len(cands), maxCandidates)]
		}
		ranked := text.Rank(q, keys)
		out := make([]Candidate, 0, min(len(ranked), maxCandidates))
		for _, r := range ranked {
			if len(out) == maxCandidates {
				break
			}
			out = append(out, cands[r.Index])
		}
		return out
	}
}

// CompleterOpen reports whether the [[ popup is showing.
func (e *Editor) CompleterOpen() bool { return e.completer.open }

// maybeOpenCompleter opens the popup after a freshly typed "[[".
func (e *Editor) maybeOpenCompleter() {
	if e.cfgCompl == nil || e.replaying || e.insRepeat {
		return
	}
	line := e.buf.line(e.cur.Line)
	c := e.cur.Col
	if c < 2 || line[c-2:c] != "[[" {
		return
	}
	if c >= 3 && (line[c-3] == '[' || line[c-3] == '\\') {
		return
	}
	e.completer = completerState{open: true, anchor: e.cur}
	e.refreshCompleter("")
}

func (e *Editor) refreshCompleter(q string) {
	items := e.cfgCompl(q)
	if len(items) > maxCandidates {
		items = items[:maxCandidates]
	}
	e.completer.items = items
	e.completer.query = q
	e.completer.queried = true
	e.completer.sel = 0
}

// updateCompleter re-queries after an edit, closing the popup when the
// cursor left the link.
func (e *Editor) updateCompleter() {
	c := &e.completer
	if !c.open {
		return
	}
	if e.cur.Line != c.anchor.Line || e.cur.Col < c.anchor.Col {
		*c = completerState{}
		return
	}
	line := e.buf.line(e.cur.Line)
	if c.anchor.Col < 2 || c.anchor.Col > len(line) || line[c.anchor.Col-2:c.anchor.Col] != "[[" {
		*c = completerState{}
		return
	}
	q := line[c.anchor.Col:e.cur.Col]
	if strings.ContainsAny(q, "[]|#^") {
		*c = completerState{}
		return
	}
	if q != c.query || !c.queried {
		e.refreshCompleter(q)
	}
}

// completerKey handles a key while the popup is open; false lets the key
// through to normal Insert-mode handling.
func (e *Editor) completerKey(tok string) bool {
	c := &e.completer
	n := len(c.items)
	switch tok {
	case "<Esc>", "<C-c>":
		// Esc closes the popup and keeps the typed text (still in Insert).
		*c = completerState{}
		return true
	case "<Up>", "<C-p>", "<S-Tab>":
		if n > 0 {
			c.sel = (c.sel - 1 + n) % n
		}
		return true
	case "<Down>", "<C-n>":
		if n > 0 {
			c.sel = (c.sel + 1) % n
		}
		return true
	case "<CR>", "<Tab>":
		target := c.query
		if n > 0 {
			target = c.items[c.sel].Target
		}
		back := e.cur.Col - c.anchor.Col
		in := input{complete: true, back: back, text: target}
		e.recordInsert(in)
		e.applyCompletion(back, target)
		return true
	}
	return false
}

// applyCompletion replaces the back bytes before the cursor with target
// and closes the link, stepping over a "]]" that is already there.
func (e *Editor) applyCompletion(back int, target string) {
	ln := e.cur.Line
	start := Pos{ln, max(0, e.cur.Col-back)}
	end := e.buf.replace(start, e.cur, target)
	line := e.buf.line(ln)
	if strings.HasPrefix(line[end.Col:], "]]") {
		e.cur = Pos{ln, end.Col + 2}
	} else {
		e.cur = e.buf.insert(end, "]]")
	}
	e.completer = completerState{}
}
