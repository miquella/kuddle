package kuddle_test

import (
	"testing"

	"github.com/miquella/kuddle"
	"github.com/miquella/kuddle/kdltext"
)

func format(t *testing.T, in string, opts ...kuddle.FormatOption) string {
	t.Helper()
	var doc kuddle.Document
	if err := kuddle.Unmarshal([]byte(in), &doc); err != nil {
		t.Fatal(err)
	}
	doc.Format(opts...)
	out, err := kuddle.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestFormatDefaults(t *testing.T) {
	for in, want := range map[string]string{
		"//x  \nn\n":                 "// x\nn\n",
		"//\nn\n":                    "//\nn\n",
		"/// doc\nn\n":               "/// doc\nn\n",
		"/*x*/\nn\n":                 "/* x */\nn\n",
		"/** doc */\nn\n":            "/** doc */\nn\n",
		"/*\n  a  \n*/\nn\n":         "/*\n   a\n*/\nn\n",
		"n /*x*/ 1\n":                "n /* x */ 1\n",
		"/-n\n":                      "/- n\n",
		"n /-1 2 //x\n":              "n /- 1 2 // x\n",
		"n \"a\" #\"b\"# 0x0_1 {}\n": "n \"a\" #\"b\"# 0x0_1 {}\n",
		"p {\n    /-c\n}\n":          "p {\n    /- c\n}\n",
	} {
		if got := format(t, in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestFormatOptions(t *testing.T) {
	const in = "n b=1 \"a\" a=0x0A_bc #\"r\"# {}\n"
	for _, tt := range []struct {
		name string
		opts []kuddle.FormatOption
		want string
	}{
		{"sort", []kuddle.FormatOption{kuddle.FormatSortProperties(true)}, "n \"a\" #\"r\"# a=0x0A_bc b=1 {}\n"},
		{"canonical", []kuddle.FormatOption{kuddle.FormatNumbers(kuddle.NumberCanonical)}, "n b=1 \"a\" a=0xabc #\"r\"# {}\n"},
		{"decimal", []kuddle.FormatOption{kuddle.FormatNumbers(kuddle.NumberDecimal)}, "n b=1 \"a\" a=2748 #\"r\"# {}\n"},
		{"omit", []kuddle.FormatOption{kuddle.FormatEmptyBlocks(kuddle.EmptyBlockOmitted)}, "n b=1 \"a\" a=0x0A_bc #\"r\"#\n"},
		{"quote", []kuddle.FormatOption{kuddle.FormatQuoteValues(true)}, "n b=1 \"a\" a=0x0A_bc \"r\" {}\n"},
		{"normalize", []kuddle.FormatOption{kuddle.FormatNormalizeStrings(true)}, "n b=1 a a=0x0A_bc r {}\n"},
	} {
		if got := format(t, in, tt.opts...); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestFormatSortKeepsSlashdashedBlockLast(t *testing.T) {
	const in, want = "n b=1 a=2 3 /- {}\n", "n 3 a=2 b=1 /- {}\n"
	if got := format(t, in, kuddle.FormatSortProperties(true)); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatReindentsBlocks(t *testing.T) {
	for _, tt := range []struct{ name, in, want string }{
		{"aligned text moves with its node",
			"p {\n        /* a\n           b */\n    n\n}\n",
			"p {\n    /* a\n       b */\n    n\n}\n"},
		{"relative indentation is kept",
			"p {\n  /*\n      a\n        b\n  */\n  n\n}\n",
			"p {\n    /*\n       a\n         b\n    */\n    n\n}\n"},
		{"stars stay aligned",
			"p {\n/*\n * a\n * b\n */\nn\n}\n",
			"p {\n    /*\n     * a\n     * b\n     */\n    n\n}\n"},
		{"blank lines are emptied",
			"/*\n   a\n  \n   b\n*/\nn\n",
			"/*\n   a\n\n   b\n*/\nn\n"},
	} {
		got := format(t, tt.in)
		if got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
		if again := format(t, got); again != got {
			t.Errorf("%s: formatting again gave %q, from %q", tt.name, again, got)
		}
	}
	// The helper marshals with the default indentation, for the node's lines.
	if got, want := format(t, "p {\n/*\n a\n*/\nn\n}\n", kuddle.FormatIndent("\t")), "p {\n    /*\n\t   a\n\t*/\n    n\n}\n"; got != want {
		t.Errorf("WithIndent: got %q, want %q", got, want)
	}
}

func TestFormatSlashdash(t *testing.T) {
	for _, tt := range []struct{ name, in, want string }{
		{"a node is laid out at its depth",
			"p {\n/-a   1 {\nb\n}\nn\n}\n",
			"p {\n    /- a 1 {\n        b\n    }\n    n\n}\n"},
		{"a children block of an entry",
			"p {\n    n /-{\nb\n}\n}\n",
			"p {\n    n /- {\n        b\n    }\n}\n"},
		{"a string's lines are left alone",
			"p {\n/-a \"\"\"\n  x\n  \"\"\"\nn\n}\n",
			"p {\n    /- a \"\"\"\n  x\n  \"\"\"\n    n\n}\n"},
		{"a part starting with a comment is left alone",
			"/- /*c*/ a\nn\n",
			"/- /*c*/ a\nn\n"},
		{"a part that would end in a comment is left alone",
			"/- a /*c*/;\nn\n",
			"/- a /*c*/;\nn\n"},
		{"comments inside are tidied",
			"/-a {\n//x\nb\n}\nn\n",
			"/- a {\n    // x\n    b\n}\nn\n"},
	} {
		got := format(t, tt.in)
		if got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
		if again := format(t, got); again != got {
			t.Errorf("%s: formatting again gave %q, from %q", tt.name, again, got)
		}
	}
}

func TestFormatCommentNewlines(t *testing.T) {
	crlf := kuddle.FormatNewline("\r\n")
	for in, want := range map[string]string{
		"/*\n   a\n*/\nn\n":              "/*\r\n   a\r\n*/\nn\n",
		"/-a {\nb\n}\nn\n":               "/- a {\r\n    b\r\n}\nn\n",
		"/-a \"\"\"\n  x\n  \"\"\"\nn\n": "/- a \"\"\"\n  x\n  \"\"\"\nn\n",
	} {
		got := format(t, in, crlf)
		if got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
		if again := format(t, got, crlf); again != got {
			t.Errorf("%q: formatting again gave %q, from %q", in, again, got)
		}
	}
}

func TestFormatSortFindsBlockBehindContinuation(t *testing.T) {
	const in, want = "n b=1 a=2 /-\\\n{}\n", "n a=2 b=1 /- \\\n{}\n"
	if got := format(t, in, kuddle.FormatSortProperties(true)); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatLiteralComments(t *testing.T) {
	const in = "//x  \n/*y*/\nn /-1\n"
	if got := format(t, in, kuddle.FormatLiteralComments(true)); got != in {
		t.Errorf("got %q, want %q", got, in)
	}
}

func TestFormatKeepsCommentOnlyBlock(t *testing.T) {
	const in = "n {\n    // kept\n}\n"
	if got := format(t, in, kuddle.FormatEmptyBlocks(kuddle.EmptyBlockOmitted)); got != in {
		t.Errorf("got %q, want %q", got, in)
	}
}

func TestFormatStringNewlines(t *testing.T) {
	const in = "n \"a\nb\"\n"
	v1 := kdltext.WithVersion(kdltext.V1)
	for _, tt := range []struct {
		name string
		opts []kuddle.FormatOption
		want string
	}{
		{"not by default", []kuddle.FormatOption{kuddle.FormatNewline("\r\n")}, in},
		{"on request", []kuddle.FormatOption{kuddle.FormatNewline("\r\n"), kuddle.FormatStringNewlines(true)}, "n \"a\r\nb\"\n"},
	} {
		var doc kuddle.Document
		if err := kuddle.Unmarshal([]byte(in), &doc, v1); err != nil {
			t.Fatal(err)
		}
		doc.Format(tt.opts...)
		if got, err := kuddle.Marshal(doc, v1); err != nil || string(got) != tt.want {
			t.Errorf("%s: got %q, %v; want %q", tt.name, got, err, tt.want)
		}
	}
}

// TestEncoderNewlineLeavesStrings checks that only the Encoder's own line
// endings change: a v1 string holding a newline would otherwise change value.
func TestEncoderNewlineLeavesStrings(t *testing.T) {
	v1 := kdltext.WithVersion(kdltext.V1)
	var doc kuddle.Document
	if err := kuddle.Unmarshal([]byte("n \"a\nb\"\n"), &doc, v1); err != nil {
		t.Fatal(err)
	}
	out, err := kuddle.Marshal(doc, v1, kdltext.WithNewline("\r\n"))
	if want := "n \"a\nb\"\r\n"; err != nil || string(out) != want {
		t.Errorf("got %q, %v; want %q", out, err, want)
	}
}

func TestEncoderNewline(t *testing.T) {
	out, err := kuddle.Marshal(map[string]int{"a": 1}, kdltext.WithNewline("\r\n"))
	if want := "a 1\r\n"; err != nil || string(out) != want {
		t.Errorf("got %q, %v; want %q", out, err, want)
	}
}
