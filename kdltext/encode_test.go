package kdltext_test

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/miquella/kuddle/kdltext"
)

func encode(toks []kdltext.Token, opts ...kdltext.Options) (string, error) {
	var b strings.Builder
	e := kdltext.NewEncoder(&b, opts...)
	for _, t := range toks {
		if err := e.WriteToken(t); err != nil {
			return b.String(), err
		}
	}
	return b.String(), nil
}

var (
	end      = kdltext.Token{Kind: kdltext.KindEnd}
	endBlock = kdltext.Token{Kind: kdltext.KindEnd, Children: true} // the end of a node with a children block
)

func node(name string) kdltext.Token {
	return kdltext.Token{Kind: kdltext.KindNode, Value: kdltext.String(name)}
}
func val(v kdltext.Value) kdltext.Token {
	return kdltext.Token{Kind: kdltext.KindValue, Value: v}
}
func prop(name string) kdltext.Token {
	return kdltext.Token{Kind: kdltext.KindProp, Value: kdltext.String(name)}
}

func TestEncoder(t *testing.T) {
	typed := kdltext.Int(1)
	typed.Type = new("u8")
	toks := []kdltext.Token{
		{Kind: kdltext.KindNode, Value: kdltext.String("a").WithType(new("my type"))},
		val(kdltext.String("bare")), val(kdltext.String("not bare")), prop("k"), val(typed),
		val(kdltext.Bool(true)), val(kdltext.Null()),
		node("b"), val(kdltext.String("x\n\"/\u0085\u00e9")),
		node("c"), end,
		end,
		node("d"), end,
		end,
		node("true"), prop(""), val(kdltext.String("1")), end,
		{Kind: kdltext.KindNode, Value: kdltext.String("e").WithType(new(""))}, endBlock,
	}
	got, err := encode(toks)
	if err != nil {
		t.Fatal(err)
	}
	want := `("my type")a bare "not bare" k=(u8)1 #true #null {
    b "x\n\"/\u{85}é" {
        c
    }
    d
}
"true" ""="1"
("")e {}
`
	if got != want {
		t.Errorf("v2:\ngot\n%s\nwant\n%s", got, want)
	}

	got, err = encode(toks, kdltext.WithVersion(kdltext.V1), kdltext.WithIndent("\t"))
	if err != nil {
		t.Fatal(err)
	}
	want = `("my type")a "bare" "not bare" k=(u8)1 true null {
	b "x\n\"/\u{85}é" {
		c
	}
	d
}
"true" ""="1"
("")e {}
`
	if got != want {
		t.Errorf("v1:\ngot\n%s\nwant\n%s", got, want)
	}
}

func TestEncoderOutputOffset(t *testing.T) {
	var b strings.Builder
	e := kdltext.NewEncoder(&b)
	for i, tt := range []struct {
		tok  kdltext.Token
		want int64
	}{
		{node("a"), 1},
		{val(kdltext.Int(1)), 3},
		{end, 4},
		{node("bc"), 6},
	} {
		if err := e.WriteToken(tt.tok); err != nil {
			t.Fatal(err)
		}
		if got := e.OutputOffset(); got != tt.want {
			t.Errorf("token %d: OutputOffset() = %d, want %d", i, got, tt.want)
		}
	}
	if b.Len() != 4 {
		t.Errorf("wrote %q, want the first node only", b.String())
	}
}

func TestEncoderStackIndex(t *testing.T) {
	var b strings.Builder
	e := kdltext.NewEncoder(&b)
	state := func() string {
		s := fmt.Sprint(e.StackDepth())
		for i := range e.StackDepth() + 1 {
			k, n := e.StackIndex(i)
			s += fmt.Sprintf(" %v:%d", k, n)
		}
		return s
	}
	for _, tt := range []struct {
		tok  kdltext.Token
		want string
	}{
		{node("a"), "1 node:1 invalid:0"},
		{val(kdltext.Int(1)), "1 node:1 value:1"},
		{prop("k"), "1 node:1 property:2"},
		{val(kdltext.Int(2)), "1 node:1 value:3"},
		{node("b"), "2 node:1 node:4 invalid:0"},
		{end, "1 node:1 node:4"},
		{end, "0 node:1"},
		{node("c"), "1 node:2 invalid:0"},
	} {
		if err := e.WriteToken(tt.tok); err != nil {
			t.Fatal(err)
		}
		if got := state(); got != tt.want {
			t.Errorf("after %v: got %s, want %s", tt.tok.Kind, got, tt.want)
		}
	}
}

func TestEncoderRejectDuplicateProperties(t *testing.T) {
	reject := kdltext.RejectDuplicateProperties(true)
	tests := []struct {
		toks []kdltext.Token
		opts []kdltext.Options
		want string // the output, or the error if wantErr
		err  bool
	}{
		{
			[]kdltext.Token{node("n"), prop("a"), val(kdltext.Int(1)), prop("a"), val(kdltext.Int(2)), end},
			nil, "n a=1 a=2\n", false,
		},
		{
			[]kdltext.Token{node("n"), prop("a"), val(kdltext.Int(1)), prop("a"), val(kdltext.Int(2)), end},
			[]kdltext.Options{reject}, `kdltext: node has more than one property "a"`, true,
		},
		// The properties of other nodes are not counted.
		{
			[]kdltext.Token{
				node("n"), prop("a"), val(kdltext.Int(1)),
				node("m"), prop("a"), val(kdltext.Int(2)), end,
				end,
				node("o"), prop("a"), val(kdltext.Int(3)), end,
			},
			[]kdltext.Options{reject}, "n a=1 {\n    m a=2\n}\no a=3\n", false,
		},
	}
	for _, tt := range tests {
		got, err := encode(tt.toks, tt.opts...)
		switch {
		case tt.err && err == nil:
			t.Errorf("got %q, want error %q", got, tt.want)
		case tt.err && err.Error() != tt.want:
			t.Errorf("error %q, want %q", err, tt.want)
		case !tt.err && err != nil:
			t.Errorf("unexpected error: %v", err)
		case !tt.err && got != tt.want:
			t.Errorf("got %q, want %q", got, tt.want)
		}
	}
}

func TestEncoderQuoteIdentifiers(t *testing.T) {
	typed := kdltext.String("c")
	typed.Type = new("t")
	n := node("n")
	n.Value.Type = new("u")
	toks := []kdltext.Token{n, val(kdltext.String("a")), prop("p"), val(typed), end}
	tests := []struct {
		opts []kdltext.Options
		want string
	}{
		{nil, `(u)n a p=(t)c`},
		{[]kdltext.Options{kdltext.QuoteIdentifiers(true)}, `("u")"n" a "p"=("t")c`},
		{[]kdltext.Options{kdltext.WithVersion(kdltext.V1), kdltext.QuoteIdentifiers(true)}, `("u")"n" "a" "p"=("t")"c"`},
	}
	for _, tt := range tests {
		got, err := encode(toks, tt.opts...)
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want+"\n" {
			t.Errorf("got %q, want %q", got, tt.want)
		}
	}
}

func TestEncoderComments(t *testing.T) {
	line := func(text string) kdltext.Comment { return kdltext.Comment{Kind: kdltext.CommentLine, Text: text} }
	block := kdltext.Comment{Kind: kdltext.CommentBlock, Text: " x "}
	toks := []kdltext.Token{
		{Kind: kdltext.KindNode, Value: kdltext.String("a"), Comment: line(" a")},
		val(kdltext.Int(1)),
		{Kind: kdltext.KindEntryComment, Comment: block},
		{Kind: kdltext.KindComment, Comment: line(" only a comment")},
		endBlock,
		{Kind: kdltext.KindComment, Comment: line(" b")},
		node("b"), end,
	}
	tests := []struct {
		opts []kdltext.Options
		want string
	}{
		{nil, "// a\na 1 /* x */ {\n    // only a comment\n}\n// b\n\nb\n"},
		{[]kdltext.Options{kdltext.DiscardComments(true)}, "a 1 {}\nb\n"},
	}
	for _, tt := range tests {
		got, err := encode(toks, tt.opts...)
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Errorf("got %q, want %q", got, tt.want)
		}
	}
}

func TestEncoderIdentifiers(t *testing.T) {
	tests := []struct {
		name   string
		v1, v2 string
	}{
		{"foo-bar_9", "foo-bar_9", "foo-bar_9"},
		{"-", "-", "-"},
		{"0a", `"0a"`, `"0a"`},
		{".5", ".5", `".5"`},
		{"a#b", "a#b", `"a#b"`},
		{"a,b", `"a,b"`, "a,b"},
		{"inf", `"inf"`, `"inf"`},
		{"a b", `"a b"`, `"a b"`},
		{"😁", "😁", "😁"},
	}
	for _, tt := range tests {
		for v, want := range map[kdltext.Version]string{kdltext.V1: tt.v1, kdltext.V2: tt.v2} {
			got, err := encode([]kdltext.Token{node(tt.name), end}, kdltext.WithVersion(v))
			if err != nil || got != want+"\n" {
				t.Errorf("v%d %q: got %q, %v; want %q", v, tt.name, got, err, want)
			}
		}
	}
}

func TestEncoderErrors(t *testing.T) {
	tests := []struct {
		toks []kdltext.Token
		opts []kdltext.Options
		want string
	}{
		{[]kdltext.Token{val(kdltext.Int(1))}, nil, "kdltext: value outside of a node"},
		{[]kdltext.Token{end}, nil, "kdltext: end outside of a node"},
		{[]kdltext.Token{node("a"), node("b"), end, val(kdltext.Int(1))}, nil, "kdltext: value after the node's children"},
		{[]kdltext.Token{node("a"), val(kdltext.Value{})}, nil, "kdltext: invalid value"},
		{[]kdltext.Token{prop("k")}, nil, "kdltext: property outside of a node"},
		{[]kdltext.Token{node("a"), prop("k"), end}, nil, `kdltext: end after property "k", before its value`},
		{[]kdltext.Token{node("a"), prop("k"), prop("j")}, nil, `kdltext: property after property "k", before its value`},
		{[]kdltext.Token{node("a"), prop("k"), node("b")}, nil, `kdltext: node after property "k", before its value`},
		{[]kdltext.Token{node("a"), prop("k"), val(kdltext.Value{})}, nil, "kdltext: invalid value"},
		{[]kdltext.Token{{}}, nil, "kdltext: invalid token kind invalid"},
		{[]kdltext.Token{node("a"), val(kdltext.Float(math.NaN()))}, []kdltext.Options{kdltext.WithVersion(kdltext.V1)},
			"kdltext: #nan can't be written in KDL v1"},
		{[]kdltext.Token{node("a\xffb")}, nil, "kdltext: invalid UTF-8 in \"a\ufffdb\""},
		{[]kdltext.Token{{Kind: kdltext.KindNode, Value: kdltext.String("a").WithType(new("t\xff"))}}, nil, "kdltext: invalid UTF-8 in \"t\ufffd\""},
		{[]kdltext.Token{node("a"), prop("k\xff"), val(kdltext.Int(1))}, nil, "kdltext: invalid UTF-8 in \"k\ufffd\""},
		{[]kdltext.Token{node("a"), val(kdltext.String("\xff"))}, nil, "kdltext: invalid UTF-8 in \"\ufffd\""},
	}
	for _, tt := range tests {
		if _, err := encode(tt.toks, tt.opts...); err == nil || err.Error() != tt.want {
			t.Errorf("got %v, want %s", err, tt.want)
		}
	}
}

// TestEncodeDecode checks that encoded documents decode to the same tokens.
func TestEncodeDecode(t *testing.T) {
	inputs := map[kdltext.Version]string{
		kdltext.V1: "(t)a 1 \"x y\" k=r#\"raw\\\"# {\n  b 0xff 1e10; (\"\")c {}\n}\n\"\\u{feff}\" \"\\u{85}\"\n",
		kdltext.V2: "(t)a 1 \"x y\" k=#\"raw\\\"# {\n  b 0xff #-inf; (\"\")c {}\n}\n\"\\u{feff}\" \"\\u{85}\"\n",
	}
	for v, input := range inputs {
		want, err := readAll(input, kdltext.WithVersion(v))
		if err != nil {
			t.Fatal(err)
		}
		var toks []kdltext.Token
		d := kdltext.NewDecoder(strings.NewReader(input), kdltext.WithVersion(v))
		for {
			tok, err := d.ReadToken()
			if err != nil {
				break
			}
			toks = append(toks, tok)
		}
		out, err := encode(toks, kdltext.WithVersion(v))
		if err != nil {
			t.Fatal(err)
		}
		got, err := readAll(out, kdltext.WithVersion(v))
		if err != nil {
			t.Fatalf("v%d: %v\n%s", v, err, out)
		}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("v%d:\ngot  %q\nwant %q", v, got, want)
		}
	}
}

// TestEncoderNestingDepth checks that an Encoder writes children blocks
// nested up to 10000 deep, as a Decoder reads them, and reports an error for
// a block nested deeper, whether it has nodes or is empty.
func TestEncoderNestingDepth(t *testing.T) {
	const limit = 10000
	// chain writes nodes nested in each other, the innermost with an empty
	// children block if block is set, without indentation, which would be
	// quadratic in the depth.
	chain := func(nodes int, block bool) (string, error) {
		var b strings.Builder
		e := kdltext.NewEncoder(&b, kdltext.WithIndent(""))
		for range nodes {
			if err := e.WriteToken(node("a")); err != nil {
				return "", err
			}
		}
		for i := range nodes {
			tok := endBlock
			if i == 0 && !block {
				tok = end
			}
			if err := e.WriteToken(tok); err != nil {
				return "", err
			}
		}
		return b.String(), nil
	}
	for _, tt := range []struct {
		nodes int
		block bool
		ok    bool
	}{
		{limit, true, true},
		{limit + 1, false, true},
		{limit + 1, true, false},
		{limit + 2, false, false},
	} {
		out, err := chain(tt.nodes, tt.block)
		if tt.ok != (err == nil) {
			t.Errorf("%d nodes, block %t: err = %v", tt.nodes, tt.block, err)
			continue
		}
		if err != nil {
			if !strings.Contains(err.Error(), "children blocks nested deeper than 10000") {
				t.Errorf("%d nodes, block %t: err = %v", tt.nodes, tt.block, err)
			}
			continue
		}
		if _, err := readAll(out); err != nil {
			t.Errorf("%d nodes, block %t: reading the output: %v", tt.nodes, tt.block, err)
		}
	}
}

func TestEncoderOptions(t *testing.T) {
	e := kdltext.NewEncoder(new(strings.Builder), kdltext.WithIndent("\t"))
	// An Encoder created with the options of another writes as it does.
	var b strings.Builder
	e = kdltext.NewEncoder(&b, e.Options())
	for _, tok := range []kdltext.Token{
		{Kind: kdltext.KindNode, Value: kdltext.String("a")},
		{Kind: kdltext.KindNode, Value: kdltext.String("b")},
		{Kind: kdltext.KindEnd},
		{Kind: kdltext.KindEnd},
	} {
		if err := e.WriteToken(tok); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := b.String(), "a {\n\tb\n}\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
