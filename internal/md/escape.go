package md

import (
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

func isASCIIPunct(c byte) bool {
	return c >= '!' && c <= '/' || c >= ':' && c <= '@' || c >= '[' && c <= '`' || c >= '{' && c <= '~'
}

// unescapeString decodes backslash escapes of ASCII punctuation and HTML
// entities, as CommonMark does for link destinations, titles and info
// strings.
func unescapeString(s string) string {
	if strings.IndexByte(s, '\\') < 0 && strings.IndexByte(s, '&') < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c == '\\' && i+1 < len(s) && isASCIIPunct(s[i+1]) {
			b.WriteByte(s[i+1])
			i += 2
			continue
		}
		if c == '&' {
			if r, n := decodeEntity(s[i:]); n > 0 {
				b.WriteString(r)
				i += n
				continue
			}
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// decodeEntity decodes an HTML entity at the start of s ("&amp;",
// "&#123;", "&#x1F;"). It returns the replacement and the number of bytes
// consumed, or n == 0 when s does not start with a known entity.
func decodeEntity(s string) (string, int) {
	if len(s) < 3 || s[0] != '&' {
		return "", 0
	}
	if s[1] == '#' {
		i := 2
		hex := false
		if i < len(s) && (s[i] == 'x' || s[i] == 'X') {
			hex = true
			i++
		}
		start := i
		for i < len(s) && i-start < 8 && (isDigit(s[i]) || hex && isHexDigit(s[i])) {
			i++
		}
		if i == start || i >= len(s) || s[i] != ';' {
			return "", 0
		}
		if !hex && i-start > 7 || hex && i-start > 6 {
			return "", 0
		}
		base := 10
		if hex {
			base = 16
		}
		v, err := strconv.ParseUint(s[start:i], base, 32)
		r := rune(v)
		if err != nil || v == 0 || !utf8.ValidRune(r) {
			r = utf8.RuneError
		}
		return string(r), i + 1
	}
	i := 1
	for i < len(s) && i < 32 && (isAlnum(s[i])) {
		i++
	}
	if i == 1 || i >= len(s) || s[i] != ';' {
		return "", 0
	}
	if r, ok := entities[s[1:i]]; ok {
		return r, i + 1
	}
	return "", 0
}

func isDigit(c byte) bool    { return c >= '0' && c <= '9' }
func isHexDigit(c byte) bool { return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }
func isAlnum(c byte) bool    { return isDigit(c) || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// entities holds the named HTML entities likely to appear in notes. The
// full HTML5 table (2,000+ names) is deliberately not embedded.
var entities = map[string]string{
	"amp": "&", "lt": "<", "gt": ">", "quot": "\"", "apos": "'", "nbsp": " ",
	"copy": "©", "reg": "®", "trade": "™", "hellip": "…", "mdash": "—", "ndash": "–",
	"lsquo": "‘", "rsquo": "’", "ldquo": "“", "rdquo": "”", "sbquo": "‚", "bdquo": "„",
	"laquo": "«", "raquo": "»", "lsaquo": "‹", "rsaquo": "›",
	"times": "×", "divide": "÷", "deg": "°", "plusmn": "±", "minus": "−", "middot": "·",
	"bull": "•", "prime": "′", "Prime": "″", "permil": "‰", "micro": "µ",
	"euro": "€", "pound": "£", "yen": "¥", "cent": "¢", "curren": "¤",
	"sect": "§", "para": "¶", "dagger": "†", "Dagger": "‡", "shy": "­",
	"ensp": " ", "emsp": " ", "thinsp": " ", "zwnj": "‌", "zwj": "‍",
	"lrm": "‎", "rlm": "‏",
	"larr": "←", "rarr": "→", "uarr": "↑", "darr": "↓", "harr": "↔",
	"lArr": "⇐", "rArr": "⇒", "uArr": "⇑", "dArr": "⇓", "hArr": "⇔",
	"le": "≤", "ge": "≥", "ne": "≠", "asymp": "≈", "equiv": "≡", "infin": "∞",
	"sum": "∑", "prod": "∏", "radic": "√", "part": "∂", "nabla": "∇", "isin": "∈",
	"notin": "∉", "cap": "∩", "cup": "∪", "sub": "⊂", "sup": "⊃", "and": "∧", "or": "∨",
	"not": "¬", "forall": "∀", "exist": "∃", "empty": "∅", "there4": "∴",
	"frac12": "½", "frac14": "¼", "frac34": "¾", "sup1": "¹", "sup2": "²", "sup3": "³",
	"ordf": "ª", "ordm": "º", "iexcl": "¡", "iquest": "¿", "brvbar": "¦", "macr": "¯",
	"acute": "´", "cedil": "¸", "uml": "¨",
	"hearts": "♥", "spades": "♠", "clubs": "♣", "diams": "♦", "loz": "◊", "star": "☆",
	"check": "✓", "cross": "✗",
	"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ", "epsilon": "ε", "zeta": "ζ",
	"eta": "η", "theta": "θ", "iota": "ι", "kappa": "κ", "lambda": "λ", "mu": "μ",
	"nu": "ν", "xi": "ξ", "omicron": "ο", "pi": "π", "rho": "ρ", "sigma": "σ",
	"tau": "τ", "upsilon": "υ", "phi": "φ", "chi": "χ", "psi": "ψ", "omega": "ω",
	"Alpha": "Α", "Beta": "Β", "Gamma": "Γ", "Delta": "Δ", "Theta": "Θ", "Lambda": "Λ",
	"Pi": "Π", "Sigma": "Σ", "Phi": "Φ", "Psi": "Ψ", "Omega": "Ω",
	"Agrave": "À", "Aacute": "Á", "Acirc": "Â", "Atilde": "Ã", "Auml": "Ä", "Aring": "Å",
	"AElig": "Æ", "Ccedil": "Ç", "Egrave": "È", "Eacute": "É", "Ecirc": "Ê", "Euml": "Ë",
	"Igrave": "Ì", "Iacute": "Í", "Icirc": "Î", "Iuml": "Ï", "Ntilde": "Ñ",
	"Ograve": "Ò", "Oacute": "Ó", "Ocirc": "Ô", "Otilde": "Õ", "Ouml": "Ö", "Oslash": "Ø",
	"Ugrave": "Ù", "Uacute": "Ú", "Ucirc": "Û", "Uuml": "Ü", "Yacute": "Ý", "szlig": "ß",
	"agrave": "à", "aacute": "á", "acirc": "â", "atilde": "ã", "auml": "ä", "aring": "å",
	"aelig": "æ", "ccedil": "ç", "egrave": "è", "eacute": "é", "ecirc": "ê", "euml": "ë",
	"igrave": "ì", "iacute": "í", "icirc": "î", "iuml": "ï", "ntilde": "ñ",
	"ograve": "ò", "oacute": "ó", "ocirc": "ô", "otilde": "õ", "ouml": "ö", "oslash": "ø",
	"ugrave": "ù", "uacute": "ú", "ucirc": "û", "uuml": "ü", "yacute": "ý", "yuml": "ÿ",
	"OElig": "Œ", "oelig": "œ", "Scaron": "Š", "scaron": "š", "thorn": "þ", "eth": "ð",
	"THORN": "Þ", "ETH": "Ð",
}

// normalizeLabel folds a link label for matching: case-insensitive,
// surrounding blanks trimmed, inner whitespace runs collapsed.
func normalizeLabel(s string) string {
	return strings.ToLower(strings.ToUpper(strings.Join(strings.Fields(s), " ")))
}

// parseLinkDest parses a link destination starting at s[i]. Pointy
// destinations "<…>" may contain spaces but no line breaks; plain ones
// end at a blank or control character and must have balanced parentheses.
// It returns the decoded destination and the index just past it. A plain
// destination may be empty (end == i).
func parseLinkDest(s string, i int) (dest string, end int, ok bool) {
	if i >= len(s) {
		return "", i, true
	}
	if s[i] == '<' {
		for j := i + 1; j < len(s); j++ {
			switch s[j] {
			case '\\':
				if j+1 < len(s) && isASCIIPunct(s[j+1]) {
					j++
				}
			case '>':
				return unescapeString(s[i+1 : j]), j + 1, true
			case '<', '\n', '\r':
				return "", i, false
			}
		}
		return "", i, false
	}
	depth := 0
	j := i
loop:
	for j < len(s) {
		c := s[j]
		switch {
		case c == '\\' && j+1 < len(s) && isASCIIPunct(s[j+1]):
			j += 2
			continue
		case c == '(':
			depth++
			if depth > 32 {
				return "", i, false
			}
		case c == ')':
			if depth == 0 {
				break loop
			}
			depth--
		case c <= ' ' || c == 0x7f:
			break loop
		}
		j++
	}
	if depth != 0 {
		return "", i, false
	}
	return unescapeString(s[i:j]), j, true
}

// parseLinkTitle parses a quoted or parenthesised title starting at s[i].
func parseLinkTitle(s string, i int) (title string, end int, ok bool) {
	if i >= len(s) {
		return "", i, false
	}
	open := s[i]
	closer := open
	switch open {
	case '"', '\'':
	case '(':
		closer = ')'
	default:
		return "", i, false
	}
	for j := i + 1; j < len(s); j++ {
		c := s[j]
		switch {
		case c == '\\' && j+1 < len(s) && isASCIIPunct(s[j+1]):
			j++
		case c == closer:
			return unescapeString(s[i+1 : j]), j + 1, true
		case open == '(' && c == '(':
			return "", i, false
		case c == '\n':
			// A title may span lines but not contain a blank line.
			k := j + 1
			for k < len(s) && isSpaceOrTab(s[k]) {
				k++
			}
			if k >= len(s) || s[k] == '\n' {
				return "", i, false
			}
		}
	}
	return "", i, false
}

// skipBlank skips spaces and tabs, and at most one line ending when
// newline is true. It reports whether a line ending was crossed.
func skipBlank(s string, i int, newline bool) (int, bool) {
	for i < len(s) && isSpaceOrTab(s[i]) {
		i++
	}
	crossed := false
	if newline && i < len(s) && s[i] == '\n' {
		crossed = true
		i++
		for i < len(s) && isSpaceOrTab(s[i]) {
			i++
		}
	}
	return i, crossed
}

// parseRefDef parses a link reference definition at the start of s. On
// success consumed covers whole lines (it ends just after a line ending,
// or at len(s)).
func parseRefDef(s string) (label, dest, title string, consumed int, ok bool) {
	if len(s) < 4 || s[0] != '[' {
		return
	}
	j := 1
	for ; j < len(s); j++ {
		c := s[j]
		if c == '\\' && j+1 < len(s) {
			j++
			continue
		}
		if c == '[' || j > 1000 {
			return
		}
		if c == ']' {
			break
		}
	}
	if j >= len(s) || strings.TrimSpace(s[1:j]) == "" || j+1 >= len(s) || s[j+1] != ':' {
		return
	}
	label = s[1:j]
	if strings.HasPrefix(label, "^") {
		return // footnote definition syntax
	}
	i, _ := skipBlank(s, j+2, true)
	d, end, dok := parseLinkDest(s, i)
	if !dok || end == i {
		return
	}
	dest = d
	k, _ := skipBlank(s, end, false)
	destAtEOL := k >= len(s) || s[k] == '\n'
	lineEndAfterDest := k
	t, _ := skipBlank(s, end, true)
	if t < len(s) && t > end && (s[t] == '"' || s[t] == '\'' || s[t] == '(') {
		if tt, tend, tok := parseLinkTitle(s, t); tok {
			e, _ := skipBlank(s, tend, false)
			if e >= len(s) || s[e] == '\n' {
				if e < len(s) {
					e++
				}
				return label, dest, tt, e, true
			}
		}
	}
	if destAtEOL {
		e := lineEndAfterDest
		if e < len(s) {
			e++
		}
		return label, dest, "", e, true
	}
	return "", "", "", 0, false
}

// splitDest classifies a link destination: external when it has a URI
// scheme; otherwise the percent-decoded path and fragment are returned.
func splitDest(dest string) (external bool, path, fragment string) {
	if hasScheme(dest) {
		return true, "", ""
	}
	p, f := dest, ""
	if i := strings.IndexByte(dest, '#'); i >= 0 {
		p, f = dest[:i], dest[i+1:]
	}
	if q := strings.IndexByte(p, '?'); q >= 0 {
		p = p[:q]
	}
	if u, err := url.PathUnescape(p); err == nil {
		p = u
	}
	if u, err := url.PathUnescape(f); err == nil {
		f = u
	}
	return false, p, f
}

// hasScheme reports whether s starts with a URI scheme ("https:",
// "mailto:", "obsidian:"). A single letter followed by ':' (a Windows
// drive) does not count.
func hasScheme(s string) bool {
	for i := 0; i < len(s) && i < 33; i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
		case i > 0 && (isDigit(c) || c == '+' || c == '.' || c == '-'):
		case c == ':':
			return i >= 2
		default:
			return false
		}
	}
	return false
}
