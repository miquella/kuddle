package kdltext

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/miquella/kuddle/internal/lexer"
)

// ValueKind is the kind of a Value.
type ValueKind byte

const (
	ValueInvalid ValueKind = 0
	ValueString  ValueKind = '"'
	ValueNumber  ValueKind = '0'
	ValueBool    ValueKind = 'b'
	ValueNull    ValueKind = 'n'
)

func (k ValueKind) String() string {
	switch k {
	case ValueString:
		return "string"
	case ValueNumber:
		return "number"
	case ValueBool:
		return "bool"
	case ValueNull:
		return "null"
	}
	return "invalid"
}

// Value is a KDL value: a string, number, boolean or null, with an optional
// type annotation. The zero Value is invalid.
//
// A Token also uses a Value for the name of a node or property, which is a
// string; see [Token.Value] for what the Type means there.
//
// A number keeps the literal it was written as, so that numbers of any size
// and precision are preserved.
type Value struct {
	// Type is the type annotation, such as "u8" in (u8)10, or nil for none.
	// An empty annotation, ("")10, is a pointer to "". Copies of a Value
	// share the annotation, so it is replaced, as in v.Type = new("u8"),
	// rather than changed through the pointer; Clone makes a copy that does
	// not share it. In a Token, it is the annotation of what the Token's
	// Value holds, which is a value or a node's name; see [Token.Value].
	Type *string

	text   string     // the literal of a number; otherwise, as source says
	kind   lexer.Kind // a kind of string, Number, True, False or Null
	source bool       // whether text is a string as written, rather than its contents
	ver    uint8      // the KDL version of a string as written, or 0 for either
}

// String returns a string value.
func String(s string) Value { return Value{text: s, kind: lexer.String} }

// Quoted returns a string value that is written as a quoted string, even
// where a bare identifier would do. If s is not valid UTF-8, it is the same
// as String, and is an error to write.
func Quoted(s string) Value {
	b, err := lexer.AppendQuote(nil, s)
	if err != nil {
		return String(s)
	}
	return Value{text: string(b), kind: lexer.String, source: true}
}

// number returns a number value with the given literal.
func number(s string) Value { return Value{text: s, kind: lexer.Number} }

// Int returns a number value.
func Int(i int64) Value { return number(strconv.FormatInt(i, 10)) }

// Uint returns a number value.
func Uint(u uint64) Value { return number(strconv.FormatUint(u, 10)) }

// Float returns a number value. Infinities and NaN can only be written in
// KDL v2.
func Float(f float64) Value { return number(lexer.FromFloat(f).String()) }

// Bool returns a boolean value.
func Bool(b bool) Value {
	if b {
		return Value{kind: lexer.True}
	}
	return Value{kind: lexer.False}
}

// Null returns the null value.
func Null() Value { return Value{kind: lexer.Null} }

// Number returns a number value with the literal s, which may be in any form
// that KDL v2 allows, such as "0xff", "1_000", "1.5e300" or "#inf".
func Number(s string) (Value, error) {
	l := lexer.New(bytes.NewBufferString(s), lexer.V2)
	t, err := l.NextToken()
	if err == nil && t.Kind == lexer.Number {
		v := number(t.String())
		if t, err = l.NextToken(); err == nil && t.Kind == lexer.EOF {
			return v, nil
		}
	}
	return Value{}, fmt.Errorf("kdltext: invalid number %q", s)
}

// WithType returns v with the type annotation typ, which may be nil for none.
func (v Value) WithType(typ *string) Value {
	v.Type = typ
	return v
}

// valueOf returns the value of the token t, read in KDL version ver, with the
// type annotation typ.
func valueOf(typ *string, t lexToken, ver lexer.Version) Value {
	switch t.Kind {
	case lexer.Ident, lexer.String, lexer.RawString:
		return Value{Type: typ, text: t.src, kind: t.Kind, source: true, ver: uint8(ver)}
	case lexer.Number:
		return Value{Type: typ, text: t.src, kind: lexer.Number}
	}
	return Value{Type: typ, kind: t.Kind}
}

// Clone returns a copy of v that does not share its type annotation.
func (v Value) Clone() Value {
	v.Type = cloneType(v.Type)
	return v
}

// Equal reports whether v and w have the same kind, type annotation and
// value. Numbers are equal if they have the same literal, so 1.0 and 1.00
// are not equal.
func (v Value) Equal(w Value) bool {
	return v.Kind() == w.Kind() && v.String() == w.String() && equalType(v.Type, w.Type)
}

func cloneType(typ *string) *string {
	if typ == nil {
		return nil
	}
	return new(*typ)
}

func equalType(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

// Kind returns the kind of the value.
func (v Value) Kind() ValueKind {
	switch v.kind {
	case lexer.Ident, lexer.String, lexer.RawString:
		return ValueString
	case lexer.Number:
		return ValueNumber
	case lexer.True, lexer.False:
		return ValueBool
	case lexer.Null:
		return ValueNull
	}
	return ValueInvalid
}

// contents returns the contents of a string, or the literal of a number.
func (v Value) contents() string {
	if v.source && v.kind != lexer.Ident {
		s, _ := lexer.DecodeString(v.text)
		return s
	}
	return v.text
}

// String returns the contents of a string value. For other kinds of value, it
// returns the value as written in KDL v2, such as "1.5" or "#true", without
// any type annotation.
func (v Value) String() string {
	switch v.kind {
	case lexer.True:
		return "#true"
	case lexer.False:
		return "#false"
	case lexer.Null:
		return "#null"
	case lexer.Invalid:
		return "<invalid>"
	}
	return v.contents()
}

// Canonical returns v, a number, in canonical form, or v itself for other
// kinds of value: without underscores, a '+' sign or leading zeros, with the
// digits of an integer in lower case, and with the exponent of a float
// written as 'E' and a sign, as in 1.5E+10. If decimal is set, an integer is
// written in decimal; otherwise it keeps its base.
func (v Value) Canonical(decimal bool) Value {
	if v.kind == lexer.Number {
		v.text = v.numberToken().Canonical(decimal)
	}
	return v
}

// WithNewlines returns v, if it is a string as it was read, with the newlines
// written in it replaced by newline. It changes neither escapes such as \n nor
// a string built with String. KDL normalizes the newlines of a multi-line
// string when it is read, but not those in a KDL v1 string, whose value
// changes. Other values are returned as they are.
func (v Value) WithNewlines(newline string) Value {
	if v.source && v.kind != lexer.Ident {
		s := strings.ReplaceAll(strings.ReplaceAll(v.text, "\r\n", "\n"), "\r", "\n")
		v.text = strings.ReplaceAll(s, "\n", newline)
	}
	return v
}

// numberToken returns the value as a lexer token, for its numeric methods.
func (v Value) numberToken() lexer.Token { return lexer.FromText(v.kind, v.text) }

// Bool reports whether the value is #true.
func (v Value) Bool() bool { return v.kind == lexer.True }

// Int returns the value of an integer number.
func (v Value) Int() (int64, error) { return v.numberToken().Int() }

// Uint returns the value of a non-negative integer number.
func (v Value) Uint() (uint64, error) { return v.numberToken().Uint() }

// Float returns the value of a number. A number too large for a float64 is
// an error.
func (v Value) Float() (float64, error) { return v.numberToken().Float() }
