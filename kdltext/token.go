package kdltext

// Kind is the kind of a Token.
type Kind byte

const (
	KindInvalid      Kind = 0
	KindNode         Kind = 'n' // the start of a node
	KindValue        Kind = 'v' // an argument, or the value of the property before it
	KindProp         Kind = 'p' // the name of a property, whose value follows
	KindEntryComment Kind = 'i' // a comment among the entries of the current node
	KindEnd          Kind = 'e' // the end of the current node
	KindComment      Kind = 'c' // a comment between nodes
)

func (k Kind) String() string {
	switch k {
	case KindNode:
		return "node"
	case KindValue:
		return "value"
	case KindProp:
		return "property"
	case KindEnd:
		return "end"
	case KindEntryComment:
		return "entry comment"
	case KindComment:
		return "comment"
	}
	return "invalid"
}

// Token is an element of a document's token stream (see the package
// documentation).
type Token struct {
	Kind Kind

	// Children reports, for a KindEnd token, whether the node it ends has a
	// children block, even if it is empty: e {} rather than e. It is known
	// only once the node's entries have been read, so it is not on the
	// KindNode token. It matters only for a node without children, since a
	// node with child nodes has a children block whether or not Children is
	// set.
	Children bool

	// Value depends on the Kind of the token:
	//
	//   - KindNode: the node's name, a string, with the node's type
	//     annotation as its Type.
	//   - KindProp: the property's name, a string. Its Type is always nil,
	//     as KDL has no type annotations on property names: an annotation
	//     belongs to the property's value, in the KindValue token after it.
	//   - KindValue: an argument, or the value of the property whose KindProp
	//     token precedes it, with its own type annotation as its Type.
	//
	// It is the zero Value for other kinds of token.
	Value Value

	// Comment is the comment of a KindComment or KindEntryComment token, or
	// the comment directly above the node of a KindNode token, if any.
	Comment Comment
}

// Name returns the name of a KindNode or KindProp token.
func (t Token) Name() string { return t.Value.contents() }

// Clone returns a copy of t that does not share its type annotation.
func (t Token) Clone() Token {
	t.Value = t.Value.Clone()
	return t
}

// Equal reports whether t and u are the same token, comments included. The
// values of tokens that don't have one, such as KindEnd tokens, are not
// compared.
func (t Token) Equal(u Token) bool {
	if t.Kind != u.Kind || t.Children != u.Children || t.Comment != u.Comment {
		return false
	}
	switch t.Kind {
	case KindNode, KindProp, KindValue:
		return t.Value.Equal(u.Value)
	}
	return true
}
