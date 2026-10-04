package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestDraw(t *testing.T) {
	// Row 0: truecolour ground across the row; row 1: bold accent "A<", a
	// wide CJK cell, a box-drawing rule and a partial-block bar.
	ground := "\x1b[48;2;22;19;14m"
	raw := ground + strings.Repeat(" ", 10) + "\n" +
		ground + "\x1b[1;38;2;201;162;39mA<\x1b[22m星─▎\x1b[0m\n"
	vt := load([]byte(raw), 10, 3)
	svg := draw(vt, options{cols: 10, rows: 3, title: "t & co", frame: true})

	for _, want := range []string{
		`fill="#16130e" stroke=`,        // the frame takes the painted ground
		`font-weight:bold`,              // bold class
		`>A</text>`,                     // one element per grapheme
		`>&lt;</text>`,                  // escaped
		`text-anchor="middle">星</text>`, // wide cell centred on its two cells
		`t &amp; co`,                    // escaped title
		`unicode-bidi:bidi-override`,    // no browser reordering
	} {
		if !strings.Contains(svg, want) {
			t.Errorf("SVG lacks %q", want)
		}
	}
	// "A" sits at cell 0 and "<" at cell 1 of row 1, absolutely.
	x0 := num(padX)
	x1 := num(padX + cellW)
	y1 := num(barH + padY*0.6 + cellH + baseline)
	for _, want := range []string{
		fmt.Sprintf(`x="%s" y="%s">A<`, x0, y1),
		fmt.Sprintf(`x="%s" y="%s">&lt;<`, x1, y1),
	} {
		if !strings.Contains(svg, want) {
			t.Errorf("SVG lacks positioned glyph %q", want)
		}
	}
	// Box drawing and the bar are vector shapes, not text.
	if strings.Contains(svg, ">─<") || strings.Contains(svg, ">▎<") {
		t.Error("box-drawing glyphs should be drawn as shapes")
	}
}

func TestLoadDoesNotWrapFullRows(t *testing.T) {
	raw := "abcd\nefgh\n"
	vt := load([]byte(raw), 4, 2)
	if got := vt.Row(0); got != "abcd" {
		t.Errorf("row 0 = %q", got)
	}
	if got := vt.Row(1); got != "efgh" {
		t.Errorf("row 1 = %q", got)
	}
}

func TestPaletteAndAttrs(t *testing.T) {
	raw := "\x1b[31mr\x1b[1mR\x1b[0m\x1b[7mv\x1b[0m\x1b[2mf\x1b[0m\x1b[38;5;196mx\x1b[0m"
	vt := load([]byte(raw), 6, 1)
	d := &drawer{pal: darkPalette, classes: map[string]string{}}
	if c := d.resolve(vt.Cell(0, 0).Style).fg; c != darkPalette.ansi[1] {
		t.Errorf("ANSI red = %s", c)
	}
	if c := d.resolve(vt.Cell(1, 0).Style).fg; c != darkPalette.ansi[9] {
		t.Errorf("bold red should brighten, got %s", c)
	}
	if cs := d.resolve(vt.Cell(2, 0).Style); cs.fg != darkPalette.bg || cs.bg != darkPalette.fg {
		t.Errorf("reverse: %+v", cs)
	}
	if c := d.resolve(vt.Cell(3, 0).Style).fg; c == darkPalette.fg {
		t.Error("faint should dim the foreground")
	}
	if c := d.resolve(vt.Cell(4, 0).Style).fg; c != "#ff0000" {
		t.Errorf("256-colour 196 = %s", c)
	}
}
