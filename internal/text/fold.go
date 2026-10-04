package text

import (
	"bytes"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Search folding: one normalisation applied to both the query and the text
// searched, so that a search matches what a reader considers the same word.
//
//   - Unicode NFC, so composed and decomposed Latin diacritics match;
//   - case folding (unless the caller asks for a case-sensitive fold);
//   - Arabic: harakat (U+064B–065F, U+0670), tatweel and zero-width and bidi
//     controls are dropped; alef forms (أ إ آ ٱ) become ا, ى becomes ي, ة
//     becomes ه, the hamza carriers ؤ and ئ become و and ي, Persian and Urdu
//     kaf and yeh (ک ی) become ك and ي; presentation forms (as drawn by
//     astrolabe's own shaping) become base letters, lam-alef ligatures two;
//   - Arabic-Indic and Persian digits become ASCII digits.
//
// Folding can drop and split runes, so it comes with an offset map from
// every folded byte back to the source, to highlight matches in the
// original text.

// FoldRune appends the search fold of r to dst. lower selects case folding.
func FoldRune(dst []rune, r rune, lower bool) []rune {
	if r < utf8.RuneSelf {
		if lower && 'A' <= r && r <= 'Z' {
			r += 'a' - 'A'
		}
		return append(dst, r)
	}
	switch {
	case r >= 0x064B && r <= 0x065F, r == 0x0670, r == 0x0640, // harakat, superscript alef, tatweel
		r >= 0x200B && r <= 0x200F, r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069,
		r == 0xFEFF, r == 0x061C, r == 0x00AD:
		return dst
	case r >= 0xFE70 && r <= 0xFE7F && r != 0xFE74:
		return dst // isolated and medial harakat forms
	case r >= 0xFB50 && r <= 0xFDFF, r >= 0xFE80 && r <= 0xFEFC:
		for _, d := range norm.NFKC.String(string(r)) {
			if d != ' ' {
				dst = FoldRune(dst, d, lower)
			}
		}
		return dst
	case r >= 0x0660 && r <= 0x0669:
		return append(dst, '0'+r-0x0660)
	case r >= 0x06F0 && r <= 0x06F9:
		return append(dst, '0'+r-0x06F0)
	}
	switch r {
	case 0x0622, 0x0623, 0x0625, 0x0671: // آ أ إ ٱ
		return append(dst, 0x0627)
	case 0x0649, 0x0626, 0x06CC: // ى ئ ی
		return append(dst, 0x064A)
	case 0x0629: // ة
		return append(dst, 0x0647)
	case 0x0624: // ؤ
		return append(dst, 0x0648)
	case 0x06A9: // ک
		return append(dst, 0x0643)
	}
	if lower {
		r = unicode.ToLower(r)
	}
	return append(dst, r)
}

// isComposingMark reports a combining mark that NFC may compose with the
// rune before it (Arabic harakat are dropped instead).
func isComposingMark(r rune) bool {
	return r >= 0x0300 && r < 0x0370 || r >= 0x1DC0 && r < 0x1E00 || r >= 0x20D0 && r < 0x2100 ||
		(r >= 0x3099 && r <= 0x309A)
}

func startsComposingMark(b []byte) bool {
	r, _ := utf8.DecodeRune(b)
	return isComposingMark(r)
}

// AppendFold appends the search fold of src to dst and, for every byte
// appended, the byte offset in src of the character it came from to offs.
// lower selects case folding. Use FoldRange to map a folded match back.
func AppendFold(dst []byte, offs []int32, src []byte, lower bool) ([]byte, []int32) {
	var buf [8]rune
	for i := 0; i < len(src); {
		c := src[i]
		composing := i+1 < len(src) && src[i+1] >= 0xCC && src[i+1] < 0xF0 && startsComposingMark(src[i+1:])
		if c < utf8.RuneSelf && !composing {
			if lower && 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			dst = append(dst, c)
			offs = append(offs, int32(i))
			i++
			continue
		}
		r, w := utf8.DecodeRune(src[i:])
		start := i
		i += w
		var rs []rune
		j := i
		for j < len(src) && src[j] >= 0xCC && startsComposingMark(src[j:]) {
			_, mw := utf8.DecodeRune(src[j:])
			j += mw
		}
		if j > i {
			// A base followed by Latin-style combining marks: compose.
			for _, cr := range norm.NFC.String(string(src[start:j])) {
				rs = FoldRune(rs, cr, lower)
			}
			i = j
		} else {
			rs = FoldRune(buf[:0], r, lower)
		}
		for _, fr := range rs {
			n := len(dst)
			dst = utf8.AppendRune(dst, fr)
			for k := n; k < len(dst); k++ {
				offs = append(offs, int32(start))
			}
		}
	}
	return dst, offs
}

// AppendFoldString is AppendFold for a string.
func AppendFoldString(dst []byte, offs []int32, src string, lower bool) ([]byte, []int32) {
	return AppendFold(dst, offs, []byte(src), lower)
}

// Fold returns the lower-cased search fold of s.
func Fold(s string) string {
	b, _ := AppendFold(nil, nil, []byte(s), true)
	return string(b)
}

// FoldExact returns the search fold of s without case folding.
func FoldExact(s string) string {
	b, _ := AppendFold(nil, nil, []byte(s), false)
	return string(b)
}

// FoldRange maps the folded byte range [i, j) back to the source: the
// range covers every source character that contributed to it. offs is the
// offset map from AppendFold and n the source length.
func FoldRange(offs []int32, n, i, j int) (int, int) {
	if i >= len(offs) {
		return n, n
	}
	s := int(offs[i])
	if j <= i {
		return s, s
	}
	last := offs[j-1]
	for k := j; k < len(offs); k++ {
		if offs[k] > last {
			return s, int(offs[k])
		}
	}
	return s, n
}

// FoldIndexAll returns the source byte ranges of the non-overlapping
// occurrences of needle (already folded, with the same lower flag) in s.
func FoldIndexAll(s, needle string, lower bool) [][2]int {
	if needle == "" {
		return nil
	}
	f, offs := AppendFold(nil, nil, []byte(s), lower)
	return FoldedRanges(nil, f, offs, len(s), []byte(needle))
}

// FoldedRanges appends to dst the source ranges of the occurrences of
// needle in a folded text (folded, offs) whose source is n bytes long.
func FoldedRanges(dst [][2]int, folded []byte, offs []int32, n int, needle []byte) [][2]int {
	if len(needle) == 0 {
		return dst
	}
	for off := 0; off < len(folded); {
		k := bytes.Index(folded[off:], needle)
		if k < 0 {
			break
		}
		a, b := FoldRange(offs, n, off+k, off+k+len(needle))
		if b > a {
			if l := len(dst); l > 0 && dst[l-1][1] >= a {
				dst[l-1][1] = max(dst[l-1][1], b)
			} else {
				dst = append(dst, [2]int{a, b})
			}
		}
		off += k + len(needle)
	}
	return dst
}
