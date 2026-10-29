package kuddle

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/miquella/kuddle/kdltext"
)

// NumberFormat is the form in which Format leaves numbers.
type NumberFormat int

const (
	// NumberLiteral leaves a number as it was written. It is the default.
	NumberLiteral NumberFormat = iota

	// NumberCanonical writes a number without underscores, a '+' sign or
	// leading zeros, with the digits of an integer in lower case, and with
	// the exponent of a float written as 'E' and a sign, as in 1.5E+10. An
	// integer keeps its base: 0x0A_bc becomes 0xabc.
	NumberCanonical

	// NumberDecimal is NumberCanonical, but writes integers in decimal:
	// 0x0A_bc becomes 2748.
	NumberDecimal
)

// EmptyBlockFormat is what Format does with an empty children block: one with
// no nodes.
type EmptyBlockFormat int

const (
	// EmptyBlockCompact keeps an empty block, written as {}. It is the
	// default.
	EmptyBlockCompact EmptyBlockFormat = iota

	// EmptyBlockOmitted removes an empty block, so that the node has no
	// children block.
	EmptyBlockOmitted
)

// FormatOption is an option of Format.
type FormatOption func(*formatter)

// formatter holds the options of a Format call.
type formatter struct {
	numbers        NumberFormat
	blocks         EmptyBlockFormat
	sort           bool
	quote          bool
	normalize      bool
	stringNewlines bool
	newline        string
}

// FormatNumbers sets the form of numbers.
func FormatNumbers(f NumberFormat) FormatOption {
	return func(o *formatter) { o.numbers = f }
}

// FormatEmptyBlocks sets what is done with empty children blocks.
func FormatEmptyBlocks(f EmptyBlockFormat) FormatOption {
	return func(o *formatter) { o.blocks = f }
}

// FormatSortProperties sets whether the properties of each node are moved after
// its other entries and sorted by name. Equal names keep their order, and so
// do the other entries.
func FormatSortProperties(v bool) FormatOption {
	return func(o *formatter) { o.sort = v }
}

// FormatQuoteValues sets whether every string argument and property value is
// written as a quoted string, even where a bare identifier would do. It takes
// precedence over FormatNormalizeStrings.
func FormatQuoteValues(v bool) FormatOption {
	return func(o *formatter) { o.quote = v }
}

// FormatNormalizeStrings sets whether strings stop being written as they were read:
// each is written as a bare identifier where the version allows, and as a
// quoted string otherwise, so "a" and #"a"# are both written as a.
func FormatNormalizeStrings(v bool) FormatOption {
	return func(o *formatter) { o.normalize = v }
}

// FormatNewline sets the line ending that FormatStringNewlines writes, which
// is "\n" or "\r\n". The default is "\n".
//
// FormatNewline panics if newline is neither.
func FormatNewline(newline string) FormatOption {
	if newline != "\n" && newline != "\r\n" {
		panic(fmt.Sprintf("kuddle: invalid newline %q", newline))
	}
	return func(o *formatter) { o.newline = newline }
}

// FormatStringNewlines sets whether the newlines written in strings are
// replaced by the line ending set by FormatNewline. It is off by default, and
// not implied by FormatNewline, because it can change data: KDL normalizes the
// newlines of a multi-line string when it is read, but a KDL v1 string that
// holds a newline has a different value afterward. Escapes such as \n are not
// changed. FormatQuoteValues and FormatNormalizeStrings take precedence over it, as they
// write newlines as escapes.
func FormatStringNewlines(v bool) FormatOption {
	return func(o *formatter) { o.stringNewlines = v }
}

// Format tidies d in place. It never removes content: it changes only how the
// document is written, other than as its options ask.
//
// By default, it keeps numbers and strings as they were written and empty
// children blocks as they are.
func (d *Document) Format(opts ...FormatOption) {
	f := newFormatter(opts)
	for i := range d.Nodes {
		f.node(&d.Nodes[i])
	}
}

// Format tidies n and its children in place, as Document.Format does.
func (n *Node) Format(opts ...FormatOption) {
	newFormatter(opts).node(n)
}

func newFormatter(opts []FormatOption) *formatter {
	f := &formatter{newline: "\n"}
	for _, o := range opts {
		o(f)
	}
	return f
}

func (f *formatter) node(n *Node) {
	for i := range n.Entries {
		n.Entries[i].Value = f.value(n.Entries[i].Value)
	}
	if f.sort {
		slices.SortStableFunc(n.Entries, func(a, b Entry) int {
			if c := cmp.Compare(propRank(a), propRank(b)); c != 0 || !a.IsProp() {
				return c
			}
			return strings.Compare(a.Name, b.Name)
		})
	}
	for i := range n.Children {
		f.node(&n.Children[i])
	}
	if f.blocks == EmptyBlockOmitted && len(n.Children) == 0 {
		n.Children = nil
	}
}

// propRank returns the place of e among the sorted entries.
func propRank(e Entry) int {
	if e.IsProp() {
		return 1
	}
	return 0
}

func (f *formatter) value(v kdltext.Value) kdltext.Value {
	switch v.Kind() {
	case kdltext.ValueString:
		switch {
		case f.quote:
			return kdltext.Quoted(v.String()).WithType(v.Type)
		case f.normalize:
			return kdltext.String(v.String()).WithType(v.Type)
		case f.stringNewlines:
			return v.WithNewlines(f.newline)
		}
	case kdltext.ValueNumber:
		if f.numbers != NumberLiteral {
			return v.Canonical(f.numbers == NumberDecimal)
		}
	}
	return v
}
