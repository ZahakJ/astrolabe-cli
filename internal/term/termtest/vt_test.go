package termtest

import (
	"testing"

	"github.com/ZahakJ/folio/internal/theme"
)

func TestVTText(t *testing.T) {
	v := New(10, 3)
	v.Write([]byte("hello\r\nworld"))
	if v.String() != "hello\nworld" {
		t.Errorf("String = %q", v.String())
	}
	if x, y := v.Cursor(); x != 5 || y != 1 {
		t.Errorf("cursor = %d,%d", x, y)
	}
}

func TestVTAutowrapAndScroll(t *testing.T) {
	v := New(4, 2)
	v.Write([]byte("abcdefghij"))
	if got := v.Lines(); got[0] != "efgh" || got[1] != "ij" {
		t.Errorf("wrap/scroll = %q", got)
	}
	v = New(4, 2)
	v.Write([]byte("\x1b[?7labcdef"))
	if got := v.Row(0); got != "abcf" {
		t.Errorf("no-autowrap row = %q", got)
	}
}

func TestVTWide(t *testing.T) {
	v := New(5, 2)
	v.Write([]byte("日本語"))
	// 語 does not fit after 日本 (4 cells), so it wraps to the next row.
	if v.Row(0) != "日本 " || v.Row(1) != "語   " {
		t.Errorf("rows = %q %q", v.Row(0), v.Row(1))
	}
	v = New(6, 1)
	v.Write([]byte("a日b"))
	if c := v.Cell(2, 0); c.Width != 0 || v.Cell(1, 0).Width != 2 {
		t.Errorf("wide cells: %+v %+v", v.Cell(1, 0), c)
	}
	v.Write([]byte("\x1b[1;3HX")) // overwrite the right half of 日
	if v.Row(0) != "a Xb  " {
		t.Errorf("row = %q", v.Row(0))
	}
}

func TestVTSGR(t *testing.T) {
	v := New(20, 1)
	v.Write([]byte("\x1b[1;3;38;2;201;162;39;48;5;233ma\x1b[22;4:3;91mb\x1b[0;7mc\x1b[38:2::1:2:3md"))
	tests := []struct {
		x    int
		want theme.Style
	}{
		{0, theme.Style{FG: theme.RGB(201, 162, 39), BG: theme.ANSI(233), Attrs: theme.Bold | theme.Italic}},
		{1, theme.Style{FG: theme.BrightRed, BG: theme.ANSI(233), Attrs: theme.Italic | theme.CurlyUnderline}},
		{2, theme.Style{Attrs: theme.Reverse}},
		{3, theme.Style{FG: theme.RGB(1, 2, 3), Attrs: theme.Reverse}},
	}
	for _, tt := range tests {
		if got := v.Style(tt.x, 0); got != tt.want {
			t.Errorf("cell %d style = %+v, want %+v", tt.x, got, tt.want)
		}
	}
}

func TestVTEraseAndModes(t *testing.T) {
	v := New(6, 2)
	v.Write([]byte("abcdef\x1b[2;1Hghijkl\x1b[1;3H\x1b[K"))
	if v.Row(0) != "ab    " {
		t.Errorf("EL = %q", v.Row(0))
	}
	v.Write([]byte("\x1b[2J"))
	if v.String() != "" {
		t.Errorf("ED = %q", v.String())
	}
	v.Write([]byte("\x1b[Hmain\x1b[?1049h\x1b[H\x1b[?25l\x1b[?2026hALT\x1b[?2026l\x1b[6 q"))
	if !v.AltScreen() || v.CursorVisible() || v.CursorShape() != 6 || v.Row(0)[:3] != "ALT" {
		t.Errorf("alt=%v cursor=%v shape=%d row=%q", v.AltScreen(), v.CursorVisible(), v.CursorShape(), v.Row(0))
	}
	v.Write([]byte("\x1b[?1049l"))
	if v.AltScreen() || v.Row(0)[:4] != "main" {
		t.Errorf("main screen not restored: %q", v.Row(0))
	}
	if ok, n := v.SyncBalanced(); !ok || n != 1 {
		t.Error("sync count")
	}
}

func TestVTOSC(t *testing.T) {
	v := New(10, 1)
	v.Write([]byte("\x1b]8;;https://example.org\x1b\\ab\x1b]8;;\x1b\\c\x1b]52;c;aGVsbG8=\x07"))
	if v.Style(0, 0).Link != "https://example.org" || v.Style(2, 0).Link != "" {
		t.Error("OSC 8 links")
	}
	if v.Clipboard() != "hello" {
		t.Errorf("clipboard = %q", v.Clipboard())
	}
	if v.Row(0) != "abc       " {
		t.Errorf("OSC leaked into text: %q", v.Row(0))
	}
}
