package term

import (
	"fmt"
	"strings"
)

// Event is an input event: KeyEvent, PasteEvent, MouseEvent, ResizeEvent,
// FocusEvent or ErrorEvent.
type Event interface{ isEvent() }

// Key identifies a key. KeyRune means a character key whose character is in
// KeyEvent.Rune; the others are named keys.
type Key int

// Keys.
const (
	KeyRune Key = iota
	KeyEnter
	KeyTab
	KeyBackspace
	KeyEscape
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
	KeyInsert
	KeyDelete
	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
)

var keyNames = map[Key]string{
	KeyEnter: "enter", KeyTab: "tab", KeyBackspace: "backspace", KeyEscape: "esc",
	KeyUp: "up", KeyDown: "down", KeyLeft: "left", KeyRight: "right",
	KeyHome: "home", KeyEnd: "end", KeyPageUp: "pgup", KeyPageDown: "pgdown",
	KeyInsert: "insert", KeyDelete: "delete",
	KeyF1: "f1", KeyF2: "f2", KeyF3: "f3", KeyF4: "f4", KeyF5: "f5", KeyF6: "f6",
	KeyF7: "f7", KeyF8: "f8", KeyF9: "f9", KeyF10: "f10", KeyF11: "f11", KeyF12: "f12",
}

// Mod is a set of key modifiers.
type Mod uint8

// Modifiers.
const (
	ModShift Mod = 1 << iota
	ModAlt
	ModCtrl
	ModSuper
)

// KeyEvent is a key press.
//
// Conventions: printable characters arrive as KeyRune with the character as
// typed ('A' for Shift-a, without ModShift); Ctrl-letter arrives as KeyRune
// with the lower-case letter and ModCtrl ("ctrl+d"); Shift-Tab is KeyTab
// with ModShift; Space is KeyRune ' '. Ctrl-H and DEL both arrive as
// KeyBackspace (terminals disagree on which Backspace sends).
type KeyEvent struct {
	Key  Key
	Rune rune
	Mod  Mod
}

func (KeyEvent) isEvent() {}

// String renders the key in the form keymaps use: "a", "A", "ctrl+d",
// "alt+x", "shift+tab", "enter", "space", "ctrl+]".
func (k KeyEvent) String() string {
	var b strings.Builder
	if k.Mod&ModCtrl != 0 {
		b.WriteString("ctrl+")
	}
	if k.Mod&ModAlt != 0 {
		b.WriteString("alt+")
	}
	if k.Mod&ModSuper != 0 {
		b.WriteString("super+")
	}
	if k.Mod&ModShift != 0 {
		b.WriteString("shift+")
	}
	if k.Key == KeyRune {
		if k.Rune == ' ' {
			b.WriteString("space")
		} else {
			b.WriteRune(k.Rune)
		}
	} else if n, ok := keyNames[k.Key]; ok {
		b.WriteString(n)
	} else {
		fmt.Fprintf(&b, "key(%d)", int(k.Key))
	}
	return b.String()
}

// PasteEvent carries bracketed-paste text, delivered whole.
type PasteEvent struct{ Text string }

func (PasteEvent) isEvent() {}

// MouseButton identifies a mouse button or wheel direction.
type MouseButton int

// Mouse buttons.
const (
	MouseNone MouseButton = iota
	MouseLeft
	MouseMiddle
	MouseRight
	MouseWheelUp
	MouseWheelDown
	MouseWheelLeft
	MouseWheelRight
)

// MouseAction is what happened to the button.
type MouseAction int

// Mouse actions. Wheel events are always MousePress.
const (
	MousePress MouseAction = iota
	MouseRelease
	MouseMotion
)

// MouseEvent is a mouse report (SGR 1006 encoding). X and Y are 0-based
// cell coordinates.
type MouseEvent struct {
	X, Y   int
	Button MouseButton
	Action MouseAction
	Mod    Mod
}

func (MouseEvent) isEvent() {}

// ResizeEvent reports a new terminal size. The application should resize
// its Screen and redraw.
type ResizeEvent struct{ Width, Height int }

func (ResizeEvent) isEvent() {}

// FocusEvent reports the terminal window gaining or losing focus (only
// when focus reporting is enabled).
type FocusEvent struct{ Focused bool }

func (FocusEvent) isEvent() {}

// ErrorEvent reports that input stopped because of an error (e.g. the
// terminal went away). No further events follow.
type ErrorEvent struct{ Err error }

func (ErrorEvent) isEvent() {}
