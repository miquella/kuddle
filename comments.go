package kuddle

import (
	"reflect"
	"strings"

	"github.com/miquella/kuddle/kdltext"
)

// This file holds the comment fields of structs: the comment and headercomment
// kinds, which hold a list of comments, and how they convert to and from the
// comments in the token stream.
//
// A comment field holds the comments as blocks. A block is what a
// kdltext.Comment is: a run of line comments on consecutive lines, or one
// block comment. A field of one of these types holds them:
//
//   - A string holds the text of every block, with a blank line between
//     blocks.
//   - A []string, or an array of strings, holds the text of each block.
//   - A []kdltext.Comment, or an array of them, holds each block as it is,
//     with its kind and its spaces.

// isCommentBlocks reports whether t is a type that a comment or headercomment
// field can have.
func isCommentBlocks(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.String:
		return true
	case reflect.Slice, reflect.Array:
		return t.Elem().Kind() == reflect.String || t.Elem() == commentType
	}
	return false
}

// setBlocks sets fv, a comment or headercomment field, to the blocks above,
// followed by the block attached, if there is one (if its kind is not zero).
// An array takes as many as it has room for.
func setBlocks(fv reflect.Value, above []kdltext.Comment, attached kdltext.Comment) {
	n := len(above)
	if attached.Kind != 0 {
		n++
	}
	block := func(i int) kdltext.Comment {
		if i < len(above) {
			return above[i]
		}
		return attached
	}

	fv.SetZero()
	if n == 0 {
		return
	}
	switch fv.Kind() {
	case reflect.String:
		if n == 1 {
			fv.SetString(commentText(block(0)))
			return
		}
		var b strings.Builder
		for i := range n {
			if i > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(commentText(block(i)))
		}
		fv.SetString(b.String())
	case reflect.Slice:
		fv.Set(reflect.MakeSlice(fv.Type(), n, n))
		setElems(fv, n, block)
	case reflect.Array:
		setElems(fv, min(n, fv.Len()), block)
	}
}

// setElems sets the first n elements of fv, a slice or array of strings or
// kdltext.Comment, to the blocks.
func setElems(fv reflect.Value, n int, block func(int) kdltext.Comment) {
	for i := range n {
		if el := fv.Index(i); el.Type() == commentType {
			el.Set(reflect.ValueOf(block(i)))
		} else {
			el.SetString(commentText(block(i)))
		}
	}
}

// blocksOf appends to blocks the blocks held by fv, a comment or
// headercomment field, in order, and returns it. A string is split at its
// blank lines. The elements of a slice or array that are empty are left out.
func blocksOf(fv reflect.Value, blocks []kdltext.Comment) []kdltext.Comment {
	switch fv.Kind() {
	case reflect.String:
		return appendStringBlocks(blocks, fv.String())
	case reflect.Slice, reflect.Array:
		for i := range fv.Len() {
			var c kdltext.Comment
			if el := fv.Index(i); el.Type() == commentType {
				c = el.Interface().(kdltext.Comment)
			} else {
				c = lineCommentOf(el.String())
			}
			if c.Kind != 0 {
				blocks = append(blocks, c)
			}
		}
	}
	return blocks
}

// appendStringBlocks appends to blocks a line comment for each run of lines
// in s between blank lines.
func appendStringBlocks(blocks []kdltext.Comment, s string) []kdltext.Comment {
	if s == "" {
		return blocks
	}
	if !strings.Contains(s, "\n\n") && s[0] != '\n' && s[len(s)-1] != '\n' {
		// There is no blank line, so it is a single block.
		return append(blocks, lineCommentOf(s))
	}
	var lines []string
	flush := func() {
		if len(lines) > 0 {
			blocks = append(blocks, lineCommentOf(strings.Join(lines, "\n")))
			lines = nil
		}
	}
	for line := range strings.SplitSeq(s, "\n") {
		if line == "" {
			flush()
		} else {
			lines = append(lines, line)
		}
	}
	flush()
	return blocks
}

// lineCommentOf returns the line comment whose lines are those of s, with a
// space after each "//", which is no comment if s is empty.
func lineCommentOf(s string) kdltext.Comment {
	if s == "" {
		return kdltext.Comment{}
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = " " + line
		}
	}
	return kdltext.Comment{Kind: kdltext.CommentLine, Text: strings.Join(lines, "\n")}
}
