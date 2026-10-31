package kdltext

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/miquella/kuddle/internal/kdlopts"
	"github.com/miquella/kuddle/internal/lexer"
)

// Encoder writes a stream of tokens as a KDL document.
//
// Each node is written on its own line, with its children indented inside
// braces, or as {} if it has an empty children block (see Token.Children).
// Strings, names and numbers read by a Decoder are written as they were
// read, if the version being written allows that form. Others are written as
// bare identifiers where the version allows, and as quoted strings otherwise
// (see QuoteIdentifiers); one that is not valid UTF-8 is an error. To change
// the style of a document, see the Format method of kuddle.Document.
//
// Output is buffered until each top-level node ends. Nodes may be nested in
// up to 10000 children blocks; a node nested deeper is an error.
type Encoder struct {
	w        io.Writer
	opts     *kdlopts.Struct // the options of NewEncoder (see Options)
	buf      []byte
	flushed  int64 // the number of bytes written to w
	version  Version
	indent   string
	newline  string // WithNewline
	quoteIDs bool   // QuoteIdentifiers

	// props holds the names of the properties of the current node, if a node
	// may not repeat a property (see RejectDuplicateProperties); it is nil
	// otherwise.
	props map[string]struct{}

	open []openNode
	doc  level // the tokens written at the top level
	err  error // sticky error
}

// openNode is a node that has been started but not ended.
type openNode struct {
	level
	opened bool  // whether its children block has been written
	prop   Value // the name of the property whose value is next, if inProp
	inProp bool
}

// errTooDeep is the error for a children block nested deeper than
// maxNestingDepth.
var errTooDeep = fmt.Errorf("children blocks nested deeper than %d", maxNestingDepth)

// NewEncoder returns an Encoder writing to w. The options that apply are
// WithVersion, WithIndent, WithNewline, QuoteIdentifiers and
// RejectDuplicateProperties.
func NewEncoder(w io.Writer, opts ...Options) *Encoder {
	o := kdlopts.Join(opts...)
	e := &Encoder{
		w:        w,
		opts:     o,
		version:  V2,
		indent:   "    ",
		newline:  "\n",
		quoteIDs: o.Flags&kdlopts.QuoteIdentifiers != 0,
	}
	if o.Present&kdlopts.Version != 0 {
		e.version = Version(o.Version)
	}
	if o.Present&kdlopts.Indent != 0 {
		e.indent = o.Indent
	}
	if o.Present&kdlopts.Newline != 0 {
		e.newline = o.Newline
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
	switch t.Kind {
	case KindNode:
		if len(e.open) > maxNestingDepth {
			return errTooDeep
		}
		if n := len(e.open); n > 0 && !e.open[n-1].opened {
			e.buf = append(append(e.buf, " {"...), e.newline...)
			e.open[n-1].opened = true
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

	case KindValue, KindProp:
		n := len(e.open)
		switch {
		case n == 0:
			return fmt.Errorf("%s outside of a node", t.Kind)
		case e.open[n-1].opened:
			return fmt.Errorf("%s after the node's children", t.Kind)
		}
		o := &e.open[n-1]
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
		if err := e.writeEntry(t, name); err != nil {
			return err
		}

	case KindEnd:
		n := len(e.open)
		if n == 0 {
			return errors.New("end outside of a node")
		}
		switch o := e.open[n-1]; {
		case o.opened:
			e.writeIndent(n - 1)
			e.buf = append(e.buf, '}')
		case !t.Children:
		case n > maxNestingDepth:
			return errTooDeep
		default:
			e.buf = append(e.buf, " {}"...)
		}
		e.buf = append(e.buf, e.newline...)
		e.open = e.open[:n-1]
		if len(e.open) == 0 {
			return e.flush()
		}

	default:
		return fmt.Errorf("invalid token kind %v", t.Kind)
	}
	return nil
}

// writeEntry writes the argument or property t of the current node. A
// KindProp token holds the property's value, and name its name.
func (e *Encoder) writeEntry(t Token, name Value) error {
	e.buf = append(e.buf, ' ')
	if t.Kind == KindProp {
		if err := e.writeStringValue(name, !e.quoteIDs); err != nil {
			return err
		}
		e.buf = append(e.buf, '=')
	}
	return e.writeValue(t.Value)
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
