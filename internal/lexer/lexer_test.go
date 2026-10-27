package lexer_test

import (
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/miquella/kuddle/internal/lexer"
)

type tok struct {
	Kind  lexer.Kind
	Raw   string
	Value string
}

// tk returns the expected token; the value defaults to the raw text.
func tk(kind lexer.Kind, raw string, value ...string) tok {
	if len(value) > 0 {
		return tok{kind, raw, value[0]}
	}
	return tok{kind, raw, raw}
}

var (
	ws  = tk(lexer.Whitespace, " ")
	nl  = tk(lexer.Newline, "\n")
	eof = tk(lexer.EOF, "")
	bom = tk(lexer.Whitespace, "\uFEFF")
)

// lexAll reads all tokens from r, copying them out of the lexer's buffer, and
// returns them with the lexer's final version.
func lexAll(r io.Reader, v lexer.Version) ([]tok, lexer.Version, error) {
	l := lexer.New(r, v)
	var toks []tok
	for {
		t, err := l.NextToken()
		if err != nil {
			return toks, l.Version(), err
		}
		toks = append(toks, tok{t.Kind, string(t.Raw()), t.String()})
		if t.Kind == lexer.EOF {
			return toks, l.Version(), nil
		}
	}
}

// readers returns readers over input that exercise different buffer
// boundaries.
func readers(input string) map[string]func() io.Reader {
	return map[string]func() io.Reader{
		"whole":   func() io.Reader { return strings.NewReader(input) },
		"onebyte": func() io.Reader { return iotest.OneByteReader(strings.NewReader(input)) },
	}
}

func TestNextToken(t *testing.T) {
	tests := []struct {
		name    string
		version lexer.Version // lexer version
		input   string
		want    []tok
		detect  lexer.Version // version after lexing
	}{
		{"idents", lexer.Undecided, `hello world "thing"`, []tok{
			tk(lexer.Ident, "hello"), ws,
			tk(lexer.Ident, "world"), ws,
			tk(lexer.String, `"thing"`, "thing"),
			eof,
		}, lexer.Undecided},
		{"props", lexer.Undecided, "(t)node key=-1.5e+3_0 0xf_f\r\n", []tok{
			tk(lexer.LParen, "("),
			tk(lexer.Ident, "t"),
			tk(lexer.RParen, ")"),
			tk(lexer.Ident, "node"), ws,
			tk(lexer.Ident, "key"),
			tk(lexer.Equals, "="),
			tk(lexer.Number, "-1.5e+3_0"), ws,
			tk(lexer.Number, "0xf_f"),
			tk(lexer.Newline, "\r\n"),
			eof,
		}, lexer.Undecided},
		{"children", lexer.Undecided, `a {b; c}`, []tok{
			tk(lexer.Ident, "a"), ws,
			tk(lexer.LBrace, "{"),
			tk(lexer.Ident, "b"),
			tk(lexer.Semicolon, ";"), ws,
			tk(lexer.Ident, "c"),
			tk(lexer.RBrace, "}"),
			eof,
		}, lexer.Undecided},
		{"comments", lexer.Undecided, "a // c\n/-b /* x /* y */ */\\ \n", []tok{
			tk(lexer.Ident, "a"), ws,
			tk(lexer.LineComment, "// c"), nl,
			tk(lexer.Slashdash, "/-"),
			tk(lexer.Ident, "b"), ws,
			tk(lexer.BlockComment, "/* x /* y */ */"),
			tk(lexer.Escline, `\`), ws, nl,
			eof,
		}, lexer.Undecided},
		{"unicode", lexer.Undecided, "\u3000ünï\u00A0\u2028\r", []tok{
			tk(lexer.Whitespace, "\u3000"),
			tk(lexer.Ident, "ünï"),
			tk(lexer.Whitespace, "\u00A0"),
			tk(lexer.Newline, "\u2028"),
			tk(lexer.Newline, "\r"),
			eof,
		}, lexer.Undecided},
		{"leading BOM", lexer.V2, "\uFEFFa", []tok{bom, tk(lexer.Ident, "a"), eof}, lexer.V2},

		// v1
		{"v1 keywords", lexer.V1, "true false null inf -foo + - #x rr", []tok{
			tk(lexer.True, "true"), ws,
			tk(lexer.False, "false"), ws,
			tk(lexer.Null, "null"), ws,
			tk(lexer.Ident, "inf"), ws,
			tk(lexer.Ident, "-foo"), ws,
			tk(lexer.Ident, "+"), ws,
			tk(lexer.Ident, "-"), ws,
			tk(lexer.Ident, "#x"), ws,
			tk(lexer.Ident, "rr"),
			eof,
		}, lexer.V1},
		{"v1 strings", lexer.V1, `"a\n\"\\\/\u{1F600}b" r"x\n" r##"a"#b"## "multi
line"`, []tok{
			tk(lexer.String, `"a\n\"\\\/\u{1F600}b"`, "a\n\"\\/\U0001F600b"), ws,
			tk(lexer.RawString, `r"x\n"`, `x\n`), ws,
			tk(lexer.RawString, `r##"a"#b"##`, `a"#b`), ws,
			tk(lexer.String, "\"multi\nline\"", "multi\nline"),
			eof,
		}, lexer.V1},
		{"v1 empty strings", lexer.V1, `""""`, []tok{tk(lexer.String, `""`, ""), tk(lexer.String, `""`, ""), eof}, lexer.V1},
		{"v1 BOM", lexer.V1, "a\uFEFFb", []tok{tk(lexer.Ident, "a"), bom, tk(lexer.Ident, "b"), eof}, lexer.V1},
		{"v1 control in string", lexer.V1, "\"\x01\"", []tok{tk(lexer.String, "\"\x01\"", "\x01"), eof}, lexer.V1},
		{"v1 dotted", lexer.V1, ".1 -.1", []tok{tk(lexer.Ident, ".1"), ws, tk(lexer.Ident, "-.1"), eof}, lexer.V1},

		// v2
		{"v2 keywords", lexer.V2, "#true #false #null #inf #-inf #nan a<b>,c", []tok{
			tk(lexer.True, "#true", "#true"), ws,
			tk(lexer.False, "#false", "#false"), ws,
			tk(lexer.Null, "#null", "#null"), ws,
			tk(lexer.Number, "#inf"), ws,
			tk(lexer.Number, "#-inf"), ws,
			tk(lexer.Number, "#nan"), ws,
			tk(lexer.Ident, "a<b>,c"),
			eof,
		}, lexer.V2},
		{"v2 strings", lexer.V2, `"\s\
		  x" #"\n"# ##"a"#b"## r"x"`, []tok{
			tk(lexer.String, "\"\\s\\\n\t\t  x\"", " x"), ws,
			tk(lexer.RawString, `#"\n"#`, `\n`), ws,
			tk(lexer.RawString, `##"a"#b"##`, `a"#b`), ws,
			tk(lexer.Ident, "r"),
			tk(lexer.String, `"x"`, "x"),
			eof,
		}, lexer.V2},
		{"v2 multi-line", lexer.V2, "\"\"\"\n    foo\n\n  \\s bar\n  \"\"\" #\"\"\"\r\n  \"\"\"\r\n  \"\"\"#", []tok{
			tk(lexer.String, "\"\"\"\n    foo\n\n  \\s bar\n  \"\"\"", "  foo\n\n  bar"), ws,
			tk(lexer.RawString, "#\"\"\"\r\n  \"\"\"\r\n  \"\"\"#", `"""`),
			eof,
		}, lexer.V2},
		{"v2 multi-line ws escape", lexer.V2, "\"\"\"\n  foo \\\nbar\n  baz\n  \\   \"\"\"", []tok{
			tk(lexer.String, "\"\"\"\n  foo \\\nbar\n  baz\n  \\   \"\"\"", "foo bar\nbaz"),
			eof,
		}, lexer.V2},
		{"v2 VT", lexer.V2, "a\vb", []tok{tk(lexer.Ident, "a"), tk(lexer.Newline, "\v"), tk(lexer.Ident, "b"), eof}, lexer.V2},

		// Detection
		{"detect v1 raw string", lexer.Undecided, `a r"x"`, []tok{tk(lexer.Ident, "a"), ws, tk(lexer.RawString, `r"x"`, "x"), eof}, lexer.V1},
		{"detect v1 keyword", lexer.Undecided, `a true`, []tok{tk(lexer.Ident, "a"), ws, tk(lexer.True, "true"), eof}, lexer.V1},
		{"detect v1 escape", lexer.Undecided, `"\/" #x`, []tok{tk(lexer.String, `"\/"`, "/"), ws, tk(lexer.Ident, "#x"), eof}, lexer.V1},
		{"detect v1 ident", lexer.Undecided, `a#b`, []tok{tk(lexer.Ident, "a#b"), eof}, lexer.V1},
		{"detect v2 keyword", lexer.Undecided, `a #true`, []tok{tk(lexer.Ident, "a"), ws, tk(lexer.True, "#true", "#true"), eof}, lexer.V2},
		{"detect v2 ident", lexer.Undecided, `a<b`, []tok{tk(lexer.Ident, "a<b"), eof}, lexer.V2},
		{"detect v2 escape", lexer.Undecided, `"\s"`, []tok{tk(lexer.String, `"\s"`, " "), eof}, lexer.V2},
		{"marker v1", lexer.Undecided, "/- kdl-version 1\nr\"x\"", []tok{
			tk(lexer.Slashdash, "/-"), ws,
			tk(lexer.Ident, "kdl-version"), ws,
			tk(lexer.Number, "1"), nl,
			tk(lexer.RawString, `r"x"`, "x"),
			eof,
		}, lexer.V1},
		{"marker v2", lexer.Undecided, "\uFEFF/-kdl-version\t2 \n", []tok{
			bom,
			tk(lexer.Slashdash, "/-"),
			tk(lexer.Ident, "kdl-version"), tk(lexer.Whitespace, "\t"),
			tk(lexer.Number, "2"), ws, nl,
			eof,
		}, lexer.V2},
		{"not a marker", lexer.Undecided, "/- kdl-version 2 x\n", nil, lexer.Undecided},
		{"marker after space", lexer.Undecided, " /- kdl-version 2\n", nil, lexer.Undecided},
		{"marker after BOM and space", lexer.Undecided, "\uFEFF /- kdl-version 2\n", nil, lexer.Undecided},
		{"marker at EOF", lexer.Undecided, "/- kdl-version 2", nil, lexer.Undecided},
		{"partial marker", lexer.Undecided, "/- kdl-version", nil, lexer.Undecided},
	}
	for _, tt := range tests {
		for name, r := range readers(tt.input) {
			t.Run(tt.name+"/"+name, func(t *testing.T) {
				got, v, err := lexAll(r(), tt.version)
				if err != nil {
					t.Fatalf("%q: %v", tt.input, err)
				}
				if tt.want != nil && !slices.Equal(got, tt.want) {
					t.Errorf("%q:\n got %q\nwant %q", tt.input, got, tt.want)
				}
				if v != tt.detect {
					t.Errorf("%q: version = %d, want %d", tt.input, v, tt.detect)
				}
			})
		}
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		version lexer.Version
		input   string
		offset  int64
	}{
		{lexer.Undecided, `a "unterminated`, 2},
		{lexer.Undecided, `"bad \q"`, 5},
		{lexer.Undecided, `"\u{}"`, 1},
		{lexer.Undecided, `"\u{1234567}"`, 1},
		{lexer.Undecided, `"\u{D800}"`, 1},
		{lexer.Undecided, `"\u{110000}"`, 1},
		{lexer.V2, "-inf", 0},
		{lexer.Undecided, `r#"x"`, 0},
		{lexer.Undecided, `/* x /* */`, 0},
		{lexer.Undecided, `a / b`, 2},
		{lexer.Undecided, `1abc`, 0},
		{lexer.Undecided, `1.`, 0},
		{lexer.Undecided, `0x`, 0},
		{lexer.Undecided, `0x_f`, 0},
		{lexer.Undecided, `0o8`, 0},
		{lexer.Undecided, `1e`, 0},
		{lexer.Undecided, "a \xff", 2},
		{lexer.Undecided, "a \xe2\x80", 2},
		{lexer.Undecided, `"\/\s"`, 3},
		{lexer.V1, "a ,", 2},
		{lexer.V1, `"\s"`, 1},
		{lexer.V1, "a\vb", 1},
		{lexer.V1, "a\x01", 1},
		{lexer.V2, `"\/"`, 1},
		{lexer.V2, "a true", 2},
		{lexer.V2, "a -inf", 2},
		{lexer.V2, "a .1", 2},
		{lexer.V2, "a +.1", 2},
		{lexer.V2, "#foo", 0},
		{lexer.V2, "a#b", 1},
		{lexer.V2, "a \x01", 2},
		{lexer.V2, "a\uFEFF", 1},
		{lexer.V2, "\"a\nb\"", 2},
		{lexer.V2, "#\"a\nb\"#", 3},
		{lexer.V2, `"""foo"""`, 3},
		{lexer.V2, "\"\"\"\n  foo\"\"\"", 0},
		{lexer.V2, "\"\"\"\n  foo\n bar\n  \"\"\"", 0},
		{lexer.V2, "\"\"\"\n  foo\n  bar\\\n  \"\"\"", 0},
	}
	for _, tt := range tests {
		for name, r := range readers(tt.input) {
			t.Run(name, func(t *testing.T) {
				_, _, err := lexAll(r(), tt.version)
				se, ok := errors.AsType[*lexer.SyntaxError](err)
				if !ok {
					t.Fatalf("v%d %q: err = %v, want SyntaxError", tt.version, tt.input, err)
				}
				if se.Offset != tt.offset {
					t.Errorf("v%d %q: offset = %d, want %d (%v)", tt.version, tt.input, se.Offset, tt.offset, err)
				}
			})
		}
	}
}

func TestVersionMismatch(t *testing.T) {
	for _, tt := range []struct {
		version lexer.Version
		input   string
	}{
		{lexer.V1, "/- kdl-version 2\n"},
		{lexer.V2, "\uFEFF/- kdl-version 1\nnode"},
	} {
		_, _, err := lexAll(strings.NewReader(tt.input), tt.version)
		if !errors.Is(err, lexer.ErrVersionMismatch) {
			t.Errorf("v%d %q: err = %v, want ErrVersionMismatch", tt.version, tt.input, err)
		}
	}
}

// TestErrorInMarker checks that tokens read while checking for a version
// marker are emitted before an error that ends the check.
func TestErrorInMarker(t *testing.T) {
	for name, r := range readers(`/- kdl-version "x`) {
		got, _, err := lexAll(r(), lexer.Undecided)
		want := []tok{tk(lexer.Slashdash, "/-"), ws, tk(lexer.Ident, "kdl-version"), ws}
		if se, ok := errors.AsType[*lexer.SyntaxError](err); !ok || se.Offset != 15 || !slices.Equal(got, want) {
			t.Errorf("%s: got %q, %v; want %q and an error at offset 15", name, got, err, want)
		}
	}
}

// TestLongWhitespace lexes a whitespace run spanning many reads, with
// multi-byte runes split across read boundaries, as a single token.
func TestLongWhitespace(t *testing.T) {
	input := "a" + strings.Repeat(" \u3000", 1<<16) + "b"
	got, _, err := lexAll(iotest.HalfReader(strings.NewReader(input)), lexer.Undecided)
	if err != nil {
		t.Fatal(err)
	}
	want := []tok{tk(lexer.Ident, "a"), tk(lexer.Whitespace, input[1:len(input)-1]), tk(lexer.Ident, "b"), eof}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
}
