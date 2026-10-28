package kuddle

import (
	"bytes"
	"encoding"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/miquella/kuddle/internal/kdlopts"
	"github.com/miquella/kuddle/internal/lexer"
	"github.com/miquella/kuddle/kdltext"
)

// ErrUnknown is wrapped by the error returned when Unmarshal meets an
// argument, property or child node that a struct has no field for, unless
// DiscardUnknown is set.
var ErrUnknown = errors.New("unknown to the Go type")

// Unmarshal decodes the KDL document in into out, which must be a non-nil
// pointer, by the rules described in the package documentation. The options
// that apply are DiscardUnknown and those of kdltext.NewDecoder.
func Unmarshal(in []byte, out any, opts ...Options) error {
	return UnmarshalRead(bytes.NewReader(in), out, opts...)
}

// UnmarshalRead is like Unmarshal, but reads the document from in. If in is
// a *bytes.Buffer, its contents are read directly, as by kdltext.NewDecoder,
// and it must not be written to until UnmarshalRead returns.
func UnmarshalRead(in io.Reader, out any, opts ...Options) error {
	return UnmarshalDecode(kdltext.NewDecoder(in, opts...), out, opts...)
}

// UnmarshalDecode is like Unmarshal, but reads from in. At the top level, it
// reads the rest of the document. If in is within a node, it reads the rest
// of the node's contents instead (its arguments, properties and children),
// decoding them into out as a node's contents, and leaves the node's KindEnd
// token unread. An UnmarshalKDL method can call it to decode its node's
// contents by the rules of another type, such as one without the method.
// UnmarshalDecode can't decode a single argument or property value, since it
// reads the rest of the node; for that, a method returns
// errors.ErrUnsupported without reading anything, or reads the KindValue
// token itself.
//
// The options that apply are those in was created with, and the options of
// this package among opts, which take precedence. The UnmarshalKDL methods
// that UnmarshalDecode calls see only the options of in, through its Options
// method, and pass them on when they call UnmarshalDecode themselves.
func UnmarshalDecode(in *kdltext.Decoder, out any, opts ...Options) error {
	return unmarshal(in, out, opts...)
}

// unmarshal decodes the rest of the document, or of the current node, from
// in into out, with the options of in and opts.
func unmarshal(in *kdltext.Decoder, out any, opts ...Options) error {
	o := kdlopts.Join(in.Options())
	o.Join(opts...)
	rv := reflect.ValueOf(out)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("kuddle: cannot unmarshal into non-pointer %T", out)
	}
	v := rv.Elem()
	d := &decoder{in: in, ignore: o.Flags&kdlopts.DiscardUnknown != 0}
	root := &nodeStart{root: true, inNode: in.StackDepth() > 0}
	if !root.inNode {
		if v.Kind() == reflect.Interface && v.NumMethod() == 0 {
			doc := new(Document)
			if err := doc.UnmarshalKDL(in); err != nil {
				return err
			}
			v.Set(reflect.ValueOf(doc))
			return nil
		}
		if isValueType(v.Type()) {
			return d.error(root, entryRef{}, v.Type(), errors.New("a document can't be decoded into a value type"))
		}
	}
	return d.node(root, v)
}

type decoder struct {
	in     *kdltext.Decoder
	ignore bool          // whether what a struct has no field for is ignored
	path   []pathSegment // the nodes being decoded
}

// nodeStart is what is read at the start of a node, from its KindNode token.
// The contents of the node, its entries and children, are decoded as they
// are read. The root nodeStart is that of the document, or of the node whose
// contents UnmarshalDecode reads, if inNode is set.
type nodeStart struct {
	typ  *string // the type annotation, or nil
	name string
	pos  kdltext.Position

	root, inNode bool
}

// child reads the start of the next child, a KindNode token, adding it to
// the path. The caller must call leave once the child is decoded.
func (d *decoder) child() *nodeStart {
	t, _ := d.in.ReadToken()
	c := &nodeStart{typ: t.Value.Type, name: t.Name(), pos: d.in.Pos()}
	d.path = append(d.path, pathSegment{c.name, -1})
	return c
}

func (d *decoder) leave() { d.path = d.path[:len(d.path)-1] }

// element records in the path that the child being decoded is element i of
// a slice or array.
func (d *decoder) element(i int) { d.path[len(d.path)-1].index = i }

// nextChild reports whether another child of the node is next.
func (d *decoder) nextChild() bool {
	return d.in.PeekKind() == kdltext.KindNode
}

// atEnd reports whether the contents of ns have all been read.
func (d *decoder) atEnd(ns *nodeStart) bool {
	if ns.root && !ns.inNode {
		return d.in.PeekKind() == kdltext.KindInvalid
	}
	return d.in.PeekKind() == kdltext.KindEnd
}

// end reads the end of ns, once its contents have been read: its KindEnd
// token, or the end of the document. The KindEnd token of a root, whose
// contents UnmarshalDecode reads, is left to be read.
func (d *decoder) end(ns *nodeStart) error {
	switch d.in.PeekKind() {
	case kdltext.KindEnd:
		if !ns.root {
			d.in.ReadToken()
		}
		return nil
	case kdltext.KindInvalid:
		// The end of the document, or an error.
		if _, err := d.in.ReadToken(); err != io.EOF || ns.inNode || !ns.root {
			return err
		}
		return nil
	}
	return d.error(ns, entryRef{}, nil, fmt.Errorf("unexpected %s", d.in.PeekKind()))
}

// fullNode reads the rest of the node ns, as a Node.
func (d *decoder) fullNode(ns *nodeStart) (Node, error) {
	var b builder
	b.token(kdltext.Token{Kind: kdltext.KindNode, Value: kdltext.String(ns.name).WithType(ns.typ)})
	if err := b.read(d.in); err != nil {
		return Node{}, err
	}
	// The node's KindEnd token, which says whether it has a children block.
	end, _ := d.in.PeekToken()
	b.token(kdltext.Token{Kind: kdltext.KindEnd, Children: end.Children})
	return b.nodes[0], d.end(ns)
}

// skip reads and discards the rest of the node ns.
func (d *decoder) skip(ns *nodeStart) error {
	for depth := 0; ; {
		switch d.in.PeekKind() {
		case kdltext.KindNode:
			depth++
		case kdltext.KindEnd:
			if depth == 0 {
				return d.end(ns)
			}
			depth--
		case kdltext.KindInvalid:
			return d.end(ns)
		}
		d.in.ReadToken()
	}
}

func (d *decoder) node(ns *nodeStart, v reflect.Value) error {
	if isValuePointer(v.Type()) {
		return d.valueNode(ns, v)
	}
	v, err := allocPointers(v)
	if err != nil {
		return d.error(ns, entryRef{}, v.Type(), err)
	}
	if v.Kind() == reflect.Struct && v.Type() != nodeType && !ns.root {
		if err := d.startFields(ns, v); err != nil {
			return err
		}
	}
	if u, ok := asUnmarshaler(v); ok {
		if done, err := d.nodeMethod(ns, v.Type(), u); done {
			return err
		}
	}

	switch t := v.Type(); {
	case t == nodeType:
		n, err := d.fullNode(ns)
		if err != nil {
			return err
		}
		v.Set(reflect.ValueOf(n))
		return nil
	case v.Kind() == reflect.Interface && v.NumMethod() == 0:
		n, err := d.fullNode(ns)
		if err != nil {
			return err
		}
		arg, ok := singleArg(&n)
		if !ok {
			v.Set(reflect.ValueOf(&n))
			return nil
		}
		gv, err := goValue(arg)
		if err != nil {
			return d.error(ns, argRef(1), t, err)
		}
		v.Set(reflect.ValueOf(&gv).Elem())
		return nil
	case isDefaultValueType(t):
		return d.valueNode(ns, v)
	}

	switch v.Kind() {
	case reflect.Struct:
		return d.structNode(ns, v)
	case reflect.Map:
		return d.mapNode(ns, v)
	case reflect.Slice, reflect.Array:
		return d.listNode(ns, v)
	}
	return d.error(ns, entryRef{}, v.Type(), errors.New("unsupported type"))
}

// isValuePointer reports whether t is a pointer to a value type other than an
// interface, which a node's argument is decoded into as a value.
func isValuePointer(t reflect.Type) bool {
	if t.Kind() != reflect.Pointer || !isValueType(t) {
		return false
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Kind() != reflect.Interface
}

// startFields sets the name and type fields of the struct v from ns.
func (d *decoder) startFields(ns *nodeStart, v reflect.Value) error {
	fs, err := cachedFields(v.Type())
	if err != nil {
		if hasMethods(v.Type()) {
			return nil
		}
		return d.error(ns, entryRef{}, v.Type(), err)
	}
	if fs.name != nil {
		fieldByIndex(v, fs.name.index).SetString(ns.name)
	}
	if fs.typ != nil {
		setType(fieldByIndex(v, fs.typ.index), ns.typ)
	}
	return nil
}

// asUnmarshaler returns v's Unmarshaler, if it has one.
func asUnmarshaler(v reflect.Value) (Unmarshaler, bool) {
	if !v.CanAddr() || !typeInfoOf(v.Type()).unmarshaler {
		return nil, false
	}
	return v.Addr().Interface().(Unmarshaler), true
}

// nodeMethod calls the UnmarshalKDL method u of a value of type t, for the
// node ns, and reads the end of the node. It reports false if the method
// returned errors.ErrUnsupported without reading anything.
func (d *decoder) nodeMethod(ns *nodeStart, t reflect.Type, u Unmarshaler) (bool, error) {
	m := d.mark()
	err := u.UnmarshalKDL(d.in)
	read := d.readSince(m)
	switch {
	case err == errors.ErrUnsupported && read == 0:
		return false, nil
	case err == errors.ErrUnsupported:
		err = errors.New("UnmarshalKDL returned errors.ErrUnsupported after reading input")
	case err != nil:
		if se, ok := errors.AsType[*SemanticError](err); ok {
			return true, se.within(formatPath(d.path), ns.pos)
		}
		if _, ok := errors.AsType[*kdltext.SyntacticError](err); ok {
			return true, err
		}
	case read < 0:
		err = errors.New("UnmarshalKDL read past the end of the node")
	case d.in.StackDepth() > m.depth || !d.atEnd(ns):
		err = errors.New("UnmarshalKDL did not read all of the node")
	default:
		return true, d.end(ns)
	}
	return true, d.error(ns, entryRef{}, t, err)
}

// mark is a point in the token stream, as the Decoder's stack records it: the
// depth of the stack, and the number of tokens read at its innermost level
// and at the level above.
type mark struct {
	depth     int
	n, parent int64
}

func (d *decoder) mark() mark {
	m := mark{depth: d.in.StackDepth()}
	_, m.n = d.in.StackIndex(m.depth)
	if m.depth > 0 {
		_, m.parent = d.in.StackIndex(m.depth - 1)
	}
	return m
}

// readSince returns the number of tokens read since m at the level of m, a
// child node counting as one, or -1 if the end of the level has been read.
func (d *decoder) readSince(m mark) int64 {
	if d.in.StackDepth() < m.depth {
		return -1
	}
	if m.depth > 0 {
		if _, n := d.in.StackIndex(m.depth - 1); n != m.parent {
			return -1
		}
	}
	_, n := d.in.StackIndex(m.depth)
	return n - m.n
}

// valueNode decodes the node ns, which must have a single argument, into v,
// of a value type or a pointer to one, by the default rules.
func (d *decoder) valueNode(ns *nodeStart, v reflect.Value) error {
	if d.in.PeekKind() == kdltext.KindValue {
		t, _ := d.in.ReadToken()
		if d.atEnd(ns) {
			if err := d.wrap(ns, argRef(1), v.Type(), setValue(v, t.Value)); err != nil {
				return err
			}
			return d.end(ns)
		}
		return d.notSingle(ns, v.Type(), 1)
	}
	return d.notSingle(ns, v.Type(), 0)
}

// notSingle returns the error for the node ns, which does not have the single
// argument that type t takes. It reads the rest of the node's contents,
// after the first args arguments, to describe them.
func (d *decoder) notSingle(ns *nodeStart, t reflect.Type, args int) error {
	props, children := 0, false
	for !children && !d.atEnd(ns) {
		switch d.in.PeekKind() {
		case kdltext.KindValue:
			args++
		case kdltext.KindProp:
			props++
			d.in.ReadToken()
		case kdltext.KindNode:
			children = true
			continue
		}
		if _, err := d.in.ReadToken(); err != nil {
			return err
		}
	}
	var parts []string
	count := func(n int, one, many string) {
		if n == 1 {
			parts = append(parts, "1 "+one)
		} else if n > 1 {
			parts = append(parts, fmt.Sprintf("%d %s", n, many))
		}
	}
	count(args, "argument", "arguments")
	count(props, "property", "properties")
	if children {
		parts = append(parts, "children")
	}
	var s string
	switch len(parts) {
	case 0:
		s = "nothing"
	case 1:
		s = parts[0]
	default:
		s = strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
	return d.error(ns, entryRef{}, t, fmt.Errorf("node has %s, but %s takes a single argument", s, t))
}

func (d *decoder) structNode(ns *nodeStart, v reflect.Value) error {
	fs, err := cachedFields(v.Type())
	if err != nil {
		return d.error(ns, entryRef{}, v.Type(), err)
	}

	// Slices are emptied when first decoded into, and then appended to.
	var reset []bool
	slice := func(f *field) reflect.Value {
		fv := fieldByIndex(v, f.index)
		if reset == nil {
			reset = make([]bool, len(fs.all))
		}
		if !reset[f.id] {
			reset[f.id] = true
			fv.SetLen(0)
		}
		return fv
	}
	var props, nodes mapSetter
	if fs.restProps != nil {
		props.m = fieldByIndex(v, fs.restProps.index)
	}
	if fs.restNodes != nil && fs.restNodes.typ.Kind() == reflect.Map {
		nodes.m = fieldByIndex(v, fs.restNodes.index)
	}

	// The entries field holds the arguments and properties that have no other
	// field, in order.
	var entries reflect.Value
	if fs.entries != nil {
		entries = slice(fs.entries)
	}

	args := 0
	for {
		var err error
		switch d.in.PeekKind() {
		case kdltext.KindValue:
			args++
			entry := argRef(args)
			switch {
			case args <= len(fs.args):
				f := fs.args[args-1]
				err = d.fieldValue(ns, entry, fieldByIndex(v, f.index))
			case fs.restArgs != nil:
				err = d.fieldValue(ns, entry, grow(slice(fs.restArgs)))
			case entries.IsValid():
				t, _ := d.in.ReadToken()
				appendEntry(entries, Arg(t.Value))
			default:
				err = d.unknownValue(ns, entry, v.Type())
			}
		case kdltext.KindProp:
			t, _ := d.in.ReadToken()
			entry := propRef(t.Name())
			switch f := fs.props[t.Name()]; {
			case f != nil:
				err = d.fieldValue(ns, entry, fieldByIndex(v, f.index))
			case fs.restProps != nil:
				err = d.mapEntry(ns, entry, &props, t.Name(), fieldValue)
			case entries.IsValid():
				val, _ := d.in.ReadToken()
				appendEntry(entries, Prop(t.Name(), val.Value))
			default:
				err = d.unknownValue(ns, entry, v.Type())
			}
		default:
			return d.structChildren(ns, v, fs, slice, &nodes)
		}
		if err != nil {
			return err
		}
	}
}

// appendEntry appends e to entries, the slice of an entries field, if valid.
func appendEntry(entries reflect.Value, e Entry) {
	if entries.IsValid() {
		grow(entries).Set(reflect.ValueOf(e))
	}
}

func (d *decoder) structChildren(ns *nodeStart, v reflect.Value, fs *structFields, slice func(*field) reflect.Value, nodes *mapSetter) error {
	for d.nextChild() {
		c := d.child()
		var err error
		switch f := fs.children[c.name]; {
		case f != nil && f.repeated:
			err = d.appendNode(c, slice(f))
		case f != nil:
			err = d.node(c, fieldByIndex(v, f.index))
		case fs.restNodes != nil && fs.restNodes.typ.Kind() == reflect.Slice:
			err = d.appendNode(c, slice(fs.restNodes))
		case fs.restNodes != nil:
			err = d.mapChild(c, nodes)
		default:
			err = d.unknownNode(c, v.Type())
		}
		d.leave()
		if err != nil {
			return err
		}
	}
	return d.end(ns)
}

// fieldValue decodes the next token, the given entry of ns, into v, which
// belongs to a field that holds arguments or properties.
func (d *decoder) fieldValue(ns *nodeStart, entry entryRef, v reflect.Value) error {
	return d.wrap(ns, entry, v.Type(), d.value(v, fieldValue))
}

func (d *decoder) mapNode(ns *nodeStart, v reflect.Value) error {
	if !isKeyType(v.Type().Key()) {
		return d.error(ns, entryRef{}, v.Type(), errors.New("unsupported map key type"))
	}
	m := mapSetter{m: v}
	args := 0
	for {
		var err error
		switch d.in.PeekKind() {
		case kdltext.KindValue:
			args++
			err = d.noPlace(ns, argRef(args), v.Type(), "arguments")
		case kdltext.KindProp:
			t, _ := d.in.ReadToken()
			err = d.mapEntry(ns, propRef(t.Name()), &m, t.Name(), elemValue)
		default:
			for d.nextChild() {
				c := d.child()
				err := d.mapChild(c, &m)
				d.leave()
				if err != nil {
					return err
				}
			}
			return d.end(ns)
		}
		if err != nil {
			return err
		}
	}
}

func (d *decoder) mapChild(c *nodeStart, m *mapSetter) error {
	if err := d.node(c, m.elem()); err != nil {
		return err
	}
	return d.wrap(c, entryRef{}, m.m.Type().Key(), m.set(c.name))
}

// mapEntry decodes the value of a property, the next token, into the map m.
func (d *decoder) mapEntry(ns *nodeStart, entry entryRef, m *mapSetter, key string, mode valueMode) error {
	elem := m.elem()
	if err := d.wrap(ns, entry, elem.Type(), d.value(elem, mode)); err != nil {
		return err
	}
	return d.wrap(ns, entry, m.m.Type().Key(), m.set(key))
}

// mapSetter sets the entries of the map m, reusing the values it decodes keys
// and elements into.
type mapSetter struct {
	m, k, e reflect.Value
}

// elem returns a zero element to decode into, for the next call to set.
func (s *mapSetter) elem() reflect.Value {
	if !s.e.IsValid() {
		s.e = reflect.New(s.m.Type().Elem()).Elem()
	} else {
		s.e.SetZero()
	}
	return s.e
}

// set sets the element of key to the element returned by elem. The key is
// decoded with UnmarshalText if its type implements encoding.TextUnmarshaler.
func (s *mapSetter) set(key string) error {
	if !s.k.IsValid() {
		s.k = reflect.New(s.m.Type().Key()).Elem()
	}
	if u, ok := s.k.Addr().Interface().(encoding.TextUnmarshaler); ok {
		s.k.SetZero()
		if err := u.UnmarshalText([]byte(key)); err != nil {
			return err
		}
	} else if s.k.Kind() == reflect.String {
		s.k.SetString(key)
	} else {
		return errors.New("unsupported map key type")
	}
	if s.m.IsNil() {
		s.m.Set(reflect.MakeMap(s.m.Type()))
	}
	s.m.SetMapIndex(s.k, s.e)
	return nil
}

// grow appends a zero element to the slice v, and returns it.
func grow(v reflect.Value) reflect.Value {
	n := v.Len()
	v.Grow(1)
	v.SetLen(n + 1)
	elem := v.Index(n)
	elem.SetZero()
	return elem
}

func (d *decoder) listNode(ns *nodeStart, v reflect.Value) error {
	if v.Kind() == reflect.Slice {
		v.SetLen(0)
	}
	i := 0
	// next returns the next element to decode the entry or child of n into.
	next := func(n *nodeStart, entry entryRef) (reflect.Value, error) {
		if v.Kind() == reflect.Slice {
			i++
			return grow(v), nil
		}
		if i >= v.Len() {
			return reflect.Value{}, d.error(n, entry, v.Type(), fmt.Errorf("too many elements for an array of %d", v.Len()))
		}
		i++
		return v.Index(i - 1), nil
	}

	args := 0
	for k := d.in.PeekKind(); k == kdltext.KindValue || k == kdltext.KindProp; k = d.in.PeekKind() {
		if k == kdltext.KindProp {
			t, _ := d.in.ReadToken()
			return d.noPlace(ns, propRef(t.Name()), v.Type(), "properties")
		}
		args++
		entry := argRef(args)
		elem, err := next(ns, entry)
		if err != nil {
			return err
		}
		if err := d.wrap(ns, entry, elem.Type(), d.value(elem, elemValue)); err != nil {
			return err
		}
	}
	for d.nextChild() {
		c := d.child()
		d.element(i)
		elem, err := next(c, entryRef{})
		if err == nil {
			err = d.node(c, elem)
		}
		d.leave()
		if err != nil {
			return err
		}
	}
	if v.Kind() == reflect.Array {
		for ; i < v.Len(); i++ {
			v.Index(i).SetZero()
		}
	}
	return d.end(ns)
}

// appendNode appends the child node c to the slice v, which belongs to a
// field holding repeated nodes. If the element is a value type, each of the
// node's arguments is appended, followed, if it is an empty interface, by
// each of the node's children. The node's properties, and otherwise its
// children, are an error.
func (d *decoder) appendNode(c *nodeStart, v reflect.Value) error {
	et := v.Type().Elem()
	if !isValueType(et) {
		d.element(v.Len())
		return d.node(c, grow(v))
	}
	children := et.Kind() == reflect.Interface && et.NumMethod() == 0
	args := 0
	for {
		var err error
		switch d.in.PeekKind() {
		case kdltext.KindValue:
			args++
			elem := grow(v)
			err = d.wrap(c, argRef(args), elem.Type(), d.value(elem, elemValue))
		case kdltext.KindProp:
			t, _ := d.in.ReadToken()
			err = d.noPlace(c, propRef(t.Name()), v.Type(), "properties")
		default:
			for d.nextChild() {
				gc := d.child()
				if children {
					d.element(v.Len())
					err = d.node(gc, grow(v))
				} else {
					err = d.noPlace(gc, entryRef{}, v.Type(), "children")
				}
				d.leave()
				if err != nil {
					return err
				}
			}
			return d.end(c)
		}
		if err != nil {
			return err
		}
	}
}

// valueMode says how value decodes a value into a type implementing
// Unmarshaler.
type valueMode int

const (
	elemValue  valueMode = iota // it can't be: the type is decoded from a node
	fieldValue                  // with its method, as for an arg or prop field
)

// value decodes the next token, a KDL value token, into v. The error, if any,
// is reworded by the caller.
func (d *decoder) value(v reflect.Value, mode valueMode) error {
	t, err := d.in.PeekToken()
	if err != nil {
		return err
	}
	// #null sets a pointer to nil, without calling a method.
	if t.Value.Kind() != kdltext.ValueNull {
		if v, err = allocPointers(v); err != nil {
			return err
		}
	}
	if u, ok := asUnmarshaler(v); ok {
		switch mode {
		case elemValue:
			return fmt.Errorf("%s can't be decoded from a single value", v.Type())
		case fieldValue:
			if done, err := d.valueMethod(u); done {
				return err
			}
		}
	}
	d.in.ReadToken()
	return setValue(v, t.Value)
}

// allocPointers follows the pointers of v, allocating any that are nil. On a
// cycle, it returns errCycle and the pointer it stopped at.
func allocPointers(v reflect.Value) (reflect.Value, error) {
	for n := 0; v.Kind() == reflect.Pointer; n++ {
		if n == maxIndirections {
			return v, errCycle
		}
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	return v, nil
}

// setValue sets v to the KDL value val by the default rules.
func setValue(v reflect.Value, val kdltext.Value) error {
	null := val.Kind() == kdltext.ValueNull
	for n := 0; v.Kind() == reflect.Pointer; n++ {
		if n == maxIndirections {
			return errCycle
		}
		if null {
			v.SetZero()
			return nil
		}
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	if v.Type() == valueType {
		v.Set(reflect.ValueOf(val))
		return nil
	}
	if null {
		v.SetZero()
		return nil
	}
	if v.CanAddr() {
		if u, ok := v.Addr().Interface().(encoding.TextUnmarshaler); ok {
			if val.Kind() != kdltext.ValueString {
				return mismatch(val, "a string")
			}
			return u.UnmarshalText([]byte(val.String()))
		}
	}

	switch v.Kind() {
	case reflect.Bool:
		if val.Kind() != kdltext.ValueBool {
			return mismatch(val, "#true or #false")
		}
		v.SetBool(val.Bool())
	case reflect.String:
		if val.Kind() != kdltext.ValueString {
			return mismatch(val, "a string")
		}
		v.SetString(val.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if val.Kind() != kdltext.ValueNumber {
			return mismatch(val, "a number")
		}
		i, err := val.Int()
		if err != nil {
			return err
		}
		if v.OverflowInt(i) {
			return fmt.Errorf("%s overflows %s", val, v.Type())
		}
		v.SetInt(i)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if val.Kind() != kdltext.ValueNumber {
			return mismatch(val, "a number")
		}
		u, err := val.Uint()
		if err != nil {
			return err
		}
		if v.OverflowUint(u) {
			return fmt.Errorf("%s overflows %s", val, v.Type())
		}
		v.SetUint(u)
	case reflect.Float32, reflect.Float64:
		if val.Kind() != kdltext.ValueNumber {
			return mismatch(val, "a number")
		}
		f, err := val.Float()
		if err != nil {
			return err
		}
		if v.OverflowFloat(f) {
			return fmt.Errorf("%s overflows %s", val, v.Type())
		}
		v.SetFloat(f)
	case reflect.Slice:
		if v.Type().Elem().Kind() != reflect.Uint8 {
			return errors.New("a slice can't be decoded from a single value")
		}
		if val.Kind() != kdltext.ValueString {
			return mismatch(val, "a base64 string")
		}
		b, err := base64.StdEncoding.DecodeString(val.String())
		if err != nil {
			return fmt.Errorf("invalid base64: %w", err)
		}
		v.SetBytes(b)
	case reflect.Interface:
		if v.NumMethod() > 0 {
			return errors.New("unsupported type")
		}
		gv, err := goValue(val)
		if err != nil {
			return err
		}
		v.Set(reflect.ValueOf(gv))
	default:
		return fmt.Errorf("%s can't be decoded from a single value", v.Type())
	}
	return nil
}

// valueMethod calls the UnmarshalKDL method u for the next token, a KDL
// value token, for the method to read. It reports false if the method
// returned errors.ErrUnsupported without reading anything.
func (d *decoder) valueMethod(u Unmarshaler) (bool, error) {
	m := d.mark()
	err := u.UnmarshalKDL(d.in)
	read := d.readSince(m)
	switch {
	case err == errors.ErrUnsupported && read == 0:
		return false, nil
	case err == errors.ErrUnsupported:
		return true, errors.New("UnmarshalKDL returned errors.ErrUnsupported after reading input")
	case err != nil:
		return true, err
	case read == 0:
		return true, errors.New("UnmarshalKDL did not read the value")
	case read != 1:
		return true, errors.New("UnmarshalKDL read more than the value")
	}
	return true, nil
}

// goValue returns the value of val as a string, bool, nil, int64 or float64.
func goValue(val kdltext.Value) (any, error) {
	switch val.Kind() {
	case kdltext.ValueString:
		return val.String(), nil
	case kdltext.ValueBool:
		return val.Bool(), nil
	case kdltext.ValueNumber:
		if i, err := val.Int(); err == nil {
			return i, nil
		}
		return val.Float()
	}
	return nil, nil
}

func mismatch(val kdltext.Value, want string) error {
	return fmt.Errorf("expected %s, found %s %s", want, val.Kind(), val)
}

// noPlace returns the error for the given entry of ns, or for the child node ns
// if entry is zero, which t has no place for whatever its name: t takes no
// entries or children of the kind that what names, such as "properties".
func (d *decoder) noPlace(ns *nodeStart, entry entryRef, t reflect.Type, what string) error {
	return d.error(ns, entry, t, fmt.Errorf("%s takes no %s", t, what))
}

// unknown returns the error for the given entry of ns, or for the child node ns
// if entry is zero, which the struct type t has no field for, unless what has
// no field is ignored.
func (d *decoder) unknown(ns *nodeStart, entry entryRef, t reflect.Type) error {
	if d.ignore {
		return nil
	}
	return d.error(ns, entry, t, ErrUnknown)
}

// unknownValue skips the next token, the value of the given entry of ns,
// which t has no place for.
func (d *decoder) unknownValue(ns *nodeStart, entry entryRef, t reflect.Type) error {
	if err := d.unknown(ns, entry, t); err != nil {
		return err
	}
	d.in.ReadToken()
	return nil
}

// unknownNode skips the child node c, which t has no place for.
func (d *decoder) unknownNode(c *nodeStart, t reflect.Type) error {
	if err := d.unknown(c, entryRef{}, t); err != nil {
		return err
	}
	return d.skip(c)
}

// wrap returns err, if not nil, as a SemanticError. A SemanticError is
// returned as is, since it already describes the innermost node.
func (d *decoder) wrap(ns *nodeStart, entry entryRef, t reflect.Type, err error) error {
	if err == nil {
		return nil
	}
	if se, ok := errors.AsType[*SemanticError](err); ok {
		return se
	}
	return d.error(ns, entry, t, err)
}

func (d *decoder) error(ns *nodeStart, entry entryRef, t reflect.Type, err error) error {
	return &SemanticError{action: "unmarshal", Path: formatPath(d.path), Pos: ns.pos, Entry: entry.String(), GoType: t, Err: err}
}

// singleArg returns n's argument if it is all the node has.
func singleArg(n *Node) (kdltext.Value, bool) {
	if len(n.Entries) != 1 || n.Entries[0].IsProp() || len(n.Children) > 0 {
		return kdltext.Value{}, false
	}
	return n.Entries[0].Value, true
}

// entryRef identifies an entry of a node in errors. It is formatted only if
// an error is returned. The zero entryRef is the node as a whole.
type entryRef struct {
	isProp bool
	name   string // the name of a property
	arg    int    // the 1-based index of an argument
}

func argRef(i int) entryRef        { return entryRef{arg: i} }
func propRef(name string) entryRef { return entryRef{isProp: true, name: name} }

// String describes the entry, such as `property "port"` or "argument 2".
func (r entryRef) String() string {
	switch {
	case r.isProp:
		return "property " + lexer.Quote(r.name)
	case r.arg > 0:
		return fmt.Sprintf("argument %d", r.arg)
	}
	return ""
}

// setType sets the type field v, a string or *string, to the type annotation
// typ. A string field is set to "" if there is no annotation.
func setType(v reflect.Value, typ *string) {
	switch {
	case v.Kind() == reflect.Pointer:
		v.Set(reflect.ValueOf(typ))
	case typ != nil:
		v.SetString(*typ)
	default:
		v.SetString("")
	}
}
