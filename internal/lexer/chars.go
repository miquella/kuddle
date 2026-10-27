package lexer

// This file classifies KDL code points.

// BOM is the byte order mark.
const BOM = '\uFEFF'

// IsSpace reports whether r is non-newline whitespace (unicode-space). The set
// is the same in KDL v1 and v2.
func IsSpace(r rune) bool {
	switch r {
	case '\t', ' ', '\u00A0', '\u1680', '\u202F', '\u205F', '\u3000':
		return true
	}
	return '\u2000' <= r && r <= '\u200A'
}

// IsNewline reports whether r is a newline in KDL v2. KDL v1 is the same except
// that VT (U+000B) is not a newline. CRLF is handled by callers.
func IsNewline(r rune) bool {
	switch r {
	case '\r', '\n', '\u0085', '\v', '\f', '\u2028', '\u2029':
		return true
	}
	return false
}

// IsDisallowed reports whether KDL v2 disallows r anywhere in a document,
// except that BOM is allowed as the first code point.
func IsDisallowed(r rune) bool {
	switch {
	case 0 <= r && r <= 0x08, 0x0E <= r && r <= 0x1F, r == 0x7F:
		return true
	case 0xD800 <= r && r <= 0xDFFF:
		return true
	case r == 0x200E, r == 0x200F, 0x202A <= r && r <= 0x202E, 0x2066 <= r && r <= 0x2069:
		return true
	}
	return r == BOM
}
