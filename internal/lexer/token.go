package lexer

// This file defines the lexical tokens of KDL and decodes their values.

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// Kind is the kind of a token. As in jsontext, each kind is a single
// character: the token's first character where that identifies it, and a
// mnemonic letter otherwise.
type Kind byte

var errNotNumber = errors.New("not a number")

const (
	Invalid Kind = 0

	Ident     Kind = 'i'
	String    Kind = '"'
	RawString Kind = 'r'
	Number    Kind = '0'
	True      Kind = 't'
	False     Kind = 'f'
	Null      Kind = 'n'

	LParen    Kind = '('
	RParen    Kind = ')'
	LBrace    Kind = '{'
	RBrace    Kind = '}'
	Equals    Kind = '='
	Semicolon Kind = ';'
	Escline   Kind = '\\'
	Slashdash Kind = '-'

	// Whitespace is a run of non-newline whitespace.
	Whitespace Kind = ' '
	Newline    Kind = '\n'

	// LineComment does not include the terminating newline, which is a
	// separate Newline token.
	LineComment  Kind = '/'
	BlockComment Kind = '*'

	EOF Kind = '$'
)

// String returns the name of the kind, such as "ident" or "{".
func (k Kind) String() string {
	switch k {
	case Ident:
		return "ident"
	case String:
		return "string"
	case RawString:
		return "raw string"
	case Number:
		return "number"
	case True:
		return "true"
	case False:
		return "false"
	case Null:
		return "null"
	case Slashdash:
		return "/-"
	case Whitespace:
		return "whitespace"
	case Newline:
		return "newline"
	case LineComment:
		return "line comment"
	case BlockComment:
		return "block comment"
	case EOF:
		return "EOF"
	case Invalid:
		return "invalid"
	}
	return string(rune(k))
}

// Token is a lexical token.
//
// Tokens read by the lexer hold the raw bytes from the input, which the
// accessor methods decode on demand. Tokens built with the constructors (or
// detached with Detach) hold decoded values instead, to be encoded when
// written to an output stream.
type Token struct {
	Kind Kind

	raw []byte // encoded bytes from the input; nil if not read by the lexer
	str string // decoded value of a constructed token
}

// FromRaw returns a token of kind k backed by raw, which must be a valid
// encoding of that kind of token. The token does not copy raw.
func FromRaw(k Kind, raw []byte) Token {
	return Token{Kind: k, raw: raw}
}

// FromText returns a token of kind k holding the text s, as Detach leaves one.
func FromText(k Kind, s string) Token { return Token{Kind: k, str: s} }

// FromString returns a String token with the value s.
func FromString(s string) Token { return Token{Kind: String, str: s} }

// FromInt returns a Number token with the value i.
func FromInt(i int64) Token { return Token{Kind: Number, str: strconv.FormatInt(i, 10)} }

// FromUint returns a Number token with the value u.
func FromUint(u uint64) Token { return Token{Kind: Number, str: strconv.FormatUint(u, 10)} }

// FromFloat returns a Number token with the value f. Infinities and NaN are only
// representable in KDL v2.
func FromFloat(f float64) Token {
	var s string
	switch {
	case math.IsInf(f, 1):
		s = "#inf"
	case math.IsInf(f, -1):
		s = "#-inf"
	case math.IsNaN(f):
		s = "#nan"
	default:
		s = FormatFloat(f, 64)
	}
	return Token{Kind: Number, str: s}
}

// FormatFloat returns the shortest literal of the finite f that reads back as
// the same float of bitSize bits, 32 or 64. The literal always has a fraction
// or an exponent, as in 1.0 or 1e+21, so that it is not read as an integer.
func FormatFloat(f float64, bitSize int) string {
	s := strconv.FormatFloat(f, 'g', -1, bitSize)
	if !strings.ContainsAny(s, ".e") {
		s += ".0"
	}
	return s
}

// FromBool returns a True or False token.
func FromBool(b bool) Token {
	if b {
		return Token{Kind: True}
	}
	return Token{Kind: False}
}

// Raw returns the bytes the token was read from, or nil if it was not read by
// the lexer. The bytes are only valid until the lexer's next read; see Clone
// and Detach.
func (t Token) Raw() []byte { return t.raw }

// Clone returns a copy of the token with its own copy of the raw bytes.
func (t Token) Clone() Token {
	t.raw = bytes.Clone(t.raw)
	return t
}

// Detach returns the token with its raw bytes decoded into a value, as if it
// had been built with a constructor.
func (t Token) Detach() Token {
	if t.raw != nil {
		t.str = t.String()
		t.raw = nil
	}
	return t
}

// fixedText returns the text of a token of kind k, if it is always the same.
func fixedText(k Kind) (string, bool) {
	switch k {
	case True:
		return "true", true
	case False:
		return "false", true
	case Null:
		return "null", true
	case Slashdash:
		return "/-", true
	case Whitespace:
		return " ", true
	case LParen, RParen, LBrace, RBrace, Equals, Semicolon, Escline, Newline:
		return string(rune(k)), true
	}
	return "", false
}

// String returns the decoded value of an Ident, String or RawString token, and
// the text of any other token.
func (t Token) String() string {
	if t.raw == nil {
		if s, ok := fixedText(t.Kind); ok {
			return s
		}
		return t.str
	}
	switch t.Kind {
	case String, RawString:
		s, _ := DecodeString(string(t.raw))
		return s
	}
	return string(t.raw)
}

// Bool reports whether the token is True.
func (t Token) Bool() bool { return t.Kind == True }

// Int returns the value of a Number token as an integer.
func (t Token) Int() (int64, error) {
	if t.Kind != Number {
		return 0, errNotNumber
	}
	return parseInt(t.String())
}

// Uint returns the value of a Number token as an unsigned integer.
func (t Token) Uint() (uint64, error) {
	if t.Kind != Number {
		return 0, errNotNumber
	}
	s := t.String()
	sign, digits, base := splitNumber(s)
	if base == 10 && strings.ContainsAny(digits, ".eE") {
		return 0, fmt.Errorf("%s is not an integer", s)
	}
	if sign == "-" {
		return 0, fmt.Errorf("%s is negative", s)
	}
	u, err := strconv.ParseUint(strings.ReplaceAll(digits, "_", ""), base, 64)
	if err != nil {
		return 0, fmt.Errorf("%s overflows a 64-bit unsigned integer", s)
	}
	return u, nil
}

// Float returns the value of a Number token as a float.
func (t Token) Float() (float64, error) {
	if t.Kind != Number {
		return 0, errNotNumber
	}
	s := t.String()
	switch s {
	case "#inf":
		return math.Inf(1), nil
	case "#-inf":
		return math.Inf(-1), nil
	case "#nan":
		return math.NaN(), nil
	}
	if sign, digits, base := splitNumber(s); base != 10 {
		i, ok := new(big.Int).SetString(sign+strings.ReplaceAll(digits, "_", ""), base)
		if !ok {
			return 0, fmt.Errorf("invalid number %s", s)
		}
		f, _ := new(big.Float).SetInt(i).Float64()
		if math.IsInf(f, 0) {
			return 0, fmt.Errorf("%s overflows a 64-bit float", s)
		}
		return f, nil
	}
	f, err := strconv.ParseFloat(strings.ReplaceAll(s, "_", ""), 64)
	if err != nil {
		return 0, fmt.Errorf("%s overflows a 64-bit float", s)
	}
	return f, nil
}

// Canonical returns the literal of a Number token in canonical form: without
// underscores, a '+' sign or leading zeros, with the digits of integers in
// lower case, and with the exponent of floats written as 'E' and a sign, as
// in 1.5E+10. If decimal is set, integers are written in decimal. Other
// tokens are returned as their String.
func (t Token) Canonical(decimal bool) string {
	s := t.String()
	if t.Kind != Number || strings.HasPrefix(s, "#") {
		return s
	}
	sign, digits, base := splitNumber(strings.ReplaceAll(s, "_", ""))
	if sign == "+" {
		sign = ""
	}

	if base == 10 && strings.ContainsAny(digits, ".eE") {
		mantissa, exp, hasExp := strings.Cut(strings.ToLower(digits), "e")
		whole, frac, hasFrac := strings.Cut(mantissa, ".")
		mantissa = trimZeros(whole)
		if hasFrac {
			mantissa += "." + frac
		}
		if !hasExp {
			return sign + mantissa
		}
		expSign := "+"
		if exp[0] == '+' || exp[0] == '-' {
			expSign, exp = exp[:1], exp[1:]
		}
		return sign + mantissa + "E" + expSign + trimZeros(exp)
	}

	digits = trimZeros(strings.ToLower(digits))
	if digits == "0" {
		sign = ""
	}
	switch {
	case base == 10:
		return sign + digits
	case decimal:
		i, _ := new(big.Int).SetString(sign+digits, base)
		return i.String()
	}
	return sign + map[int]string{16: "0x", 8: "0o", 2: "0b"}[base] + digits
}

// trimZeros returns the digits s without leading zeros, leaving at least one
// digit.
func trimZeros(s string) string {
	t := strings.TrimLeft(s, "0")
	if t == "" && s != "" {
		return "0"
	}
	return t
}

// splitNumber splits a number literal into its sign, digits and base.
func splitNumber(s string) (sign, digits string, base int) {
	if s != "" && (s[0] == '+' || s[0] == '-') {
		sign, s = s[:1], s[1:]
	}
	if len(s) >= 2 && s[0] == '0' {
		switch s[1] {
		case 'x':
			return sign, s[2:], 16
		case 'o':
			return sign, s[2:], 8
		case 'b':
			return sign, s[2:], 2
		}
	}
	return sign, s, 10
}

func parseInt(s string) (int64, error) {
	sign, digits, base := splitNumber(s)
	if base == 10 && strings.ContainsAny(digits, ".eE") {
		return 0, fmt.Errorf("%s is not an integer", s)
	}
	i, err := strconv.ParseInt(sign+strings.ReplaceAll(digits, "_", ""), base, 64)
	if err != nil {
		return 0, fmt.Errorf("%s overflows a 64-bit integer", s)
	}
	return i, nil
}
