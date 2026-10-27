package lexer

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

// DecodeString decodes the raw bytes of a String or RawString token, in either
// its KDL v1 or v2 form. Escapes are assumed to have been validated by the
// lexer; the indentation of multi-line strings is validated here.
func DecodeString(s string) (string, error) {
	escaped := true
	switch s[0] {
	case 'r': // v1 raw string: r#"..."#
		hashes := strings.IndexByte(s, '"') - 1
		return s[hashes+2 : len(s)-hashes-1], nil
	case '#': // v2 raw string: #"..."# or #"""..."""#
		escaped = false
		hashes := strings.IndexByte(s, '"')
		s = s[hashes : len(s)-hashes]
	}

	// v1 strings never start with """: "" is a complete v1 string.
	if strings.HasPrefix(s, `"""`) {
		return decodeMultiline(s[3:len(s)-3], escaped)
	}
	s = s[1 : len(s)-1]
	if escaped {
		s = unescape(s, false)
	}
	return s, nil
}

// decodeMultiline decodes the body of a v2 multi-line string, between its
// opening and closing quotes. As the spec requires, whitespace escapes are
// resolved before dedenting and the other escapes after.
func decodeMultiline(body string, escaped bool) (string, error) {
	if escaped {
		body = unescape(body, true)
	}

	// The body starts with a newline, so lines[0] is empty.
	lines := splitLines(body)
	prefix := lines[len(lines)-1]
	if !isBlank(prefix) {
		return "", errors.New("multi-line string must end with a line of only whitespace")
	}
	lines = lines[1 : len(lines)-1]
	for i, line := range lines {
		if isBlank(line) {
			lines[i] = ""
			continue
		}
		rest, ok := strings.CutPrefix(line, prefix)
		if !ok {
			return "", errors.New("multi-line string line does not start with the final line's whitespace")
		}
		lines[i] = rest
	}

	s := strings.Join(lines, "\n")
	if escaped {
		s = unescape(s, false)
	}
	return s, nil
}

// unescape resolves the escapes in s. If wsOnly is set, only whitespace escapes
// are resolved and the others are kept as is.
func unescape(s string, wsOnly bool) string {
	i := strings.IndexByte(s, '\\')
	if i < 0 {
		return s
	}

	var sb strings.Builder
	sb.Grow(len(s))
	for i >= 0 {
		sb.WriteString(s[:i])
		r, size := utf8.DecodeRuneInString(s[i+1:])
		esc := s[i : i+1+size]
		s = s[i+1+size:]

		switch {
		case isSpaceOrNewline(r):
			s = strings.TrimLeftFunc(s, isSpaceOrNewline)
		case wsOnly:
			sb.WriteString(esc)
		case r == 'u':
			end := strings.IndexByte(s, '}')
			v, _ := strconv.ParseUint(s[1:end], 16, 32)
			sb.WriteRune(rune(v))
			s = s[end+1:]
		default:
			sb.WriteByte(simpleEscapes[r])
		}
		i = strings.IndexByte(s, '\\')
	}
	sb.WriteString(s)
	return sb.String()
}

var simpleEscapes = map[rune]byte{
	'"':  '"',
	'\\': '\\',
	'/':  '/', // v1 only
	'b':  '\b',
	'f':  '\f',
	'n':  '\n',
	'r':  '\r',
	's':  ' ', // v2 only
	't':  '\t',
}

// splitLines splits s at each newline, treating CRLF as a single newline.
func splitLines(s string) []string {
	var lines []string
	for {
		i := strings.IndexFunc(s, IsNewline)
		if i < 0 {
			return append(lines, s)
		}
		lines = append(lines, s[:i])
		_, size := utf8.DecodeRuneInString(s[i:])
		if strings.HasPrefix(s[i:], "\r\n") {
			size = 2
		}
		s = s[i+size:]
	}
}

func isSpaceOrNewline(r rune) bool { return IsSpace(r) || IsNewline(r) }

func isBlank(s string) bool { return strings.TrimLeftFunc(s, IsSpace) == "" }
