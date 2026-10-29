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
//   - A struct holds the node's arguments, properties and children, and its
//     name and type annotation, in fields, as their tags say (see Struct
//     tags).
//   - A map holds the node's properties and children, keyed by name. Its keys
//     are of a string type, or of a type implementing
//     [encoding.TextUnmarshaler] and [encoding.TextMarshaler], which convert
//     names to keys and keys to names. Marshal writes the values of value
//     types as properties, and the others as child nodes, in the order of
//     their keys' names.
//   - A slice or array holds the node's arguments, followed by its children,
//     whose names Unmarshal ignores. Marshal writes the elements of value
//     types as arguments, and the others as child nodes.
//   - A value type (see Values) holds the node's single argument, which must
//     be all there is in the node.
//   - A [Node] holds the node as it is, and a [Document] the node's children.
//   - An empty interface holds the Go value of the node's argument, if the
//     node has a single argument and nothing else, and a *Node otherwise.
//   - A pointer holds what it points to. Unmarshal allocates a nil pointer,
//     but for a pointer to a value type other than an interface, which it
//     decodes from the node's argument, as a value, so that #null sets it to
//     nil.
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
// most one each of args, props, entries, children, name, type, comment and
// linecomment.
//
// # Kinds
//
// A field has one of these kinds:
//
//   - child, the default: the child node with the name. A slice field without
//     methods, other than a []byte, holds every child node with the name, one
//     per element, but if its elements are of a value type, they hold the
//     arguments of each node, and if they are empty interfaces, each node's
//     children too. Marshal writes these as the arguments and children of a
//     single node, and writes no node for an empty slice.
//   - arg: the next argument, in the order of the arg fields.
//   - args: the remaining arguments, after those of the arg fields, as a
//     slice.
//   - prop: the property with the name. Of repeated properties, the last is
//     used.
//   - props: the remaining properties, those without a prop field, as a map
//     keyed by name.
//   - entries: the remaining arguments and properties, those without an arg
//     or prop field, in order, as a []Entry. Marshal writes them after the
//     arguments of the arg fields and before the properties of the prop
//     fields. A struct with an entries field can't have an args or props
//     field.
//   - children: the remaining child nodes, those without a child field, as a
//     slice, or as a map keyed by name.
//   - name: the node's name, as a string.
//   - type: the node's type annotation, as a string, "" for none, or as a
//     *string, nil for none, so that an empty annotation can be told apart
//     from none.
//
// The arg, args, prop and props fields hold values of value types, or of types
// with methods (see Custom marshaling), other than Document, which holds only
// nodes. A child or children field can't hold an Entry, even as the elements
// of a slice or map: it is an argument or property rather than a node, and is
// held by an entries field. The keys of the maps of props and children fields
// are as for a map. The omitzero option has Marshal leave the field out if it
// is zero, as reported by its IsZero method if it has one.
//
// A field with the tag "-" is ignored, and so are unexported fields. The
// fields of an embedded struct, or pointer to a struct, without a tag are
// treated as fields of the outer struct. Names must be unique among the child
// fields, and among the prop fields, and a struct can have only one field each
// of args, props, entries, children, name and type. Unmarshal reports an
// error for the arguments, properties and children that have no field, unless
// DiscardUnknown is set.
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
// For a node, Marshal and Unmarshal write and read its KindNode token, with a
// struct's name and type fields as for any value of the type, and its KindEnd
// token, and the methods the node's contents between them: its arguments,
// properties and children. UnmarshalKDL must read all of the contents, and
// stop before the KindEnd token. At the top level, the contents are the
// document's nodes. For an argument or property, the methods write and read
// the single KindValue token of the value, after the KindProp token of a
// property. Marshal and Unmarshal report an error if a method writes or reads
// more or less than that.
//
// A method may return [errors.ErrUnsupported] without writing or reading
// anything, though it may peek at the next token, for the value to be
// marshaled by the default rules, as if its type had no methods. Returning it
// after writing or reading anything is an error. For a node, a method can also
// delegate the node's contents to the default rules, by calling
// [MarshalEncode] or [UnmarshalDecode] with its value converted to a type
// without the methods, such as plain(v) or (*plain)(v), where plain is defined
// by type plain T. For a single value, it must not, since those functions
// write and read a node's contents, not one value: it returns
// errors.ErrUnsupported instead, or writes or reads the KindValue token
// itself.
//
// The methods may have value or pointer receivers. Unmarshal calls them
// through a pointer to the value it decodes into. Marshal calls methods with
// pointer receivers on the values it can address. It copies the value it is
// given, and the values of maps, to make them addressable, but a value held in
// an interface (such as an element of a []any) is not addressable, and
// neither are its fields and elements.
//
// For an argument or property, Marshal writes a nil pointer as #null, and
// Unmarshal sets a pointer to nil for #null, without calling a method. A value
// that is not a pointer has its UnmarshalKDL method read #null, as any other
// value.
//
// # Documents
//
// A [Document] holds a document as it is: its nodes, and the arguments and
// properties of each node, including repeated properties, in order, but not
// its comments or formatting. Its MarshalKDL and UnmarshalKDL methods, which
// implement Marshaler and Unmarshaler, write and read the document's nodes, or
// the children of a node without arguments or properties. Where they can't be
// called, as for a Document held in an interface, the tag of its Nodes field
// has Marshal write the nodes all the same.
//
// A [Node] holds a node as it is: its name, type annotation, entries and
// children, where an [Entry] is an argument or property. Marshal and Unmarshal
// write and read it themselves.
//
// # Formatting
//
// [Document.Format] and [Node.Format] change how a document is written, between
// reading and writing it. By default they keep numbers and strings as they were
// written. Their options are [FormatNumbers], [FormatEmptyBlocks],
// [FormatSortProperties], [FormatQuoteValues], [FormatNormalizeStrings],
// [FormatStringNewlines] and [FormatNewline].
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
