package term

import (
	"bytes"
	"strconv"
	"unicode"
	"unicode/utf8"
)

// Bracketed paste delimiters.
const (
	pasteStart = "\x1b[200~"
	pasteEnd   = "\x1b[201~"
)

// maxPaste bounds a bracketed paste whose end marker never arrives.
const maxPaste = 32 << 20

// Decode decodes the first input event in buf and returns it with the number
// of bytes consumed. A recognised sequence that carries no event (a terminal
// response, a key release, an unknown sequence) returns a nil Event with
// n > 0. When buf holds only the beginning of a sequence, Decode returns
// n == 0 to ask for more input, unless final is true — meaning no more bytes
// arrived within the escape timeout — in which case the bytes are resolved
// as keys: a lone ESC is the Escape key and ESC followed by a character is
// that character with Alt. A bracketed paste waits for its end marker
// regardless of final (up to 32 MiB).
//
// Decoded forms: UTF-8 text; C0 controls as Ctrl keys; xterm CSI and SS3
// cursor/function keys with modifiers ("CSI 1;5A"); "CSI n~" editing and
// function keys; Shift-Tab (CSI Z); kitty CSI-u and xterm modifyOtherKeys
// (CSI 27;m;c~) keys; SGR 1006 and X10 mouse reports; bracketed paste;
// focus in/out.
func Decode(buf []byte, final bool) (Event, int) {
	if len(buf) == 0 {
		return nil, 0
	}
	c := buf[0]
	switch {
	case c == 0x1b:
		return decodeEsc(buf, final)
	case c < 0x20 || c == 0x7f:
		return controlKey(c), 1
	case c < 0x80:
		return KeyEvent{Key: KeyRune, Rune: rune(c)}, 1
	}
	if !utf8.FullRune(buf) {
		if !final {
			return nil, 0
		}
		return KeyEvent{Key: KeyRune, Rune: utf8.RuneError}, 1
	}
	r, size := utf8.DecodeRune(buf)
	return KeyEvent{Key: KeyRune, Rune: r}, size
}

func controlKey(c byte) KeyEvent {
	switch c {
	case 0x0d:
		return KeyEvent{Key: KeyEnter}
	case 0x09:
		return KeyEvent{Key: KeyTab}
	case 0x7f, 0x08:
		return KeyEvent{Key: KeyBackspace}
	case 0x1b:
		return KeyEvent{Key: KeyEscape}
	case 0x00:
		return KeyEvent{Key: KeyRune, Rune: ' ', Mod: ModCtrl}
	}
	if c >= 0x01 && c <= 0x1a {
		return KeyEvent{Key: KeyRune, Rune: rune('a' + c - 1), Mod: ModCtrl}
	}
	// 0x1c–0x1f: Ctrl-\ Ctrl-] Ctrl-^ Ctrl-_
	return KeyEvent{Key: KeyRune, Rune: rune("\\]^_"[c-0x1c]), Mod: ModCtrl}
}

func decodeEsc(buf []byte, final bool) (Event, int) {
	if len(buf) == 1 {
		if final {
			return KeyEvent{Key: KeyEscape}, 1
		}
		return nil, 0
	}
	switch buf[1] {
	case '[':
		if ev, n, ok := decodeCSI(buf, final); ok {
			return ev, n
		}
		if !final {
			return nil, 0
		}
		return KeyEvent{Key: KeyRune, Rune: '[', Mod: ModAlt}, 2
	case 'O':
		if len(buf) < 3 {
			if final {
				return KeyEvent{Key: KeyRune, Rune: 'O', Mod: ModAlt}, 2
			}
			return nil, 0
		}
		return decodeSS3(buf)
	case ']', 'P', '_', '^':
		// OSC/DCS/APC/PM: a terminal response. Consume through ST or BEL.
		for i := 2; i < len(buf); i++ {
			if buf[i] == 0x07 {
				return nil, i + 1
			}
			if buf[i] == 0x1b && i+1 < len(buf) && buf[i+1] == '\\' {
				return nil, i + 2
			}
		}
		if final {
			return KeyEvent{Key: KeyRune, Rune: rune(buf[1]), Mod: ModAlt}, 2
		}
		return nil, 0
	case 0x1b:
		// ESC ESC: the first is a plain Escape (Esc pressed twice quickly).
		return KeyEvent{Key: KeyEscape}, 1
	}
	// Alt + key.
	ev, n := Decode(buf[1:], final)
	if n == 0 {
		return nil, 0
	}
	if k, ok := ev.(KeyEvent); ok {
		k.Mod |= ModAlt
		return k, n + 1
	}
	return ev, n + 1
}

// decodeCSI parses "ESC [ params intermediates final". ok is false when the
// sequence is incomplete; ok with n == 0 means "incomplete, keep waiting
// even after the escape timeout" (a bracketed paste in progress).
func decodeCSI(buf []byte, final bool) (Event, int, bool) {
	// Bracketed paste.
	if bytes.HasPrefix(buf, []byte(pasteStart)) {
		if i := bytes.Index(buf[len(pasteStart):], []byte(pasteEnd)); i >= 0 {
			text := buf[len(pasteStart) : len(pasteStart)+i]
			return PasteEvent{Text: string(text)}, len(pasteStart) + i + len(pasteEnd), true
		}
		if final && len(buf) > maxPaste {
			return PasteEvent{Text: string(buf[len(pasteStart):])}, len(buf), true
		}
		return nil, 0, true // incomplete paste: wait even past the timeout
	}
	// Legacy X10 mouse: ESC [ M Cb Cx Cy.
	if len(buf) >= 3 && buf[2] == 'M' {
		if len(buf) < 6 {
			return nil, 0, false
		}
		return x10Mouse(buf[3]-32, int(buf[4])-33, int(buf[5])-33), 6, true
	}
	i := 2
	for i < len(buf) && buf[i] >= 0x30 && buf[i] <= 0x3f {
		i++
	}
	pEnd := i
	for i < len(buf) && buf[i] >= 0x20 && buf[i] <= 0x2f {
		i++
	}
	if i >= len(buf) {
		return nil, 0, false
	}
	f := buf[i]
	if f < 0x40 || f > 0x7e {
		// Malformed: drop the introducer and let the rest decode as keys.
		return KeyEvent{Key: KeyRune, Rune: '[', Mod: ModAlt}, 2, true
	}
	n := i + 1
	params := buf[2:pEnd]
	if pEnd != i { // intermediates present: nothing we use
		return nil, n, true
	}
	return csiEvent(params, f), n, true
}

func csiEvent(params []byte, f byte) Event {
	if len(params) > 0 {
		switch params[0] {
		case '<':
			if f == 'M' || f == 'm' {
				return sgrMouse(params[1:], f == 'm')
			}
			return nil
		case '?', '>', '=':
			return nil // terminal responses (DA, kitty flags, …)
		}
	}
	ps := splitParams(params)
	p := func(i, def int) int {
		if i < len(ps) && len(ps[i]) > 0 && ps[i][0] >= 0 {
			return ps[i][0]
		}
		return def
	}
	mod := decodeMod(p(1, 1))
	switch f {
	case 'A':
		return KeyEvent{Key: KeyUp, Mod: mod}
	case 'B':
		return KeyEvent{Key: KeyDown, Mod: mod}
	case 'C':
		return KeyEvent{Key: KeyRight, Mod: mod}
	case 'D':
		return KeyEvent{Key: KeyLeft, Mod: mod}
	case 'H':
		return KeyEvent{Key: KeyHome, Mod: mod}
	case 'F':
		return KeyEvent{Key: KeyEnd, Mod: mod}
	case 'P':
		return KeyEvent{Key: KeyF1, Mod: mod}
	case 'Q':
		return KeyEvent{Key: KeyF2, Mod: mod}
	case 'R':
		return KeyEvent{Key: KeyF3, Mod: mod}
	case 'S':
		return KeyEvent{Key: KeyF4, Mod: mod}
	case 'Z':
		return KeyEvent{Key: KeyTab, Mod: ModShift | mod}
	case 'I':
		return FocusEvent{Focused: true}
	case 'O':
		return FocusEvent{Focused: false}
	case '~':
		n := p(0, 0)
		if n == 27 { // xterm modifyOtherKeys: CSI 27 ; mod ; code ~
			return codeKey(p(2, 0), 0, decodeMod(p(1, 1)))
		}
		if k, ok := tildeKeys[n]; ok {
			return KeyEvent{Key: k, Mod: mod}
		}
		return nil
	case 'u':
		return kittyKey(ps)
	}
	return nil
}

var tildeKeys = map[int]Key{
	1: KeyHome, 2: KeyInsert, 3: KeyDelete, 4: KeyEnd, 5: KeyPageUp, 6: KeyPageDown,
	7: KeyHome, 8: KeyEnd,
	11: KeyF1, 12: KeyF2, 13: KeyF3, 14: KeyF4, 15: KeyF5,
	17: KeyF6, 18: KeyF7, 19: KeyF8, 20: KeyF9, 21: KeyF10, 23: KeyF11, 24: KeyF12,
}

// decodeMod converts an xterm modifier parameter (1 + bits) to Mod.
func decodeMod(m int) Mod {
	if m <= 1 {
		return 0
	}
	b := m - 1
	var mod Mod
	if b&1 != 0 {
		mod |= ModShift
	}
	if b&2 != 0 || b&32 != 0 { // alt, or kitty "meta"
		mod |= ModAlt
	}
	if b&4 != 0 {
		mod |= ModCtrl
	}
	if b&8 != 0 {
		mod |= ModSuper
	}
	return mod
}

// splitParams parses "1;5:3;..." into groups of colon-separated numbers
// (-1 for empty).
func splitParams(p []byte) [][]int {
	if len(p) == 0 {
		return nil
	}
	var out [][]int
	for _, part := range bytes.Split(p, []byte{';'}) {
		var g []int
		for _, sub := range bytes.Split(part, []byte{':'}) {
			n, err := strconv.Atoi(string(sub))
			if err != nil {
				n = -1
			}
			g = append(g, n)
		}
		out = append(out, g)
	}
	return out
}

// kittyKey decodes "CSI code[:shifted[:base]] ; mods[:event] ; text u".
func kittyKey(ps [][]int) Event {
	if len(ps) == 0 || len(ps[0]) == 0 {
		return nil
	}
	code := ps[0][0]
	shifted := 0
	if len(ps[0]) > 1 && ps[0][1] > 0 {
		shifted = ps[0][1]
	}
	mods := 1
	if len(ps) > 1 && len(ps[1]) > 0 && ps[1][0] > 0 {
		mods = ps[1][0]
		if len(ps[1]) > 1 && ps[1][1] == 3 {
			return nil // key release
		}
	}
	return codeKey(code, shifted, decodeMod(mods))
}

var kittyFunctional = map[int]Key{
	57414: KeyEnter, 57417: KeyLeft, 57418: KeyRight, 57419: KeyUp, 57420: KeyDown,
	57421: KeyPageUp, 57422: KeyPageDown, 57423: KeyHome, 57424: KeyEnd,
	57425: KeyInsert, 57426: KeyDelete,
}

// codeKey turns a Unicode key code plus modifiers (kitty / modifyOtherKeys)
// into a KeyEvent using the same conventions as legacy input.
func codeKey(code, shifted int, mod Mod) Event {
	switch code {
	case 9:
		return KeyEvent{Key: KeyTab, Mod: mod}
	case 13:
		return KeyEvent{Key: KeyEnter, Mod: mod}
	case 27:
		return KeyEvent{Key: KeyEscape, Mod: mod}
	case 127, 8:
		return KeyEvent{Key: KeyBackspace, Mod: mod}
	}
	if k, ok := kittyFunctional[code]; ok {
		return KeyEvent{Key: k, Mod: mod}
	}
	if code >= 57399 && code <= 57415 { // keypad characters
		return KeyEvent{Key: KeyRune, Rune: rune("0123456789./*-+\x00="[code-57399]), Mod: mod}
	}
	if code >= 57344 && code <= 63743 || code <= 0 || code > unicode.MaxRune {
		return nil // other functional keys and lone modifier keys
	}
	r := rune(code)
	if mod&ModShift != 0 && mod&ModCtrl == 0 {
		// Shift with a printable key: report the shifted character.
		switch {
		case shifted > 0:
			r = rune(shifted)
			mod &^= ModShift
		case unicode.IsLower(r):
			r = unicode.ToUpper(r)
			mod &^= ModShift
		}
	}
	if mod&ModCtrl != 0 {
		r = unicode.ToLower(r)
	}
	return KeyEvent{Key: KeyRune, Rune: r, Mod: mod}
}

func decodeSS3(buf []byte) (Event, int) {
	i := 2
	for i < len(buf) && buf[i] >= '0' && buf[i] <= '9' {
		i++
	}
	if i >= len(buf) {
		return nil, 0
	}
	mod := Mod(0)
	if i > 2 {
		n, _ := strconv.Atoi(string(buf[2:i]))
		mod = decodeMod(n)
	}
	var k Key
	switch buf[i] {
	case 'A':
		k = KeyUp
	case 'B':
		k = KeyDown
	case 'C':
		k = KeyRight
	case 'D':
		k = KeyLeft
	case 'H':
		k = KeyHome
	case 'F':
		k = KeyEnd
	case 'P':
		k = KeyF1
	case 'Q':
		k = KeyF2
	case 'R':
		k = KeyF3
	case 'S':
		k = KeyF4
	case 'M':
		k = KeyEnter
	default:
		return nil, i + 1
	}
	return KeyEvent{Key: k, Mod: mod}, i + 1
}

func mouseMods(b int) Mod {
	var m Mod
	if b&4 != 0 {
		m |= ModShift
	}
	if b&8 != 0 {
		m |= ModAlt
	}
	if b&16 != 0 {
		m |= ModCtrl
	}
	return m
}

func mouseButton(b int) (MouseButton, bool) {
	if b&64 != 0 {
		return [4]MouseButton{MouseWheelUp, MouseWheelDown, MouseWheelLeft, MouseWheelRight}[b&3], true
	}
	return [4]MouseButton{MouseLeft, MouseMiddle, MouseRight, MouseNone}[b&3], false
}

func sgrMouse(params []byte, release bool) Event {
	ps := splitParams(params)
	if len(ps) < 3 || len(ps[0]) == 0 || len(ps[1]) == 0 || len(ps[2]) == 0 {
		return nil
	}
	b := ps[0][0]
	if b < 0 {
		return nil
	}
	btn, wheel := mouseButton(b)
	ev := MouseEvent{X: ps[1][0] - 1, Y: ps[2][0] - 1, Button: btn, Mod: mouseMods(b)}
	switch {
	case wheel:
		ev.Action = MousePress
	case b&32 != 0:
		ev.Action = MouseMotion
	case release:
		ev.Action = MouseRelease
	}
	return ev
}

func x10Mouse(b byte, x, y int) Event {
	bi := int(b)
	btn, wheel := mouseButton(bi)
	ev := MouseEvent{X: x, Y: y, Button: btn, Mod: mouseMods(bi)}
	switch {
	case wheel:
	case bi&32 != 0:
		ev.Action = MouseMotion
	case btn == MouseNone:
		ev.Action = MouseRelease
	}
	return ev
}
