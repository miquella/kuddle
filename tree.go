package kuddle

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"

	"github.com/miquella/kuddle/internal/kdlopts"
	"github.com/miquella/kuddle/internal/lexer"
	"github.com/miquella/kuddle/kdltext"
)

// From sets d to the document for in, as Marshal would write it. Rather than
// writing KDL text, From copies the data of in into d directly, except for
// the values of types implementing Marshaler, whose MarshalKDL methods write
// KDL text that From reads into d. The options of kdltext.NewEncoder, which
// concern KDL text, do not apply. Unlike Marshal, From does not check that
// the comments and values of d can be written as KDL text. If From returns
// an error, d is unchanged.
func (d *Document) From(in any, opts ...Options) error {
	w := &treeWriter{levels: make([]tokenCount, 1), opts: kdlopts.Join(opts...)}
	if err := marshal(w, in); err != nil {
		return err
	}
	d.Nodes = clone(w.b.nodes)
	return nil
}

// To decodes d into out, which must be a non-nil pointer, as Unmarshal would
// decode the KDL text of d. Rather than reading KDL text, To copies the data
// of d into out directly, except for the values of types implementing
// Unmarshaler, whose UnmarshalKDL methods read their part of d as KDL text.
// The option that applies is DiscardUnknown. Since d has no positions in an
// input, the Pos of a SemanticError from To is zero.
func (d *Document) To(out any, opts ...Options) error {
	r := &treeReader{levels: []treeLevel{{children: d.Nodes}}, opts: kdlopts.Join(opts...)}
	return unmarshal(r, out)
}

// tokenReader is a stream of tokens that a decoder reads: a *kdltext.Decoder,
// or a *treeReader. Its methods are those of kdltext.Decoder.
type tokenReader interface {
	ReadToken() (kdltext.Token, error)
	PeekKind() kdltext.Kind
	PeekToken() (kdltext.Token, error)
	Pos() kdltext.Position
	StackDepth() int
	StackIndex(int) (kdltext.Kind, int64)
	Options() Options
}

// tokenWriter is a stream of tokens that an encoder writes: a
// *kdltext.Encoder, or a *treeWriter. Its methods are those of
// kdltext.Encoder.
type tokenWriter interface {
	WriteToken(kdltext.Token) error
	StackDepth() int
	StackIndex(int) (kdltext.Kind, int64)
	Options() Options
}

// tokenCount is the number of tokens at a level of a stream, and the kind of
// the last of them, as kdltext.Decoder.StackIndex reports them.
type tokenCount struct {
	n    int64
	last kdltext.Kind
}

// treeReader reads the nodes of a Document as a stream of tokens, as a
// kdltext.Decoder would read the Document's KDL text, but for the comment
// entries after a node's children, which come before them, and for
// positions, which are zero.
type treeReader struct {
	levels []treeLevel     // the document, and the nodes started and not ended
	err    error           // errTooDeep, once a node nested too deeply is next
	opts   *kdlopts.Struct // the options of To
}

// errTooDeep is the error for a children block nested deeper than a
// kdltext.Decoder reads.
var errTooDeep = fmt.Errorf("kuddle: children blocks nested deeper than %d", kdlopts.MaxNestingDepth)

// treeLevel is a level of a treeReader: the document, or a node, whose
// entries and children are read in order.
type treeLevel struct {
	tokenCount
	node     *Node // nil for the document
	entries  []Entry
	children []Node
	entry    int  // the index of the next entry
	inProp   bool // whether the KindProp token of the next entry has been read
	child    int  // the index of the next child
}

func (r *treeReader) top() *treeLevel { return &r.levels[len(r.levels)-1] }

// next returns the next token, which shares its type annotations with the
// tree, or the zero Token at the end of the document.
func (r *treeReader) next() kdltext.Token {
	l := r.top()
	if l.entry < len(l.entries) {
		e := &l.entries[l.entry]
		switch {
		case e.IsComment():
			return kdltext.Token{Kind: kdltext.KindEntryComment, Comment: e.Comment}
		case e.IsProp() && !l.inProp:
			return kdltext.Token{Kind: kdltext.KindProp, Value: kdltext.String(e.Name)}
		}
		return kdltext.Token{Kind: kdltext.KindValue, Value: e.Value}
	}
	if l.child < len(l.children) {
		n := &l.children[l.child]
		if n.IsComment() {
			return kdltext.Token{Kind: kdltext.KindComment, Comment: n.Comment}
		}
		// As a Decoder would, stop at a children block nested too deeply.
		if n.Children != nil && len(r.levels) > kdlopts.MaxNestingDepth {
			r.err = errTooDeep
			return kdltext.Token{}
		}
		return kdltext.Token{Kind: kdltext.KindNode, Value: kdltext.String(n.Name).WithType(n.Type), Comment: n.Comment}
	}
	if len(r.levels) > 1 {
		return kdltext.Token{Kind: kdltext.KindEnd, Children: l.node.Children != nil}
	}
	return kdltext.Token{}
}

// advance moves past the next token, of kind k.
func (r *treeReader) advance(k kdltext.Kind) {
	l := r.top()
	var child *Node
	switch k {
	case kdltext.KindEnd:
		r.levels = r.levels[:len(r.levels)-1]
		return
	case kdltext.KindProp:
		l.inProp = true
	case kdltext.KindValue, kdltext.KindEntryComment:
		l.entry++
		l.inProp = false
	case kdltext.KindNode:
		child = &l.children[l.child]
		l.child++
	case kdltext.KindComment:
		l.child++
	}
	l.n++
	l.last = k
	if child != nil {
		r.levels = append(r.levels, treeLevel{node: child, entries: child.Entries, children: child.Children})
	}
}

func (r *treeReader) ReadToken() (kdltext.Token, error) {
	t := r.next()
	if t.Kind == kdltext.KindInvalid {
		return t, r.end()
	}
	r.advance(t.Kind)
	return t.Clone(), nil
}

func (r *treeReader) PeekKind() kdltext.Kind { return r.next().Kind }

func (r *treeReader) PeekToken() (kdltext.Token, error) {
	t := r.next()
	if t.Kind == kdltext.KindInvalid {
		return t, r.end()
	}
	return t.Clone(), nil
}

// end returns the error for the end of the tokens: io.EOF, or errTooDeep.
func (r *treeReader) end() error {
	if r.err != nil {
		return r.err
	}
	return io.EOF
}

func (r *treeReader) Pos() kdltext.Position { return kdltext.Position{} }

func (r *treeReader) StackDepth() int { return len(r.levels) - 1 }

func (r *treeReader) Options() Options {
	return r.opts.Clone()
}

func (r *treeReader) StackIndex(i int) (kdltext.Kind, int64) {
	l := &r.levels[i]
	return l.last, l.n
}

// skipRest moves past the rest of the current node's contents, up to its
// KindEnd token, or past the rest of the document.
func (r *treeReader) skipRest() {
	depth := len(r.levels)
	for k := r.PeekKind(); k != kdltext.KindEnd && k != kdltext.KindInvalid; k = r.PeekKind() {
		r.advance(k)
		r.levels = r.levels[:depth]
	}
}

// treeWriter builds the nodes of a Document from a stream of tokens.
type treeWriter struct {
	b      builder
	levels []tokenCount    // the document, and the nodes started and not ended
	opts   *kdlopts.Struct // the options of From
}

// WriteToken writes t, which must be valid where it is, as the encoder makes
// sure, but for the nodes of a Document written in place of a property's
// value.
func (w *treeWriter) WriteToken(t kdltext.Token) error {
	if w.b.inProp && t.Kind != kdltext.KindValue {
		return fmt.Errorf("%s after property %s, before its value", t.Kind, lexer.Quote(w.b.prop))
	}
	depth := len(w.levels) - 1
	// As an Encoder would, stop at a children block nested too deeply, here
	// for the nodes of a Document, which are copied without the checks of
	// the encoder.
	if (t.Kind == kdltext.KindNode || t.Kind == kdltext.KindEnd && t.Children) && depth > kdlopts.MaxNestingDepth {
		return fmt.Errorf("children blocks nested deeper than %d", kdlopts.MaxNestingDepth)
	}
	if t.Kind == kdltext.KindEnd {
		w.levels = w.levels[:depth]
	} else {
		l := &w.levels[depth]
		l.n++
		l.last = t.Kind
		if t.Kind == kdltext.KindNode {
			w.levels = append(w.levels, tokenCount{})
		}
	}
	w.b.token(t.Clone())
	return nil
}

func (w *treeWriter) StackDepth() int { return len(w.levels) - 1 }

func (w *treeWriter) Options() Options {
	return w.opts.Clone()
}

func (w *treeWriter) StackIndex(i int) (kdltext.Kind, int64) {
	l := w.levels[i]
	return l.last, l.n
}

// isDocument reports whether the method of m is that of a *Document, which
// reads and writes a tree directly, unlike any other.
func isDocument(m any) bool {
	_, ok := m.(*Document)
	return ok
}

// call calls the UnmarshalKDL method u with d.in, which is a
// *kdltext.Decoder, unless u is a *Document, which reads any tokenReader.
func (d *decoder) call(u Unmarshaler) error {
	if dec, ok := d.in.(*kdltext.Decoder); ok {
		return u.UnmarshalKDL(dec)
	}
	return u.(*Document).read(d.in)
}

// call calls the MarshalKDL method m with e.out, which is a
// *kdltext.Encoder, unless m is a *Document, which writes to any
// tokenWriter.
func (e *encoder) call(m Marshaler) error {
	if enc, ok := e.out.(*kdltext.Encoder); ok {
		return m.MarshalKDL(enc)
	}
	return writeNodes(e.out, m.(*Document).Nodes)
}

// textNodeMethod is nodeMethod for the node ns, decoded from r: the method
// reads the rest of the node from a kdltext.Decoder, as KDL text.
func (d *decoder) textNodeMethod(r *treeReader, ns *nodeStart, t reflect.Type, u Unmarshaler) (bool, error) {
	dec, err := d.textDecoder(r, false)
	if err != nil {
		return true, d.error(ns, entryRef{}, t, err)
	}
	d.in = dec
	done, err := d.nodeMethod(ns, t, u)
	d.in = r
	if !done || err != nil {
		return done, noPos(err)
	}
	r.skipRest()
	return true, d.end(ns)
}

// textValueMethod is valueMethod for the next value of r: the method reads
// it from a kdltext.Decoder, as KDL text.
func (d *decoder) textValueMethod(r *treeReader, u Unmarshaler) (bool, error) {
	dec, err := d.textDecoder(r, true)
	if err != nil {
		return true, err
	}
	d.in = dec
	done, err := d.valueMethod(u)
	d.in = r
	if done && err == nil {
		r.ReadToken()
	}
	return done, noPos(err)
}

// textDecoder returns a kdltext.Decoder reading the next value of r, if value
// is set, or the rest of the contents of r's current node, positioned as r
// is: within a node like r's current one, if r is within a node, and after
// the KindProp token of the property whose value is next, if any.
func (d *decoder) textDecoder(r *treeReader, value bool) (*kdltext.Decoder, error) {
	var buf bytes.Buffer
	enc := kdltext.NewEncoder(&buf)
	l := r.top()
	entries, children := l.entries[l.entry:], l.children[l.child:]
	if value {
		entries, children = entries[:1], nil
	}
	n := l.node
	if n != nil {
		n = &Node{Type: n.Type, Name: n.Name, Entries: entries, Children: children}
		if err := writeNode(enc, n); err != nil {
			return nil, err
		}
	} else if err := writeNodes(enc, children); err != nil {
		return nil, err
	}

	// The method sees the options of To, but its text is read as written.
	dec := kdltext.NewDecoder(&buf, r.opts.Only(kdlopts.Semantic), kdltext.WithVersion(kdltext.V2))
	if n != nil {
		dec.ReadToken()
	}
	if l.inProp {
		dec.ReadToken()
	}
	return dec, nil
}

// noPos returns err, but with no position if it is a *SemanticError.
func noPos(err error) error {
	if se, ok := errors.AsType[*SemanticError](err); ok {
		c := *se
		c.Pos = kdltext.Position{}
		return &c
	}
	return err
}

// textMethod calls fn, which calls a MarshalKDL method, with e.out set to a
// kdltext.Encoder positioned as w is: within a node like w's current one, if
// w is within a node, and after the KindProp token of the property whose
// value is next, if any. What the method writes, as KDL text, is read into
// w, unless fn reports false.
func (e *encoder) textMethod(w *treeWriter, fn func() (bool, error)) (bool, error) {
	var buf bytes.Buffer
	// The method sees the options of From, but its text is written plainly.
	enc := kdltext.NewEncoder(&buf, w.opts.Only(kdlopts.Semantic))
	inNode := w.StackDepth() > 0
	if inNode {
		n := &w.b.open[len(w.b.open)-1].node
		enc.WriteToken(kdltext.Token{Kind: kdltext.KindNode, Value: kdltext.String(n.Name).WithType(n.Type)})
		if w.b.inProp {
			enc.WriteToken(kdltext.Token{Kind: kdltext.KindProp, Value: kdltext.String(w.b.prop)})
		}
	}
	e.out = enc
	done, err := fn()
	e.out = w
	if !done || err != nil {
		return done, err
	}
	if inNode {
		if err := enc.WriteToken(kdltext.Token{Kind: kdltext.KindEnd}); err != nil {
			return true, err
		}
	}

	dec := kdltext.NewDecoder(&buf, kdltext.WithVersion(kdltext.V2))
	if inNode {
		dec.ReadToken()
	}
	if w.b.inProp {
		dec.ReadToken()
	}
	for {
		t, err := dec.ReadToken()
		switch {
		case err == io.EOF, t.Kind == kdltext.KindEnd && dec.StackDepth() == 0:
			return true, nil
		case err != nil:
			return true, err
		}
		if err := w.WriteToken(t); err != nil {
			return true, err
		}
	}
}
