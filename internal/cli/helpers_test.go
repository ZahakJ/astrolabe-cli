package cli

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ZahakJ/folio/internal/theme"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;:?]*[A-Za-z]|\x1b\]8;[^\x1b]*\x1b\\`)

// stripANSI removes SGR and OSC 8 sequences.
func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func fmtSscan(s string, v *int) (int, error) { return fmt.Sscan(s, v) }

func glyphsASCII() theme.Glyphs { return theme.ASCIIGlyphs }
