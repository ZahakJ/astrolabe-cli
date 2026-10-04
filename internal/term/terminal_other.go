//go:build !linux && !darwin

package term

import (
	"errors"
	"os/exec"
)

// Terminal is unavailable on this platform; Open always fails.
type Terminal struct{ Caps Caps }

// Open is not supported on this platform.
func Open(o OpenOptions) (*Terminal, error) {
	return nil, errors.New("interactive terminal not supported on this platform")
}

// Screen returns nil.
func (t *Terminal) Screen() *Screen { return nil }

// Events returns nil.
func (t *Terminal) Events() <-chan Event { return nil }

// Flush does nothing.
func (t *Terminal) Flush() error { return nil }

// Close does nothing.
func (t *Terminal) Close() error { return nil }

// RecoverPanic does nothing.
func (t *Terminal) RecoverPanic() {}

// RunExternal runs cmd.
func (t *Terminal) RunExternal(cmd *exec.Cmd) error { return cmd.Run() }
