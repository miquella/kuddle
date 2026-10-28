package kuddle_test

import (
	"errors"
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
(t)a 1 k=(u8)2 "x" k=3 {
	b #true; c
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
		}},
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
	if want := "kuddle: cannot unmarshal node a (line 2, column 1) into Go kuddle.Document: a Document holds only nodes, not arguments or properties"; err == nil || err.Error() != want {
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
