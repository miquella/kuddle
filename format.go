package kuddle

import (
	"bytes"
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/miquella/kuddle/internal/lexer"
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
// no nodes. A block holding only comments is not empty.
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
	literal        bool
	indent         string
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
// its other entries and sorted by name, before any slash-dashed children
// blocks. Equal names keep their order, and so do the other entries.
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

// FormatIndent sets the indentation of one level of children, which Format
// indents comments of several lines to. The default is four spaces. It should
// be the indentation the document is written with (see kdltext.WithIndent).
func FormatIndent(indent string) FormatOption {
	return func(o *formatter) { o.indent = indent }
}

// FormatLiteralComments sets whether comments are left exactly as they are, rather
// than tidied.
func FormatLiteralComments(v bool) FormatOption {
	return func(o *formatter) { o.literal = v }
}

// FormatNewline sets the line ending of the newlines inside block comments and
// slash-dashed parts, which is "\n" or "\r\n". The default is "\n". It should
// be the line ending the document is written with (see kdltext.WithNewline).
// The newlines in strings are left alone, unless FormatStringNewlines is set.
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
// children blocks as they are, and tidies comments:
//
//   - A line comment gets one space after its "//" and loses its trailing
//     spaces. One that starts with another '/' is not given a space.
//   - A block comment on one line gets one space at each end, unless it
//     starts with another '*'.
//   - The later lines of a longer block comment are indented to the depth of
//     its node, keeping their indentation relative to each other. The text
//     stays aligned with the text after the "/*" if there is some, and is
//     otherwise indented inside the "/*" and "*/". A column of leading '*'s
//     stays aligned under the "/*", and a "*/" on its own line is aligned
//     with it.
//   - A slash-dashed part gets one space after its "/-", and is laid out and
//     formatted as the KDL it is, at the depth of its node, leaving the
//     contents of its strings alone. One that can't be read is left as it
//     is.
func (d *Document) Format(opts ...FormatOption) {
	f := newFormatter(opts)
	for i := range d.Nodes {
		f.node(&d.Nodes[i], 0)
	}
}

// Format tidies n and its children in place, as Document.Format does, taking
// n to be at the top level of its document.
func (n *Node) Format(opts ...FormatOption) {
	newFormatter(opts).node(n, 0)
}

func newFormatter(opts []FormatOption) *formatter {
	f := &formatter{indent: "    ", newline: "\n"}
	for _, o := range opts {
		o(f)
	}
	return f
}

func (f *formatter) node(n *Node, depth int) {
	n.Comment = f.comment(n.Comment, depth, false)
	for i := range n.Entries {
		e := &n.Entries[i]
		if e.IsComment() {
			e.Comment = f.comment(e.Comment, depth, true)
		} else {
			e.Value = f.value(e.Value)
		}
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
		f.node(&n.Children[i], depth+1)
	}
	if f.blocks == EmptyBlockOmitted && len(n.Children) == 0 {
		n.Children = nil
	}
}

// propRank returns the place of e among the sorted entries: other entries,
// then properties, then slash-dashed children blocks, which can't come before
// an entry.
func propRank(e Entry) int {
	switch {
	case e.IsProp():
		return 1
	case e.IsComment() && e.Comment.Kind == kdltext.CommentSlashdash && isSlashdashedBlock(e.Comment.Text):
		return 2
	}
	return 0
}

// isSlashdashedBlock reports whether text, that of a slash-dashed entry, is a
// children block: whether the first thing in it, after any whitespace,
// comments and line continuations, is a '{'.
func isSlashdashedBlock(text string) bool {
	l := lexer.New(bytes.NewBufferString(text), lexer.Undecided)
	for {
		t, err := l.NextToken()
		if err != nil {
			return false
		}
		switch t.Kind {
		case lexer.Whitespace, lexer.Newline, lexer.Escline, lexer.LineComment, lexer.BlockComment:
		default:
			return t.Kind == lexer.LBrace
		}
	}
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

// comment returns c, the comment of a node at the given depth, or one of its
// entries if entry is set, tidied.
func (f *formatter) comment(c kdltext.Comment, depth int, entry bool) kdltext.Comment {
	if f.literal {
		return c
	}
	switch c.Kind {
	case kdltext.CommentLine:
		lines := strings.Split(c.Text, "\n")
		for i, line := range lines {
			lines[i] = pad(strings.TrimRight(line, " \t\r"), "/")
		}
		c.Text = strings.Join(lines, "\n")
	case kdltext.CommentBlock:
		text := strings.ReplaceAll(strings.ReplaceAll(c.Text, "\r\n", "\n"), "\r", "\n")
		if strings.Contains(text, "\n") {
			c.Text = strings.ReplaceAll(reindentBlock(text, strings.Repeat(f.indent, depth)), "\n", f.newline)
		} else if strings.TrimSpace(text) != "" {
			c.Text = pad(strings.TrimRight(text, " \t"), "*") + " "
		}
	case kdltext.CommentSlashdash:
		c.Text = " " + f.slashdash(strings.TrimLeft(c.Text, " \t\r\n"), depth, entry)
	}
	return c
}

// reindentBlock returns text, that of a block comment of several lines, with
// its later lines indented by indent. Like gofmt, it finds the indentation
// the lines have in common, without needing to know the column the comment
// started in, and treats what the comment's layout implies as alignment: a
// column of '*'s, text aligned after the "/*", or a body indented inside it.
// Unlike gofmt, it always writes that alignment the same way, so that
// formatting again changes nothing, whatever the indentation.
func reindentBlock(text, indent string) string {
	lines := strings.Split(text, "\n")
	n := len(lines) - 1
	first, last := lines[0], lines[n]
	closing := strings.TrimSpace(last) == "" // the "*/" is on its own line

	body := lines[1:n]
	if !closing {
		body = lines[1:]
	}
	prefix, set := "", false
	for _, line := range body {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !set {
			prefix, set = line, true
		}
		prefix = commonIndent(prefix, line)
	}

	var align, end string
	if p, _, stars := strings.Cut(prefix, "*"); stars {
		prefix, align, end = p, " ", " "
	} else if strings.TrimSpace(first) == "" {
		align = "   "
		if strings.HasSuffix(prefix, "\t") {
			align = "\t"
		}
	} else {
		space := first[:len(first)-len(strings.TrimLeft(first, " \t"))]
		align = "  " + space
		if strings.HasPrefix(space, "\t") {
			align = space
		}
	}

	out := make([]string, len(lines))
	out[0] = strings.TrimRight(first, " \t")
	for i, line := range lines[1:] {
		switch {
		case strings.TrimSpace(line) != "" && i+1 == n:
			out[i+1] = indent + align + line[len(prefix):]
		case strings.TrimSpace(line) != "":
			out[i+1] = indent + align + strings.TrimRight(line[len(prefix):], " \t")
		}
	}
	if closing {
		out[n] = indent + end
	}
	return strings.Join(out, "\n")
}

// commonIndent returns the longest common start of a and b made of spaces,
// tabs and '*'s.
func commonIndent(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] && strings.IndexByte(" \t*", a[i]) >= 0 {
		i++
	}
	return a[:i]
}

// slashdash returns text, the KDL of a slash-dashed node, or of an entry or
// children block if entry is set, laid out for the given depth. Its strings
// are written as they were read, so it changes none of their contents. Text
// that can't be read as KDL is returned as it is.
func (f *formatter) slashdash(text string, depth int, entry bool) string {
	// A line continuation before the part is only v2 at the top level, so
	// reading the part alone could take it for v2.
	if strings.HasPrefix(text, "\\") {
		return text
	}
	src := text
	if entry {
		src = "n " + text
	}
	var doc Document
	dec := kdltext.NewDecoder(strings.NewReader(src))
	if err := UnmarshalDecode(dec, &doc); err != nil || len(doc.Nodes) != 1 {
		return text
	}
	// A comment before the part would be moved to a line of its own, but
	// only KDL v2 allows a newline after a "/-".
	if doc.Nodes[0].Comment.Kind != 0 {
		return text
	}
	if n := doc.Nodes[0]; entry && len(n.Entries)+btoi(n.Children != nil) != 1 {
		return text
	}

	// A part is always written at the depth of its node, inside as many
	// parents, which are then taken off, so that the Encoder indents it
	// without touching the lines inside its strings. Its blocks are kept,
	// since one may be the whole part, and its strings aren't normalized,
	// since a bare string may not suit the version of the whole document.
	sub := *f
	sub.blocks, sub.normalize = EmptyBlockCompact, false
	nodes := doc.Nodes
	var open, end strings.Builder
	for i := range depth {
		nodes = []Node{{Name: "w", Children: nodes}}
		open.WriteString(strings.Repeat(f.indent, i) + "w {" + f.newline)
		end.WriteString(f.newline + strings.Repeat(f.indent, depth-1-i) + "}")
	}
	end.WriteString(f.newline)
	out := Document{Nodes: nodes}
	for i := range out.Nodes {
		sub.node(&out.Nodes[i], 0)
	}
	// A part that doesn't settle the version must stay valid in both. A name
	// that only KDL v2 could write bare is quoted by writing it in v1, and
	// what v1 alone allows is caught by reading it back as v2.
	version, both := dec.Version(), false
	if version == 0 {
		version, both = kdltext.V1, true
	}
	b, err := Marshal(out, kdltext.WithIndent(f.indent), kdltext.WithNewline(f.newline), kdltext.WithVersion(version))
	if err != nil || both && !readsAs(b, kdltext.V2) {
		return text
	}

	got, ok := strings.CutPrefix(string(b), open.String()+strings.Repeat(f.indent, depth))
	if !ok {
		return text
	}
	if got, ok = strings.CutSuffix(got, end.String()); !ok {
		return text
	}
	if entry {
		if got, ok = strings.CutPrefix(got, "n "); !ok {
			return text
		}
	}
	if !isOnePart(got, entry, version) {
		return text
	}
	return got
}

// isOnePart reports whether text reads as a single slash-dashed part in KDL
// version v. Laying a part out can leave a comment at its end, which is then
// read as a comment of its own.
func isOnePart(text string, entry bool, v kdltext.Version) bool {
	src := "/- " + text
	if entry {
		src = "n " + src
	}
	var doc Document
	if Unmarshal([]byte(src), &doc, kdltext.WithVersion(v)) != nil || len(doc.Nodes) != 1 {
		return false
	}
	n := doc.Nodes[0]
	if entry {
		return len(n.Entries) == 1 && n.Entries[0].IsComment() && n.Children == nil
	}
	return n.IsComment()
}

// readsAs reports whether b is a valid document in KDL version v.
func readsAs(b []byte, v kdltext.Version) bool {
	var doc Document
	return Unmarshal(b, &doc, kdltext.WithVersion(v)) == nil
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// pad returns s with a space before it, unless it is empty, already starts
// with whitespace, or starts with the character skip.
func pad(s, skip string) string {
	if s == "" || strings.HasPrefix(s, " ") || strings.HasPrefix(s, "\t") || strings.HasPrefix(s, skip) {
		return s
	}
	return " " + s
}
