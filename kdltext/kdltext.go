// Package kdltext reads and writes the syntax of KDL documents, in versions 1
// and 2 of the language.
//
// A Decoder reads a document as a stream of tokens, and an Encoder writes
// one. A document's stream is a sequence of nodes and KindComment tokens,
// where a node is
//
//	KindNode (KindValue | KindProp KindValue | KindEntryComment)* (node | KindComment)* KindEntryComment* KindEnd
//
// A KindValue token on its own is an argument, and one after a KindProp
// token is that property's value. The nested nodes and comments are the
// node's children. KindEntryComment tokens after the children are written
// after the node's closing '}'. Whitespace has no tokens.
//
// # Comments
//
// A Comment is a run of line comments on consecutive lines, a block comment,
// or a slash-dashed part of the document. Each appears in the stream once:
//
//   - A comment directly above a node, with no blank line in between,
//     belongs to the node: it is the Comment of the node's KindNode token. A
//     slash-dashed node is never another node's comment.
//   - A comment on a node's line, whether among its entries, after them, or
//     after the '{' of its children, is a KindEntryComment token in its place
//     among the entries. A slash-dashed children block after the node's
//     closing '}' is also a KindEntryComment token, after the children. A
//     comment inside an entry, such as between a property's name and value,
//     comes after the entry.
//   - Any other comment, between nodes, is a KindComment token: a comment
//     node.
//
// When writing, a blank line is written after a comment node where it would
// otherwise be read as the comment of the node that follows, or be joined
// with the line comment that follows.
//
// The kuddle package builds on this package to convert between KDL documents
// and Go values.
package kdltext

import (
	"errors"
	"fmt"

	"github.com/miquella/kuddle/internal/kdlopts"
	"github.com/miquella/kuddle/internal/lexer"
)

// Version is a version of the KDL language.
type Version int

const (
	// V1 is KDL version 1.0.0.
	V1 Version = 1

	// V2 is KDL version 2.0.0.
	V2 Version = 2
)

// Options configures a Decoder or Encoder. It is the same type as
// kuddle.Options: options for either package may be passed to the other,
// which ignores the ones that do not apply.
type Options = kdlopts.Options

// WithVersion sets the KDL version that a Decoder reads and an Encoder
// writes.
//
// When decoding without a version, the version is detected from the document:
// a version marker (/- kdl-version 1) at the start of the document decides it,
// as does the first syntax that is only valid in one of the versions. When
// decoding with a version, a version marker for the other version is an
// error that wraps ErrVersionMismatch.
//
// When encoding without a version, V2 is written.
//
// WithVersion panics if v is not V1 or V2.
func WithVersion(v Version) Options {
	if v != V1 && v != V2 {
		panic(fmt.Sprintf("kdltext: invalid version %d", v))
	}
	return &kdlopts.Struct{Present: kdlopts.Version, Version: int(v)}
}

// WithNewline sets the line ending of the lines an Encoder writes, which is
// "\n" or "\r\n". By default, it is "\n", and the newlines inside comments are
// written as they are. If it is set, it applies to the newlines inside block
// comments as well as to those between line comments. The text of strings and
// slash-dashed parts, which can hold strings, is always written as it is (see
// the options of kuddle.Document.Format).
//
// WithNewline panics if newline is neither.
func WithNewline(newline string) Options {
	if newline != "\n" && newline != "\r\n" {
		panic(fmt.Sprintf("kdltext: invalid newline %q", newline))
	}
	return &kdlopts.Struct{Present: kdlopts.Newline, Newline: newline}
}

// WithIndent sets the indentation of child nodes written by an Encoder, which
// is four spaces by default. It applies to both KDL versions.
//
// WithIndent panics if indent contains characters other than spaces and tabs.
func WithIndent(indent string) Options {
	for _, c := range indent {
		if c != ' ' && c != '\t' {
			panic("kdltext: indent must only contain spaces and tabs")
		}
	}
	return &kdlopts.Struct{Present: kdlopts.Indent, Indent: indent}
}

// QuoteIdentifiers sets whether an Encoder writes every node name, property
// name and type annotation as a quoted string, even where it could be written
// as a bare identifier. String values are not affected. It
// applies to both KDL versions.
func QuoteIdentifiers(v bool) Options {
	return kdlopts.Bool(kdlopts.QuoteIdentifiers, v)
}

// DiscardComments sets whether an Encoder leaves out comments: comment nodes,
// comment entries, and the comments of nodes. It applies to both KDL
// versions.
func DiscardComments(v bool) Options {
	return kdlopts.Bool(kdlopts.DiscardComments, v)
}

// RejectMultipleChildrenBlocks sets whether a Decoder limits a node to one
// children block, slash-dashed or not, as the v1 grammar does. By default, it
// allows slash-dashed children blocks around the node's one real block, like
// v2 and other v1 implementations: node /-{ a } { b } is node { b }. It
// applies only to KDL v1.
func RejectMultipleChildrenBlocks(v bool) Options {
	return kdlopts.Bool(kdlopts.RejectMultipleChildrenBlocks, v)
}

// RejectDuplicateProperties sets whether a node may have only one property
// of each name. If so, a Decoder reports a later property of the same name
// as a syntax error, and an Encoder reports an error for writing one. By
// default, repeated properties are allowed, as KDL allows them: the
// rightmost takes precedence. Slash-dashed properties are not counted. It
// applies to both the Decoder and the Encoder, and to both KDL versions.
func RejectDuplicateProperties(v bool) Options {
	return kdlopts.Bool(kdlopts.RejectDuplicateProperties, v)
}

// WithMaxTokenSize sets the maximum number of bytes of input a Decoder holds
// to read a single token, such as a string or comment. Zero, the default,
// means no limit. Reading a larger token is an error that wraps
// ErrTokenTooLarge. The limit applies to the input of one token only: it
// doesn't limit a *bytes.Buffer, which the Decoder holds in full, or the text
// of a slash-dashed part, which it keeps as a Comment. It applies to both KDL
// versions.
//
// WithMaxTokenSize panics if n is negative.
func WithMaxTokenSize(n int) Options {
	if n < 0 {
		panic(fmt.Sprintf("kdltext: invalid maximum token size %d", n))
	}
	return &kdlopts.Struct{Present: kdlopts.MaxTokenSize, MaxTokenSize: n}
}

// ErrVersionMismatch is wrapped by the error returned when a document's
// version marker conflicts with the version given by WithVersion.
var ErrVersionMismatch = lexer.ErrVersionMismatch

// ErrTokenTooLarge is wrapped by the error returned when a token is larger
// than the size given by WithMaxTokenSize.
var ErrTokenTooLarge = lexer.ErrTooLarge

// Position is a location in a KDL document.
type Position struct {
	Offset int64 // byte offset, starting at 0
	Line   int   // line number, starting at 1
	Column int   // column number in code points, starting at 1
}

func (p Position) String() string { return fmt.Sprintf("line %d, column %d", p.Line, p.Column) }

// SyntacticError is returned for a document that is not valid KDL.
type SyntacticError struct {
	Position
	Err error
}

func (e *SyntacticError) Error() string {
	return fmt.Sprintf("kdltext: syntax error at %v: %v", e.Position, e.Err)
}

func (e *SyntacticError) Unwrap() error { return e.Err }

// syntaxError converts an error from the lexer to a SyntacticError.
func syntaxError(err error) error {
	le, ok := errors.AsType[*lexer.SyntaxError](err)
	if !ok {
		return err
	}
	return &SyntacticError{Position: Position(le.Position), Err: &msgError{le.Msg, le.Err}}
}

// msgError is an error with a message of its own that wraps another error,
// which may be nil.
type msgError struct {
	msg string
	err error
}

func (e *msgError) Error() string { return e.msg }
func (e *msgError) Unwrap() error { return e.err }
