package kuddle

import (
	"bytes"
	"encoding"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"

	"github.com/miquella/kuddle/internal/lexer"
	"github.com/miquella/kuddle/kdltext"
)

// Marshal returns the KDL document for in, encoded by the rules described in
// the package documentation. The options that apply are those of
// kdltext.NewEncoder. The document ends in a newline, as each node does. An
// empty document is a single newline.
func Marshal(in any, opts ...Options) ([]byte, error) {
	var b bytes.Buffer
	if err := MarshalWrite(&b, in, opts...); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// MarshalWrite is like Marshal, but writes the document to out.
func MarshalWrite(out io.Writer, in any, opts ...Options) error {
	enc := kdltext.NewEncoder(out, opts...)
	if err := MarshalEncode(enc, in, opts...); err != nil {
		return err
	}
	if enc.OutputOffset() == 0 {
		_, err := out.Write([]byte{'\n'})
		return err
	}
	return nil
}

// MarshalEncode is like Marshal, but writes to out. At the top level, it
// writes a document's nodes. If out is within a node, it writes the node's
// contents instead (its arguments, properties and children), as they would be
// marshaled from in. A MarshalKDL method can call it to write its node's
// contents by the rules of another type, such as one without the method.
// MarshalEncode can't write a single argument or property value; for that, a
// method returns errors.ErrUnsupported without writing anything, or writes
// the KindValue token itself.
//
// The options that apply are those out was created with, and the options of
// this package among opts, though none of them concern marshaling yet. The
// MarshalKDL methods that MarshalEncode calls see only the options of out,
// through its Options method.
func MarshalEncode(out *kdltext.Encoder, in any, opts ...Options) error {
	return marshal(out, in)
}

func marshal(out *kdltext.Encoder, in any) error {
	switch in := in.(type) {
	case nil:
		return errors.New("kuddle: cannot marshal nil")
	case *Node:
		return writeNode(out, in)
	case Node:
		return writeNode(out, &in)
	}

	v := addressable(reflect.ValueOf(in))
	if isNilNode(v) {
		return errors.New("kuddle: cannot marshal nil")
	}
	e := &encoder{out: out, root: v.Type(), inNode: out.StackDepth() > 0}
	if !e.inNode && isValueType(v.Type()) {
		return e.error(v.Type(), errors.New("a document can't be marshaled from a value type"))
	}
	return e.node(v, "", false, true)
}

type encoder struct {
	out    *kdltext.Encoder
	root   reflect.Type  // the type of the document's Go value
	inNode bool          // whether the root is the contents of the node out is within
	open   int           // the number of nodes started and not yet ended
	path   []pathSegment // the nodes being marshaled

	// taken holds the child fields of the struct whose children field is
	// being marshaled, if it is a slice, for start to check that the node it
	// starts does not have the name of one of them.
	taken map[string]*field

	// seen holds the pointers, maps and slices being marshaled, once nodes
	// are nested deeper than startDetectingCyclesAfter.
	seen map[visit]struct{}
}

// startDetectingCyclesAfter is the nesting of nodes past which Marshal checks
// for cycles, as json/v2 does. Only a cyclic value is likely to be nested so
// deeply, and it is reported long before it reaches the Encoder's limit on
// nesting, and fills its buffer.
const startDetectingCyclesAfter = 1000

// visit identifies a pointer, map or slice being marshaled.
type visit struct {
	ptr uintptr
	typ reflect.Type
	len int // the length of a slice, which may share its array with another
}

// visitOf returns the visit for v, if v is a non-nil pointer, map or slice,
// or an interface holding one.
func visitOf(v reflect.Value) (visit, bool) {
	for v.Kind() == reflect.Interface && !v.IsNil() {
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice:
		if v.IsNil() {
			return visit{}, false
		}
		k := visit{ptr: v.Pointer(), typ: v.Type()}
		if v.Kind() == reflect.Slice {
			k.len = v.Len()
		}
		return k, true
	}
	return visit{}, false
}

// enter and leave bracket the marshaling of the child node name, keeping the
// path of the node being marshaled for errors. The node's name in the path
// becomes the one it is written with once its start is written, since it may
// have a name of its own (see node).
func (e *encoder) enter(name string) { e.path = append(e.path, pathSegment{name, -1}) }
func (e *encoder) leave()            { e.path = e.path[:len(e.path)-1] }

// element records in the path that the child being marshaled is element i of
// a slice or array.
func (e *encoder) element(i int) { e.path[len(e.path)-1].index = i }

// start and end write the KindNode token t and the KindEnd token of a node.
// The node of the document itself (the root) has neither.
func (e *encoder) start(t kdltext.Token, root bool) error {
	if root {
		return nil
	}
	e.path[len(e.path)-1].name = t.Name()
	taken := e.taken
	e.taken = nil
	if f := taken[t.Name()]; f != nil {
		return e.error(nil, takenError("child node", t.Name(), f))
	}
	e.open++
	if err := e.out.WriteToken(t); err != nil {
		return e.error(nil, err)
	}
	return nil
}

func (e *encoder) end(root, block bool) error {
	if root {
		return nil
	}
	e.open--
	if err := e.out.WriteToken(kdltext.Token{Kind: kdltext.KindEnd, Children: block}); err != nil {
		return e.error(nil, err)
	}
	return nil
}

// node writes v as a node named name. If own is set, a name the node has of
// its own (that of a Node, or a struct's non-empty name field) is used
// instead. If root is set, the node is the document, or the node out is
// within, so only its contents are written.
func (e *encoder) node(v reflect.Value, name string, own, root bool) error {
	if e.open > startDetectingCyclesAfter {
		if k, ok := visitOf(v); ok {
			if _, cyclic := e.seen[k]; cyclic {
				return e.error(dynamicType(v), errCycle)
			}
			if e.seen == nil {
				e.seen = make(map[visit]struct{})
			}
			e.seen[k] = struct{}{}
			defer delete(e.seen, k)
		}
	}
	if isNilNode(v) {
		// As a struct field or map value, it is left out instead.
		return e.error(dynamicType(v), errors.New("a nil pointer to a node can't be marshaled as an element"))
	}
	for n := 0; v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface; n++ {
		if n == maxIndirections {
			return e.error(v.Type(), errCycle)
		}
		if v.IsNil() {
			return e.null(v.Type(), name, root)
		}
		if m, ok := asMarshaler[Marshaler](v); ok {
			return e.marshaler(m, v, name, own, root)
		}
		v = v.Elem()
	}
	if m, ok := asMarshaler[Marshaler](v); ok {
		return e.marshaler(m, v, name, own, root)
	}
	if err := e.start(e.nodeToken(v, name, own, root), root); err != nil {
		return err
	}
	if err := e.contents(v, root); err != nil {
		return err
	}
	// A Node keeps its children block even if it is empty.
	return e.end(root, v.Type() == nodeType && pointerTo[Node](v).Children != nil)
}

// nodeToken returns the KindNode token of the node for v, which is not a
// pointer or interface, named as node describes.
func (e *encoder) nodeToken(v reflect.Value, name string, own, root bool) kdltext.Token {
	t := kdltext.Token{Kind: kdltext.KindNode, Value: kdltext.String(name)}
	switch {
	case root:
	case v.Type() == nodeType:
		n := pointerTo[Node](v)
		t.Value.Type = n.Type
		if own {
			t.Value = kdltext.String(n.Name).WithType(t.Value.Type)
		}
	case v.Kind() == reflect.Struct:
		fs, err := cachedFields(v.Type())
		if err != nil {
			break
		}
		if fv, ok := structField(v, fs.name); ok && own && fv.String() != "" {
			t.Value = kdltext.String(fv.String()).WithType(t.Value.Type)
		}
		if fv, ok := structField(v, fs.typ); ok {
			t.Value.Type = typeOf(fv)
		}
	}
	return t
}

// contents writes the contents of the node for v, which is not a pointer or
// interface, by the default rules.
func (e *encoder) contents(v reflect.Value, root bool) error {
	switch t := v.Type(); {
	case t == nodeType:
		n := pointerTo[Node](v)
		args := 0
		for _, en := range n.Entries {
			if en.IsArg() {
				args++
			}
			if err := e.entry(en, entryOf(en, args-1), t); err != nil {
				return err
			}
		}
		return writeNodes(e.out, n.Children)
	case isDefaultValueType(t):
		return e.value(v, argRef(1), defaultValue)
	}

	switch v.Kind() {
	case reflect.Struct:
		return e.structContents(v, root)
	case reflect.Map:
		return e.mapContents(v, root)
	case reflect.Slice, reflect.Array:
		return e.listContents(v, root)
	}
	return e.error(v.Type(), errors.New("unsupported type"))
}

// null writes a node with a single #null argument, for a nil interface or
// nil pointer to a value type, of type t.
func (e *encoder) null(t reflect.Type, name string, root bool) error {
	if err := e.start(kdltext.Token{Kind: kdltext.KindNode, Value: kdltext.String(name)}, root); err != nil {
		return err
	}
	if err := e.entry(Arg(kdltext.Null()), argRef(1), t); err != nil {
		return err
	}
	return e.end(root, false)
}

// marshaler writes the node for v, whose MarshalKDL method is m: its start,
// as for v by the default rules, its contents, from the method, and its end.
// If the method returns errors.ErrUnsupported without writing anything, the
// contents are written by the default rules.
func (e *encoder) marshaler(m Marshaler, v reflect.Value, name string, own, root bool) error {
	t := v.Type()
	for n := 0; (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && !v.IsNil(); n++ {
		if n == maxIndirections {
			return e.error(t, errCycle)
		}
		v = v.Elem()
	}
	if err := e.start(e.nodeToken(v, name, own, root), root); err != nil {
		return err
	}
	depth := e.out.StackDepth()
	_, n := e.out.StackIndex(depth)
	err := m.MarshalKDL(e.out)
	switch {
	case err == errors.ErrUnsupported && e.unchanged(depth, n):
		if err := e.contents(v, root); err != nil {
			return err
		}
	case err == errors.ErrUnsupported:
		return e.error(t, errors.New("MarshalKDL returned errors.ErrUnsupported after writing output"))
	case err != nil:
		if se, ok := errors.AsType[*SemanticError](err); ok {
			return se.within(formatPath(e.path), kdltext.Position{})
		}
		return e.error(t, err)
	case e.out.StackDepth() > depth:
		return e.error(t, errors.New("MarshalKDL started a node it did not end"))
	case e.out.StackDepth() < depth:
		return e.error(t, errors.New("MarshalKDL ended the node it was writing"))
	}
	return e.end(root, false)
}

// unchanged reports whether nothing has been written since the stack of out
// was depth levels deep, with n tokens at the innermost.
func (e *encoder) unchanged(depth int, n int64) bool {
	if e.out.StackDepth() != depth {
		return false
	}
	_, m := e.out.StackIndex(depth)
	return m == n
}

// structField returns the value of the field f of the struct v, or false if
// it is to be left out.
func structField(v reflect.Value, f *field) (reflect.Value, bool) {
	if f == nil {
		return reflect.Value{}, false
	}
	fv, err := v.FieldByIndexErr(f.index)
	if err != nil || f.omitzero && isZero(fv) {
		return reflect.Value{}, false
	}
	return fv, true
}

func (e *encoder) structContents(v reflect.Value, root bool) error {
	fs, err := cachedFields(v.Type())
	if err != nil {
		return e.error(v.Type(), err)
	}
	field := func(f *field) (reflect.Value, bool) { return structField(v, f) }

	args := 0
	for _, f := range fs.args {
		if fv, ok := field(f); ok {
			args++
			if err := e.value(fv, argRef(args), fieldValue); err != nil {
				return err
			}
		}
	}
	if fv, ok := field(fs.restArgs); ok {
		for i := range fv.Len() {
			args++
			if err := e.value(fv.Index(i), argRef(args), fieldValue); err != nil {
				return err
			}
		}
	}
	if fv, ok := field(fs.entries); ok {
		for i := range fv.Len() {
			en := fv.Index(i).Interface().(Entry)
			switch {
			case en.IsArg():
				args++
			case en.IsProp():
				if f := fs.props[en.Name]; f != nil {
					return e.entryError(propRef(en.Name), fv.Type(), takenError("property", en.Name, f))
				}
			}
			if err := e.entry(en, entryOf(en, args-1), fv.Type()); err != nil {
				return err
			}
		}
	}

	for _, f := range fs.all {
		if f.kind != fieldProp {
			continue
		}
		if fv, ok := field(f); ok {
			if err := e.prop(fv, f.name, fieldValue); err != nil {
				return err
			}
		}
	}
	if fv, ok := field(fs.restProps); ok {
		entries, err := e.sortedMap(fv)
		if err != nil {
			return err
		}
		for _, me := range entries {
			if f := fs.props[me.key]; f != nil {
				return e.entryError(propRef(me.key), fv.Type(), takenError("property", me.key, f))
			}
		}
		if err := e.mapProps(entries, fieldValue); err != nil {
			return err
		}
	}

	for _, f := range fs.all {
		if f.kind != fieldChild {
			continue
		}
		fv, ok := field(f)
		if !ok || isNilNode(fv) {
			continue
		}
		e.enter(f.name)
		if f.repeated {
			err = e.repeated(fv, f.name)
		} else {
			err = e.node(fv, f.name, false, false)
		}
		e.leave()
		if err != nil {
			return err
		}
	}
	if fv, ok := field(fs.restNodes); ok {
		var err error
		if fv.Kind() == reflect.Map {
			var entries []mapEntry
			if entries, err = e.sortedMap(fv); err == nil {
				err = e.mapChildren(entries, true, fs.children)
			}
		} else {
			err = e.listChildren(fv, true, fs.children)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// repeated writes the nodes for a child field holding the slice v, named
// name.
func (e *encoder) repeated(v reflect.Value, name string) error {
	if v.Len() == 0 {
		return nil
	}
	if isValueType(v.Type().Elem()) {
		if err := e.start(kdltext.Token{Kind: kdltext.KindNode, Value: kdltext.String(name)}, false); err != nil {
			return err
		}
		if err := e.listContents(v, false); err != nil {
			return err
		}
		return e.end(false, false)
	}
	for i := range v.Len() {
		e.element(i)
		if err := e.node(v.Index(i), name, false, false); err != nil {
			return err
		}
	}
	return nil
}

type mapEntry struct {
	key  string
	elem reflect.Value // addressable
}

func (e *encoder) sortedMap(v reflect.Value) ([]mapEntry, error) {
	if !isKeyType(v.Type().Key()) {
		return nil, e.error(v.Type(), errors.New("unsupported map key type"))
	}
	if v.Len() == 0 {
		return nil, nil
	}
	// Copy the elements to a slice, which makes them addressable.
	elems := reflect.MakeSlice(reflect.SliceOf(v.Type().Elem()), v.Len(), v.Len())
	key := reflect.New(v.Type().Key()).Elem()
	entries := make([]mapEntry, 0, v.Len())
	for iter := v.MapRange(); iter.Next(); {
		key.SetIterKey(iter)
		name, err := keyName(key)
		if err != nil {
			return nil, e.error(key.Type(), err)
		}
		elem := elems.Index(len(entries))
		elem.SetIterValue(iter)
		entries = append(entries, mapEntry{name, elem})
	}
	slices.SortFunc(entries, func(a, b mapEntry) int { return strings.Compare(a.key, b.key) })
	return entries, nil
}

// keyName returns the name of the map key k, which is addressable: its text
// if its type implements encoding.TextMarshaler, or else its string.
func keyName(k reflect.Value) (string, error) {
	if (k.Kind() == reflect.Pointer || k.Kind() == reflect.Interface) && k.IsNil() {
		return "", errors.New("a nil map key can't be marshaled")
	}
	if m, ok := asMarshaler[encoding.TextMarshaler](k); ok {
		text, err := m.MarshalText()
		return string(text), err
	}
	if k.Kind() != reflect.String {
		return "", errors.New("unsupported map key type")
	}
	return k.String(), nil
}

// mapContents writes the map v: its values of value types as properties,
// unless top is set, and its other values as children.
func (e *encoder) mapContents(v reflect.Value, top bool) error {
	entries, err := e.sortedMap(v)
	if err != nil {
		return err
	}
	if !top {
		if err := e.mapProps(entries, elemValue); err != nil {
			return err
		}
	}
	return e.mapChildren(entries, top, nil)
}

// mapProps writes the map entries with values of value types as properties,
// and, for a props field (mode fieldValue), those of types with methods too.
func (e *encoder) mapProps(entries []mapEntry, mode valueMode) error {
	for _, me := range entries {
		if t := dynamicType(me.elem); isValueType(t) || mode == fieldValue && hasMethods(t) {
			if err := e.prop(me.elem, me.key, mode); err != nil {
				return err
			}
		}
	}
	return nil
}

// mapChildren writes the map entries with values that are not of value
// types, or all of them if all is set, as child nodes. Nil pointers to nodes
// are left out. A child node may not have the name of a field in taken.
func (e *encoder) mapChildren(entries []mapEntry, all bool, taken map[string]*field) error {
	for _, me := range entries {
		if !all && isValueType(dynamicType(me.elem)) || isNilNode(me.elem) {
			continue
		}
		e.enter(me.key)
		if f := taken[me.key]; f != nil {
			err := e.error(nil, takenError("child node", me.key, f))
			e.leave()
			return err
		}
		err := e.node(me.elem, me.key, false, false)
		e.leave()
		if err != nil {
			return err
		}
	}
	return nil
}

// listContents writes the slice or array v: its elements of value types as
// arguments, unless top is set, and its other elements as children.
func (e *encoder) listContents(v reflect.Value, top bool) error {
	if !top {
		args := 0
		for i := range v.Len() {
			elem := v.Index(i)
			if !isValueType(dynamicType(elem)) {
				continue
			}
			args++
			if err := e.value(elem, argRef(args), elemValue); err != nil {
				return err
			}
		}
	}
	return e.listChildren(v, top, nil)
}

// listChildren writes the elements of the slice or array v that are not of
// value types, or all of them if all is set, as child nodes. A child node may
// not have the name of a field in taken.
func (e *encoder) listChildren(v reflect.Value, all bool, taken map[string]*field) error {
	for i := range v.Len() {
		elem := v.Index(i)
		if !all && isValueType(dynamicType(elem)) {
			continue
		}
		e.enter("-")
		e.element(i)
		e.taken = taken
		err := e.node(elem, "-", true, false)
		e.taken = nil
		e.leave()
		if err != nil {
			return err
		}
	}
	return nil
}

// checkEntry reports an error if the node being written is the document,
// which can't have arguments or properties.
func (e *encoder) checkEntry() error {
	if e.open == 0 && !e.inNode {
		return e.error(e.root, errors.New("a document can't have arguments or properties"))
	}
	return nil
}

// entry writes en, which is the given entry of the node, marshaled from a
// value of type typ.
func (e *encoder) entry(en Entry, entry entryRef, typ reflect.Type) error {
	if err := e.checkEntry(); err != nil {
		return err
	}
	if err := writeEntry(e.out, en); err != nil {
		return e.entryError(entry, typ, err)
	}
	return nil
}

// prop writes v as the property name.
func (e *encoder) prop(v reflect.Value, name string, mode valueMode) error {
	if err := e.checkEntry(); err != nil {
		return err
	}
	if err := e.out.WriteToken(kdltext.Token{Kind: kdltext.KindProp, Value: kdltext.String(name)}); err != nil {
		return e.entryError(propRef(name), v.Type(), err)
	}
	return e.value(v, propRef(name), mode)
}

// value writes v as a KDL value, the given entry of the node: an argument,
// or the value of a property whose name has been written. If mode is
// fieldValue, a type implementing Marshaler marshals itself.
func (e *encoder) value(v reflect.Value, entry entryRef, mode valueMode) error {
	if err := e.checkEntry(); err != nil {
		return err
	}
	typ := v.Type()
	if mode == fieldValue {
		// A nil pointer is #null, without calling its method. The method of
		// the value in an interface is called on the value.
		for n, w := 0, v; ; n, w = n+1, w.Elem() {
			if n == maxIndirections {
				return e.entryError(entry, typ, errCycle)
			}
			indirect := w.Kind() == reflect.Pointer || w.Kind() == reflect.Interface
			if indirect && w.IsNil() {
				break
			}
			if w.Kind() != reflect.Interface {
				if m, ok := asMarshaler[Marshaler](w); ok {
					done, err := e.valueMethod(m)
					if err != nil {
						return e.entryError(entry, typ, err)
					}
					if done {
						return nil
					}
					break
				}
			}
			if !indirect {
				break
			}
		}
	}
	val, err := rawValue(v)
	if err != nil {
		return e.entryError(entry, typ, err)
	}
	if err := e.out.WriteToken(kdltext.Token{Kind: kdltext.KindValue, Value: val}); err != nil {
		return e.entryError(entry, typ, err)
	}
	return nil
}

// rawValue returns the KDL value for v by the default rules.
func rawValue(v reflect.Value) (kdltext.Value, error) {
	for n := 0; ; n++ {
		if n == maxIndirections {
			return kdltext.Value{}, errCycle
		}
		if m, ok := asMarshaler[encoding.TextMarshaler](v); ok {
			text, err := m.MarshalText()
			return kdltext.String(string(text)), err
		}
		if v.Kind() != reflect.Pointer && v.Kind() != reflect.Interface {
			break
		}
		if v.IsNil() {
			return kdltext.Null(), nil
		}
		v = v.Elem()
	}
	if v.Type() == valueType {
		val := v.Interface().(kdltext.Value)
		if val.Kind() == kdltext.ValueInvalid {
			return val, errors.New("the zero kdltext.Value can't be marshaled")
		}
		return val, nil
	}

	switch v.Kind() {
	case reflect.Bool:
		return kdltext.Bool(v.Bool()), nil
	case reflect.String:
		return kdltext.String(v.String()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return kdltext.Int(v.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return kdltext.Uint(v.Uint()), nil
	case reflect.Float32:
		// Format with 32 bits for the shortest literal that round-trips.
		f := v.Float()
		if val, err := kdltext.Number(lexer.FormatFloat(f, 32)); err == nil {
			return val, nil
		}
		return kdltext.Float(f), nil
	case reflect.Float64:
		return kdltext.Float(v.Float()), nil
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return kdltext.String(base64.StdEncoding.EncodeToString(v.Bytes())), nil
		}
	}
	return kdltext.Value{}, fmt.Errorf("%s can't be marshaled as a single value", v.Type())
}

// valueMethod calls the MarshalKDL method m to write a single value. It
// reports false if the method returned errors.ErrUnsupported without
// writing anything, for the value to be written by the default rules.
func (e *encoder) valueMethod(m Marshaler) (bool, error) {
	depth := e.out.StackDepth()
	_, n := e.out.StackIndex(depth)
	err := m.MarshalKDL(e.out)
	switch {
	case err == errors.ErrUnsupported && e.unchanged(depth, n):
		return false, nil
	case err == errors.ErrUnsupported:
		return true, errors.New("MarshalKDL returned errors.ErrUnsupported after writing output")
	case err != nil:
		return true, err
	}
	if e.out.StackDepth() == depth {
		if k, m := e.out.StackIndex(depth); k == kdltext.KindValue && m == n+1 {
			return true, nil
		}
	}
	return true, errors.New("MarshalKDL did not write a single value")
}

// takenError returns the error for a props or children field that holds the
// property or child node name, which is that of the field f: Unmarshal would
// decode it into f.
func takenError(what, name string, f *field) error {
	return fmt.Errorf("%s %q has the name of field %s", what, name, f.goName)
}

// entryOf returns the entryRef of e, whose index among the arguments, if it is
// one, is i.
func entryOf(e Entry, i int) entryRef {
	if e.IsProp() {
		return propRef(e.Name)
	}
	return argRef(i + 1)
}

func (e *encoder) error(t reflect.Type, err error) error {
	if se, ok := errors.AsType[*SemanticError](err); ok {
		return se
	}
	return &SemanticError{action: "marshal", Path: formatPath(e.path), GoType: t, Err: err}
}

// entryError returns err, which is about the given entry of the node,
// marshaled from a value of type t, as a *SemanticError.
func (e *encoder) entryError(entry entryRef, t reflect.Type, err error) error {
	return &SemanticError{action: "marshal", Path: formatPath(e.path), Entry: entry.String(), GoType: t, Err: err}
}

// asMarshaler returns v, or a pointer to it, as a T, unless v is a nil
// pointer or interface. The types are checked first, since converting v to
// an interface may allocate.
func asMarshaler[T any](v reflect.Value) (T, bool) {
	var zero T
	if (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && v.IsNil() {
		return zero, false
	}
	iface := reflect.TypeFor[T]()
	if v.Type().Implements(iface) {
		return v.Interface().(T), true
	}
	if v.CanAddr() && reflect.PointerTo(v.Type()).Implements(iface) {
		return v.Addr().Interface().(T), true
	}
	return zero, false
}

// pointerTo returns a pointer to the T in v, without copying it if v is
// addressable.
func pointerTo[T any](v reflect.Value) *T {
	if v.CanAddr() {
		return v.Addr().Interface().(*T)
	}
	t := v.Interface().(T)
	return &t
}

// addressable returns v, copied to an addressable value if it is not
// already one, so that methods with pointer receivers can be called.
func addressable(v reflect.Value) reflect.Value {
	if v.CanAddr() {
		return v
	}
	p := reflect.New(v.Type()).Elem()
	p.Set(v)
	return p
}

// isNilNode reports whether v is a nil pointer to a type marshaled as a
// node, or an interface or pointer holding one, for which no node is written.
func isNilNode(v reflect.Value) bool {
	for n := 0; (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && !v.IsNil(); n++ {
		if n == maxIndirections {
			return false // a cycle, which node reports
		}
		v = v.Elem()
	}
	return v.Kind() == reflect.Pointer && !isValueType(v.Type())
}

// dynamicType returns the type of the value in v, if v is a non-nil
// interface, and v's type otherwise.
func dynamicType(v reflect.Value) reflect.Type {
	if v.Kind() == reflect.Interface && !v.IsNil() {
		return v.Elem().Type()
	}
	return v.Type()
}

// isZero reports whether v is zero, using its IsZero method if it has one.
func isZero(v reflect.Value) bool {
	if v.Kind() == reflect.Interface && !v.IsNil() {
		z, ok := v.Elem().Interface().(interface{ IsZero() bool })
		return ok && z.IsZero()
	}
	if z, ok := asMarshaler[interface{ IsZero() bool }](v); ok {
		return z.IsZero()
	}
	return v.IsZero()
}

// typeOf returns the type annotation held by the type field v, a string, ""
// for none, or a *string.
func typeOf(v reflect.Value) *string {
	switch {
	case v.Kind() == reflect.Pointer && v.IsNil():
		return nil
	case v.Kind() == reflect.Pointer:
		return new(v.Elem().String())
	case v.String() == "":
		return nil
	}
	return new(v.String())
}
