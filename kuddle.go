// Package kuddle converts between KDL documents and Go values, in the manner
// of encoding/json/v2. The [kdltext] package handles the syntax of KDL,
// reading and writing documents as streams of tokens.
//
// # Documents
//
// A [Document] holds a document as it is: its nodes, and the arguments and
// properties of each node, including repeated properties, in order, but not
// its comments or formatting. Its UnmarshalKDL method reads the document's
// nodes from a [kdltext.Decoder], or the children of a node without arguments
// or properties.
//
// A [Node] holds a node as it is: its name, type annotation, entries and
// children, where an [Entry] is an argument or property.
package kuddle
