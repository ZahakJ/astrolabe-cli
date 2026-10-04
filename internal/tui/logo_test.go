package tui

import (
	"testing"
	"unicode/utf8"

	"github.com/ZahakJ/astrolabe-cli/internal/text"
)

func TestLogoShape(t *testing.T) {
	for name, l := range map[string]struct{ rows, classes []string }{"braille": logoArt, "ascii": logoASCII} {
		if len(l.rows) != len(l.classes) || len(l.rows) < 5 || len(l.rows) > 11 {
			t.Fatalf("%s: %d rows, %d class rows", name, len(l.rows), len(l.classes))
		}
		w := text.Width(l.rows[0])
		for i, r := range l.rows {
			if text.Width(r) != w || utf8.RuneCountInString(r) != w || len(l.classes[i]) != w {
				t.Errorf("%s row %d: width %d, runes %d, classes %d, want %d", name, i, text.Width(r), utf8.RuneCountInString(r), len(l.classes[i]), w)
			}
			if name == "ascii" {
				for _, c := range r {
					if c >= 0x80 {
						t.Errorf("ascii row %d has %q", i, c)
					}
				}
			}
		}
	}
}

func TestHomeShowsLogo(t *testing.T) {
	h := newHarness(t, harnessOpt{w: 100, h: 40})
	h.start(0, "", 0)
	h.contains(logoArt.rows[5])
	small := newHarness(t, harnessOpt{w: 100, h: 20})
	small.start(0, "", 0)
	small.lacks(logoArt.rows[5])
}
