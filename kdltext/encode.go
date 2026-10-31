package kdltext

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/miquella/kuddle/internal/kdlopts"
	"github.com/miquella/kuddle/internal/lexer"
)

// Encoder writes a stream of tokens as a KDL document.
//
// Each node is written on its own line, with its children indented inside
// braces, or as {} if it has an empty children block (see Token.Children).
// Strings, names and numbers read by a Decoder are written as they were read,
// if the version being written allows that form. Others are written as bare
// identifiers where the version allows, and as quoted strings otherwise (see
// QuoteIdentifiers); one that is not valid UTF-8 is an error. Comments are
// written where they belong (see the package documentation), with their text
// as it is, other than the newlines of block comments (see WithNewline). To change the style of a document, see the Format method of kuddle.Document.
//
// Output is buffered until each top-level node ends. Nodes may be nested in
// up to 10000 children blocks; a node nested deeper is an error.
type Encoder struct {
	w               io.Writer
	opts            *kdlopts.Struct // the options of NewEncoder (see Options)
	buf             []byte
	flushed         int64 // the number of bytes written to w
	version         Version
	indent          string
	discardComments bool   // DiscardComments
	newline         string // WithNewline
	newlineSet      bool   // whether WithNewline was given
	quoteIDs        bool   // QuoteIdentifiers

	// props holds the names of the properties of the current node, if a node
	// may not repeat a property (see RejectDuplicateProperties); it is nil
	// otherwise.
	props map[string]struct{}

	open []openNode
	doc  level       // the tokens written at the top level
	last CommentKind // the kind of the comment node just written, if any
	err  error       // sticky error
}

// openNode is a node that has been started but not ended.
type openNode struct {
	level
	opened bool      // whether its children block has been written
	after  []Comment // its comment entries after its children
	line   string    // the text of a line comment entry to be written
	inLine bool      // whether there is one
	cont   bool      // whether a line continuation was just written
	prop   Value     // the name of the property whose value is next, if inProp
	inProp bool
}

// errTooDeep is the error for a children block nested deeper than
// maxNestingDepth.
var errTooDeep = fmt.Errorf("children blocks nested deeper than %d", maxNestingDepth)

// space writes the space before the next part of o's line.
func (e *Encoder) space(o *openNode) {
	if !o.cont {
		e.buf = append(e.buf, ' ')
	}
	o.cont = false
}

// NewEncoder returns an Encoder writing to w. It accepts the options
// WithVersion, WithIndent, WithNewline, DiscardComments, QuoteIdentifiers and
// RejectDuplicateProperties.
func NewEncoder(w io.Writer, opts ...Options) *Encoder {
	o := kdlopts.Join(opts...)
	e := &Encoder{
		w:               w,
		opts:            o,
		version:         V2,
		indent:          "    ",
		newline:         "\n",
		discardComments: o.Flags&kdlopts.DiscardComments != 0,
		quoteIDs:        o.Flags&kdlopts.QuoteIdentifiers != 0,
	}
	if o.Present&kdlopts.Version != 0 {
		e.version = Version(o.Version)
	}
	if o.Present&kdlopts.Indent != 0 {
		e.indent = o.Indent
	}
	if o.Present&kdlopts.Newline != 0 {
		e.newline, e.newlineSet = o.Newline, true
	}
	if o.Flags&kdlopts.RejectDuplicateProperties != 0 {
		e.props = make(map[string]struct{})
	}
	return e
}

// WriteToken writes the next token. The tokens must form a valid stream (see
// the package documentation). Errors are sticky.
func (e *Encoder) WriteToken(t Token) error {
	if e.err != nil {
		return e.err
	}
	depth := len(e.open)
	if err := e.write(t); err != nil {
		e.err = fmt.Errorf("kdltext: %w", err)
		return e.err
	}
	if t.Kind != KindEnd {
		l := &e.doc
		if depth > 0 {
			l = &e.open[depth-1].level
		}
		l.n++
		l.last = t.Kind
	}
	return nil
}

// Options returns the options the Encoder was created with, including those
// that only other packages, such as kuddle, apply.
func (e *Encoder) Options() Options {
	return e.opts.Clone()
}

// StackDepth returns the number of nodes that have been started but not
// ended.
func (e *Encoder) StackDepth() int { return len(e.open) }

// StackIndex returns the kind of the last token written at level i of the
// stack, or KindInvalid if there is none, and the number of tokens written
// at the level. Level 0 is the document, and level i, for 0 < i <=
// StackDepth, is the i-th of the nodes that have been started but not ended.
// A child node counts as one token at the level of its parent, its KindNode
// token. StackIndex panics if i is not between 0 and StackDepth, inclusive.
func (e *Encoder) StackIndex(i int) (Kind, int64) {
	l := e.doc
	if i != 0 {
		l = e.open[i-1].level
	}
	return l.last, l.n
}

// OutputOffset returns the number of bytes written so far, including those
// that are buffered.
func (e *Encoder) OutputOffset() int64 { return e.flushed + int64(len(e.buf)) }

func (e *Encoder) write(t Token) error {
	if n := len(e.open); n > 0 && e.open[n-1].inProp && t.Kind != KindValue {
		return fmt.Errorf("%s after property %s, before its value", t.Kind, lexer.Quote(e.open[n-1].prop.String()))
	}
	if e.discardComments {
		switch t.Kind {
		case KindComment, KindEntryComment:
			return e.checkEntry(t)
		case KindNode:
			t.Comment = Comment{}
		}
	}
	switch t.Kind {
	case KindNode:
		if t.Comment.Kind == CommentSlashdash {
			return errors.New("a node's comment can't be slash-dashed")
		}
		if len(e.open) > maxNestingDepth {
			return errTooDeep
		}
		e.openChildren()
		e.separate(t.Comment.Kind, true)
		e.last = 0
		if t.Comment.Kind != 0 {
			if err := e.writeLines(t.Comment); err != nil {
				return err
			}
		}
		e.writeIndent(len(e.open))
		if t.Value.Kind() != ValueString {
			return errors.New("a node's name must be a string")
		}
		if t.Value.Type != nil {
			if err := e.writeType(*t.Value.Type); err != nil {
				return err
			}
		}
		if err := e.writeStringValue(t.Value, !e.quoteIDs); err != nil {
			return err
		}
		e.open = append(e.open, openNode{})
		clear(e.props)

	case KindValue, KindProp, KindEntryComment:
		if err := e.checkEntry(t); err != nil {
			return err
		}
		o := &e.open[len(e.open)-1]
		if t.Kind == KindProp {
			if t.Value.Kind() != ValueString {
				return errors.New("a property's name must be a string")
			}
			if t.Value.Type != nil {
				return errors.New("a property's name can't have a type annotation")
			}
			name := t.Name()
			if e.props != nil {
				if _, ok := e.props[name]; ok {
					return fmt.Errorf("node has more than one property %s", lexer.Quote(name))
				}
				e.props[name] = struct{}{}
			}
			o.prop, o.inProp = t.Value, true
			return nil
		}
		var name Value
		if o.inProp {
			t, name = Token{Kind: KindProp, Value: t.Value}, o.prop
			o.inProp = false
		}
		switch {
		case t.Kind == KindEntryComment && o.opened:
			// Written after the '}'.
			o.after = append(o.after, t.Comment)
		default:
			return e.writeEntry(o, t, name)
		}

	case KindComment:
		if err := e.checkComment(t.Comment, false); err != nil {
			return err
		}
		e.openChildren()
		e.separate(t.Comment.Kind, false)
		if err := e.writeLines(t.Comment); err != nil {
			return err
		}
		e.last = t.Comment.Kind
		if len(e.open) == 0 {
			return e.flush()
		}

	case KindEnd:
		n := len(e.open)
		if n == 0 {
			return errors.New("end outside of a node")
		}
		o := &e.open[n-1]
		switch {
		case !t.Children || o.opened:
		case n > maxNestingDepth:
			return errTooDeep
		case !o.inLine:
			e.space(o)
			e.buf = append(e.buf, "{}"...)
		default:
			e.openChildren()
		}
		if o.opened {
			e.writeIndent(n - 1)
			e.buf = append(e.buf, '}')
			for _, c := range o.after {
				e.continueLine(o)
				e.writeEntryComment(o, c)
			}
		}
		if o.inLine {
			e.space(o)
			e.buf = append(e.buf, "//"...)
			e.buf = append(e.buf, o.line...)
		}
		e.buf = append(e.buf, e.newline...)
		e.open = e.open[:n-1]
		e.last = 0
		if len(e.open) == 0 {
			return e.flush()
		}

	default:
		return fmt.Errorf("invalid token kind %v", t.Kind)
	}
	return nil
}

// checkEntry reports whether the entry or comment token t can be written
// where it is.
func (e *Encoder) checkEntry(t Token) error {
	n := len(e.open)
	switch {
	case t.Kind == KindComment:
		return e.checkComment(t.Comment, false)
	case n == 0:
		return fmt.Errorf("%s outside of a node", t.Kind)
	case t.Kind == KindEntryComment:
		return e.checkComment(t.Comment, true)
	case e.open[n-1].opened:
		return fmt.Errorf("%s after the node's children", t.Kind)
	}
	return nil
}

// writeEntry writes the entry t of o, before its children. A KindProp token
// holds the property's value, and name its name.
func (e *Encoder) writeEntry(o *openNode, t Token, name Value) error {
	e.continueLine(o)
	if t.Kind == KindEntryComment {
		e.writeEntryComment(o, t.Comment)
		return nil
	}
	e.space(o)
	if t.Kind == KindProp {
		if err := e.writeStringValue(name, !e.quoteIDs); err != nil {
			return err
		}
		e.buf = append(e.buf, '=')
	}
	return e.writeValue(t.Value)
}

// separate writes a blank line after the comment node just written, if any,
// where it would otherwise be read with what follows: a comment of kind next,
// which is a node's if node is set. Line comments would be joined, and a
// node without a comment would take the comment node as its own.
func (e *Encoder) separate(next CommentKind, node bool) {
	switch {
	case e.last == CommentLine && next == CommentLine,
		node && next == 0 && (e.last == CommentLine || e.last == CommentBlock):
		e.buf = append(e.buf, e.newline...)
	}
}

// openChildren writes the start of the current node's children block, if it
// has not been written, ending the node's line.
func (e *Encoder) openChildren() {
	n := len(e.open)
	if n == 0 || e.open[n-1].opened {
		return
	}
	o := &e.open[n-1]
	e.space(o)
	e.buf = append(e.buf, '{')
	// A line comment that ends the line goes after the '{'.
	if o.inLine {
		e.buf = append(e.buf, " //"...)
		e.buf = append(e.buf, o.line...)
		o.inLine = false
	}
	e.buf = append(e.buf, e.newline...)
	o.opened = true
	e.last = 0
}

// continueLine writes the line comment entry of o, if there is one to be
// written, with a line continuation, as something follows it on the line.
func (e *Encoder) continueLine(o *openNode) {
	if !o.inLine {
		return
	}
	e.space(o)
	e.buf = append(e.buf, `\ //`...)
	e.buf = append(e.buf, o.line...)
	e.buf = append(e.buf, e.newline...)
	e.writeIndent(len(e.open))
	o.inLine, o.cont = false, true
}

// writeEntryComment writes the comment entry c of o. A line comment is held
// until what follows it is known, since it ends the line.
func (e *Encoder) writeEntryComment(o *openNode, c Comment) {
	switch c.Kind {
	case CommentLine:
		o.line, o.inLine = c.Text, true
	case CommentBlock:
		e.space(o)
		e.appendBlock(c.Text)
	case CommentSlashdash:
		e.space(o)
		e.buf = append(e.buf, "/-"...)
		e.buf = append(e.buf, c.Text...)
	}
}

// writeLines writes c on lines of its own, in the current block.
func (e *Encoder) writeLines(c Comment) error {
	if err := e.checkComment(c, false); err != nil {
		return err
	}
	depth := len(e.open)
	switch c.Kind {
	case CommentLine:
		for line := range strings.SplitSeq(c.Text, "\n") {
			e.writeIndent(depth)
			e.buf = append(e.buf, "//"...)
			e.buf = append(e.buf, strings.TrimSuffix(line, "\r")...)
			e.buf = append(e.buf, e.newline...)
		}
	case CommentBlock:
		e.writeIndent(depth)
		e.appendBlock(c.Text)
		e.buf = append(e.buf, e.newline...)
	case CommentSlashdash:
		e.writeIndent(depth)
		e.buf = append(e.buf, "/-"...)
		e.buf = append(e.buf, c.Text...)
		e.buf = append(e.buf, e.newline...)
	}
	return nil
}

// appendBlock appends a block comment of text s.
func (e *Encoder) appendBlock(s string) {
	if e.newlineSet {
		s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
		s = strings.ReplaceAll(s, "\n", e.newline)
	}
	e.buf = append(e.buf, "/*"...)
	e.buf = append(e.buf, s...)
	e.buf = append(e.buf, "*/"...)
}

// checkComment reports whether c can be written, within a line if inline is
// set and on lines of its own otherwise.
func (e *Encoder) checkComment(c Comment, inline bool) error {
	if !utf8.ValidString(c.Text) {
		return errors.New("comment is not valid UTF-8")
	}
	// Only v2 disallows characters, such as U+0001, in comments.
	if i := strings.IndexFunc(c.Text, lexer.IsDisallowed); i >= 0 && e.version == V2 {
		r, _ := utf8.DecodeRuneInString(c.Text[i:])
		return fmt.Errorf("comment contains the disallowed character %U", r)
	}
	switch c.Kind {
	case CommentLine:
		// On lines of its own, each line of the text is a line comment.
		text := c.Text
		if !inline {
			text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", ""), "\n", "")
		}
		// VT is a newline only in v2.
		if strings.ContainsFunc(text, func(r rune) bool { return lexer.IsNewline(r) && (r != '\v' || e.version == V2) }) {
			return fmt.Errorf("line comment %q can't span lines here", c.Text)
		}
	case CommentBlock:
		if !e.lexesAs("/*"+c.Text+"*/", lexer.BlockComment) {
			return fmt.Errorf("block comment %q is not valid: its /* and */ must be balanced", c.Text)
		}
	case CommentSlashdash:
		doc := "/-" + c.Text
		if inline {
			doc = "n " + doc
		}
		if !e.isCommentedOut(doc, inline) {
			return fmt.Errorf("slash-dashed %q is not valid KDL here", c.Text)
		}
	default:
		return fmt.Errorf("invalid comment kind %q", c.Kind)
	}
	return nil
}

func (e *Encoder) lexesAs(s string, k lexer.Kind) bool {
	l := lexer.New(bytes.NewBufferString(s), lexer.Version(e.version))
	t, err := l.NextToken()
	if err != nil || t.Kind != k {
		return false
	}
	t, err = l.NextToken()
	return err == nil && t.Kind == lexer.EOF
}

// isCommentedOut reports whether the document doc is a single slash-dashed
// part: of the document, or of its single node n if inline is set.
func (e *Encoder) isCommentedOut(doc string, inline bool) bool {
	d := NewDecoder(bytes.NewBufferString(doc), WithVersion(e.version))
	var toks []Token
	for {
		t, err := d.ReadToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return false
		}
		toks = append(toks, t)
	}
	if !inline {
		return len(toks) == 1 && toks[0].Kind == KindComment && toks[0].Comment.Kind == CommentSlashdash
	}
	return len(toks) == 3 && toks[1].Kind == KindEntryComment && toks[1].Comment.Kind == CommentSlashdash
}

func (e *Encoder) flush() error {
	n, err := e.w.Write(e.buf)
	e.flushed += int64(n)
	e.buf = e.buf[:0]
	return err
}

func (e *Encoder) writeIndent(depth int) {
	for range depth {
		e.buf = append(e.buf, e.indent...)
	}
}

func (e *Encoder) writeType(typ string) error {
	e.buf = append(e.buf, '(')
	if err := e.writeString(typ, !e.quoteIDs); err != nil {
		return err
	}
	e.buf = append(e.buf, ')')
	return nil
}

func (e *Encoder) writeValue(v Value) error {
	if v.Type != nil {
		if err := e.writeType(*v.Type); err != nil {
			return err
		}
	}
	switch v.Kind() {
	case ValueString:
		// Bare identifiers are only values in KDL v2.
		return e.writeStringValue(v, e.version == V2)
	case ValueNumber:
		s := v.String()
		if strings.HasPrefix(s, "#") && e.version == V1 {
			return fmt.Errorf("%s can't be written in KDL v1", s)
		}
		e.buf = append(e.buf, s...)
	case ValueBool, ValueNull:
		s := v.String()
		if e.version == V1 {
			s = s[1:] // without the '#'
		}
		e.buf = append(e.buf, s...)
	default:
		return errors.New("invalid value")
	}
	return nil
}

// writeStringValue writes the string v as it was read, if that form can be
// used here, and as writeString writes its contents otherwise.
func (e *Encoder) writeStringValue(v Value, bare bool) error {
	if v.source && (v.ver == 0 || Version(v.ver) == e.version) && (v.kind != lexer.Ident || bare) {
		e.buf = append(e.buf, v.text...)
		return nil
	}
	return e.writeString(v.String(), bare)
}

// writeString writes s as a bare identifier if bare is set and s is a valid
// one, and as a quoted string otherwise.
func (e *Encoder) writeString(s string, bare bool) error {
	if bare && lexer.IsIdentifier(s, lexer.Version(e.version)) {
		e.buf = append(e.buf, s...)
		return nil
	}
	start := len(e.buf)
	var err error
	if e.buf, err = lexer.AppendQuote(e.buf, s); err != nil {
		return fmt.Errorf("%w in %s", err, e.buf[start:])
	}
	return nil
}
