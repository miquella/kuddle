package lexer

// This file writes strings as KDL quoted strings, by one set of rules for
// everything that writes them.

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// ErrInvalidUTF8 is returned by AppendQuote for a string that is not valid
// UTF-8.
var ErrInvalidUTF8 = errors.New("invalid UTF-8")

// AppendQuote appends s to dst as a quoted string, valid in both KDL
// versions. It escapes '"' and '\\', writes \b, \f, \n, \r and \t as those
// escapes, and writes the other code points that can't appear as they are in
// a string, control characters, newlines and those that KDL v2 disallows, as
// \u{...} escapes. Invalid UTF-8 is written as U+FFFD, and reported as
// ErrInvalidUTF8 once all of s has been appended.
func AppendQuote(dst []byte, s string) ([]byte, error) {
	var err error
	dst = append(dst, '"')
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		s = s[size:]
		switch r {
		case '"':
			dst = append(dst, `\"`...)
		case '\\':
			dst = append(dst, `\\`...)
		case '\b':
			dst = append(dst, `\b`...)
		case '\f':
			dst = append(dst, `\f`...)
		case '\n':
			dst = append(dst, `\n`...)
		case '\r':
			dst = append(dst, `\r`...)
		case '\t':
			dst = append(dst, `\t`...)
		default:
			switch {
			case r == utf8.RuneError && size == 1:
				err = ErrInvalidUTF8
				dst = utf8.AppendRune(dst, r)
			case r < 0x20 || IsDisallowed(r) || IsNewline(r):
				dst = fmt.Appendf(dst, `\u{%x}`, r)
			default:
				dst = utf8.AppendRune(dst, r)
			}
		}
	}
	return append(dst, '"'), err
}

// Quote returns s as AppendQuote writes it, for error messages. Invalid UTF-8
// is written as U+FFFD.
func Quote(s string) string {
	b, _ := AppendQuote(nil, s)
	return string(b)
}
