package kdltext

// CommentKind is the kind of a Comment.
type CommentKind byte

const (
	CommentLine      CommentKind = '/' // a run of line comments: // Text
	CommentBlock     CommentKind = '*' // a block comment: /* Text */
	CommentSlashdash CommentKind = '-' // a slash-dashed node, entry or children block: /- Text
)

// Comment is a comment, or a slash-dashed part of a document.
//
// A comment is a run of line comments on consecutive lines, a single block
// comment, or a single slash-dashed part. A blank line, or a change of kind,
// starts another.
type Comment struct {
	Kind CommentKind // zero for no comment

	// Text is the literal text of the comment, whatever follows its opening
	// marker, spaces, indentation and newlines included. It is written back
	// after the marker just as it is, so a Text of "x" is written as //x, and
	// of " x" as // x:
	//
	//   - CommentLine: a line for each line comment, of all the text after
	//     its "//", separated by "\n".
	//   - CommentBlock: all the text between the "/*" and "*/".
	//   - CommentSlashdash: the KDL text after the "/-", including any
	//     whitespace after the "/-", but not any after the part.
	//
	// An Encoder given WithNewline writes the newlines of a block comment with
	// that line ending. Otherwise, the text is written unchanged.
	//
	// To tidy comments, such as to put one space after each marker or to
	// indent a comment of several lines to the depth of its node, format the
	// document. The string form of a comment, headercomment or linecomment
	// struct field is trimmed when read and padded when written; a field of
	// type Comment, or a slice of them, holds this literal text.
	Text string
}
