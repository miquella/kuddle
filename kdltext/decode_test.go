package kdltext_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/miquella/kuddle/internal/kdltest"
	"github.com/miquella/kuddle/kdltext"
)

// readAll reads every token of a document, formatted for comparison.
func readAll(input string, opts ...kdltext.Options) ([]string, error) {
	return readTokens(kdltext.NewDecoder(strings.NewReader(input), opts...))
}

// readTokens reads every token from d, formatted for comparison.
func readTokens(d *kdltext.Decoder) ([]string, error) {
	var got []string
	var nodes []int // the indexes in got of the nodes not yet ended
	for {
		t, err := d.ReadToken()
		if err == io.EOF {
			return got, nil
		} else if err != nil {
			return got, err
		}
		switch t.Kind {
		case kdltext.KindNode:
			nodes = append(nodes, len(got))
		case kdltext.KindEnd:
			// A node with a children block is written with a "{", which its
			// KindEnd token reports.
			if n := len(nodes); n > 0 {
				if t.Children {
					got[nodes[n-1]] += " {"
				}
				nodes = nodes[:n-1]
			}
		}
		got = append(got, format(t))
	}
}

// format formats t, with its type annotation, if any, in parentheses.
func format(t kdltext.Token) string {
	typ := func(typ *string) string {
		if typ == nil {
			return ""
		}
		return "(" + *typ + ")"
	}
	val := func() string {
		return typ(t.Value.Type) + fmt.Sprintf("%s:%s", t.Value.Kind(), t.Value)
	}
	switch t.Kind {
	case kdltext.KindNode:
		return "node " + typ(t.Value.Type) + t.Name()
	case kdltext.KindValue:
		return "value " + val()
	case kdltext.KindProp:
		return "prop " + t.Name()
	}
	return t.Kind.String()
}

func TestReadToken(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", nil},
		{"entries", `(t)node 1 "two" k=(u8)0x10 #true #null`, []string{
			"node (t)node", "value number:1", "value string:two",
			"prop k", "value (u8)number:0x10", "value bool:#true", "value null:#null", "end",
		}},
		{"empty type annotations", `("")node ("")1 k=("")2`, []string{
			"node ()node", "value ()number:1", "prop k", "value ()number:2", "end",
		}},
		{"children", "a {\n  b; c { d }\n}\ne", []string{
			"node a {", "node b", "end", "node c {", "node d", "end", "end", "end",
			"node e", "end",
		}},
		{"empty children", "a {}\nb { }\nc", []string{
			"node a {", "end", "node b {", "end", "node c", "end",
		}},
		{"slashdash", "/-a 1\nb /-1 2 /-{ c } {\n /- d {}\n e\n} /- { f }", []string{
			"node b {", "value number:2", "node e", "end", "end",
		}},
		{"slashdashed children", "a /-{ b }", []string{"node a", "end"}},
		{"bare strings", "node arg key = value", []string{
			"node node", "value string:arg", "prop key", "value string:value", "end",
		}},
		{"escline", "node \\ // comment\n  1", []string{"node node", "value number:1", "end"}},
		{"duplicate props", "n a=1 a=2", []string{"node n", "prop a", "value number:1", "prop a", "value number:2", "end"}},
	}
	for _, tt := range tests {
		got, err := readAll(tt.input)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if fmt.Sprint(got) != fmt.Sprint(tt.want) {
			t.Errorf("%s:\ngot  %q\nwant %q", tt.name, got, tt.want)
		}
	}
}

func TestSyntaxErrors(t *testing.T) {
	tests := []struct {
		input   string
		version kdltext.Version
		want    string
	}{
		{"a {\n  b\n", 0, `line 3, column 1: unexpected end of input: missing '}' for the children of node "a"`},
		{"a }", 0, `line 1, column 3: unexpected '}'`},
		{"a 1=2", 0, `line 1, column 4: unexpected '='`},
		{`a "b"c`, 0, `line 1, column 6: expected whitespace before identifier "c"`},
		{"a {} 1", 0, `line 1, column 6: arguments and properties of node "a" must come before its children`},
		{"a {} {}", 0, `line 1, column 6: node "a" has more than one children block`},
		{"a (t)k=1", 0, `line 1, column 3: a property's type annotation must follow the '='`},
		{"a k=", 0, `line 1, column 5: expected a value, found end of input`},
		{"a ()1", 0, `line 1, column 4: expected a type name, found ')'`},
		{"a (t 1", 0, `line 1, column 6: expected ')' to end the type annotation, found number 1`},
		{"a /-", 0, `line 1, column 5: expected an argument, property or children after '/-', found end of input`},
		{"a \\ b", 0, `line 1, column 5: expected a newline after '\', found identifier "b"`},
		{"a b", kdltext.V1, `line 1, column 3: bare identifier "b" is not a value in KDL v1 (quote it)`},
		{"a k = 1", kdltext.V1, `line 1, column 5: whitespace before '=' is not allowed in KDL v1`},
		{"a\n  \"x\\q\"", 0, `line 2, column 5: invalid escape`},
		{"/- kdl-version 1\na", kdltext.V2, `line 1, column 1: document is marked as KDL v1, but reading it as KDL v2`},
	}
	for _, tt := range tests {
		var opts []kdltext.Options
		if tt.version != 0 {
			opts = append(opts, kdltext.WithVersion(tt.version))
		}
		_, err := readAll(tt.input, opts...)
		if _, ok := errors.AsType[*kdltext.SyntacticError](err); !ok {
			t.Errorf("%q: err = %v, want SyntacticError", tt.input, err)
			continue
		}
		if got := strings.TrimPrefix(err.Error(), "kdltext: syntax error at "); got != tt.want {
			t.Errorf("%q:\ngot  %s\nwant %s", tt.input, got, tt.want)
		}
	}

	_, err := readAll("/- kdl-version 1\n", kdltext.WithVersion(kdltext.V2))
	if !errors.Is(err, kdltext.ErrVersionMismatch) {
		t.Errorf("version mismatch: err = %v, want ErrVersionMismatch", err)
	}
}

// TestSlashdashSpace checks where whitespace is needed before a slash-dash:
// before an entry always, and before a children block only in v2.
func TestSlashdashSpace(t *testing.T) {
	tests := []struct {
		input   string
		version kdltext.Version
		ok      bool
	}{
		{`node/-{}`, kdltext.V1, true},
		{`node "a"/-{}`, kdltext.V1, true},
		{`node "a"/- {}`, kdltext.V1, true},
		{"node \"a\"/-\\\n{}", kdltext.V1, true},
		{`node "a"/-{}`, 0, true},
		{`node "a"/-"b"`, kdltext.V1, false},
		{`node "a"/-k=1`, kdltext.V1, false},
		{`node "a"/-{}`, kdltext.V2, true},
		{`node "a" /-{}`, kdltext.V2, true},
	}
	for _, tt := range tests {
		var opts []kdltext.Options
		if tt.version != 0 {
			opts = append(opts, kdltext.WithVersion(tt.version))
		}
		got, err := readAll(tt.input, opts...)
		if (err == nil) != tt.ok {
			t.Errorf("v%d %q: got %q, %v; want ok %v", tt.version, tt.input, got, err, tt.ok)
		}
	}
}

// TestEsclineEnd checks how a line continuation may end the input: in v1
// only after a line comment, and in v2 directly too.
func TestEsclineEnd(t *testing.T) {
	tests := []struct {
		input   string
		version kdltext.Version
		ok      bool
	}{
		{"node \\ // comment", kdltext.V1, true},
		{"node \\ // comment", kdltext.V2, true},
		{"node \\ // comment", 0, true},
		{"node \\", kdltext.V1, false},
		{"node \\ ", kdltext.V1, false},
		{"node \\", kdltext.V2, true},
	}
	for _, tt := range tests {
		var opts []kdltext.Options
		if tt.version != 0 {
			opts = append(opts, kdltext.WithVersion(tt.version))
		}
		got, err := readAll(tt.input, opts...)
		if (err == nil) != tt.ok {
			t.Errorf("v%d %q: got %q, %v; want ok %v", tt.version, tt.input, got, err, tt.ok)
		}
	}
}

func TestMaxTokenSize(t *testing.T) {
	limit := kdltext.WithMaxTokenSize(64)

	long := `a "` + strings.Repeat("x", 100) + `"`
	_, err := readAll(long, limit)
	se, ok := errors.AsType[*kdltext.SyntacticError](err)
	if !ok || !errors.Is(err, kdltext.ErrTokenTooLarge) {
		t.Fatalf("long token: err = %v, want SyntacticError wrapping ErrTokenTooLarge", err)
	}
	if se.Line != 1 || se.Column != 3 {
		t.Errorf("long token: error at %v, want line 1, column 3", se.Position)
	}

	// The limit is on a token, not the document.
	if _, err := readAll(strings.Repeat("node 1 \"short\"\n", 1000), limit); err != nil {
		t.Errorf("short tokens: %v", err)
	}
	if _, err := readAll(long, limit, kdltext.WithMaxTokenSize(0)); err != nil {
		t.Errorf("no limit: %v", err)
	}

	// A *bytes.Buffer is read in full, without a limit.
	d := kdltext.NewDecoder(bytes.NewBufferString(long), limit)
	for err = nil; err == nil; {
		_, err = d.ReadToken()
	}
	if err != io.EOF {
		t.Errorf("*bytes.Buffer: err = %v, want io.EOF", err)
	}

	defer func() {
		if recover() == nil {
			t.Error("WithMaxTokenSize(-1) did not panic")
		}
	}()
	kdltext.WithMaxTokenSize(-1)
}

func TestRejectMultipleChildrenBlocks(t *testing.T) {
	v1, strict := kdltext.WithVersion(kdltext.V1), kdltext.RejectMultipleChildrenBlocks(true)
	tests := []struct {
		input string
		opts  []kdltext.Options
		want  []string // nil if the input must fail
	}{
		{`node /-{ a } { b }`, []kdltext.Options{v1}, []string{"node node {", "node b", "end", "end"}},
		{`node { b } /-{ a }`, []kdltext.Options{v1}, []string{"node node {", "node b", "end", "end"}},
		{`node /-{} /-{}`, []kdltext.Options{v1}, []string{"node node", "end"}},
		{`node /-{ a } { b }`, []kdltext.Options{v1, strict}, nil},
		{`node { b } /-{ a }`, []kdltext.Options{v1, strict}, nil},
		{`node /-{} /-{}`, []kdltext.Options{v1, strict}, nil},
		{`node /-{ a }`, []kdltext.Options{v1, strict}, []string{"node node", "end"}},
		{`node { b }`, []kdltext.Options{v1, strict}, []string{"node node {", "node b", "end", "end"}},
		// It does not apply to v2, and takes an undecided version as v2.
		{`node /-{ a } { b }`, []kdltext.Options{kdltext.WithVersion(kdltext.V2), strict}, []string{"node node {", "node b", "end", "end"}},
		{`node /-{ a } { b }`, []kdltext.Options{strict}, []string{"node node {", "node b", "end", "end"}},
		// Two blocks that are not slash-dashed are never allowed.
		{`node { a } { b }`, []kdltext.Options{v1}, nil},
	}
	for _, tt := range tests {
		got, err := readAll(tt.input, tt.opts...)
		switch {
		case tt.want == nil && err == nil:
			t.Errorf("%q: read %q, want error", tt.input, got)
		case tt.want != nil && err != nil:
			t.Errorf("%q: %v", tt.input, err)
		case tt.want != nil && fmt.Sprint(got) != fmt.Sprint(tt.want):
			t.Errorf("%q:\ngot  %q\nwant %q", tt.input, got, tt.want)
		}
	}
}

func TestDecoderRejectDuplicateProperties(t *testing.T) {
	reject := kdltext.RejectDuplicateProperties(true)
	tests := []struct {
		input string
		opts  []kdltext.Options
		want  string // the error, or "" if the input must be read
	}{
		{`n a=1 b=2 a=3`, nil, ""},
		{`n a=1 b=2 a=3`, []kdltext.Options{reject}, `line 1, column 11: node "n" has more than one property "a"`},
		{`n a=1 "a"=2`, []kdltext.Options{reject}, `line 1, column 7: node "n" has more than one property "a"`},
		{`n a=1 a=2`, []kdltext.Options{kdltext.WithVersion(kdltext.V1), reject}, `line 1, column 7: node "n" has more than one property "a"`},
		{"n {\n  m a=1 a=2\n}", []kdltext.Options{reject}, `line 2, column 9: node "m" has more than one property "a"`},
		// Slash-dashed properties and those of other nodes are not counted.
		{`n a=1 /-a=2`, []kdltext.Options{reject}, ""},
		{`n /-a=1 a=2`, []kdltext.Options{reject}, ""},
		{`n a=1 /-{ m a=1 a=2 }`, []kdltext.Options{reject}, ""},
		{`n a=1 { m a=1 }; o a=1`, []kdltext.Options{reject}, ""},
		{`/-n a=1 a=2`, []kdltext.Options{reject}, ""},
	}
	for _, tt := range tests {
		_, err := readAll(tt.input, tt.opts...)
		if tt.want == "" {
			if err != nil {
				t.Errorf("%q: %v", tt.input, err)
			}
			continue
		}
		if _, ok := errors.AsType[*kdltext.SyntacticError](err); !ok {
			t.Errorf("%q: err = %v, want SyntacticError", tt.input, err)
			continue
		}
		if got := strings.TrimPrefix(err.Error(), "kdltext: syntax error at "); got != tt.want {
			t.Errorf("%q:\ngot  %s\nwant %s", tt.input, got, tt.want)
		}
	}
}

func TestDecoderState(t *testing.T) {
	d := kdltext.NewDecoder(strings.NewReader("a {\n  b 1\n}"))
	var got []string
	for {
		k := d.PeekKind()
		peeked, perr := d.PeekToken()
		tok, err := d.ReadToken()
		if perr != err || format(peeked) != format(tok) {
			t.Errorf("PeekToken() = %s, %v, then read %s, %v", format(peeked), perr, format(tok), err)
		}
		if err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if k != tok.Kind {
			t.Errorf("PeekKind() = %v, then read %v", k, tok.Kind)
		}
		got = append(got, fmt.Sprintf("%v@%d:%d/%d", tok.Kind, d.Pos().Line, d.Pos().Column, d.StackDepth()))
	}
	want := "[node@1:1/1 node@2:3/2 value@2:5/2 end@2:6/1 end@3:1/0]"
	if fmt.Sprint(got) != want {
		t.Errorf("got  %v\nwant %v", got, want)
	}
	if d.PeekKind() != kdltext.KindInvalid {
		t.Error("PeekKind() at EOF is not KindInvalid")
	}
}

func TestDecoderStackIndex(t *testing.T) {
	d := kdltext.NewDecoder(strings.NewReader("a 1 k=2 {\n    b\n}\nc\n"))
	state := func() string {
		s := fmt.Sprint(d.StackDepth())
		for i := range d.StackDepth() + 1 {
			k, n := d.StackIndex(i)
			s += fmt.Sprintf(" %v:%d", k, n)
		}
		return s
	}
	if got := state(); got != "0 invalid:0" {
		t.Errorf("before reading: got %s, want 0 invalid:0", got)
	}
	for _, want := range []string{
		"1 node:1 invalid:0",
		"1 node:1 value:1",
		"1 node:1 property:2",
		"1 node:1 value:3",
		"2 node:1 node:4 invalid:0",
		"1 node:1 node:4",
		"0 node:1",
		"1 node:2 invalid:0",
	} {
		tok, err := d.ReadToken()
		if err != nil {
			t.Fatal(err)
		}
		// Peeking, which parses ahead, does not change the stack.
		d.PeekKind()
		if got := state(); got != want {
			t.Errorf("after %v: got %s, want %s", tok.Kind, got, want)
		}
	}
}

// TestInputOffset checks the position and input offset of each token: a
// property's value is a token of its own, which starts after the '='.
func TestInputOffset(t *testing.T) {
	input := "a 1 k = (t)2\nb {\n  c \"x\"\n}\nd /-e\nf 1 "
	d := kdltext.NewDecoder(strings.NewReader(input))
	if d.InputOffset() != 0 {
		t.Errorf("InputOffset() = %d before reading", d.InputOffset())
	}
	var got []string
	for {
		tok, err := d.ReadToken()
		if err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		got = append(got, fmt.Sprintf("%v@%d:%d-%d", tok.Kind, d.Pos().Line, d.Pos().Column, d.InputOffset()))
	}
	want := []string{
		"node@1:1-1", "value@1:3-3", "property@1:5-5", "value@1:9-12", "end@1:13-13",
		"node@2:1-14", "node@3:3-20", "value@3:5-24", "end@3:8-25", "end@4:1-27",
		"node@5:1-28", "end@5:6-33", "node@6:1-34", "value@6:3-36", "end@6:5-36",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// TestDecodeConformance checks that the decoder accepts the valid inputs of
// the KDL conformance tests, both with the suite's version given and
// detected, and rejects the invalid ones. Invalid inputs are only checked with
// the version given, since many are valid in the other version. Cases that
// contradict the spec are skipped.
func TestDecodeConformance(t *testing.T) {
	for _, v := range []kdltext.Version{kdltext.V1, kdltext.V2} {
		cases, err := kdltest.Cases(int(v))
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			t.Run(fmt.Sprintf("v%d/%s", v, c.Name), func(t *testing.T) {
				if c.Divergence != "" {
					t.Skip(c.Divergence)
				}
				for _, detect := range []bool{false, true} {
					if detect && c.Fail {
						continue
					}
					var opts []kdltext.Options
					if !detect {
						opts = append(opts, kdltext.WithVersion(v))
					}
					d := kdltext.NewDecoder(bytes.NewReader(c.Input), opts...)
					err := drain(d)
					how := "version given"
					if detect {
						how = "version detected"
					}
					switch {
					case c.Fail && err == nil:
						t.Errorf("%s: parsed, want error", how)
					case !c.Fail && err != nil:
						t.Errorf("%s: %v", how, err)
					case detect && d.Version() != v && d.Version() != 0:
						t.Errorf("%s: detected v%d", how, d.Version())
					}
				}
			})
		}
	}
}

// drain reads all of d's tokens, returning any error other than io.EOF.
func drain(d *kdltext.Decoder) error {
	for {
		if _, err := d.ReadToken(); err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
	}
}

// TestOneByteReader checks that a document read a byte at a time decodes the
// same as one read whole.
func TestOneByteReader(t *testing.T) {
	input := "a 1 \"two\" {\n  b k=#\"raw\"#; c\n}\n"
	want, err := readAll(input)
	if err != nil {
		t.Fatal(err)
	}
	got, err := readTokens(kdltext.NewDecoder(iotest.OneByteReader(strings.NewReader(input))))
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// TestNestingDepth checks that a Decoder reads children blocks nested up to
// 10000 deep, slash-dashed or not, and reports a syntax error at the block
// past that, without running out of stack however deep the input is.
func TestNestingDepth(t *testing.T) {
	const limit = 10000
	nested := func(prefix string, n int) string {
		return prefix + strings.Repeat("a{", n) + strings.Repeat("}", n) + "\n"
	}
	for _, prefix := range []string{"", "/- "} {
		if _, err := readAll(nested(prefix, limit)); err != nil {
			t.Errorf("%q, %d deep: %v", prefix, limit, err)
		}
		for _, n := range []int{limit + 1, 5_000_000} {
			_, err := readAll(nested(prefix, n))
			se, ok := errors.AsType[*kdltext.SyntacticError](err)
			switch {
			case !ok || !strings.Contains(err.Error(), "children blocks nested deeper than 10000"):
				t.Errorf("%q, %d deep: err = %v", prefix, n, err)
			case se.Line != 1 || se.Column != len(prefix)+2*(limit+1):
				t.Errorf("%q, %d deep: error at %v, want the '{' at column %d", prefix, n, se.Position, len(prefix)+2*(limit+1))
			}
		}
	}
}

// TestReadTokenStreams checks that the tokens of a node are returned as they
// are parsed, before the rest of the node is read.
func TestReadTokenStreams(t *testing.T) {
	boom := errors.New("boom")
	d := kdltext.NewDecoder(io.MultiReader(strings.NewReader("node 1 "), iotest.ErrReader(boom)))
	for _, want := range []kdltext.Kind{kdltext.KindNode, kdltext.KindValue} {
		if tok, err := d.ReadToken(); err != nil || tok.Kind != want {
			t.Fatalf("ReadToken() = %v, %v, want %v", tok.Kind, err, want)
		}
	}
	if _, err := d.ReadToken(); !errors.Is(err, boom) {
		t.Errorf("ReadToken() error = %v, want %v", err, boom)
	}
}

// TestChildrenOnEnd checks that a node's KindEnd token, not its KindNode
// token, reports whether it has a children block.
func TestChildrenOnEnd(t *testing.T) {
	d := kdltext.NewDecoder(strings.NewReader("a {}\nb\nc { d; }\n"))
	var got []string
	for {
		tok, err := d.ReadToken()
		if err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		got = append(got, fmt.Sprintf("%v:%t", tok.Kind, tok.Children))
	}
	want := []string{
		"node:false", "end:true",
		"node:false", "end:false",
		"node:false", "node:false", "end:false", "end:true",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestDecoderOptions(t *testing.T) {
	d := kdltext.NewDecoder(strings.NewReader(""), kdltext.WithVersion(kdltext.V1))
	// A Decoder created with the options of another reads as it does.
	d = kdltext.NewDecoder(strings.NewReader("node #true\n"), d.Options())
	var err error
	for err == nil {
		_, err = d.ReadToken()
	}
	if err == io.EOF {
		t.Fatal("v2 syntax read with the options of a v1 Decoder")
	}
}

// TestLooseSpacing checks the places where whitespace may be left out.
func TestLooseSpacing(t *testing.T) {
	tests := []struct {
		input   string
		version kdltext.Version
		want    []string // nil if the input must fail
	}{
		{`node "string"/-foo=1`, kdltext.V2, []string{"node node", "value string:string", "end"}},
		{`node "string"/-{}`, kdltext.V2, []string{"node node", "value string:string", "end"}},
		{`node "string" {}/-{}`, kdltext.V2, []string{"node node {", "value string:string", "end"}},
		{`foo123{bar}`, kdltext.V2, []string{"node foo123 {", "node bar", "end", "end"}},
		{`node "a"/-"b" "c"`, kdltext.V2, []string{"node node", "value string:a", "value string:c", "end"}},
		{`node "a"/-"b""c"`, kdltext.V2, nil},
		{`node "a""b"`, kdltext.V2, nil},
		// Detection takes the loose slash-dash as v2.
		{`node "a"/-"b"`, 0, []string{"node node", "value string:a", "end"}},
		// It does not apply to v1.
		{`node "a"/-"b"`, kdltext.V1, nil},
	}
	for _, tt := range tests {
		var opts []kdltext.Options
		if tt.version != 0 {
			opts = append(opts, kdltext.WithVersion(tt.version))
		}
		got, err := readAll(tt.input, opts...)
		switch {
		case tt.want == nil && err == nil:
			t.Errorf("v%d %q: read %q, want error", tt.version, tt.input, got)
		case tt.want != nil && err != nil:
			t.Errorf("v%d %q: %v", tt.version, tt.input, err)
		case tt.want != nil && fmt.Sprint(got) != fmt.Sprint(tt.want):
			t.Errorf("v%d %q:\ngot  %q\nwant %q", tt.version, tt.input, got, tt.want)
		}
	}
}
