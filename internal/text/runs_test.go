package text

import (
	"math/rand"
	"strings"
	"testing"
	"unicode"
)

// kittyFont models the font a run-reversing terminal (kitty) picks for a
// cell in a common setup: a programming font for ASCII, Latin, punctuation
// and astrolabe's glyphs; a fallback font for right-to-left letters and their
// presentation forms; Arabic-script digits in yet another; blanks break
// every run.
func kittyFont(g string) int {
	if g == "" || g == " " || g == "\u2002" || g == "\t" {
		return -1 // BLANK_FONT
	}
	r := []rune(g)[0]
	switch {
	case r >= 0x0660 && r <= 0x0669, r >= 0x06F0 && r <= 0x06F9:
		return 2
	case unicode.IsLetter(r) && unicode.In(r, unicode.Arabic, unicode.Hebrew):
		return 1
	}
	return 0
}

// kittyScriptRTL reports whether the first character of a real script in
// run is right to left (hb_buffer_guess_segment_properties: Common and
// Inherited characters are skipped).
func kittyScriptRTL(run []RunCell) bool {
	for _, c := range run {
		for _, r := range c.Text {
			switch {
			case r >= 0x0660 && r <= 0x0669, r >= 0x06F0 && r <= 0x06F9:
				return true // Arabic-Indic digits are script Arabic
			case unicode.IsLetter(r):
				return RuneDirection(r) == RTL
			}
		}
	}
	return false
}

// simulateKitty returns what a run-reversing terminal shows for the emitted
// row: cells are split into runs of one font and face, and every run laid
// out right to left has its texts reversed in place (faces, standing for
// all per-cell styling, stay).
func simulateKitty(row []RunCell) []RunCell {
	out := append([]RunCell(nil), row...)
	for i := 0; i < len(out); {
		f := kittyFont(out[i].Text)
		if f < 0 {
			i++
			continue
		}
		j := i + 1
		for j < len(out) && kittyFont(out[j].Text) == f && out[j].Face == out[i].Face {
			j++
		}
		if kittyScriptRTL(out[i:j]) {
			for a, b := i, j-1; a < b; a, b = a+1, b-1 {
				out[a].Text, out[b].Text = out[b].Text, out[a].Text
			}
		}
		i = j
	}
	return out
}

func cellsOf(s string) []RunCell {
	var row []RunCell
	for _, g := range Graphemes(s) {
		row = append(row, RunCell{Text: g.Text})
	}
	return row
}

func joinCells(row []RunCell) string {
	var b strings.Builder
	for _, c := range row {
		b.WriteString(c.Text)
	}
	return b.String()
}

func TestRunsString(t *testing.T) {
	sh := Shape
	tests := []struct {
		name, logical, want string
	}{
		{"single word", "مرحبا", sh("مرحبا")},
		{"two words", "مرحبا بالعالم", sh("بالعالم") + " " + sh("مرحبا")},
		{"three words of different lengths", "ب كتاب المكتبات", sh("المكتبات") + " " + sh("كتاب") + " " + sh("ب")},
		{"full stop after", "مرحبا.", "." + sh("مرحبا")},
		{"punctuation both sides", "(مرحبا)", "(" + sh("مرحبا") + ")"},
		{"quote and colon", "«قال»: نعم", sh("نعم") + " :«" + sh("قال") + "»"},
		{"tag", "#وسم", sh("وسم") + "#"},
		{"Latin in Arabic", "مرحبا world عالم", sh("عالم") + " world " + sh("مرحبا")},
		{"digits in Arabic", "عدد 123 كتب", sh("كتب") + " 123 " + sh("عدد")},
		{"Arabic in Latin", "the word كتاب here", "the word " + sh("كتاب") + " here"},
		{"Arabic-Indic digits", "سنة ١٤٤٥", "٥٤٤١" + " " + sh("سنة")}, // pre-reversed: the terminal reverses the digit run back
		{"harakat", "مَرْحَبًا", sh("مَرْحَبًا")},
		{"lam-alef", "السلام عليكم", sh("عليكم") + " " + sh("السلام")},
		{"Hebrew", "שלום עולם", "עולם שלום"},
		{"ellipsis", "مرحبا بالعالم…", "…" + sh("بالعالم") + " " + sh("مرحبا")},
		{"LTR only", "hello (world) 123 · ✦", "hello (world) 123 · ✦"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		vis := Visual(tt.logical, BaseDirection(tt.logical))
		got := RunsString(vis)
		if got != tt.want {
			t.Errorf("%s: RunsString(Visual(%q)) = %q, want %q", tt.name, tt.logical, got, tt.want)
		}
		// What the terminal shows is the visual line again.
		if shown := joinCells(simulateKitty(cellsOf(got))); shown != vis {
			t.Errorf("%s: terminal shows %q, want visual %q", tt.name, shown, vis)
		}
		// Idempotence on the transform pair: applying it twice restores.
		if back := RunsString(got); back != vis {
			t.Errorf("%s: RunsString twice = %q, want %q", tt.name, back, vis)
		}
	}
}

func TestReverseRTLRunsFaces(t *testing.T) {
	const bold = 1
	// "كتاب جميل" with the second word bold except its last letter: the face
	// change ends a run, so each face's part is reversed on its own.
	vis := []rune(Visual("كتاب جميل", RTL))
	row := make([]RunCell, len(vis))
	for i, r := range vis {
		row[i].Text = string(r)
	}
	// Visual: cells 0..3 hold the second word (left), 4 a space, 5..8 the first.
	for i := 1; i <= 3; i++ {
		row[i].Face = bold
	}
	orig := append([]RunCell(nil), row...)
	ReverseRTLRuns(row)
	if row[0] != orig[0] {
		t.Errorf("single-cell run changed: %q → %q", orig[0].Text, row[0].Text)
	}
	for i := 1; i <= 3; i++ {
		if row[i].Text != orig[4-i].Text || row[i].Face != bold {
			t.Errorf("bold run cell %d = %+v, want text %q bold", i, row[i], orig[4-i].Text)
		}
	}
	for i := 5; i <= 8; i++ {
		if row[i].Text != orig[13-i].Text || row[i].Face != 0 {
			t.Errorf("plain run cell %d = %+v, want text %q", i, row[i], orig[13-i].Text)
		}
	}
	if got := simulateKitty(row); !equalCells(got, orig) {
		t.Errorf("terminal shows %v, want %v", got, orig)
	}
}

func TestReverseRTLRunsBreaks(t *testing.T) {
	// Empty cells (wide-glyph halves) and blanks end runs; wide cells are
	// never part of one.
	row := []RunCell{{Text: "ﺏ"}, {Text: "ﺕ"}, {Text: ""}, {Text: "ﺙ"}, {Text: "ﺝ"}, {Text: "日"}, {Text: ""}, {Text: "ﺡ"}, {Text: "ﺥ"}}
	ReverseRTLRuns(row)
	want := []string{"ﺕ", "ﺏ", "", "ﺝ", "ﺙ", "日", "", "ﺥ", "ﺡ"}
	for i, c := range row {
		if c.Text != want[i] {
			t.Fatalf("got %v, want %v", row, want)
		}
	}
	var runs [][2]int
	RTLRuns(3, func(i int) (string, uint8) { return []string{"ﺏ", "١", "٢"}[i], 0 }, func(s, e int) { runs = append(runs, [2]int{s, e}) })
	if len(runs) != 1 || runs[0] != [2]int{1, 3} {
		t.Errorf("letters and digits must form separate runs: %v", runs)
	}
	if MayHaveRTLRuns("plain ─ ✦ 日本 …") || !MayHaveRTLRuns("x ﺏ") || !MayHaveRTLRuns("שלום") {
		t.Error("MayHaveRTLRuns")
	}
}

func equalCells(a, b []RunCell) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestRTLRunsProperty: for random visual rows of letters, digits,
// punctuation, blanks, glyphs and faces, emitting ReverseRTLRuns' result on
// a run-reversing terminal shows the original visual row.
func TestRTLRunsProperty(t *testing.T) {
	alphabet := []string{
		"ﺏ", "ﺑ", "ﺒ", "ﺐ", "ﻻ", "ﻼ", "ﺍ", "ﺎ", "ﻡ", "ﻣ", "ﺮ", "ﺑَ", "ﻢْ", // presentation forms, ligatures, harakat
		"ש", "ל", "ו", "ם", // Hebrew
		"١", "٢", "۳", // Arabic-script digits
		"a", "Z", "é", "1", "2", // Latin and digits
		".", ",", "(", ")", "#", ":", "،", "…", "«", "»", "✦", "─", // neutrals and glyphs
		" ", "\u2002", "", // blanks
	}
	rng := rand.New(rand.NewSource(7))
	for iter := 0; iter < 5000; iter++ {
		n := rng.Intn(24)
		row := make([]RunCell, n)
		for i := range row {
			row[i].Text = alphabet[rng.Intn(len(alphabet))]
			if rng.Intn(5) == 0 {
				row[i].Face = uint8(rng.Intn(4))
			}
		}
		orig := append([]RunCell(nil), row...)
		ReverseRTLRuns(row)
		if got := simulateKitty(row); !equalCells(got, orig) {
			t.Fatalf("row %v\nemitted %v\nterminal shows %v", orig, row, got)
		}
		ReverseRTLRuns(row)
		if !equalCells(row, orig) {
			t.Fatalf("ReverseRTLRuns twice changed %v into %v", orig, row)
		}
	}
	// And through the whole display pipeline on random logical lines.
	words := []string{"مرحبا", "كتاب", "السلام", "مَرْحَبًا", "שלום", "hello", "42", "١٢", "(x)", "#وسم", "«قال»:", "…", "،"}
	for iter := 0; iter < 2000; iter++ {
		var parts []string
		for k := rng.Intn(6) + 1; k > 0; k-- {
			parts = append(parts, words[rng.Intn(len(words))])
		}
		s := strings.Join(parts, " ")
		vis := Visual(s, BaseDirection(s))
		if got := joinCells(simulateKitty(cellsOf(RunsString(vis)))); got != vis {
			t.Fatalf("%q: visual %q, terminal shows %q", s, vis, got)
		}
	}
}

func TestRunsStringLTRUnchanged(t *testing.T) {
	for _, s := range []string{"", "plain text", "  ✦ astrolabe · notes ─── 12:30", "日本語テキスト", "Café naïve", "x ש"} {
		if got := RunsString(s); got != s {
			t.Errorf("RunsString(%q) = %q", s, got)
		}
	}
}
