package kuddle

import (
	"errors"
	"io"
	"slices"

	"github.com/miquella/kuddle/kdltext"
)

// Document is a KDL document. It preserves the order of the nodes, and of
// each node's arguments and properties, repeated properties included. It
// also keeps the document's comments and slash-dashed parts, as comment nodes
// and entries, but not its formatting.
//
// Don't embed a Document in a struct, with or without a tag. Embedding
// promotes its MarshalKDL and UnmarshalKDL methods to the struct, which then
// marshals and unmarshals as a Document, and its other fields are ignored.
// Hold a Document in a named field instead.
type Document struct {
	// Nodes is the document's nodes. Where Marshal can't call the methods of
	// a Document, as for one held in an interface, its tag has Marshal write
	// them all the same.
	Nodes []Node `kdl:",children"`
}

// Node is a KDL node, or a comment between nodes.
type Node struct {
	Kind NodeKind

	// Type is the type annotation, or nil for none. Like kdltext.Value.Type,
	// it is shared by copies of the Node (see Clone).
	Type *string

	Name    string
	Entries []Entry // arguments, properties and comments, in order

	// Children is the node's children. It is nil for a node without a
	// children block, and empty for one with an empty block: e {}.
	Children []Node

	// Comment is the comment of a comment node. For other nodes, it is the
	// comment directly above the node, with no blank line between them, if
	// it is a line or block comment.
	Comment kdltext.Comment
}

// NodeKind is the kind of a Node.
type NodeKind byte

const (
	NodeElement NodeKind = iota // a node, with a name, entries and children
	NodeComment                 // a comment between nodes, which holds only its Comment
)

func (k NodeKind) String() string {
	switch k {
	case NodeElement:
		return "node"
	case NodeComment:
		return "comment"
	}
	return "invalid"
}

// IsComment reports whether n is a comment node.
func (n Node) IsComment() bool { return n.Kind == NodeComment }

// Entry is an argument, property or comment of a node.
type Entry struct {
	Kind EntryKind

	Name    string        // the name of a property
	Value   kdltext.Value // the value of an argument or property
	Comment kdltext.Comment
}

// EntryKind is the kind of an Entry.
type EntryKind byte

const (
	EntryArg     EntryKind = iota // an argument
	EntryProp                     // a property
	EntryComment                  // a comment, which holds only its Comment
)

func (k EntryKind) String() string {
	switch k {
	case EntryArg:
		return "argument"
	case EntryProp:
		return "property"
	case EntryComment:
		return "comment"
	}
	return "invalid"
}

// Equal reports whether d and e have equal nodes: the same content and
// comments. Whitespace is not part of a Document, so it is not compared, and
// strings are compared by their contents, not by how they were written.
func (d *Document) Equal(e *Document) bool { return equalNodes(d.Nodes, e.Nodes) }

// Clone returns a deep copy of d, which shares no slices or type annotations
// with it.
func (d *Document) Clone() *Document { return &Document{Nodes: cloneNodes(d.Nodes)} }

// Equal reports whether n and m are equal: whether both or neither are
// comment nodes, and they have the same type annotation, name, entries,
// children and comment, and both or neither have a children block.
func (n Node) Equal(m Node) bool {
	return n.IsComment() == m.IsComment() && n.Comment == m.Comment &&
		equalType(n.Type, m.Type) && n.Name == m.Name &&
		slices.EqualFunc(n.Entries, m.Entries, Entry.Equal) &&
		(n.Children == nil) == (m.Children == nil) && equalNodes(n.Children, m.Children)
}

// Clone returns a deep copy of n, which shares no slices or type annotations
// with it.
func (n Node) Clone() Node {
	c := n
	c.Type = cloneType(n.Type)
	if n.Entries != nil {
		c.Entries = make([]Entry, len(n.Entries))
		for i, e := range n.Entries {
			c.Entries[i] = e.Clone()
		}
	}
	c.Children = cloneNodes(n.Children)
	return c
}

// Equal reports whether e and f are the same kind of entry, with the same
// name, value and comment.
func (e Entry) Equal(f Entry) bool {
	return e.Kind == f.Kind && e.Name == f.Name && e.Comment == f.Comment && (e.IsComment() || e.Value.Equal(f.Value))
}

// Clone returns a copy of e that does not share its type annotation.
func (e Entry) Clone() Entry {
	e.Value = e.Value.Clone()
	return e
}

func equalNodes(a, b []Node) bool {
	return slices.EqualFunc(a, b, func(n, m Node) bool { return n.Equal(m) })
}

// cloneNodes returns a deep copy of nodes, keeping nil and empty apart.
func cloneNodes(nodes []Node) []Node {
	if nodes == nil {
		return nil
	}
	c := make([]Node, len(nodes))
	for i := range nodes {
		c[i] = nodes[i].Clone()
	}
	return c
}

func equalType(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func cloneType(typ *string) *string {
	if typ == nil {
		return nil
	}
	return new(*typ)
}

// Arg returns an argument entry.
func Arg(v kdltext.Value) Entry { return Entry{Kind: EntryArg, Value: v} }

// Prop returns a property entry.
func Prop(name string, v kdltext.Value) Entry { return Entry{Kind: EntryProp, Name: name, Value: v} }

// IsArg reports whether e is an argument.
func (e Entry) IsArg() bool { return e.Kind == EntryArg }

// IsProp reports whether e is a property.
func (e Entry) IsProp() bool { return e.Kind == EntryProp }

// IsComment reports whether e is a comment.
func (e Entry) IsComment() bool { return e.Kind == EntryComment }

// Args returns the node's arguments.
func (n Node) Args() []kdltext.Value {
	var args []kdltext.Value
	for _, e := range n.Entries {
		if e.IsArg() {
			args = append(args, e.Value)
		}
	}
	return args
}

// Prop returns the value of the node's property name. As KDL requires, the
// last of repeated properties is the one used.
func (n Node) Prop(name string) (kdltext.Value, bool) {
	for _, e := range slices.Backward(n.Entries) {
		if e.IsProp() && e.Name == name {
			return e.Value, true
		}
	}
	return kdltext.Value{}, false
}

// UnmarshalKDL reads the rest of the document from dec into d, replacing its
// nodes, with its comment nodes. If dec is within a node, it reads the rest
// of the node's children instead, leaving the node's KindEnd token to be
// read; the node's arguments and properties are an error, and the comment
// entries after its children are discarded.
func (d *Document) UnmarshalKDL(dec *kdltext.Decoder) error {
	var b builder
	if err := b.read(dec); err != nil {
		return err
	}
	d.Nodes = clone(b.nodes)
	return nil
}

// MarshalKDL writes the nodes of d to enc: the nodes of the document, or, if
// enc is within a node, the node's children.
func (d *Document) MarshalKDL(enc *kdltext.Encoder) error { return writeNodes(enc, d.Nodes) }

func writeNodes(enc *kdltext.Encoder, nodes []Node) error {
	for i := range nodes {
		if err := writeNode(enc, &nodes[i]); err != nil {
			return err
		}
	}
	return nil
}

func writeNode(enc *kdltext.Encoder, n *Node) error {
	if n.IsComment() {
		return enc.WriteToken(kdltext.Token{Kind: kdltext.KindComment, Comment: n.Comment})
	}
	if err := enc.WriteToken(kdltext.Token{Kind: kdltext.KindNode, Value: kdltext.String(n.Name).WithType(n.Type), Comment: n.Comment}); err != nil {
		return err
	}
	for _, e := range n.Entries {
		if err := writeEntry(enc, e); err != nil {
			return err
		}
	}
	if err := writeNodes(enc, n.Children); err != nil {
		return err
	}
	return enc.WriteToken(kdltext.Token{Kind: kdltext.KindEnd, Children: n.Children != nil})
}

// writeEntry writes the tokens of e: a property's name and value, an
// argument's value, or a comment.
func writeEntry(enc *kdltext.Encoder, e Entry) error {
	switch {
	case e.IsProp():
		if err := enc.WriteToken(kdltext.Token{Kind: kdltext.KindProp, Value: kdltext.String(e.Name)}); err != nil {
			return err
		}
	case e.IsComment():
		return enc.WriteToken(kdltext.Token{Kind: kdltext.KindEntryComment, Comment: e.Comment})
	}
	return enc.WriteToken(kdltext.Token{Kind: kdltext.KindValue, Value: e.Value})
}

// builder builds nodes from the tokens of a Decoder. The entries and
// children of the nodes being built are kept on stacks, so that each node's
// slices are allocated once, at their final size.
type builder struct {
	open    []openBuild // the nodes being built, innermost last
	entries []Entry     // the entries of the open nodes
	nodes   []Node      // the built children of the open nodes, and the built nodes outside them
	prop    string      // the name of the property whose value is next, if inProp
	inProp  bool
}

// openBuild is a node being built, whose entries and children start at the
// given indexes of the stacks.
type openBuild struct {
	node              Node
	entries, children int
}

// read reads tokens from dec into b up to the KindEnd token of the node that
// dec is within when read is called, which is left to be read, or up to the
// end of the document. Arguments and properties outside of the nodes being
// built are an error, and comment entries there are discarded.
func (b *builder) read(dec *kdltext.Decoder) error {
	depth := len(b.open)
	for {
		switch dec.PeekKind() {
		case kdltext.KindEnd:
			if len(b.open) == depth {
				return nil
			}
		case kdltext.KindValue, kdltext.KindProp:
			if len(b.open) == 0 {
				return errors.New("a Document holds only nodes, not arguments or properties")
			}
		case kdltext.KindEntryComment:
			if len(b.open) == 0 {
				dec.ReadToken()
				continue
			}
		case kdltext.KindInvalid:
			// The end of the document, or an error.
			if _, err := dec.ReadToken(); err != io.EOF {
				return err
			}
			return nil
		}
		t, _ := dec.ReadToken()
		b.token(t)
	}
}

// token adds t to the nodes being built.
func (b *builder) token(t kdltext.Token) {
	switch t.Kind {
	case kdltext.KindNode:
		node := Node{Type: t.Value.Type, Name: t.Name(), Comment: t.Comment}
		b.open = append(b.open, openBuild{node, len(b.entries), len(b.nodes)})
	case kdltext.KindComment:
		b.nodes = append(b.nodes, Node{Kind: NodeComment, Comment: t.Comment})
	case kdltext.KindEntryComment:
		b.entries = append(b.entries, Entry{Kind: EntryComment, Comment: t.Comment})
	case kdltext.KindProp:
		b.prop, b.inProp = t.Name(), true
	case kdltext.KindValue:
		if b.inProp {
			b.entries = append(b.entries, Prop(b.prop, t.Value))
			b.inProp = false
		} else {
			b.entries = append(b.entries, Arg(t.Value))
		}
	case kdltext.KindEnd:
		o := b.open[len(b.open)-1]
		b.open = b.open[:len(b.open)-1]
		o.node.Entries = clone(b.entries[o.entries:])
		o.node.Children = clone(b.nodes[o.children:])
		if t.Children && o.node.Children == nil {
			o.node.Children = []Node{}
		}
		b.entries = b.entries[:o.entries]
		b.nodes = append(b.nodes[:o.children], o.node)
	}
}

// clone returns a copy of s, or nil if s is empty.
func clone[S ~[]E, E any](s S) S {
	if len(s) == 0 {
		return nil
	}
	return slices.Clone(s)
}
