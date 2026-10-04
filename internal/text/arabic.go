package text

import (
	"strings"
	"unicode"
)

// Arabic shaping: a terminal draws one glyph per cell and, in most
// terminals, does no contextual shaping, so Arabic letters appear in their
// isolated forms, unjoined. Shape replaces each letter with its contextual
// presentation form (Unicode Arabic Presentation Forms-B, plus Forms-A for
// Persian and Urdu letters) so the text reads joined in any terminal that has
// the glyphs.

// arabicForms maps a letter to its {isolated, final, initial, medial}
// presentation forms; 0 marks a form that does not exist. A letter with
// initial and medial forms is dual-joining; with only a final form it is
// right-joining (it never connects to the letter after it).
var arabicForms = map[rune][4]rune{
	0x0621: {0xFE80, 0, 0, 0},                // ء hamza
	0x0622: {0xFE81, 0xFE82, 0, 0},           // آ
	0x0623: {0xFE83, 0xFE84, 0, 0},           // أ
	0x0624: {0xFE85, 0xFE86, 0, 0},           // ؤ
	0x0625: {0xFE87, 0xFE88, 0, 0},           // إ
	0x0626: {0xFE89, 0xFE8A, 0xFE8B, 0xFE8C}, // ئ
	0x0627: {0xFE8D, 0xFE8E, 0, 0},           // ا
	0x0628: {0xFE8F, 0xFE90, 0xFE91, 0xFE92}, // ب
	0x0629: {0xFE93, 0xFE94, 0, 0},           // ة
	0x062A: {0xFE95, 0xFE96, 0xFE97, 0xFE98}, // ت
	0x062B: {0xFE99, 0xFE9A, 0xFE9B, 0xFE9C}, // ث
	0x062C: {0xFE9D, 0xFE9E, 0xFE9F, 0xFEA0}, // ج
	0x062D: {0xFEA1, 0xFEA2, 0xFEA3, 0xFEA4}, // ح
	0x062E: {0xFEA5, 0xFEA6, 0xFEA7, 0xFEA8}, // خ
	0x062F: {0xFEA9, 0xFEAA, 0, 0},           // د
	0x0630: {0xFEAB, 0xFEAC, 0, 0},           // ذ
	0x0631: {0xFEAD, 0xFEAE, 0, 0},           // ر
	0x0632: {0xFEAF, 0xFEB0, 0, 0},           // ز
	0x0633: {0xFEB1, 0xFEB2, 0xFEB3, 0xFEB4}, // س
	0x0634: {0xFEB5, 0xFEB6, 0xFEB7, 0xFEB8}, // ش
	0x0635: {0xFEB9, 0xFEBA, 0xFEBB, 0xFEBC}, // ص
	0x0636: {0xFEBD, 0xFEBE, 0xFEBF, 0xFEC0}, // ض
	0x0637: {0xFEC1, 0xFEC2, 0xFEC3, 0xFEC4}, // ط
	0x0638: {0xFEC5, 0xFEC6, 0xFEC7, 0xFEC8}, // ظ
	0x0639: {0xFEC9, 0xFECA, 0xFECB, 0xFECC}, // ع
	0x063A: {0xFECD, 0xFECE, 0xFECF, 0xFED0}, // غ
	0x0641: {0xFED1, 0xFED2, 0xFED3, 0xFED4}, // ف
	0x0642: {0xFED5, 0xFED6, 0xFED7, 0xFED8}, // ق
	0x0643: {0xFED9, 0xFEDA, 0xFEDB, 0xFEDC}, // ك
	0x0644: {0xFEDD, 0xFEDE, 0xFEDF, 0xFEE0}, // ل
	0x0645: {0xFEE1, 0xFEE2, 0xFEE3, 0xFEE4}, // م
	0x0646: {0xFEE5, 0xFEE6, 0xFEE7, 0xFEE8}, // ن
	0x0647: {0xFEE9, 0xFEEA, 0xFEEB, 0xFEEC}, // ه
	0x0648: {0xFEED, 0xFEEE, 0, 0},           // و
	0x0649: {0xFEEF, 0xFEF0, 0xFBE8, 0xFBE9}, // ى
	0x064A: {0xFEF1, 0xFEF2, 0xFEF3, 0xFEF4}, // ي
	// Presentation Forms-A: Persian, Urdu and other extensions.
	0x0671: {0xFB50, 0xFB51, 0, 0},           // ٱ
	0x0679: {0xFB66, 0xFB67, 0xFB68, 0xFB69}, // ٹ
	0x067A: {0xFB5E, 0xFB5F, 0xFB60, 0xFB61}, // ٺ
	0x067B: {0xFB52, 0xFB53, 0xFB54, 0xFB55}, // ٻ
	0x067E: {0xFB56, 0xFB57, 0xFB58, 0xFB59}, // پ
	0x067F: {0xFB62, 0xFB63, 0xFB64, 0xFB65}, // ٿ
	0x0680: {0xFB5A, 0xFB5B, 0xFB5C, 0xFB5D}, // ڀ
	0x0683: {0xFB76, 0xFB77, 0xFB78, 0xFB79}, // ڃ
	0x0684: {0xFB72, 0xFB73, 0xFB74, 0xFB75}, // ڄ
	0x0686: {0xFB7A, 0xFB7B, 0xFB7C, 0xFB7D}, // چ
	0x0687: {0xFB7E, 0xFB7F, 0xFB80, 0xFB81}, // ڇ
	0x0688: {0xFB88, 0xFB89, 0, 0},           // ڈ
	0x068C: {0xFB84, 0xFB85, 0, 0},           // ڌ
	0x068D: {0xFB82, 0xFB83, 0, 0},           // ڍ
	0x068E: {0xFB86, 0xFB87, 0, 0},           // ڎ
	0x0691: {0xFB8C, 0xFB8D, 0, 0},           // ڑ
	0x0698: {0xFB8A, 0xFB8B, 0, 0},           // ژ
	0x06A4: {0xFB6A, 0xFB6B, 0xFB6C, 0xFB6D}, // ڤ
	0x06A6: {0xFB6E, 0xFB6F, 0xFB70, 0xFB71}, // ڦ
	0x06A9: {0xFB8E, 0xFB8F, 0xFB90, 0xFB91}, // ک
	0x06AD: {0xFBD3, 0xFBD4, 0xFBD5, 0xFBD6}, // ڭ
	0x06AF: {0xFB92, 0xFB93, 0xFB94, 0xFB95}, // گ
	0x06B1: {0xFB9A, 0xFB9B, 0xFB9C, 0xFB9D}, // ڱ
	0x06B3: {0xFB96, 0xFB97, 0xFB98, 0xFB99}, // ڳ
	0x06BA: {0xFB9E, 0xFB9F, 0, 0},           // ں (no initial/medial forms encoded)
	0x06BB: {0xFBA0, 0xFBA1, 0xFBA2, 0xFBA3}, // ڻ
	0x06BE: {0xFBAA, 0xFBAB, 0xFBAC, 0xFBAD}, // ھ
	0x06C0: {0xFBA4, 0xFBA5, 0, 0},           // ۀ
	0x06C1: {0xFBA6, 0xFBA7, 0xFBA8, 0xFBA9}, // ہ
	0x06C5: {0xFBE0, 0xFBE1, 0, 0},           // ۅ
	0x06C6: {0xFBD9, 0xFBDA, 0, 0},           // ۆ
	0x06C7: {0xFBD7, 0xFBD8, 0, 0},           // ۇ
	0x06C8: {0xFBDB, 0xFBDC, 0, 0},           // ۈ
	0x06C9: {0xFBE2, 0xFBE3, 0, 0},           // ۉ
	0x06CB: {0xFBDE, 0xFBDF, 0, 0},           // ۋ
	0x06CC: {0xFBFC, 0xFBFD, 0xFBFE, 0xFBFF}, // ی Farsi yeh
	0x06D0: {0xFBE4, 0xFBE5, 0xFBE6, 0xFBE7}, // ې
	0x06D2: {0xFBAE, 0xFBAF, 0, 0},           // ے
	0x06D3: {0xFBB0, 0xFBB1, 0, 0},           // ۓ
}

// lamAlef maps an alef variant to the {isolated, final} lam-alef ligature.
var lamAlef = map[rune][2]rune{
	0x0622: {0xFEF5, 0xFEF6}, // لآ
	0x0623: {0xFEF7, 0xFEF8}, // لأ
	0x0625: {0xFEF9, 0xFEFA}, // لإ
	0x0627: {0xFEFB, 0xFEFC}, // لا
}

const (
	lam     = 0x0644
	tatweel = 0x0640
	zwj     = 0x200D
	zwnj    = 0x200C
)

type joining uint8

const (
	joinU joining = iota // non-joining
	joinR                // right-joining: connects only to the letter before
	joinD                // dual-joining
	joinC                // join-causing (tatweel, ZWJ)
	joinT                // transparent (harakat and other marks)
)

func joiningType(r rune) joining {
	if f, ok := arabicForms[r]; ok {
		switch {
		case f[2] != 0 && f[3] != 0:
			return joinD
		case f[1] != 0:
			return joinR
		}
		return joinU
	}
	switch r {
	case tatweel, zwj:
		return joinC
	case zwnj:
		return joinU
	}
	if r >= 0x300 && (unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r)) {
		return joinT
	}
	return joinU
}

// needsShaping reports whether s contains a letter Shape would change.
func needsShaping(s string) bool {
	for _, r := range s {
		if r >= 0x0621 && r <= 0x06D3 {
			if _, ok := arabicForms[r]; ok {
				return true
			}
		}
	}
	return false
}

// Shape returns s with Arabic-script letters replaced by their contextual
// presentation forms (isolated, initial, medial, final) and lam-alef pairs
// fused into ligatures. Harakat and other combining marks stay attached to
// their base letter and are transparent to joining; tatweel and ZWJ cause
// joining, ZWNJ prevents it. Text without Arabic letters, and text that is
// already shaped, is returned unchanged (Shape is idempotent).
//
// Shape works in logical order; reorder afterwards (see Visual). A lam-alef
// ligature turns two letters into one cell; use ShapeClusters when a mapping
// from source clusters to output cells is needed.
func Shape(s string) string {
	if !needsShaping(s) {
		return s
	}
	gs := Graphemes(s)
	cl := make([]string, len(gs))
	for i, g := range gs {
		cl[i] = g.Text
	}
	return strings.Join(ShapeClusters(cl), "")
}

// ShapeClusters shapes a line given as grapheme clusters and returns exactly
// one output cluster per input cluster, so indexes stay aligned with the
// source (the editor relies on this to map its cursor). When a lam and the
// following alef fuse, the lam's cluster holds the ligature together with
// both letters' marks and the alef's cluster becomes "" (zero cells).
// If nothing needs shaping the input slice itself is returned.
func ShapeClusters(clusters []string) []string {
	if !needsShaping(strings.Join(clusters, "")) {
		return clusters
	}
	var runes []rune
	var owner []int
	for ci, c := range clusters {
		for _, r := range c {
			runes = append(runes, r)
			owner = append(owner, ci)
		}
	}
	n := len(runes)
	types := make([]joining, n)
	for i, r := range runes {
		types[i] = joiningType(r)
	}
	prevJoinable := func(i int) bool { // does the letter before i connect to it?
		for j := i - 1; j >= 0; j-- {
			switch types[j] {
			case joinT:
				continue
			case joinD, joinC:
				return true
			}
			return false
		}
		return false
	}
	nextIndex := func(i int) int {
		for j := i + 1; j < n; j++ {
			if types[j] != joinT {
				return j
			}
		}
		return -1
	}
	out := make([]rune, n)
	copy(out, runes)
	drop := make([]bool, n)
	moveTo := map[int]int{} // fused alef cluster → lam cluster
	for i, r := range runes {
		t := types[i]
		if t == joinU {
			// A non-joining letter with a presentation form (hamza) takes
			// it too, so a shaped word draws all its letters from one
			// font: run-reversing terminals (kitty) split runs at a font
			// change (see RTLRuns).
			if f, ok := arabicForms[r]; ok && f[0] != 0 {
				out[i] = f[0]
			}
			continue
		}
		if t != joinD && t != joinR {
			continue
		}
		jp := prevJoinable(i)
		nx := nextIndex(i)
		if r == lam && nx >= 0 {
			if lig, ok := lamAlef[runes[nx]]; ok {
				if jp {
					out[i] = lig[1]
				} else {
					out[i] = lig[0]
				}
				drop[nx] = true
				moveTo[owner[nx]] = owner[i]
				continue
			}
		}
		if drop[i] {
			continue
		}
		jn := t == joinD && nx >= 0 && (types[nx] == joinD || types[nx] == joinR || types[nx] == joinC)
		f := arabicForms[r]
		var form rune
		switch {
		case jp && jn:
			form = f[3]
		case jp:
			form = f[1]
		case jn:
			form = f[2]
		default:
			form = f[0]
		}
		if form != 0 {
			out[i] = form
		}
	}
	res := make([]strings.Builder, len(clusters))
	for i := 0; i < n; i++ {
		if drop[i] {
			continue
		}
		dst := owner[i]
		if to, ok := moveTo[dst]; ok {
			dst = to // marks of a fused alef join the ligature
		}
		res[dst].WriteRune(out[i])
	}
	result := make([]string, len(clusters))
	for i := range res {
		result[i] = res[i].String()
	}
	return result
}
