package lexer

// This file writes strings as KDL: quoted, or as bare identifiers, by one set
// of rules for everything that writes them.

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
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

// IsIdentifier reports whether s can be written as a bare identifier in KDL
// version v. The keywords of either version, which can only be written bare
// in v1, are not.
func IsIdentifier(s string, v Version) bool {
	switch s {
	case "", "true", "false", "null", "inf", "-inf", "nan":
		return false
	}
	// Most strings are simple identifiers, valid in both versions, or contain
	// a character that neither version allows in one.
	simple := true
	for i, c := range []byte(s) {
		if strings.IndexByte(" \t\r\n\\/(){};[]\"=", c) >= 0 {
			return false
		}
		letter := 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || c == '_'
		switch {
		case letter:
		case i == 0 && '0' <= c && c <= '9':
			return false // the lexer reads a leading digit as a number
		case i == 0:
			// A leading '-' is fine if a letter or another '-' follows.
			simple = c == '-' && len(s) > 1 && (s[1] == '-' || s[1] == '_' || 'a' <= s[1] && s[1] <= 'z' || 'A' <= s[1] && s[1] <= 'Z')
		case '0' <= c && c <= '9', strings.IndexByte("-.:@+", c) >= 0:
		default:
			simple = false
		}
	}
	return simple || lexesAsIdent(s, v)
}

// lexesAsIdent reports whether s is a single Ident token in version v.
func lexesAsIdent(s string, v Version) bool {
	// The lexer reads a *bytes.Buffer's contents in place.
	l := New(bytes.NewBufferString(s), v)
	t, err := l.NextToken()
	if err != nil || t.Kind != Ident || string(t.Raw()) != s {
		return false
	}
	t, err = l.NextToken()
	return err == nil && t.Kind == EOF
}
