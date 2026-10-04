package term

import (
	"reflect"
	"testing"
)

func key(k Key, m Mod) KeyEvent { return KeyEvent{Key: k, Mod: m} }
func ch(r rune, m Mod) KeyEvent { return KeyEvent{Key: KeyRune, Rune: r, Mod: m} }

func TestDecodeKeys(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Event
		n    int
	}{
		{"letter", "a", ch('a', 0), 1},
		{"upper", "A", ch('A', 0), 1},
		{"space", " ", ch(' ', 0), 1},
		{"utf8", "é", ch('é', 0), 2},
		{"cjk", "日", ch('日', 0), 3},
		{"arabic", "س", ch('س', 0), 2},
		{"enter", "\r", key(KeyEnter, 0), 1},
		{"tab", "\t", key(KeyTab, 0), 1},
		{"del backspace", "\x7f", key(KeyBackspace, 0), 1},
		{"ctrl-h backspace", "\x08", key(KeyBackspace, 0), 1},
		{"ctrl-d", "\x04", ch('d', ModCtrl), 1},
		{"ctrl-z", "\x1a", ch('z', ModCtrl), 1},
		{"ctrl-]", "\x1d", ch(']', ModCtrl), 1},
		{"ctrl-space", "\x00", ch(' ', ModCtrl), 1},
		{"ctrl-j", "\n", ch('j', ModCtrl), 1},
		{"up", "\x1b[A", key(KeyUp, 0), 3},
		{"down ss3", "\x1bOB", key(KeyDown, 0), 3},
		{"ctrl-right", "\x1b[1;5C", key(KeyRight, ModCtrl), 6},
		{"shift-up", "\x1b[1;2A", key(KeyUp, ModShift), 6},
		{"alt-left", "\x1b[1;3D", key(KeyLeft, ModAlt), 6},
		{"ctrl-shift-down", "\x1b[1;6B", key(KeyDown, ModCtrl|ModShift), 6},
		{"home", "\x1b[H", key(KeyHome, 0), 3},
		{"end ss3", "\x1bOF", key(KeyEnd, 0), 3},
		{"home tilde", "\x1b[1~", key(KeyHome, 0), 4},
		{"end tilde 4", "\x1b[4~", key(KeyEnd, 0), 4},
		{"delete", "\x1b[3~", key(KeyDelete, 0), 4},
		{"pgup", "\x1b[5~", key(KeyPageUp, 0), 4},
		{"pgdn ctrl", "\x1b[6;5~", key(KeyPageDown, ModCtrl), 6},
		{"insert", "\x1b[2~", key(KeyInsert, 0), 4},
		{"f1 ss3", "\x1bOP", key(KeyF1, 0), 3},
		{"f1 csi mod", "\x1b[1;2P", key(KeyF1, ModShift), 6},
		{"f5", "\x1b[15~", key(KeyF5, 0), 5},
		{"f12", "\x1b[24~", key(KeyF12, 0), 5},
		{"shift-tab", "\x1b[Z", key(KeyTab, ModShift), 3},
		{"alt-x", "\x1bx", ch('x', ModAlt), 2},
		{"alt-X", "\x1bX", ch('X', ModAlt), 2},
		{"alt-backspace", "\x1b\x7f", key(KeyBackspace, ModAlt), 2},
		{"alt-enter", "\x1b\r", key(KeyEnter, ModAlt), 2},
		{"alt-utf8", "\x1bé", ch('é', ModAlt), 3},
		{"alt-ctrl-a", "\x1b\x01", ch('a', ModAlt|ModCtrl), 2},
		{"esc esc", "\x1b\x1b", key(KeyEscape, 0), 1},
		{"kitty a", "\x1b[97u", ch('a', 0), 5},
		{"kitty ctrl-a", "\x1b[97;5u", ch('a', ModCtrl), 7},
		{"kitty shift-a", "\x1b[97;2u", ch('A', 0), 7},
		{"kitty shift-1 alternate", "\x1b[49:33;2u", ch('!', 0), 10},
		{"kitty ctrl-shift-a", "\x1b[97;6u", ch('a', ModCtrl|ModShift), 7},
		{"kitty esc", "\x1b[27u", key(KeyEscape, 0), 5},
		{"kitty enter", "\x1b[13u", key(KeyEnter, 0), 5},
		{"kitty shift-enter", "\x1b[13;2u", key(KeyEnter, ModShift), 7},
		{"kitty alt-tab", "\x1b[9;3u", key(KeyTab, ModAlt), 6},
		{"kitty backspace ctrl", "\x1b[127;5u", key(KeyBackspace, ModCtrl), 8},
		{"kitty release ignored", "\x1b[97;1:3u", nil, 9},
		{"kitty lone shift ignored", "\x1b[57441;2u", nil, 10},
		{"kitty keypad 5", "\x1b[57404u", ch('5', 0), 8},
		{"kitty keypad enter", "\x1b[57414u", key(KeyEnter, 0), 8},
		{"kitty capslock masked", "\x1b[97;65u", ch('a', 0), 8},
		{"modifyOtherKeys ctrl-i", "\x1b[27;5;105~", ch('i', ModCtrl), 11},
		{"modifyOtherKeys ctrl-enter", "\x1b[27;5;13~", key(KeyEnter, ModCtrl), 10},
		{"focus in", "\x1b[I", FocusEvent{Focused: true}, 3},
		{"focus out", "\x1b[O", FocusEvent{Focused: false}, 3},
		{"device attributes response ignored", "\x1b[?62;22c", nil, 9},
		{"unknown csi ignored", "\x1b[5y", nil, 4},
		{"osc response ignored", "\x1b]11;rgb:0000/0000/0000\x07", nil, 24},
		{"osc st response ignored", "\x1b]11;x\x1b\\", nil, 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, n := Decode([]byte(tt.in), false)
			if !reflect.DeepEqual(ev, tt.want) || n != tt.n {
				t.Errorf("Decode(%q) = %#v, %d; want %#v, %d", tt.in, ev, n, tt.want, tt.n)
			}
		})
	}
}

func TestDecodeIncomplete(t *testing.T) {
	for _, in := range []string{"\x1b", "\x1b[", "\x1b[1;5", "\x1bO", "\x1b[<0;10", "\xe6\x97", "\x1b]11;rgb", "\x1b[200~partial"} {
		if ev, n := Decode([]byte(in), false); n != 0 || ev != nil {
			t.Errorf("Decode(%q, false) = %#v, %d; want need-more", in, ev, n)
		}
	}
	// Final (after the escape timeout) resolves them.
	tests := []struct {
		in   string
		want Event
		n    int
	}{
		{"\x1b", key(KeyEscape, 0), 1},
		{"\x1b[", ch('[', ModAlt), 2},
		{"\x1bO", ch('O', ModAlt), 2},
		{"\xe6\x97", ch('�', 0), 1},
	}
	for _, tt := range tests {
		ev, n := Decode([]byte(tt.in), true)
		if !reflect.DeepEqual(ev, tt.want) || n != tt.n {
			t.Errorf("Decode(%q, true) = %#v, %d; want %#v, %d", tt.in, ev, n, tt.want, tt.n)
		}
	}
	// A paste in progress keeps waiting even when final.
	if ev, n := Decode([]byte("\x1b[200~slow paste"), true); n != 0 || ev != nil {
		t.Errorf("unterminated paste resolved early: %#v %d", ev, n)
	}
}

func TestDecodePaste(t *testing.T) {
	in := "\x1b[200~line one\nline \x1b[A two\x1b[201~x"
	ev, n := Decode([]byte(in), false)
	want := PasteEvent{Text: "line one\nline \x1b[A two"}
	if !reflect.DeepEqual(ev, want) || n != len(in)-1 {
		t.Errorf("paste = %#v, %d", ev, n)
	}
}

func TestDecodeMouse(t *testing.T) {
	tests := []struct {
		in   string
		want MouseEvent
	}{
		{"\x1b[<0;10;5M", MouseEvent{X: 9, Y: 4, Button: MouseLeft, Action: MousePress}},
		{"\x1b[<0;10;5m", MouseEvent{X: 9, Y: 4, Button: MouseLeft, Action: MouseRelease}},
		{"\x1b[<2;1;1M", MouseEvent{X: 0, Y: 0, Button: MouseRight, Action: MousePress}},
		{"\x1b[<1;3;4M", MouseEvent{X: 2, Y: 3, Button: MouseMiddle, Action: MousePress}},
		{"\x1b[<64;20;8M", MouseEvent{X: 19, Y: 7, Button: MouseWheelUp, Action: MousePress}},
		{"\x1b[<65;20;8M", MouseEvent{X: 19, Y: 7, Button: MouseWheelDown, Action: MousePress}},
		{"\x1b[<16;5;5M", MouseEvent{X: 4, Y: 4, Button: MouseLeft, Action: MousePress, Mod: ModCtrl}},
		{"\x1b[<32;6;6M", MouseEvent{X: 5, Y: 5, Button: MouseLeft, Action: MouseMotion}},
		{"\x1b[<69;1;1M", MouseEvent{X: 0, Y: 0, Button: MouseWheelDown, Mod: ModShift}},
		{"\x1b[<0;300;100M", MouseEvent{X: 299, Y: 99, Button: MouseLeft}}, // beyond X10 limits
		{"\x1b[M #!", MouseEvent{X: 2, Y: 0, Button: MouseLeft}},           // legacy X10
	}
	for _, tt := range tests {
		ev, n := Decode([]byte(tt.in), false)
		if !reflect.DeepEqual(ev, tt.want) || n != len(tt.in) {
			t.Errorf("Decode(%q) = %#v, %d; want %#v", tt.in, ev, n, tt.want)
		}
	}
}

func TestDecodeStream(t *testing.T) {
	// A burst as a terminal might deliver it in one read.
	in := "j\x1b[B\x1b[1;5Ck\x1b[200~p\x1b[201~\x1b[<64;1;1M\x1bq\r"
	var got []string
	buf := []byte(in)
	for len(buf) > 0 {
		ev, n := Decode(buf, false)
		if n == 0 {
			t.Fatalf("stuck at %q", buf)
		}
		buf = buf[n:]
		switch e := ev.(type) {
		case KeyEvent:
			got = append(got, e.String())
		case PasteEvent:
			got = append(got, "paste:"+e.Text)
		case MouseEvent:
			got = append(got, "mouse")
		}
	}
	want := []string{"j", "down", "ctrl+right", "k", "paste:p", "mouse", "alt+q", "enter"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stream = %q, want %q", got, want)
	}
}

func TestKeyString(t *testing.T) {
	tests := map[string]KeyEvent{
		"a":           ch('a', 0),
		"A":           ch('A', 0),
		"ctrl+d":      ch('d', ModCtrl),
		"alt+x":       ch('x', ModAlt),
		"space":       ch(' ', 0),
		"ctrl+space":  ch(' ', ModCtrl),
		"shift+tab":   key(KeyTab, ModShift),
		"enter":       key(KeyEnter, 0),
		"ctrl+]":      ch(']', ModCtrl),
		"pgdown":      key(KeyPageDown, 0),
		"f12":         key(KeyF12, 0),
		"ctrl+alt+up": key(KeyUp, ModCtrl|ModAlt),
	}
	for want, k := range tests {
		if got := k.String(); got != want {
			t.Errorf("%#v.String() = %q, want %q", k, got, want)
		}
	}
}

// FuzzDecode checks the decoder never panics, always makes progress on final
// input and never consumes more than it was given.
func FuzzDecode(f *testing.F) {
	for _, s := range []string{"a", "\x1b[1;5A", "\x1b[<0;1;1M", "\x1b[200~x\x1b[201~", "\x1b]52;c;x\x07", "\x1b[97;5u", "\xff\xfe"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		for len(b) > 0 {
			_, n := Decode(b, true)
			if n < 0 || n > len(b) {
				t.Fatalf("bad n %d for %q", n, b)
			}
			if n == 0 {
				return // unterminated paste: legitimately waits
			}
			b = b[n:]
		}
	})
}
