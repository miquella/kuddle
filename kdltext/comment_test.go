package kdltext_test

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/miquella/kuddle/kdltext"
)

func decodeAll(t *testing.T, input string, opts ...kdltext.Options) []kdltext.Token {
	t.Helper()
	var toks []kdltext.Token
	d := kdltext.NewDecoder(strings.NewReader(input), opts...)
	for {
		tok, err := d.ReadToken()
		if err == io.EOF {
			return toks
		}
		if err != nil {
			t.Fatal(err)
		}
		toks = append(toks, tok)
	}
}

func line(s string) kdltext.Comment  { return kdltext.Comment{Kind: kdltext.CommentLine, Text: s} }
func block(s string) kdltext.Comment { return kdltext.Comment{Kind: kdltext.CommentBlock, Text: s} }
func slash(s string) kdltext.Comment { return kdltext.Comment{Kind: kdltext.CommentSlashdash, Text: s} }

// tok describes t briefly: its kind, then its name or value, and comment.
func tok(t kdltext.Token) string {
	s := string(rune(t.Kind))
	switch t.Kind {
	case kdltext.KindNode:
		s += " " + t.Name()
	case kdltext.KindValue:
		s += " " + t.Value.String()
	case kdltext.KindProp:
		s += " " + t.Name()
	}
	if t.Comment.Kind != 0 {
		s += " " + string(rune(t.Comment.Kind)) + t.Comment.Text
	}
	return s
}

func TestNodeComment(t *testing.T) {
	input := `// This wouldn't be attached to a node

// But this would!
some-node

// And this multi-line
// comment would be attached to the node
// with newlines intact
another-node

/* A block. */
block-node

// Not attached either.
/- disabled
after-slashdash
// After a node.
after-node
`
	var got []string
	for _, t := range decodeAll(t, input) {
		if t.Kind != kdltext.KindEnd {
			got = append(got, tok(t))
		}
	}
	want := []string{
		"c / This wouldn't be attached to a node",
		"n some-node / But this would!",
		"n another-node / And this multi-line\n comment would be attached to the node\n with newlines intact",
		"n block-node * A block. ",
		"c / Not attached either.",
		"c - disabled",
		"n after-slashdash",
		"n after-node / After a node.",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// commented exercises every place a comment can be.
const commented = `// The header.

/- kdl-version 2
// The server.
server /* name */ main /-"old" k=1 /-{ x } { // the main one
    /* listen
       here */
    listen :80 // http
    /- listen :8080 {
        tls #true
    }
    tls #true; // after a semicolon
    run \ // why
        fast
    a; /* before b */ b
    // the end of server

} // after the brace
empty {
    // only a comment
}

/- trailing
// done
`

func TestReadComments(t *testing.T) {
	var got []string
	for _, t := range decodeAll(t, commented) {
		got = append(got, tok(t))
	}
	want := []string{
		"c / The header.",
		"c - kdl-version 2",
		"n server / The server.",
		"i * name ",
		"v main",
		`i -"old"`,
		"p k", "v 1",
		"i -{ x }",
		"i / the main one",
		"n listen * listen\n       here ",
		"v :80",
		"i / http",
		"e",
		"c - listen :8080 {\n        tls #true\n    }",
		"n tls",
		"v #true",
		"i / after a semicolon",
		"e",
		"n run",
		"i / why",
		"v fast",
		"e",
		"n a",
		"e",
		"n b * before b ",
		"e",
		"c / the end of server",
		"e",
		"c / after the brace",
		"n empty",
		"c / only a comment",
		"e",
		"c - trailing",
		"c / done",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

// TestCommentsInProperty checks that the comments within a property come
// after its name and value tokens, and leave the input offset past them.
func TestCommentsInProperty(t *testing.T) {
	d := kdltext.NewDecoder(strings.NewReader("n k /* a */ = /* b */ (t)1 2\n"))
	var got []string
	for {
		tk, err := d.ReadToken()
		if err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		got = append(got, fmt.Sprintf("%s@%d", tok(tk), d.InputOffset()))
	}
	want := []string{"n n@1", "p k@3", "v 1@26", "i * a @26", "i * b @26", "v 2@28", "e@29"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestWriteComments(t *testing.T) {
	want := `// The header.
/- kdl-version 2
// The server.
server /* name */ main /-"old" k=1 /-{ x } { // the main one
    /* listen
       here */
    listen :80 // http
    /- listen :8080 {
        tls #true
    }
    tls #true // after a semicolon
    run \ // why
        fast
    a
    /* before b */
    b
    // the end of server
}
// after the brace

empty {
    // only a comment
}
/- trailing
// done
`
	got, err := encode(decodeAll(t, commented))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	// Writing what was read changes nothing.
	again, err := encode(decodeAll(t, got))
	if err != nil || again != got {
		t.Errorf("again: %v\n%s", err, again)
	}
}

func TestWriteCommentsSetInGo(t *testing.T) {
	toks := []kdltext.Token{
		{Kind: kdltext.KindComment, Comment: line(" Two\n lines.")},
		{Kind: kdltext.KindNode, Value: kdltext.String("a"), Comment: block("multi\nline")},
		{Kind: kdltext.KindEntryComment, Comment: block(" x ")},
		{Kind: kdltext.KindEntryComment, Comment: slash(" k=0")},
		{Kind: kdltext.KindValue, Value: kdltext.Int(1)},
		{Kind: kdltext.KindEntryComment, Comment: line(" mid")},
		{Kind: kdltext.KindValue, Value: kdltext.Int(2)},
		{Kind: kdltext.KindEntryComment, Comment: line(" end")},
		{Kind: kdltext.KindEnd},
		{Kind: kdltext.KindComment, Comment: line(" one")},
		{Kind: kdltext.KindComment, Comment: line(" two")},
		{Kind: kdltext.KindComment, Comment: slash(" old {\n    b\n}")},
	}
	want := `// Two
// lines.
/*multi
line*/
a /* x */ /- k=0 1 \ // mid
    2 // end
// one

// two
/- old {
    b
}
`
	got, err := encode(toks)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if again, err := encode(decodeAll(t, got)); err != nil || again != got {
		t.Errorf("again: %v\n%s", err, again)
	}
}

// TestBlockCommentText checks that the text of a block comment is read as it
// was written: with the spaces at its ends, on one line or several.
func TestBlockCommentText(t *testing.T) {
	for _, text := range []string{"", "a", " a", "a ", "  ", "*", "0\n", "\na\n", " a\nb "} {
		toks := []kdltext.Token{{Kind: kdltext.KindComment, Comment: block(text)}}
		got, err := encode(toks)
		if err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		if read := decodeAll(t, got); len(read) != 1 || read[0].Comment != block(text) {
			t.Errorf("%q: wrote %q, read %+v", text, got, read)
		}
	}
}

// A slash-dashed children block may follow the '}' in KDL v2.
func TestSlashdashAfterChildren(t *testing.T) {
	const input = "b /- { c } {\n    e\n} /- { f } /- {\n    g\n}\n"
	got, err := encode(decodeAll(t, input))
	if err != nil {
		t.Fatal(err)
	}
	if got != input {
		t.Errorf("got\n%s\nwant\n%s", got, input)
	}
}

func TestWriteCommentErrors(t *testing.T) {
	node := func(c kdltext.Comment) []kdltext.Token {
		return []kdltext.Token{{Kind: kdltext.KindNode, Value: kdltext.String("a"), Comment: c}}
	}
	entry := func(c kdltext.Comment) []kdltext.Token {
		return []kdltext.Token{{Kind: kdltext.KindNode, Value: kdltext.String("a")}, {Kind: kdltext.KindEntryComment, Comment: c}}
	}
	comment := func(c kdltext.Comment) []kdltext.Token {
		return []kdltext.Token{{Kind: kdltext.KindComment, Comment: c}}
	}
	tests := []struct {
		toks []kdltext.Token
		want string
	}{
		{node(line("bad \x01")), "kdltext: comment contains the disallowed character U+0001"},
		{node(line("bad \xff")), "kdltext: comment is not valid UTF-8"},
		{node(block("a */ b")), `kdltext: block comment "a */ b" is not valid: its /* and */ must be balanced`},
		{node(slash("b")), "kdltext: a node's comment can't be slash-dashed"},
		{entry(line("two\nlines")), `kdltext: line comment "two\nlines" can't span lines here`},
		{entry(slash("{")), `kdltext: slash-dashed "{" is not valid KDL here`},
		{comment(slash("a {")), `kdltext: slash-dashed "a {" is not valid KDL here`},
		{comment(slash("a\nb")), `kdltext: slash-dashed "a\nb" is not valid KDL here`},
		{comment(kdltext.Comment{Text: "x"}), `kdltext: invalid comment kind '\x00'`},
		{
			[]kdltext.Token{{Kind: kdltext.KindNode, Value: kdltext.String("a")}, {Kind: kdltext.KindNode, Value: kdltext.String("b")}, {Kind: kdltext.KindEnd}, {Kind: kdltext.KindValue, Value: kdltext.Int(1)}},
			"kdltext: value after the node's children",
		},
		{
			[]kdltext.Token{{Kind: kdltext.KindNode, Value: kdltext.String("a")}, {Kind: kdltext.KindProp, Value: kdltext.String("k")}, {Kind: kdltext.KindEntryComment, Comment: line("x")}},
			`kdltext: entry comment after property "k", before its value`,
		},
	}
	for _, tt := range tests {
		if _, err := encode(tt.toks); err == nil || err.Error() != tt.want {
			t.Errorf("got %v, want %s", err, tt.want)
		}
	}
}

// TestWriteV1CommentControl checks that comments are written in v1 with the
// characters that only v2 disallows or reads as newlines, as they are read.
func TestWriteV1CommentControl(t *testing.T) {
	for _, in := range []string{"// a \x01\nn\n", "// a \v b\nn\n", "/* a \x01 */\nn\n", "/- n \"\x01\"\n"} {
		var toks []kdltext.Token
		d := kdltext.NewDecoder(strings.NewReader(in), kdltext.WithVersion(kdltext.V1))
		for {
			tok, err := d.ReadToken()
			if err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("%q: %v", in, err)
			}
			toks = append(toks, tok)
		}
		if got, err := encode(toks, kdltext.WithVersion(kdltext.V1)); err != nil {
			t.Errorf("%q: %v", in, err)
		} else if got != in {
			t.Errorf("%q: got %q", in, got)
		}
		if _, err := encode(toks); err == nil {
			t.Errorf("%q: wrote v2, want error", in)
		}
	}
}

// TestSemicolonEnd checks that the KindEnd token of a node ended by ';' is
// at the ';', though the comments on the rest of the line are read first.
func TestSemicolonEnd(t *testing.T) {
	for in, want := range map[string]string{
		"a 1; b":           "[node@1:1 value@1:3 end@1:4 node@1:6 end@1:7]",
		"a 1; // c\nb":     "[node@1:1 value@1:3 entry comment@1:6 end@1:4 node@2:1 end@2:2]",
		"a 1;/* x */ b":    "[node@1:1 value@1:3 end@1:4 node@1:13 end@1:14]",
		"a; /* x */\nb":    "[node@1:1 entry comment@1:4 end@1:2 node@2:1 end@2:2]",
		"p {\n  a;\n}\nb;": "[node@1:1 node@2:3 end@2:4 end@3:1 node@4:1 end@4:2]",
	} {
		d := kdltext.NewDecoder(strings.NewReader(in))
		var got []string
		for {
			tok, err := d.ReadToken()
			if err != nil {
				break
			}
			got = append(got, fmt.Sprintf("%v@%d:%d", tok.Kind, d.Pos().Line, d.Pos().Column))
		}
		if fmt.Sprint(got) != want {
			t.Errorf("%q:\ngot  %v\nwant %s", in, got, want)
		}
	}
}

// TestSlashdashEntrySpace checks that the space read after a slash-dashed
// string, while looking for a property's '=', is not part of the entry, so
// that what follows it is not commented out when written.
func TestSlashdashEntrySpace(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"a /-b \\ // x\n 0\n", "a /-b \\ // x\n    0\n"},
		{"a /-b\\\n0\n", "a /-b 0\n"},
		{"a /-b /* x */ 0\n", "a /-b /* x */ 0\n"},
		{"a /-b=1 \\\n 0\n", "a /-b=1 0\n"},
		{"a /-b\n", "a /-b\n"},
		{"/-a\\\n", "/-a\n"},
		{"/-a \\ // x\nb\n", "/-a \\ // x\nb\n"}, // b is an argument of a
		{"/-a \\ // x\n", "/-a\n// x\n"},
		{"a /-(t)b \\\n 0\n", "a /-(t)b 0\n"},
		{"/-a /-b \\\n 0\nc\n", "/-a /-b \\\n 0\nc\n"},
	} {
		toks := decodeAll(t, tt.in)
		got, err := encode(toks)
		if err != nil {
			t.Errorf("%q: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("%q: got %q, want %q", tt.in, got, tt.want)
		}
		if again, err := encode(decodeAll(t, got)); err != nil || again != got {
			t.Errorf("%q: wrote %q, then %q, %v", tt.in, got, again, err)
		}
	}
}

// TestWriteBlockCommentNewlines checks that a newline format, once set, is
// used inside block comments as between line comments, and that slash-dashed
// text, which can hold strings, is left alone.
func TestWriteBlockCommentNewlines(t *testing.T) {
	crlf, lf := kdltext.WithNewline("\r\n"), kdltext.WithNewline("\n")
	for _, tt := range []struct {
		name string
		c    kdltext.Comment
		opts []kdltext.Options
		want string
	}{
		{"unset keeps LF", block("a\nb"), nil, "/*a\nb*/\n"},
		{"unset keeps CRLF", block("a\r\nb"), nil, "/*a\r\nb*/\n"},
		{"CRLF from LF", block("a\nb"), []kdltext.Options{crlf}, "/*a\r\nb*/\r\n"},
		{"CRLF from CR", block("a\rb"), []kdltext.Options{crlf}, "/*a\r\nb*/\r\n"},
		{"CRLF stays CRLF", block("a\r\nb"), []kdltext.Options{crlf}, "/*a\r\nb*/\r\n"},
		{"LF from CRLF", block("a\r\nb"), []kdltext.Options{lf}, "/*a\nb*/\n"},
		{"line comments", line(" a\n b"), []kdltext.Options{crlf}, "// a\r\n// b\r\n"},
		{"slash-dash", slash(" a {\n    b\n}"), []kdltext.Options{crlf}, "/- a {\n    b\n}\r\n"},
	} {
		got, err := encode([]kdltext.Token{{Kind: kdltext.KindComment, Comment: tt.c}}, tt.opts...)
		if err != nil || got != tt.want {
			t.Errorf("%s: got %q, %v; want %q", tt.name, got, err, tt.want)
		}
	}
}
