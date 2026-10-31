package kuddle_test

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/miquella/kuddle"
	"github.com/miquella/kuddle/kdltext"
)

func num(s string) kdltext.Value {
	v, err := kdltext.Number(s)
	if err != nil {
		panic(err)
	}
	return v
}

func typed(typ string, v kdltext.Value) kdltext.Value {
	v.Type = new(typ)
	return v
}

func TestDocumentUnmarshalKDL(t *testing.T) {
	d := kdltext.NewDecoder(strings.NewReader("a {\n    b 1 k=(u8)2\n    c {}\n}\n(t)d\n"))
	if _, err := d.ReadToken(); err != nil { // a
		t.Fatal(err)
	}
	var children kuddle.Document
	if err := children.UnmarshalKDL(d); err != nil {
		t.Fatal(err)
	}
	want := kuddle.Document{Nodes: []kuddle.Node{
		{Name: "b", Entries: []kuddle.Entry{kuddle.Arg(num("1")), kuddle.Prop("k", typed("u8", num("2")))}},
		{Name: "c", Children: []kuddle.Node{}},
	}}
	if !children.Equal(&want) {
		t.Errorf("children: got %+v, want %+v", children, want)
	}
	if tok, err := d.ReadToken(); err != nil || tok.Kind != kdltext.KindEnd {
		t.Fatalf("ReadToken() = %v, %v; want the end of a", tok, err)
	}

	rest := children
	if err := rest.UnmarshalKDL(d); err != nil {
		t.Fatal(err)
	}
	want = kuddle.Document{Nodes: []kuddle.Node{{Type: new("t"), Name: "d"}}}
	if !rest.Equal(&want) {
		t.Errorf("rest: got %+v, want %+v", rest, want)
	}

	d = kdltext.NewDecoder(strings.NewReader("a 1 {\n    b\n}\n"))
	if _, err := d.ReadToken(); err != nil {
		t.Fatal(err)
	}
	if err := new(kuddle.Document).UnmarshalKDL(d); err == nil {
		t.Error("the arguments of a node: got no error")
	}
}

func TestUnmarshalDocument(t *testing.T) {
	input := `
// A comment.
(t)a 1 k=(u8)2 "x" k=3 {
	b #true; c
	/- d
}
e {}
("")f ("")1
`
	want := kuddle.Document{Nodes: []kuddle.Node{
		{Type: new("t"), Name: "a", Entries: []kuddle.Entry{
			kuddle.Arg(num("1")),
			kuddle.Prop("k", typed("u8", num("2"))),
			kuddle.Arg(kdltext.String("x")),
			kuddle.Prop("k", num("3")),
		}, Children: []kuddle.Node{
			{Name: "b", Entries: []kuddle.Entry{kuddle.Arg(kdltext.Bool(true))}},
			{Name: "c"},
			{Kind: kuddle.NodeComment, Comment: kdltext.Comment{Kind: kdltext.CommentSlashdash, Text: " d"}},
		}, Comment: kdltext.Comment{Kind: kdltext.CommentLine, Text: " A comment."}},
		{Name: "e", Children: []kuddle.Node{}},
		{Type: new(""), Name: "f", Entries: []kuddle.Entry{kuddle.Arg(typed("", num("1")))}},
	}}
	var got kuddle.Document
	if err := kuddle.Unmarshal([]byte(input), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Equal(&want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}

	var v struct {
		A kuddle.Document `kdl:"a"`
	}
	err := kuddle.Unmarshal([]byte(input), &v)
	if want := "kuddle: cannot unmarshal node a (line 3, column 1) into Go kuddle.Document: a Document holds only nodes, not arguments or properties"; err == nil || err.Error() != want {
		t.Errorf("a node with arguments:\ngot  %v\nwant %s", err, want)
	}
}

func TestUnmarshalSyntaxError(t *testing.T) {
	var doc kuddle.Document
	err := kuddle.Unmarshal([]byte("a {\n  b = 1\n}"), &doc)
	if se, ok := errors.AsType[*kdltext.SyntacticError](err); !ok || se.Line != 2 {
		t.Errorf("err = %v, want a SyntacticError on line 2", err)
	}
}

func TestUnmarshalDecodeChildren(t *testing.T) {
	dec := kdltext.NewDecoder(strings.NewReader("a { b; c }\nd"))
	if tok, err := dec.ReadToken(); err != nil || tok.Name() != "a" {
		t.Fatalf("ReadToken() = %v, %v", tok, err)
	}
	var doc kuddle.Document
	if err := kuddle.UnmarshalDecode(dec, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Nodes) != 2 || doc.Nodes[1].Name != "c" {
		t.Errorf("got %+v, want nodes b and c", doc)
	}
	for _, want := range []kdltext.Kind{kdltext.KindEnd, kdltext.KindNode} {
		if tok, err := dec.ReadToken(); err != nil || tok.Kind != want {
			t.Errorf("ReadToken() = %v, %v; want %v", tok, err, want)
		}
	}
}

func TestDocumentComments(t *testing.T) {
	line := func(s string) kdltext.Comment { return kdltext.Comment{Kind: kdltext.CommentLine, Text: s} }
	const input = `// The first node.
a 1 key=#true {
    b x // after b
    // the end of a's children
}
c
// the end of the document
`
	var doc kuddle.Document
	if err := kuddle.Unmarshal([]byte(input), &doc); err != nil {
		t.Fatal(err)
	}
	want := kuddle.Document{Nodes: []kuddle.Node{
		{
			Name:    "a",
			Entries: []kuddle.Entry{kuddle.Arg(num("1")), kuddle.Prop("key", kdltext.Bool(true))},
			Children: []kuddle.Node{
				{Name: "b", Entries: []kuddle.Entry{
					kuddle.Arg(kdltext.String("x")),
					{Kind: kuddle.EntryComment, Comment: line(" after b")},
				}},
				{Kind: kuddle.NodeComment, Comment: line(" the end of a's children")},
			},
			Comment: line(" The first node."),
		},
		{Name: "c"},
		{Kind: kuddle.NodeComment, Comment: line(" the end of the document")},
	}}
	if !doc.Equal(&want) {
		t.Fatalf("got %+v\nwant %+v", doc, want)
	}
	out, err := kuddle.Marshal(&doc)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != input {
		t.Errorf("written: got\n%s\nwant\n%s", out, input)
	}

	// Within a node, the comment entries after its children are discarded.
	dec := kdltext.NewDecoder(strings.NewReader("a {\n    b\n} /- {\n    c\n}\n"))
	dec.ReadToken()
	var children kuddle.Document
	if err := children.UnmarshalKDL(dec); err != nil {
		t.Fatal(err)
	}
	if len(children.Nodes) != 1 || children.Nodes[0].Name != "b" {
		t.Errorf("children: got %+v, want b", children)
	}
	if tok, err := dec.ReadToken(); err != nil || tok.Kind != kdltext.KindEnd {
		t.Errorf("ReadToken() = %v, %v; want the end of a", tok, err)
	}
}

// A Document can be changed and written back out, keeping its comments,
// though not all of its blank lines.
func ExampleDocument() {
	const input = `// The server to listen on.
server port=8080

// Unused.

// The users allowed.
users alice bob
`
	var doc kuddle.Document
	if err := kuddle.Unmarshal([]byte(input), &doc); err != nil {
		panic(err)
	}
	users := &doc.Nodes[2]
	fmt.Printf("%s: %q\n", users.Name, users.Comment.Text)
	users.Entries = append(users.Entries, kuddle.Arg(kdltext.String("carol")))
	users.Comment.Text = " The users allowed,\n and carol."

	if err := kuddle.MarshalWrite(os.Stdout, &doc); err != nil {
		panic(err)
	}
	// Output:
	// users: " The users allowed."
	// // The server to listen on.
	// server port=8080
	// // Unused.
	//
	// // The users allowed,
	// // and carol.
	// users alice bob carol
}

func TestEmptyChildren(t *testing.T) {
	const input = "a {}\nb\nc {\n    d {}\n}\ncomment { // c\n}\nonly {\n    // only a comment\n}\nslashdashed /- { x } {}\n"
	var doc kuddle.Document
	if err := kuddle.Unmarshal([]byte(input), &doc); err != nil {
		t.Fatal(err)
	}
	for i, n := range doc.Nodes {
		if (n.Children == nil) != (i == 1) {
			t.Errorf("%s: got children %#v", n.Name, n.Children)
		}
	}
	out, err := kuddle.Marshal(&doc)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != input {
		t.Errorf("got\n%s\nwant\n%s", out, input)
	}
}

func TestDocumentMarshalKDL(t *testing.T) {
	var b strings.Builder
	enc := kdltext.NewEncoder(&b)
	doc := &kuddle.Document{Nodes: []kuddle.Node{
		{Name: "a", Entries: []kuddle.Entry{kuddle.Arg(kdltext.Int(1)), kuddle.Prop("k", kdltext.String("v"))}},
		{Name: "b", Children: []kuddle.Node{{Name: "c"}}},
	}}
	if err := doc.MarshalKDL(enc); err != nil {
		t.Fatal(err)
	}
	enc.WriteToken(kdltext.Token{Kind: kdltext.KindNode, Value: kdltext.String("parent")})
	if err := doc.MarshalKDL(enc); err != nil {
		t.Fatal(err)
	}
	enc.WriteToken(kdltext.Token{Kind: kdltext.KindEnd})
	want := "a 1 k=v\nb {\n    c\n}\nparent {\n    a 1 k=v\n    b {\n        c\n    }\n}\n"
	if b.String() != want {
		t.Errorf("got\n%s\nwant\n%s", b.String(), want)
	}
}

func TestNodeEqual(t *testing.T) {
	node := func() kuddle.Node {
		return kuddle.Node{Type: new("t"), Name: "n", Entries: []kuddle.Entry{
			kuddle.Arg(kdltext.Int(1)),
			kuddle.Prop("k", typed("u8", kdltext.String("v"))),
		}, Children: []kuddle.Node{{Name: "c"}}}
	}
	n := node()
	tests := []struct {
		name   string
		change func(*kuddle.Node)
	}{
		{"type", func(m *kuddle.Node) { m.Type = new("u") }},
		{"no type", func(m *kuddle.Node) { m.Type = nil }},
		{"name", func(m *kuddle.Node) { m.Name = "m" }},
		{"argument", func(m *kuddle.Node) { m.Entries[0] = kuddle.Arg(kdltext.Int(2)) }},
		{"property", func(m *kuddle.Node) { m.Entries[1] = kuddle.Arg(m.Entries[1].Value) }},
		{"value type", func(m *kuddle.Node) { m.Entries[1].Value.Type = nil }},
		{"entries", func(m *kuddle.Node) { m.Entries = m.Entries[:1] }},
		{"child", func(m *kuddle.Node) { m.Children[0].Name = "d" }},
		{"no children", func(m *kuddle.Node) { m.Children = nil }},
	}
	if m := node(); !n.Equal(m) {
		t.Error("equal nodes are not Equal")
	}
	for _, tt := range tests {
		m := node()
		tt.change(&m)
		if n.Equal(m) || m.Equal(n) {
			t.Errorf("%s: different nodes are Equal", tt.name)
		}
	}

	// An empty children block is not the same as none.
	empty, none := kuddle.Node{Name: "n", Children: []kuddle.Node{}}, kuddle.Node{Name: "n"}
	if empty.Equal(none) {
		t.Error("a node with an empty children block is Equal to one without")
	}
}

func TestNodeClone(t *testing.T) {
	doc := &kuddle.Document{Nodes: []kuddle.Node{
		{Type: new("t"), Name: "n", Entries: []kuddle.Entry{
			kuddle.Prop("k", typed("u8", kdltext.Int(1))),
		}, Children: []kuddle.Node{{Name: "c", Children: []kuddle.Node{}}}},
		{Name: "m"},
	}}
	c := doc.Clone()
	if !c.Equal(doc) {
		t.Fatalf("Clone() = %+v, want %+v", c, doc)
	}
	*c.Nodes[0].Type = "changed"
	*c.Nodes[0].Entries[0].Value.Type = "changed"
	c.Nodes[0].Entries[0].Name = "changed"
	c.Nodes[0].Children[0].Name = "changed"
	n := doc.Nodes[0]
	if *n.Type != "t" || *n.Entries[0].Value.Type != "u8" || n.Entries[0].Name != "k" || n.Children[0].Name != "c" {
		t.Errorf("changing the clone changed the original: %+v", n)
	}
	if c.Nodes[0].Children[0].Children == nil || c.Nodes[1].Children != nil || c.Nodes[1].Type != nil {
		t.Errorf("Clone() did not keep nil and empty apart: %+v", c.Nodes)
	}
}

func TestNodeArgsAndProp(t *testing.T) {
	n := kuddle.Node{Name: "a", Entries: []kuddle.Entry{
		kuddle.Arg(num("1")), kuddle.Prop("k", num("2")), kuddle.Arg(kdltext.String("x")), kuddle.Prop("k", num("3")),
	}}
	if args := n.Args(); len(args) != 2 || args[1].String() != "x" {
		t.Errorf("Args() = %v", args)
	}
	if k, ok := n.Prop("k"); !ok || k.String() != "3" {
		t.Errorf(`Prop("k") = %v, %v; want 3`, k, ok)
	}
	if _, ok := n.Prop("missing"); ok {
		t.Error(`Prop("missing") found a value`)
	}
}
