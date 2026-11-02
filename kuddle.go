// Package kuddle encodes and decodes KDL documents to and from Go values. Its
// API takes some inspiration from encoding/json/v2. The [kdltext] package
// handles KDL syntax, reading and writing documents as streams of tokens.
//
// [Unmarshal] decodes a document into a Go value, and [Marshal] encodes a Go
// value as a document, following the rules below. The two are inverses: a Go
// value marshals to a document that unmarshals back to an equal value, as far
// as KDL can represent it.
//
// # Nodes
//
// How a node maps to a Go value depends on the value's type:
//
//   - A struct holds the node's arguments, properties and children in its
//     fields, along with the node's name, type annotation and comments, as
//     the fields' tags specify (see Struct tags).
//   - A map holds the node's properties and children, keyed by name. Its keys
//     must be of a string type, or of a type implementing
//     [encoding.TextMarshaler] and [encoding.TextUnmarshaler] to convert
//     between keys and names. Marshal writes values of value types as
//     properties and the rest as child nodes, sorted by name.
//   - A slice or array holds the node's arguments followed by its children.
//     Unmarshal ignores the children's names. Marshal writes elements of
//     value types as arguments and the rest as child nodes.
//   - A value type (see Values) holds the node's single argument. The node
//     must have nothing else.
//   - A [Node] holds the node as written, and a [Document] holds its children.
//   - An empty interface holds the Go value of the node's argument if the
//     node has exactly one argument and nothing else, and a *Node otherwise.
//   - A pointer holds whatever it points to, and Unmarshal allocates it if it
//     is nil. A pointer to a value type other than an interface is decoded
//     from the node's argument as a value, so #null sets it to nil.
//
// A document maps to a Go value the same way a node's children do. A
// document has no arguments or properties, so a value type can't hold one,
// and everything in a map or slice that holds one is a child node.
// Unmarshaling a whole document into an empty interface gives a *Document.
//
// Unmarshal reports an error when a node has something its Go value can't
// hold, such as arguments on a node held by a map, or properties on a node
// held by a slice. When a struct has no field for an argument, property or
// child node, the error wraps [ErrUnknown]. Setting [DiscardUnknown] skips
// these instead, so that a struct can hold just the parts of a document it
// cares about.
//
// Marshal names a child node after its struct field or map key. Elsewhere,
// such as for a slice element, a node keeps its own name: a Node's name (even
// if empty), a struct's name field (if not empty), or else "-". A nil pointer
// to a value type, or a nil interface, becomes a node with a single #null
// argument. A nil pointer to any other type has no node: Marshal omits it as
// a struct field or map value, and reports an error for it as a slice or
// array element, where omitting it would shift the elements after it.
//
// # Values
//
// A value type is one whose values are single KDL values: a bool, string,
// integer or float type, []byte, [kdltext.Value], the empty interface, a type
// implementing [encoding.TextMarshaler] or [encoding.TextUnmarshaler], or a
// pointer to a value type. A type implementing [Marshaler] or [Unmarshaler]
// is never a value type. KDL values map to Go values as follows:
//
//   - A bool is #true or #false, and a string is a string.
//   - An integer or float is a number. Unmarshal reports an error if the
//     number doesn't fit the Go type.
//   - A []byte is a base64-encoded string.
//   - A kdltext.Value is the value as written.
//   - A type implementing encoding.TextMarshaler or encoding.TextUnmarshaler
//     is a string holding its text.
//   - Unmarshal sets an empty interface to a string, bool, nil, int64 (for an
//     integer that fits) or float64. Marshal writes whatever value it holds.
//   - A pointer is whatever it points to.
//
// #null sets a pointer to nil, a kdltext.Value to #null, and any other Go
// value to its zero value, without calling UnmarshalText. Marshal writes a nil
// pointer or interface as #null.
//
// # Struct tags
//
// A struct field's tag has the form `kdl:"name,kind,modifiers"`, and every
// part of it is optional. The kind says what the field holds: an argument, a
// property, a child node, and so on. The name picks out which property or
// child node it is. Modifiers change how the field is handled, and go with
// any kind. After the name, the kind and the modifiers can come in any order.
// For example, this field holds the property "port" and is left out of the
// output when it is zero:
//
//	Port int `kdl:"port,prop,omitzero"`
//
// A field with no tag is a child node, named after the field. A tag with only
// a name, like `kdl:"port"`, is a child node with that name. A tag with a
// kind but no name starts with the comma, as in `kdl:",arg"`.
//
// Only child and prop fields have a name, so a tag that gives one to any
// other kind is an error. Names must be unique among the child fields, and
// among the prop fields. A field tagged "-" is ignored, and so are unexported
// fields. The fields of an untagged embedded struct, or pointer to a struct,
// count as fields of the outer struct.
//
// A struct can have as many arg, prop and child fields as it likes, but at
// most one each of args, props, entries, children, name, type, comment,
// linecomment and headercomment.
//
// # Kinds
//
// A field has one of these kinds:
//
//   - child (the default): the child node with the name. A slice field holds
//     every child node with the name, one per element, unless it is a []byte
//     or has methods. If its elements are of a value type, they hold the
//     arguments of those nodes instead, and if they are empty interfaces, the
//     nodes' children too. Marshal writes them back as the arguments and
//     children of a single node, and writes no node for an empty slice.
//   - arg: the next argument, in the order of the arg fields.
//   - args: the arguments after those of the arg fields, as a slice.
//   - prop: the property with the name. If the property is repeated, the last
//     one wins.
//   - props: the properties that have no prop field, as a map keyed by name.
//   - entries: the arguments and properties that have no arg or prop field,
//     and the node's comment entries (see Node.Entries), in order, as a
//     []Entry. Marshal writes them after the arguments of the arg fields and
//     before the properties of the prop fields. A struct with an entries
//     field can't have an args or props field.
//   - children: the child nodes that have no child field, as a slice or as a
//     map keyed by name.
//   - name: the node's name, as a string.
//   - type: the node's type annotation, as a string ("" for none), or as a
//     *string (nil for none) to tell an empty annotation apart from none.
//   - comment: the comments above the node, as a string, a []string or a
//     []kdltext.Comment (see Comments). The document has no comment.
//   - headercomment: the comments at the head of the node's contents, before
//     its first child, in the same types as comment (see Comments). For the
//     document, they are the comments before its first node.
//   - linecomment: the line comment at the end of the node's line, after its
//     arguments and properties or after the '{' of its children, as a string
//     of its text, or as a literal kdltext.Comment, which also tells an empty
//     line comment ("//") apart from none. If there are several, as on lines
//     continued with '\', the last one wins. Block comments, and comments
//     after the closing '}', are ignored. Marshal writes a non-empty string,
//     with a space after the "//", or a kdltext.Comment that isn't zero, after
//     the node's arguments and properties, and it must be a single line
//     comment. If the struct also has an entries field, the line comment is
//     left out of the entries. The document has no line comment.
//
// The arg, args, prop and props fields hold values of value types, or of
// types with methods (see Custom marshaling), other than Document, which
// holds only nodes. A child or children field can't hold an Entry or a
// kdltext.Comment, even as the elements of a slice or map: they are entries
// and comments rather than nodes, and are held by the entries, comment,
// linecomment and headercomment fields. The keys of props and children maps
// follow the same rules as any other map.
//
// Unmarshal reports an error for arguments, properties and children that have
// no field, unless DiscardUnknown is set.
//
// # Modifiers
//
// A field can have these modifiers, along with its kind:
//
//   - omitzero: Marshal leaves the field out when its value is zero. A value
//     is zero if it has an IsZero method that says so, such as a time.Time
//     does, and otherwise if it is the zero value of its type, as reported by
//     [reflect.Value.IsZero]. Unmarshal pays no attention to it.
//
// What leaving a field out means depends on the kind. A child field writes no
// node and a prop field writes no property. A name, type, comment,
// linecomment or headercomment field acts as if it were empty. An arg field
// writes no argument, so the arguments after it move up a place; that makes
// omitzero safe only on the last arg field. Without omitzero, Marshal writes a
// nil interface, or a nil pointer to a value type, as #null, and a zero value
// as itself. Use omitzero for fields that are optional.
//
// # Comments
//
// A struct can keep the comments of a document in its fields. There are three
// kinds of field for this: comment, headercomment and linecomment.
//
// The comments are kept in blocks. A block is a run of line comments on
// consecutive lines, or a single block comment. A blank line starts a new
// block, and so does a change from one kind of comment to the other. A block
// is attached to a node if it is directly above the node, with no blank line
// between them.
//
// Slash-dashed parts (/-) are not comments for these fields, and are never
// kept.
//
// Here is a document with a comment in each of the three places:
//
//	// Settings for the web servers.
//
//	// The main server.
//	server a.example.com { // on the line
//	    // These are its options.
//
//	    // It needs TLS.
//	    tls #true
//	}
//
// And here are the structs that keep them:
//
//	type Config struct {
//		Header string `kdl:",headercomment"` // "Settings for the web servers."
//		Server Server `kdl:"server"`
//	}
//
//	type Server struct {
//		Host    string `kdl:",arg"`
//		Comment string `kdl:",comment"`       // "The main server."
//		Line    string `kdl:",linecomment"`   // "on the line"
//		Options string `kdl:",headercomment"` // "These are its options."
//		TLS     TLS    `kdl:"tls"`
//	}
//
//	type TLS struct {
//		On      bool   `kdl:",arg"`
//		Comment string `kdl:",comment"` // "It needs TLS."
//	}
//
// Each kind holds this:
//
//   - A comment field holds all of the blocks above its node, in order. The
//     last one is the block that is attached to the node, if there is one.
//   - A headercomment field holds the blocks at the top of the struct's own
//     contents. For the document, those are the blocks before its first node.
//     For a node, they are the blocks after its '{' and before its first
//     child. A block that is attached to the first child is not one of them:
//     it is the child's own comment. If the node has no children, they are all
//     of the blocks inside its braces.
//   - A linecomment field holds the comment on the node's own line, after its
//     arguments and properties or after its '{'. It is a single line comment,
//     so it holds a string, or a single [kdltext.Comment].
//
// Each block goes to only one field. If a struct has a headercomment field, it
// takes the blocks at the top of its children, and the comment field of its
// first child does not get them. That way, the top of a document does not move
// when its first node moves. If the struct has no headercomment field, the
// first child's comment field gets them instead.
//
// A comment or headercomment field can have more than one block in it. Take a
// node with two blocks above it:
//
//	// Check the version.
//
//	// Needs a restart.
//	// Ask first.
//	server a.example.com
//
// A field of type string holds both blocks, with a blank line between them:
//
//	"Check the version.\n\nNeeds a restart.\nAsk first."
//
// A field of type []string holds one string for each block:
//
//	["Check the version." "Needs a restart.\nAsk first."]
//
// A field of type []kdltext.Comment holds one [kdltext.Comment] for each
// block, as it was written. That keeps what kind of comment each one is, and
// its text with all its spaces.
//
// Strings are read without the "//" or "/* */" and the spaces around the text.
// The lines of a block are joined with line breaks. An array works like a
// slice, and holds as many blocks as it has room for. A single
// kdltext.Comment is an error, because there can be more than one block: use
// a []kdltext.Comment.
//
// Marshal writes the blocks back to the same places: a comment field above
// the node, and a headercomment field at the top of its children. It writes
// them in order, with a blank line between two blocks where one is needed to
// keep them apart.
//
//   - A string is split into blocks at its blank lines, and each block is
//     written as line comments ("//"). To write block comments, use a
//     []kdltext.Comment.
//   - A blank line in a string always starts a new block. To write a "//" with
//     nothing after it in the middle of a block, use a []string or a
//     []kdltext.Comment.
//   - Empty strings and zero comments are not written.
//
// These comments are not kept:
//
//   - The comments after the last child of a node. There is nothing below
//     them, so no field holds them.
//   - The blank line above a comment that is between two nodes. Marshal writes
//     the comment right below the node before it. When the document is read
//     again, it has the same comments.
//
// The [kdltext.DiscardComments] option leaves all of these fields empty when
// reading, and writes no comments when writing.
//
// # Custom marshaling
//
// A type can marshal itself by implementing [Marshaler] and [Unmarshaler].
// Their methods write and read the type's tokens through the live
// [kdltext.Encoder] and [kdltext.Decoder], and take precedence over
// encoding.TextMarshaler and encoding.TextUnmarshaler. A type implementing
// either interface is a node, except in a struct's arg, args, prop and props
// fields, where it is a single value. In a map, slice or children field, its
// values are always child nodes, never properties or arguments, and a slice
// or map type with the methods is a single node. That way its values always
// round-trip through the same methods.
//
// For a node, Marshal and Unmarshal handle its KindNode and KindEnd tokens,
// along with a struct's name, type and comment fields, and the methods
// handle the node's contents in between: its arguments, properties and
// children. UnmarshalKDL must read all of the contents and stop before the
// KindEnd token. At the top level, the contents are the document's nodes. For
// an argument or property, the methods write and read the value's single
// KindValue token, which follows the KindProp token for a property. Marshal
// and Unmarshal report an error if a method writes or reads more or less than
// that.
//
// A node's contents include its comments, which the methods handle
// themselves: KindEntryComment tokens among the arguments and properties and
// after the children, and KindComment tokens (comment nodes) between the
// children. UnmarshalKDL must read or skip them like any other token. A
// method that reads arguments while PeekKind reports KindValue stops at a
// comment among them, so it reports an error if more arguments follow.
// Comment tokens left unread just before the KindEnd token are skipped. Only
// the comment field is handled along with the KindNode token: the comments
// above the node are set before UnmarshalKDL is called, and written before
// the KindNode token. For a linecomment field, UnmarshalKDL takes the text of
// the last KindEntryComment token among the entries whose Comment is a
// kdltext.CommentLine, and MarshalKDL writes one after the arguments and
// properties. For a headercomment field, UnmarshalKDL takes the KindComment
// tokens before the first child, and MarshalKDL writes them before its
// children. Delegating to MarshalEncode or UnmarshalDecode handles comments
// as the default rules do.
//
// A method may return [errors.ErrUnsupported] without writing or reading
// anything (though it may peek at the next token) to have the value
// marshaled by the default rules, as if its type had no methods. Returning it
// after writing or reading anything is an error. For a node, a method can
// also hand the node's contents to the default rules by calling
// [MarshalEncode] or [UnmarshalDecode] with its value converted to a type
// without the methods, such as plain(v) or (*plain)(v), given type plain T.
// It must not do this for a single value, since those functions handle a
// node's contents rather than one value. Instead, it returns
// errors.ErrUnsupported, or writes or reads the KindValue token itself.
//
// The methods may have value or pointer receivers. Unmarshal calls them
// through a pointer to the value it decodes into. Marshal calls methods with
// pointer receivers on the values it can address. It copies the value it is
// given, and the values of maps, to make them addressable, but a value held in
// an interface (such as an element of a []any) is not addressable, and
// neither are its fields and elements.
//
// For an argument or property, Marshal writes a nil pointer as #null, and
// Unmarshal sets a pointer to nil for #null, without calling a method. A
// value that is not a pointer has its UnmarshalKDL method read #null like any
// other value.
//
// Functions can stand in for the methods, for types that lack them, such as
// time.Duration, or whose methods don't suit. Make them with [MarshalFunc]
// and [UnmarshalFunc], and pass them to Marshal and Unmarshal with
// [WithMarshalers] and [WithUnmarshalers]. Each function is given the value,
// or a pointer to it, and the live kdltext.Encoder or kdltext.Decoder.
//
// Unlike methods, functions don't change how a type is classified, so they
// don't change where its values appear in a document. A function for a value
// type writes and reads the value's single KindValue token, wherever the
// default rules put it: as an argument, a property, or a node's single
// argument. A function for any other type writes and reads its node's
// contents, as methods do, with the same checks.
//
// Functions run in the order that [JoinMarshalers] and [JoinUnmarshalers]
// give them, before the methods, just as methods run before
// encoding.TextMarshaler and encoding.TextUnmarshaler. Each may return
// errors.ErrUnsupported without writing or reading anything, to pass the
// value on to the next one, and finally to the default rules. For a node, a
// function can also delegate the node's contents as a method can, by
// converting the value to a type without functions or methods. Calling
// MarshalEncode or UnmarshalDecode with the value unchanged calls the
// function again. The options, including the functions, carry over to the
// MarshalEncode and UnmarshalDecode calls of methods and functions, through
// the Options method of the Encoder or Decoder.
//
// # Documents
//
// A [Document] holds a document as written: its nodes, each node's arguments
// and properties in order (repeated properties included), and its comments,
// as comment nodes and entries. It doesn't keep the formatting. Its
// MarshalKDL and UnmarshalKDL methods write and read the document's nodes, or
// the children of a node without arguments or properties. Where those
// methods can't be called, such as for a Document held in an interface, the
// tag on its Nodes field still has Marshal write the nodes.
//
// A [Node] holds a node as written: its name, type annotation, entries,
// children and comment, where an [Entry] is an argument, property or comment.
// Marshal and Unmarshal handle a Node directly. Unmarshaling into any other
// Go value skips comments, except for a struct's entries, comment,
// linecomment and headercomment fields.
//
// [Document.From] and [Document.To] convert between Go values and Documents by
// the same rules that Marshal and Unmarshal use for KDL text. They copy the
// data directly, without KDL text, except for values with methods or
// functions, which write and read their parts of the document as KDL text.
// The options that concern KDL text don't apply to From and To.
//
// # Options
//
// The functions in this package take [Options] from this package or from
// kdltext. Marshal and Unmarshal pass them on to the kdltext.Encoder or
// kdltext.Decoder they create, which take the options described in the
// kdltext package documentation, such as [kdltext.WithVersion] and
// [kdltext.WithIndent]. Unmarshal also takes [DiscardUnknown], to skip the
// arguments, properties and child nodes that a struct has no field for
// instead of reporting an error. Marshal takes [WithMarshalers], and
// Unmarshal takes [WithUnmarshalers], to supply functions for particular
// types (see Custom marshaling). [JoinOptions] combines several options into
// one. Later options take precedence over earlier ones.
//
// # Formatting
//
// [Document.Format] and [Node.Format] change how a document is written, between
// reading and writing it. By default they only tidy its comments, keeping numbers and
// strings as they were written. Their options are [FormatNumbers],
// [FormatEmptyBlocks], [FormatSortProperties], [FormatQuoteValues],
// [FormatNormalizeStrings], [FormatStringNewlines], [FormatLiteralComments],
// [FormatIndent] and [FormatNewline]. The last two should match the options
// the document is written with, kdltext.WithIndent and kdltext.WithNewline.
package kuddle

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/miquella/kuddle/internal/kdlopts"
	"github.com/miquella/kuddle/internal/lexer"
	"github.com/miquella/kuddle/kdltext"
)

// Options configures marshaling and unmarshaling. It is the same type as
// kdltext.Options, so options from either package may be passed to the
// functions of this package. Later options take precedence over earlier ones.
type Options = kdlopts.Options

// JoinOptions returns the options in opts as a single Options.
func JoinOptions(opts ...Options) Options { return kdlopts.Join(opts...) }

// DiscardUnknown sets whether Unmarshal skips the arguments, properties and
// child nodes that a struct has no field for, instead of reporting an error
// that wraps ErrUnknown. Values that a Go type can't hold at all, such as the
// arguments of a node decoded into a map, are always an error. It applies to
// both KDL versions.
func DiscardUnknown(v bool) Options { return kdlopts.Bool(kdlopts.DiscardUnknown, v) }

// SemanticError is returned when a KDL document and a Go value do not
// correspond, such as when a string argument is unmarshaled into an int.
type SemanticError struct {
	action string // "marshal" or "unmarshal"

	// Path is the path of the node, such as routes/route[2] or
	// server/"a/b"/listen: the names of the node and its ancestors, from the
	// top level down, separated by "/". Each name is written as in KDL v2,
	// as a bare identifier if it is a valid one and as a quoted string
	// otherwise, and is followed by [i] if the node is element i of a Go
	// slice or array. Path is empty for the document itself.
	Path string

	// Pos is the position of the node in the input, if it was parsed from
	// one, and is zero otherwise.
	Pos kdltext.Position

	// Entry describes the argument or property of the node, such as
	// "argument 2" or `property "port"`, or is empty if the error is about
	// the node as a whole.
	Entry string

	GoType reflect.Type // the Go type, if any
	Err    error        // the underlying error
}

func (e *SemanticError) Error() string {
	var b strings.Builder
	b.WriteString("kuddle: cannot ")
	b.WriteString(e.action)
	if e.Entry != "" {
		b.WriteString(" ")
		b.WriteString(e.Entry)
		b.WriteString(" of")
	}
	if e.Path == "" {
		b.WriteString(" the document")
	} else {
		b.WriteString(" node ")
		b.WriteString(e.Path)
	}
	if e.Pos.Line > 0 {
		fmt.Fprintf(&b, " (%v)", e.Pos)
	}
	if e.GoType != nil {
		if e.action == "marshal" {
			fmt.Fprintf(&b, " from Go %v", e.GoType)
		} else {
			fmt.Fprintf(&b, " into Go %v", e.GoType)
		}
	}
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

func (e *SemanticError) Unwrap() error { return e.Err }

// within returns a copy of e, which is about the contents of the node at
// path and pos, from a nested call within the method of a type, with its
// Path made relative to the document.
func (e *SemanticError) within(path string, pos kdltext.Position) *SemanticError {
	c := *e
	if c.Path == "" {
		c.Pos = pos
	}
	switch {
	case c.Path == "":
		c.Path = path
	case path != "":
		c.Path = path + "/" + c.Path
	}
	return &c
}

// pathSegment is a node in the Path of a SemanticError.
type pathSegment struct {
	name  string
	index int // the index of the node's Go value in a slice or array, or -1
}

// formatPath returns the Path of a SemanticError for the nodes p.
func formatPath(p []pathSegment) string {
	var b strings.Builder
	for i, s := range p {
		if i > 0 {
			b.WriteByte('/')
		}
		b.WriteString(formatName(s.name))
		if s.index >= 0 {
			fmt.Fprintf(&b, "[%d]", s.index)
		}
	}
	return b.String()
}

// formatName returns name as KDL v2 writes a node's name: as a bare
// identifier if it is a valid one, and as a quoted string otherwise.
func formatName(name string) string {
	if lexer.IsIdentifier(name, lexer.V2) {
		return name
	}
	return lexer.Quote(name)
}
