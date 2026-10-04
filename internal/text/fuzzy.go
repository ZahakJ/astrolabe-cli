package text

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Fuzzy matching for finders: the pattern must appear in the candidate as a
// subsequence; the best-scoring alignment is found by dynamic programming
// (in the spirit of fzf's algorithm), rewarding matches at word boundaries,
// consecutive runs, the start of the string and the basename of a path, and
// penalising gaps.

// Scoring constants.
const (
	scoreMatch        = 16
	scoreGapStart     = -3
	scoreGapExtension = -1

	bonusBoundary     = 8  // after a separator: space / _ - . : etc.
	bonusPathSep      = 9  // right after '/'
	bonusCamel        = 7  // lower→upper or letter↔digit transition
	bonusStart        = 10 // first character of the candidate
	bonusConsecutive  = 5  // minimum bonus for continuing a run
	bonusFirstCharMul = 2  // the first pattern character's bonus is doubled
	bonusBasename     = 4  // per matched character inside the path basename
)

// Match is the result of a successful fuzzy match.
type Match struct {
	// Score is higher for better matches; only meaningful relative to other
	// scores for the same pattern.
	Score int
	// Positions are the byte offsets in the candidate of each matched rune,
	// in increasing order, for highlighting.
	Positions []int
}

// Matcher matches one pattern against many candidates, reusing scratch
// buffers between calls. A Matcher is not safe for concurrent use; create
// one per goroutine.
type Matcher struct {
	terms         [][]rune
	caseSensitive bool

	cand    []rune
	offs    []int
	bonus   []int
	h       []int32 // score matrix, rows = candidate runes, cols = pattern runes
	consec  []int16 // consecutive-run length per cell
	from    []int32 // backpointer: candidate index of the previous pattern rune
	carry   []int32
	carryAt []int32
}

// NewMatcher prepares a pattern. Space-separated terms must all match
// (AND); positions from every term are merged. Matching is smart-case: case
// insensitive unless the pattern contains an upper-case letter.
func NewMatcher(pattern string) *Matcher {
	m := &Matcher{}
	for _, r := range pattern {
		if unicode.IsUpper(r) {
			m.caseSensitive = true
			break
		}
	}
	for _, f := range strings.Fields(pattern) {
		rs := []rune(f)
		if !m.caseSensitive {
			for i, r := range rs {
				rs[i] = unicode.ToLower(r)
			}
		}
		m.terms = append(m.terms, rs)
	}
	return m
}

// Empty reports whether the pattern has no terms (every candidate matches
// with score 0).
func (m *Matcher) Empty() bool { return len(m.terms) == 0 }

// Match scores candidate against the pattern. ok is false when some term is
// not a subsequence of candidate.
func (m *Matcher) Match(candidate string) (Match, bool) {
	if len(m.terms) == 0 {
		return Match{}, true
	}
	m.prepare(candidate)
	var total Match
	for _, term := range m.terms {
		score, pos, ok := m.matchTerm(term)
		if !ok {
			return Match{}, false
		}
		total.Score += score
		total.Positions = append(total.Positions, pos...)
	}
	if len(m.terms) > 1 {
		sort.Ints(total.Positions)
		total.Positions = dedupInts(total.Positions)
	}
	return total, true
}

func dedupInts(a []int) []int {
	out := a[:0]
	for i, v := range a {
		if i == 0 || v != a[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// charClass groups runes for boundary detection.
type charClass uint8

const (
	classSep charClass = iota
	classLower
	classUpper
	classDigit
	classOther // letters without case (CJK, Arabic, …)
)

func classOf(r rune) charClass {
	switch {
	case r >= 'a' && r <= 'z':
		return classLower
	case r >= 'A' && r <= 'Z':
		return classUpper
	case r >= '0' && r <= '9':
		return classDigit
	case r < 0x80:
		return classSep
	case unicode.IsLower(r):
		return classLower
	case unicode.IsUpper(r):
		return classUpper
	case unicode.IsDigit(r):
		return classDigit
	case unicode.IsLetter(r):
		return classOther
	}
	return classSep
}

// prepare decodes the candidate, folds case and computes per-rune bonuses.
func (m *Matcher) prepare(s string) {
	m.cand = m.cand[:0]
	m.offs = m.offs[:0]
	m.bonus = m.bonus[:0]
	baseStart := strings.LastIndexByte(s, '/') + 1
	prevClass := classSep
	var prev rune = '/'
	for off, r := range s {
		cls := classOf(r)
		b := 0
		switch {
		case off == 0:
			b = bonusStart
		case cls != classSep && prev == '/':
			b = bonusPathSep
		case cls != classSep && prevClass == classSep:
			b = bonusBoundary
		case cls == classUpper && prevClass == classLower,
			cls == classDigit && prevClass != classDigit,
			cls != classDigit && cls != classSep && prevClass == classDigit:
			b = bonusCamel
		}
		// A path's basename is what users name; a string without '/' is
		// all basename.
		if off >= baseStart {
			b += bonusBasename
		}
		if !m.caseSensitive {
			r = unicode.ToLower(r)
		}
		m.cand = append(m.cand, r)
		m.offs = append(m.offs, off)
		m.bonus = append(m.bonus, b)
		prevClass = cls
		prev = r
	}
}

const negInf = -1 << 30

// matchTerm runs the alignment DP for one term over the prepared candidate.
func (m *Matcher) matchTerm(pat []rune) (int, []int, bool) {
	n, k := len(m.cand), len(pat)
	if k == 0 {
		return 0, nil, true
	}
	if k > n {
		return 0, nil, false
	}
	// Quick subsequence check, also bounding the useful candidate window.
	first, last := -1, -1
	j := 0
	for i := 0; i < n && j < k; i++ {
		if m.cand[i] == pat[j] {
			if j == 0 {
				first = i
			}
			j++
			if j == k {
				last = i
			}
		}
	}
	if j < k {
		return 0, nil, false
	}
	// The last pattern rune may match later than the greedy end.
	for i := n - 1; i > last; i-- {
		if m.cand[i] == pat[k-1] {
			last = i
			break
		}
	}
	w := last - first + 1
	size := w * k
	m.h = growInt32(m.h, size)
	m.from = growInt32(m.from, size)
	m.consec = growInt16(m.consec, size)
	m.carry = growInt32(m.carry, k)
	m.carryAt = growInt32(m.carryAt, k)
	for c := 0; c < k; c++ {
		m.carry[c] = negInf
		m.carryAt[c] = -1
	}
	for row := 0; row < w; row++ {
		i := first + row
		r := m.cand[i]
		base := row * k
		// Update carries: the best predecessor for column c at this row
		// with a gap of at least one character.
		if row >= 2 {
			for c := k - 1; c >= 1; c-- {
				if m.carry[c] > negInf {
					m.carry[c] += scoreGapExtension
				}
				prevVal := m.h[(row-2)*k+c-1]
				if prevVal > negInf && prevVal+scoreGapStart > m.carry[c] {
					m.carry[c] = prevVal + scoreGapStart
					m.carryAt[c] = int32(row - 2)
				}
			}
		}
		for c := 0; c < k; c++ {
			idx := base + c
			m.h[idx] = negInf
			m.consec[idx] = 0
			m.from[idx] = -1
			if r != pat[c] {
				continue
			}
			b := m.bonus[i]
			if c == 0 {
				m.h[idx] = int32(scoreMatch + b*bonusFirstCharMul)
				m.consec[idx] = 1
				continue
			}
			best := int32(negInf)
			var bestFrom int32 = -1
			var bestConsec int16
			// Consecutive: previous pattern rune at the previous position.
			if row >= 1 {
				pv := m.h[(row-1)*k+c-1]
				if pv > negInf {
					cl := m.consec[(row-1)*k+c-1] + 1
					cb := max(b, bonusConsecutive)
					// A run inherits the bonus of its start.
					if startBonus := m.bonus[i-int(cl)+1]; startBonus > cb {
						cb = startBonus
					}
					s := pv + int32(scoreMatch+cb)
					if s > best {
						best, bestFrom, bestConsec = s, int32(row-1), cl
					}
				}
			}
			if m.carry[c] > negInf {
				s := m.carry[c] + int32(scoreMatch+b)
				if s > best {
					best, bestFrom, bestConsec = s, m.carryAt[c], 1
				}
			}
			m.h[idx] = best
			m.from[idx] = bestFrom
			m.consec[idx] = bestConsec
		}
	}
	// Best end position for the last pattern rune.
	bestRow, best := -1, int32(negInf)
	for row := 0; row < w; row++ {
		if v := m.h[row*k+k-1]; v > best {
			best, bestRow = v, row
		}
	}
	if bestRow < 0 || best <= negInf {
		return 0, nil, false
	}
	pos := make([]int, k)
	row := bestRow
	for c := k - 1; c >= 0; c-- {
		pos[c] = m.offs[first+row]
		row = int(m.from[row*k+c])
		if c > 0 && row < 0 {
			return 0, nil, false
		}
	}
	return int(best), pos, true
}

func growInt32(a []int32, n int) []int32 {
	if cap(a) < n {
		return make([]int32, n, n*2)
	}
	return a[:n]
}

func growInt16(a []int16, n int) []int16 {
	if cap(a) < n {
		return make([]int16, n, n*2)
	}
	return a[:n]
}

// FuzzyMatch matches pattern against candidate with a one-off Matcher.
func FuzzyMatch(pattern, candidate string) (Match, bool) {
	return NewMatcher(pattern).Match(candidate)
}

// Ranked is one entry of a Rank result.
type Ranked struct {
	Index int // index into the candidate slice
	Match
}

// Rank matches pattern against every candidate and returns the matches
// best first. Ties are broken by shorter candidate, then original order. An
// empty pattern returns every candidate in original order.
func Rank(pattern string, candidates []string) []Ranked {
	m := NewMatcher(pattern)
	out := make([]Ranked, 0, len(candidates))
	for i, c := range candidates {
		if mt, ok := m.Match(c); ok {
			out = append(out, Ranked{Index: i, Match: mt})
		}
	}
	if m.Empty() {
		return out
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Score != out[b].Score {
			return out[a].Score > out[b].Score
		}
		la := utf8.RuneCountInString(candidates[out[a].Index])
		lb := utf8.RuneCountInString(candidates[out[b].Index])
		if la != lb {
			return la < lb
		}
		return out[a].Index < out[b].Index
	})
	return out
}
