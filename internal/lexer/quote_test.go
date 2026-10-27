package lexer

import (
	"bytes"
	"testing"
)

func TestAppendQuote(t *testing.T) {
	tests := []struct {
		in, want string
		err      error
	}{
		{"", `""`, nil},
		{`a "b" \c`, `"a \"b\" \\c"`, nil},
		{"\b\f\n\r\t", `"\b\f\n\r\t"`, nil},
		{"\x01\x7f\v", `"\u{1}\u{7f}\u{b}"`, nil},
		{"\u0085\u2028\u2029", `"\u{85}\u{2028}\u{2029}"`, nil},
		{"\u200e\ufeff", `"\u{200e}\u{feff}"`, nil},
		{"\u00a0\u00e9\ufffd", "\"\u00a0\u00e9\ufffd\"", nil},
		{"a/b", `"a/b"`, nil},
		{"a\xffb", "\"a\ufffdb\"", ErrInvalidUTF8},
	}
	for _, tt := range tests {
		got, err := AppendQuote([]byte("x"), tt.in)
		if string(got) != "x"+tt.want || err != tt.err {
			t.Errorf("AppendQuote(%q) = %#q, %v, want %#q, %v", tt.in, got, err, "x"+tt.want, tt.err)
		}
	}
}

// TestAppendQuoteLexes checks that quoted strings lex back to themselves, in
// both versions.
func TestAppendQuoteLexes(t *testing.T) {
	for _, s := range []string{"", "a\"\\/b", "\b\f\n\r\t\v\x00\x1f\x7f", "\u0085\u2028\u2029\u200e\u2066\ufeff", "\u00a0\u00e9\U0001f600"} {
		for _, v := range []Version{V1, V2} {
			q, _ := AppendQuote(nil, s)
			tok, err := New(bytes.NewReader(q), v).NextToken()
			if err != nil || tok.Kind != String || tok.String() != s {
				t.Errorf("v%d: %s lexes as %v %q, %v", v, q, tok.Kind, tok.String(), err)
			}
		}
	}
}
