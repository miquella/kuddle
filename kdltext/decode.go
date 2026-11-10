package kdltext

import (
	"bytes"
	"fmt"
	"io"

	"github.com/miquella/kuddle/internal/kdlopts"
	"github.com/miquella/kuddle/internal/lexer"
)

// Decoder reads a KDL document as a stream of tokens.
//
// The document is parsed as its tokens are read: each argument, property
// and child node when it is read, or peeked at. Nodes may be nested in up to
// 10000 children blocks, slash-dashed or not; a document nested deeper is a
// syntax error.
type Decoder struct {
	lex  *lexer.Lexer
	opts *kdlopts.Struct // the options of NewDecoder (see Options)

	// strictChildren is whether a node may have only one children block in
	// v1, slash-dashed or not (see RejectMultipleChildrenBlocks).
	strictChildren bool

	// discardComments is whether comments, and slash-dashed parts, are read
	// without being kept (see DiscardComments).
	discardComments bool

	// props holds the names of the properties of the node whose entries are
	// being parsed, if a node may not repeat a property (see
	// RejectDuplicateProperties); it is nil otherwise.
	props map[string]struct{}

	look    lexToken // the next lexer token, if hasLook
	hasLook bool
	queue   []queued // parsed tokens; those before head have been returned
	head    int
	pos     Position // position of the last token returned
	offset  int64    // input offset after the tokens returned (see InputOffset)
	doc     level    // the tokens returned at the top level (see StackIndex)
	stack   []level  // and within each node returned but not ended
	read    int64    // input offset after the last lexer token consumed, other than whitespace
	err     error    // sticky error

	frames []frame // the nodes being parsed, innermost last
	depth  int     // nesting of children blocks, including slash-dashed ones

	// semicolon is the position of the ';' that ended the node being
	// emitted, if hasSemicolon: the position of its KindEnd token, which is
	// pushed after the comments on the rest of the line are read.
	semicolon    Position
	hasSemicolon bool

	// Comments are collected as they are read, as mode says. Between nodes,
	// they are held in pending until the next node, or the end of the block,
	// decides whether the last is the node's comment. Within the line of a
	// node being emitted, they are its comment entries, which are held while
	// an entry is being read, so that they come after it. A slash-dashed part
	// is recorded in raw while slash is positive, and becomes one comment.
	lookRaw     []byte       // the raw text of look
	strs        *stringCache // shares the strings read, once there are enough to share
	strsRead    int          // the number of strings read, until strs is made
	mode        commentMode
	pending     []posComment
	joinable    bool // whether a line comment on the next line joins the last pending comment
	attached    bool // whether the last pending comment is directly above what follows
	held        []posComment
	inEntry     int // the depth of entries (or a node's name) being read
	slash       int
	slashPos    Position // the position of the outermost "/-"
	raw         []byte
	rawEnd      int          // the length of raw up to the end of its last token that isn't space or a comment
	rawComments []posComment // the comments recorded in raw after rawEnd
	lineStart   bool         // whether only whitespace has been read on this line
}

// lexToken is a token read by the lexer, detached from the lexer's buffer.
// Its raw text is kept in Decoder.lookRaw while it is the next token.
type lexToken struct {
	lexer.Token
	pos Position
	end int64  // the input offset after the token
	src string // the text of a string or number, as written
}

// String returns the decoded value of a string, and the text of any other
// token.
func (t lexToken) String() string {
	switch t.Kind {
	case lexer.Ident, lexer.Number:
		return t.src
	case lexer.String, lexer.RawString:
		s, _ := lexer.DecodeString(t.src)
		return s
	}
	return t.Token.String()
}

// commentMode is where the Decoder puts the comments it reads.
type commentMode byte

const (
	modeLine   commentMode = iota // between nodes: pending
	modeNode                      // within the line of a node being emitted: comment entries
	modeHold                      // held, to be decided later
	modeClosed                    // after the '}' of a node: comment entries if slash-dashed, else pending
)

// posComment is a comment, the position of its start, and the input offset
// after it.
type posComment struct {
	Comment
	pos Position
	end int64
}

type queued struct {
	tok Token
	pos Position
	end int64 // the input offset after the token's input
}

// level is the number of tokens at a level of the stack, and the kind of the
// last of them (see StackIndex).
type level struct {
	n    int64
	last Kind
}

// NewDecoder returns a Decoder reading from r. It accepts the options
// WithVersion, WithMaxTokenSize, RejectMultipleChildrenBlocks,
// RejectDuplicateProperties and DiscardComments.
//
// If r is a *bytes.Buffer, the Decoder reads directly from its contents,
// without copying them, and the buffer must not be written to while the
// Decoder is in use.
func NewDecoder(r io.Reader, opts ...Options) *Decoder {
	o := kdlopts.Join(opts...)
	l := lexer.New(r, lexer.Version(o.Version))
	l.SetMaxTokenSize(o.MaxTokenSize)
	d := &Decoder{
		lex:             l,
		opts:            o,
		strictChildren:  o.Flags&kdlopts.RejectMultipleChildrenBlocks != 0,
		discardComments: o.Flags&kdlopts.DiscardComments != 0,
		lineStart:       true,
	}
	if o.Flags&kdlopts.RejectDuplicateProperties != 0 {
		d.props = make(map[string]struct{})
	}
	return d
}

// ReadToken returns the next token, or io.EOF at the end of the document.
// Any other error is sticky, and is most often a *SyntacticError.
func (d *Decoder) ReadToken() (Token, error) {
	if err := d.fill(); err != nil {
		return Token{}, err
	}
	q := d.queue[d.head]
	d.head++
	d.pos = q.pos
	d.offset = max(d.offset, q.end)
	if q.tok.Kind == KindEnd {
		d.stack = d.stack[:len(d.stack)-1]
		return q.tok, nil
	}
	l := &d.doc
	if n := len(d.stack); n > 0 {
		l = &d.stack[n-1]
	}
	l.n++
	l.last = q.tok.Kind
	if q.tok.Kind == KindNode {
		d.stack = append(d.stack, level{})
	}
	return q.tok, nil
}

// PeekKind returns the kind of the next token without reading it. It returns
// KindInvalid at the end of the document or after an error, which the next
// call to ReadToken returns.
func (d *Decoder) PeekKind() Kind {
	t, _ := d.PeekToken()
	return t.Kind
}

// PeekToken returns the next token without reading it, or the error that the
// next call to ReadToken returns.
func (d *Decoder) PeekToken() (Token, error) {
	if err := d.fill(); err != nil {
		return Token{}, err
	}
	return d.queue[d.head].tok, nil
}

// Pos returns the position of the start of the last token read. For KindNode
// and KindValue tokens, this is the start of the type annotation, if any, or
// else of the name or value; for KindProp tokens, it is the start of the
// property's name, and for KindEnd tokens, the start of whatever ended the
// node.
func (d *Decoder) Pos() Position { return d.pos }

// InputOffset returns the input byte offset just past the last token read:
// past the name of a KindNode or KindProp token, the value of a KindValue
// token, the comment of a comment token, and, for a KindEnd token, the
// newline, ';' or line comment that ended the node, after the '}' of its
// children if it has them. For a node ended by the end of the input, or by
// the '}' of its parent's children, it is past the last of the node's input
// other than whitespace. The comments within an entry, or before a node's
// name, are read after it, and leave the offset past it. InputOffset is zero
// before the first token is read.
func (d *Decoder) InputOffset() int64 { return d.offset }

// StackDepth returns the number of nodes that have been started but not
// ended.
func (d *Decoder) StackDepth() int { return len(d.stack) }

// StackIndex returns the kind of the last token read at level i of the
// stack, or KindInvalid if there is none, and the number of tokens read at
// the level. Level 0 is the document, and level i, for 0 < i <=
// StackDepth, is the i-th of the nodes that have been started but not ended.
// A child node counts as one token at the level of its parent, its KindNode
// token. StackIndex panics if i is not between 0 and StackDepth, inclusive.
func (d *Decoder) StackIndex(i int) (Kind, int64) {
	l := d.doc
	if i != 0 {
		l = d.stack[i-1]
	}
	return l.last, l.n
}

// Options returns the options the Decoder was created with, including those
// that only other packages, such as kuddle, apply.
func (d *Decoder) Options() Options {
	return d.opts.Clone()
}

// Version returns the version of the document, which is zero until it has
// been decided (see WithVersion).
func (d *Decoder) Version() Version { return Version(d.lex.Version()) }

// fill parses tokens into the queue until it is non-empty.
func (d *Decoder) fill() error {
	if d.head == len(d.queue) {
		// Reuse the queue's storage once it has been returned.
		d.queue, d.head = d.queue[:0], 0
	}
	for d.err == nil && len(d.queue) == 0 {
		d.err = d.parseNext()
	}
	if len(d.queue) > 0 {
		return nil
	}
	return d.err
}

// maxNestingDepth is the deepest nesting of children blocks, slash-dashed or
// not, that a Decoder reads and an Encoder writes.
const maxNestingDepth = kdlopts.MaxNestingDepth

// frame is a node being parsed: its entries, children blocks and
// terminator, or, while a children block of the node is open, the nodes in
// the block.
type frame struct {
	name  Value
	emit  bool // whether the node's tokens are emitted, rather than slash-dashed
	slash bool // whether the node is a slash-dashed part, ended with the node
	st    nodeState
	space bool // whether whitespace precedes the next token among the node's entries

	block blockKind // the children block of the node that is open, if any
	brace lexToken  // the '{' of the open block
	end   Position  // the position of the '}' of an emitted node's children, once read
}

// blockKind is the kind of a children block being parsed.
type blockKind byte

const (
	noBlock      blockKind = iota
	emitBlock              // the children of an emitted node, which are emitted
	discardBlock           // the children of a slash-dashed node
	slashBlock             // a slash-dashed children block, a slash-dashed part of its own
)

// nodeState is the state of the entries of a node being parsed.
type nodeState struct {
	children bool // whether a (possibly slash-dashed) children block was seen
	opened   bool // whether the node's children block was seen
	closed   bool // whether the '}' of an emitted node's children has been read
}

// parseNext parses the next part of the document: the next entry, children
// block or terminator of the node being parsed, or else the next node of the
// current children block (or the document), or the end of the block. A
// node's entries are parsed one at a time, so that its tokens are returned
// as they are read, and its children by later calls once its block is open.
func (d *Decoder) parseNext() error {
	if n := len(d.frames); n > 0 && d.frames[n-1].block == noBlock {
		return d.parseRest(&d.frames[n-1])
	}

	if err := d.skipLineSpace(); err != nil {
		return err
	}
	t, err := d.peek()
	if err != nil {
		return err
	}
	var f *frame // the node whose block is open, if any
	if n := len(d.frames); n > 0 {
		f = &d.frames[n-1]
	}
	discard := f != nil && f.block != emitBlock
	switch t.Kind {
	case lexer.EOF:
		switch {
		case discard:
			return d.errorf(f.brace, "unterminated children block")
		case f != nil:
			return d.errorf(t, "unexpected end of input: missing '}' for the children of node %s", lexer.Quote(f.name.String()))
		case len(d.pending) > 0:
			d.flushPending()
			return nil
		}
		return io.EOF
	case lexer.RBrace:
		if f == nil {
			return d.errorf(t, "unexpected '}'")
		}
		d.advance()
		d.depth--
		block := f.block
		f.block, f.space = noBlock, false
		switch block {
		case slashBlock:
			d.endSlashdash()
		case emitBlock:
			d.flushPending()
			f.st = nodeState{children: true, opened: true, closed: true}
			f.end = t.pos
			d.mode = modeClosed
		}
		return nil
	case lexer.Slashdash:
		if !discard {
			d.beginSlashdash()
		}
		d.advance()
		if err := d.skipSlashdashSpace(); err != nil {
			return err
		}
		return d.startNode(false, !discard)
	}
	return d.startNode(!discard, false)
}

// startNode parses the type annotation and name of a node, and begins
// parsing the rest of it. If emit is set, its KindNode token is emitted. If
// slash is set, the node is a slash-dashed part, ended with the node.
func (d *Decoder) startNode(emit, slash bool) error {
	start, err := d.peek()
	if err != nil {
		return err
	}
	var comment Comment
	if emit {
		comment = d.takeComment()
		d.mode = modeNode
		// Comments before the name come after the node's token.
		d.inEntry++
	}
	typ, err := d.parseType()
	if err != nil {
		return err
	}
	t, err := d.peek()
	if err != nil {
		return err
	}
	if !isString(t) {
		if t.Kind == lexer.RBrace || t.Kind == lexer.EOF {
			if typ != nil {
				return d.errorf(t, "expected node name after type annotation, found %s", describe(t))
			}
			return d.errorf(t, "expected a node after '/-', found %s", describe(t))
		}
		return d.errorf(t, "expected node name, found %s", describe(t))
	}
	d.advance()
	name := valueOf(typ, t, d.lex.Version())
	if emit {
		clear(d.props)
		d.push(Token{Kind: KindNode, Value: name, Comment: comment}, start.pos, t.end)
		d.inEntry--
		d.pushHeld()
	}
	d.frames = append(d.frames, frame{name: name, emit: emit, slash: slash})
	return nil
}

// parseRest parses the next entry, children block or terminator of the node
// f. A node's children block is left open for later calls to parseNext:
// those of an emitted node to be emitted, and any other to be discarded.
func (d *Decoder) parseRest(f *frame) error {
	// The tokens after the '}' of an emitted node are not emitted.
	emit := f.emit && !f.st.closed
	more, err := d.skipNodeSpace()
	if err != nil {
		return err
	}
	f.space = f.space || more

	t, err := d.peek()
	if err != nil {
		return err
	}
	switch t.Kind {
	case lexer.Semicolon:
		d.advance()
		if emit {
			d.semicolon, d.hasSemicolon = t.pos, true
			if err := d.lineEnd(); err != nil {
				return err
			}
		}
		return d.endNode()
	case lexer.Newline, lexer.LineComment:
		d.advance()
		return d.endNode()
	case lexer.EOF:
		return d.endNode()
	case lexer.RBrace:
		// The '}' ends the parent's children, which parseNext handles.
		if d.depth == 0 {
			return d.errorf(t, "unexpected '}'")
		}
		return d.endNode()

	case lexer.LBrace:
		if f.st.opened || f.st.children && d.oneBlock() {
			return d.errorf(t, "node %s has more than one children block", lexer.Quote(f.name.String()))
		}
		f.st.children, f.st.opened = true, true
		if !emit {
			return d.openBlock(f, discardBlock)
		}
		if err := d.openBlock(f, emitBlock); err != nil {
			return err
		}
		err := d.lineEnd()
		d.mode = modeLine
		return err

	case lexer.Slashdash:
		d.beginSlashdash()
		d.advance()
		if err := d.skipSlashdashSpace(); err != nil {
			return err
		}
		next, err := d.peek()
		if err != nil {
			return err
		}
		// Without whitespace, a slash-dash may only come before a children
		// block in v1, and before anything in v2.
		if !f.space && next.Kind != lexer.LBrace && !d.lex.Use(lexer.V2) {
			return d.errorf(t, "expected whitespace before '/-'")
		}
		switch {
		case next.Kind == lexer.LBrace:
			if f.st.children && d.oneBlock() {
				return d.errorf(next, "node %s has more than one children block", lexer.Quote(f.name.String()))
			}
			f.st.children = true
			return d.openBlock(f, slashBlock)
		case f.st.children:
			return d.errorf(next, "arguments and properties of node %s must come before its children", lexer.Quote(f.name.String()))
		case !isEntryStart(next):
			return d.errorf(next, "expected an argument, property or children after '/-', found %s", describe(next))
		}
		if f.space, err = d.parseEntry(false); err != nil {
			return err
		}
		d.endSlashdashSpace()
		return nil
	}

	switch {
	case !isEntryStart(t):
		return d.errorf(t, "unexpected %s", describe(t))
	case f.st.children:
		return d.errorf(t, "arguments and properties of node %s must come before its children", lexer.Quote(f.name.String()))
	case !f.space:
		return d.errorf(t, "expected whitespace before %s", describe(t))
	}
	f.space, err = d.parseEntry(emit)
	return err
}

// openBlock reads the '{' of a children block of f, of the given kind.
func (d *Decoder) openBlock(f *frame, kind blockKind) error {
	brace, _ := d.peek()
	if d.depth == maxNestingDepth {
		return d.errorf(brace, "children blocks nested deeper than %d", maxNestingDepth)
	}
	d.advance()
	d.depth++
	f.block, f.brace = kind, brace
	return nil
}

// endNode ends the node being parsed, at its terminator.
func (d *Decoder) endNode() error {
	f := d.frames[len(d.frames)-1]
	d.frames = d.frames[:len(d.frames)-1]
	switch {
	case f.st.closed:
		d.mode = modeLine
		// Comments after the '}' on its line are not those of the next node.
		d.joinable, d.attached = false, false
		d.push(Token{Kind: KindEnd, Children: true}, f.end, d.read)
	case f.emit:
		d.mode = modeLine
		pos := d.endPos()
		if d.hasSemicolon {
			pos, d.hasSemicolon = d.semicolon, false
		}
		d.push(Token{Kind: KindEnd}, pos, d.read)
	}
	if f.slash {
		d.endSlashdashSpace()
	}
	return nil
}

// oneBlock reports whether a node may have only one children block,
// slash-dashed or not: in v1, with RejectMultipleChildrenBlocks. More than
// one is v2 syntax, so an undecided version is decided as v2.
func (d *Decoder) oneBlock() bool {
	return d.strictChildren && !d.lex.Use(lexer.V2)
}

// parseEntry parses an argument or property. It reports whether it consumed
// whitespace after the entry while looking for a property's '='.
func (d *Decoder) parseEntry(emit bool) (space bool, err error) {
	// Comments within the entry come after it, as comment entries.
	d.inEntry++
	defer func() {
		if d.inEntry--; d.inEntry == 0 && d.mode == modeNode {
			d.pushHeld()
		}
	}()
	start, err := d.peek()
	if err != nil {
		return false, err
	}
	typ, err := d.parseType()
	if err != nil {
		return false, err
	}

	t, err := d.peek()
	if err != nil {
		return false, err
	}
	if !isString(t) {
		v, err := d.parseValue(typ)
		if err == nil && emit {
			d.push(Token{Kind: KindValue, Value: v}, start.pos, d.read)
		}
		return false, err
	}

	// A string is a property name if it is followed by '='.
	d.advance()
	space, err = d.skipNodeSpace()
	if err != nil {
		return false, err
	}
	eq, err := d.peek()
	if err != nil {
		return false, err
	}
	if eq.Kind != lexer.Equals {
		if t.Kind == lexer.Ident && !d.lex.Use(lexer.V2) {
			return false, d.errorf(t, "bare identifier %s is not a value in KDL v1 (quote it)", lexer.Quote(t.String()))
		}
		if emit {
			d.push(Token{Kind: KindValue, Value: valueOf(typ, t, d.lex.Version())}, start.pos, t.end)
		}
		return space, nil
	}

	if typ != nil {
		return false, d.errorf(start, "a property's type annotation must follow the '='")
	}
	if space && !d.lex.Use(lexer.V2) {
		return false, d.errorf(eq, "whitespace before '=' is not allowed in KDL v1")
	}
	d.advance()
	if err := d.skipSpaceV2("whitespace after '='"); err != nil {
		return false, err
	}
	vstart, err := d.peek()
	if err != nil {
		return false, err
	}
	vtyp, err := d.parseType()
	if err != nil {
		return false, err
	}
	v, err := d.parseValue(vtyp)
	if err != nil {
		return false, err
	}
	if emit {
		if d.props != nil {
			name := t.String()
			if _, ok := d.props[name]; ok {
				return false, d.errorf(start, "node %s has more than one property %s", lexer.Quote(d.frames[len(d.frames)-1].name.String()), lexer.Quote(name))
			}
			d.props[name] = struct{}{}
		}
		d.push(Token{Kind: KindProp, Value: valueOf(nil, t, d.lex.Version())}, start.pos, t.end)
		d.push(Token{Kind: KindValue, Value: v}, vstart.pos, d.read)
	}
	return false, nil
}

// parseValue parses a value that follows the type annotation typ, if any.
func (d *Decoder) parseValue(typ *string) (Value, error) {
	t, err := d.peek()
	if err != nil {
		return Value{}, err
	}
	switch t.Kind {
	case lexer.Ident:
		if !d.lex.Use(lexer.V2) {
			return Value{}, d.errorf(t, "bare identifier %s is not a value in KDL v1 (quote it)", lexer.Quote(t.String()))
		}
	case lexer.String, lexer.RawString, lexer.Number, lexer.True, lexer.False, lexer.Null:
	default:
		return Value{}, d.errorf(t, "expected a value, found %s", describe(t))
	}
	d.advance()
	return valueOf(typ, t, d.lex.Version()), nil
}

// parseType parses a type annotation, if there is one, and the whitespace
// after it, returning nil if there is none.
func (d *Decoder) parseType() (*string, error) {
	t, err := d.peek()
	if err != nil || t.Kind != lexer.LParen {
		return nil, err
	}
	d.advance()
	if err := d.skipSpaceV2("whitespace in a type annotation"); err != nil {
		return nil, err
	}
	if t, err = d.peek(); err != nil {
		return nil, err
	}
	if !isString(t) {
		return nil, d.errorf(t, "expected a type name, found %s", describe(t))
	}
	d.advance()
	if err := d.skipSpaceV2("whitespace in a type annotation"); err != nil {
		return nil, err
	}
	end, err := d.peek()
	if err != nil {
		return nil, err
	}
	if end.Kind != lexer.RParen {
		return nil, d.errorf(end, "expected ')' to end the type annotation, found %s", describe(end))
	}
	d.advance()
	if err := d.skipSpaceV2("whitespace after a type annotation"); err != nil {
		return nil, err
	}
	return new(t.String()), nil
}

// skipNodeSpace skips whitespace, block comments and line continuations,
// reporting whether there were any.
func (d *Decoder) skipNodeSpace() (bool, error) {
	skipped := false
	for {
		t, err := d.peek()
		if err != nil {
			return false, err
		}
		switch t.Kind {
		case lexer.Whitespace, lexer.BlockComment:
			d.advance()
		case lexer.Escline:
			d.advance()
			if err := d.skipEscline(t); err != nil {
				return false, err
			}
		default:
			return skipped, nil
		}
		skipped = true
	}
}

// skipEscline skips the rest of a line continuation after the '\'.
func (d *Decoder) skipEscline(esc lexToken) error {
	comment := false // whether the line ends in a line comment
	for {
		t, err := d.peek()
		if err != nil {
			return err
		}
		switch t.Kind {
		case lexer.Whitespace, lexer.BlockComment:
			d.advance()
		case lexer.LineComment:
			d.advance()
			comment = true
		case lexer.Newline:
			d.advance()
			return nil
		case lexer.EOF:
			// v1 allows only a line comment to end the input.
			if comment || d.lex.Use(lexer.V2) {
				return nil
			}
			fallthrough
		default:
			return d.errorf(t, "expected a newline after '\\', found %s", describe(t))
		}
	}
}

// skipSpaceV2 skips node space, which is only allowed in KDL v2 where what
// describes it occurs.
func (d *Decoder) skipSpaceV2(what string) error {
	t, err := d.peek()
	if err != nil {
		return err
	}
	space, err := d.skipNodeSpace()
	if err == nil && space && !d.lex.Use(lexer.V2) {
		return d.errorf(t, "%s is not allowed in KDL v1", what)
	}
	return err
}

// skipLineSpace skips whitespace, newlines and comments between nodes.
func (d *Decoder) skipLineSpace() error {
	for {
		t, err := d.peek()
		if err != nil {
			return err
		}
		switch t.Kind {
		case lexer.Whitespace, lexer.BlockComment, lexer.Newline, lexer.LineComment:
			d.advance()
		case lexer.Escline:
			// Only KDL v2 has line continuations between nodes.
			if !d.lex.Use(lexer.V2) {
				return d.errorf(t, "unexpected line continuation between nodes")
			}
			d.advance()
			if err := d.skipEscline(t); err != nil {
				return err
			}
		default:
			return nil
		}
	}
}

// skipSlashdashSpace skips the space after a '/-', which may only span lines
// in KDL v2.
func (d *Decoder) skipSlashdashSpace() error {
	for {
		if _, err := d.skipNodeSpace(); err != nil {
			return err
		}
		t, err := d.peek()
		if err != nil {
			return err
		}
		if t.Kind != lexer.Newline && t.Kind != lexer.LineComment {
			return nil
		}
		if !d.lex.Use(lexer.V2) {
			return d.errorf(t, "a newline after '/-' is not allowed in KDL v1")
		}
		d.advance()
	}
}

// peek returns the next lexer token without consuming it.
func (d *Decoder) peek() (lexToken, error) {
	if d.hasLook {
		return d.look, nil
	}
	t, err := d.lex.NextToken()
	if err != nil {
		return lexToken{}, syntaxError(err)
	}
	d.lookRaw = append(d.lookRaw[:0], t.Raw()...)
	var src string
	switch t.Kind {
	case lexer.Ident, lexer.String, lexer.RawString:
		src = d.shareString(d.lookRaw)
	case lexer.Number:
		// Numbers are rarely repeated.
		src = string(d.lookRaw)
	}
	pos := Position(d.lex.Pos())
	// The text of other tokens is fixed, or only needed raw.
	d.look, d.hasLook = lexToken{lexer.Token{Kind: t.Kind}, pos, pos.Offset + int64(len(d.lookRaw)), src}, true
	return d.look, nil
}

// advance consumes the token returned by peek, collecting it if it is a
// comment.
func (d *Decoder) advance() {
	d.hasLook = false
	k := d.look.Kind
	if k != lexer.Whitespace {
		d.read = d.look.end
	}
	switch {
	case d.discardComments && (d.slash > 0 || k == lexer.LineComment || k == lexer.BlockComment):
		// Nothing is kept: not the text of a comment or slash-dashed part.
	case d.slash > 0:
		d.raw = append(d.raw, d.lookRaw...)
		switch k {
		case lexer.Whitespace, lexer.Newline, lexer.Escline:
		case lexer.LineComment:
			d.rawComments = append(d.rawComments, posComment{Comment{Kind: CommentLine, Text: lineText(d.lookRaw)}, d.look.pos, d.look.end})
		case lexer.BlockComment:
			d.rawComments = append(d.rawComments, posComment{Comment{Kind: CommentBlock, Text: blockText(d.lookRaw)}, d.look.pos, d.look.end})
		default:
			d.rawEnd, d.rawComments = len(d.raw), d.rawComments[:0]
		}
	case k == lexer.LineComment:
		d.addComment(Comment{Kind: CommentLine, Text: lineText(d.lookRaw)}, d.look.pos, d.look.end)
	case k == lexer.BlockComment:
		d.addComment(Comment{Kind: CommentBlock, Text: blockText(d.lookRaw)}, d.look.pos, d.look.end)
	case k == lexer.Newline && d.lineStart && d.mode == modeLine:
		// A blank line ends a comment, and separates it from what follows.
		d.joinable, d.attached = false, false
	}
	switch k {
	case lexer.Newline:
		d.lineStart = true
	case lexer.Whitespace:
	default:
		d.lineStart = false
	}
}

// addComment adds a comment read at pos, which ends at the offset end, as the
// mode says.
func (d *Decoder) addComment(c Comment, pos Position, end int64) {
	switch {
	case d.mode == modeLine, d.mode == modeClosed && c.Kind != CommentSlashdash:
		d.addPending(c, pos, end)
	case d.mode == modeHold || d.inEntry > 0:
		d.held = append(d.held, posComment{c, pos, end})
	default:
		d.push(Token{Kind: KindEntryComment, Comment: c}, pos, end)
	}
}

// addPending adds a comment read between nodes. A line comment directly below
// another joins it.
func (d *Decoder) addPending(c Comment, pos Position, end int64) {
	if n := len(d.pending); n > 0 && d.joinable && c.Kind == CommentLine {
		d.pending[n-1].Text += "\n" + c.Text
		d.pending[n-1].end = end
	} else {
		d.pending = append(d.pending, posComment{c, pos, end})
	}
	d.joinable = c.Kind == CommentLine
	d.attached = true
}

// takeComment returns the comment of the node that starts next: the last
// pending comment, if it is directly above the node and not slash-dashed.
// The other pending comments become comment nodes before it.
func (d *Decoder) takeComment() Comment {
	var c Comment
	if n := len(d.pending); n > 0 && d.attached && d.pending[n-1].Kind != CommentSlashdash {
		c = d.pending[n-1].Comment
		d.pending = d.pending[:n-1]
	}
	d.flushPending()
	return c
}

// flushPending emits the pending comments as comment nodes.
func (d *Decoder) flushPending() {
	for _, c := range d.pending {
		d.push(Token{Kind: KindComment, Comment: c.Comment}, c.pos, c.end)
	}
	d.pending = d.pending[:0]
	d.joinable, d.attached = false, false
}

// pushHeld emits the held comments as comment entries.
func (d *Decoder) pushHeld() {
	for _, c := range d.held {
		d.push(Token{Kind: KindEntryComment, Comment: c.Comment}, c.pos, c.end)
	}
	d.held = d.held[:0]
}

// beginSlashdash starts recording a slash-dashed part, whose "/-" is the next
// token. A slash-dashed part within one is recorded as part of it.
func (d *Decoder) beginSlashdash() {
	if d.slash == 0 {
		d.raw, d.rawEnd, d.rawComments = d.raw[:0], 0, d.rawComments[:0]
		d.slashPos = d.look.pos
	}
	d.slash++
}

func (d *Decoder) endSlashdash() {
	if d.slash--; d.slash == 0 && !d.discardComments {
		end := d.slashPos.Offset + int64(len(bytes.TrimRightFunc(d.raw, isSpace)))
		d.addComment(Comment{Kind: CommentSlashdash, Text: slashdashText(d.raw)}, d.slashPos, end)
	}
}

// endSlashdashSpace ends a slash-dashed entry or node, without the space read
// after it, such as while looking for a property's '='. A line continuation
// left in the text would swallow what follows it when written. The comments
// in that space are read as if the part had ended before it, as they are
// after an entry that isn't a string.
func (d *Decoder) endSlashdashSpace() {
	if d.slash > 1 {
		d.endSlashdash()
		return
	}
	d.raw = d.raw[:d.rawEnd]
	d.endSlashdash()
	for _, c := range d.rawComments {
		d.addComment(c.Comment, c.pos, c.end)
	}
}

// lineEnd reads the comments after the '{' or ';' of a node being emitted,
// which are its comment entries if they end the line. If something else
// follows on the line, they are left for it.
func (d *Decoder) lineEnd() error {
	d.mode = modeHold
	defer func() { d.mode = modeNode }()
	for {
		t, err := d.peek()
		if err != nil {
			return err
		}
		switch t.Kind {
		case lexer.Whitespace, lexer.BlockComment:
			d.advance()
			continue
		case lexer.LineComment:
			d.advance()
		case lexer.Newline, lexer.EOF:
		default:
			for _, c := range d.held {
				d.addPending(c.Comment, c.pos, c.end)
			}
			d.held = d.held[:0]
			return nil
		}
		d.pushHeld()
		return nil
	}
}

// lineText returns the text of the line comment raw: all of it after the
// "//".
func lineText(raw []byte) string { return string(raw[2:]) }

// blockText returns the text of the block comment raw: all of it between the
// "/*" and "*/".
func blockText(raw []byte) string { return string(raw[2 : len(raw)-2]) }

// slashdashText returns the text of the slash-dashed part raw: all of it
// after the "/-", up to the end of the part.
func slashdashText(raw []byte) string {
	return string(bytes.TrimRightFunc(raw[2:], isSpace))
}

// isSpace reports whether r is whitespace or a newline. A BOM is too, since
// it may start the document.
func isSpace(r rune) bool { return lexer.IsSpace(r) || lexer.IsNewline(r) || r == '\uFEFF' }

// push adds t, which starts at pos and whose input ends at the offset end, to
// the queue.
func (d *Decoder) push(t Token, pos Position, end int64) {
	d.queue = append(d.queue, queued{t, pos, end})
}

// endPos returns the position of the end of the last node parsed: the start of
// its terminator, or of the token after it.
func (d *Decoder) endPos() Position {
	if d.hasLook {
		return d.look.pos
	}
	return Position(d.lex.Pos())
}

func (d *Decoder) errorf(t lexToken, format string, args ...any) error {
	return &SyntacticError{Position: t.pos, Err: fmt.Errorf(format, args...)}
}

func isString(t lexToken) bool {
	return t.Kind == lexer.Ident || t.Kind == lexer.String || t.Kind == lexer.RawString
}

// isEntryStart reports whether t can start an argument or property.
func isEntryStart(t lexToken) bool {
	switch t.Kind {
	case lexer.Ident, lexer.String, lexer.RawString, lexer.Number,
		lexer.True, lexer.False, lexer.Null, lexer.LParen:
		return true
	}
	return false
}

// describe describes a token for an error message.
func describe(t lexToken) string {
	switch t.Kind {
	case lexer.EOF:
		return "end of input"
	case lexer.Newline:
		return "newline"
	case lexer.Whitespace:
		return "whitespace"
	case lexer.LineComment, lexer.BlockComment:
		return "comment"
	case lexer.Ident:
		return "identifier " + lexer.Quote(t.String())
	case lexer.String, lexer.RawString:
		s := t.String()
		if r := []rune(s); len(r) > 20 {
			s = string(r[:20]) + "..."
		}
		return "string " + lexer.Quote(s)
	case lexer.Number:
		return "number " + t.String()
	case lexer.True, lexer.False, lexer.Null:
		return "keyword " + t.String()
	}
	return "'" + t.String() + "'"
}
