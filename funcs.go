package kuddle

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/miquella/kuddle/internal/kdlopts"
	"github.com/miquella/kuddle/kdltext"
)

// Marshalers is a list of functions that marshal values of particular types.
// Create one with MarshalFunc or JoinMarshalers, and pass it to Marshal,
// MarshalWrite, MarshalEncode or Document.From with WithMarshalers. A nil
// *Marshalers is an empty list.
type Marshalers struct {
	fns   []*marshalFunc
	cache sync.Map // reflect.Type -> []*marshalFunc, those that apply to it
}

// Unmarshalers is a list of functions that unmarshal values of particular
// types. Create one with UnmarshalFunc or JoinUnmarshalers, and pass it to
// Unmarshal, UnmarshalRead, UnmarshalDecode or Document.To with
// WithUnmarshalers. A nil *Unmarshalers is an empty list.
type Unmarshalers struct {
	fns   []*unmarshalFunc
	cache sync.Map // reflect.Type -> []*unmarshalFunc, those that apply to it
}

// marshalFunc is a function of MarshalFunc, for the type T.
type marshalFunc struct {
	typ reflect.Type // T
	fn  func(*kdltext.Encoder, reflect.Value) error
}

// unmarshalFunc is a function of UnmarshalFunc, for the type T.
type unmarshalFunc struct {
	typ reflect.Type // T
	fn  func(*kdltext.Decoder, reflect.Value) error
}

// MarshalFunc returns a Marshalers holding just fn, which marshals values of
// type T. For a value type, fn writes the value's single KindValue token to
// the Encoder, wherever the value appears. For any other type, fn writes the
// contents of the value's node, as a MarshalKDL method would (see
// Marshaler). fn runs before T's methods. To have the value marshaled as if
// fn weren't there, fn returns errors.ErrUnsupported without writing
// anything. For a node, fn can also call MarshalEncode with the value
// converted to a type without functions or methods; calling it with the value
// unchanged would call fn again. The package documentation covers the full
// rules, under Custom marshaling.
//
// If T is an interface type, fn marshals values whose types, or pointers to
// them, implement T. If T is a pointer type, fn marshals values of the type
// it points to, and is given a pointer to each. T can't be a named pointer
// type. A nil pointer or interface is marshaled without calling fn. fn must
// not keep the Encoder or the value.
func MarshalFunc[T any](fn func(*kdltext.Encoder, T) error) *Marshalers {
	t := reflect.TypeFor[T]()
	if t.Kind() == reflect.Pointer && t.Name() != "" {
		panic(fmt.Sprintf("kuddle: MarshalFunc for named pointer type %v", t))
	}
	call := func(enc *kdltext.Encoder, v reflect.Value) error {
		x, _ := reflect.TypeAssert[T](v)
		return fn(enc, x)
	}
	return &Marshalers{fns: []*marshalFunc{{t, call}}}
}

// UnmarshalFunc returns an Unmarshalers holding just fn, which unmarshals
// values of the type T points to, and is given a pointer to each. For a value
// type, fn reads the value's single KindValue token from the Decoder,
// wherever the value appears. For any other type, fn reads the contents of
// the value's node, as an UnmarshalKDL method would (see Unmarshaler). fn
// runs before the type's methods. To have the value unmarshaled as if fn
// weren't there, fn returns errors.ErrUnsupported without reading anything.
// For a node, fn can also call UnmarshalDecode with the pointer converted to
// a pointer to a type without functions or methods; calling it with the
// pointer unchanged would call fn again. The package documentation covers the
// full rules, under Custom marshaling.
//
// T must be an unnamed pointer type, or an interface type, in which case fn
// unmarshals values whose pointers implement T. UnmarshalFunc panics
// otherwise. fn must not keep the Decoder or the pointer.
func UnmarshalFunc[T any](fn func(*kdltext.Decoder, T) error) *Unmarshalers {
	t := reflect.TypeFor[T]()
	if !(t.Kind() == reflect.Pointer && t.Name() == "" || t.Kind() == reflect.Interface) {
		panic(fmt.Sprintf("kuddle: UnmarshalFunc for %v, which is not an unnamed pointer or interface type", t))
	}
	call := func(dec *kdltext.Decoder, v reflect.Value) error {
		x, _ := reflect.TypeAssert[T](v)
		return fn(dec, x)
	}
	return &Unmarshalers{fns: []*unmarshalFunc{{t, call}}}
}

// JoinMarshalers combines the functions of ms into a single Marshalers. For
// a given value, functions from earlier Marshalers run first, and later ones
// run only if those return errors.ErrUnsupported.
func JoinMarshalers(ms ...*Marshalers) *Marshalers {
	j := new(Marshalers)
	for _, m := range ms {
		if m != nil {
			j.fns = append(j.fns, m.fns...)
		}
	}
	return j
}

// JoinUnmarshalers combines the functions of us into a single Unmarshalers.
// For a given value, functions from earlier Unmarshalers run first, and later
// ones run only if those return errors.ErrUnsupported.
func JoinUnmarshalers(us ...*Unmarshalers) *Unmarshalers {
	j := new(Unmarshalers)
	for _, u := range us {
		if u != nil {
			j.fns = append(j.fns, u.fns...)
		}
	}
	return j
}

// WithMarshalers sets functions that marshal values of particular types.
// They run before the types' methods and the default rules (see Custom
// marshaling). A later WithMarshalers replaces an earlier one; use
// JoinMarshalers to combine functions.
func WithMarshalers(m *Marshalers) Options {
	return &kdlopts.Struct{Present: kdlopts.Marshalers, Marshalers: m}
}

// WithUnmarshalers sets functions that unmarshal values of particular types.
// They run before the types' methods and the default rules (see Custom
// marshaling). A later WithUnmarshalers replaces an earlier one; use
// JoinUnmarshalers to combine functions.
func WithUnmarshalers(u *Unmarshalers) Options {
	return &kdlopts.Struct{Present: kdlopts.Unmarshalers, Unmarshalers: u}
}

// applies reports whether fn marshals the values of type t, which is not a
// pointer.
func (fn *marshalFunc) applies(t reflect.Type) bool {
	switch fn.typ.Kind() {
	case reflect.Interface:
		return t.Implements(fn.typ) || reflect.PointerTo(t).Implements(fn.typ)
	case reflect.Pointer:
		return reflect.PointerTo(t) == fn.typ
	}
	return t == fn.typ
}

// call calls fn for v, of a type it applies to, passing v itself, or a
// pointer to it, made addressable if need be.
func (fn *marshalFunc) call(enc *kdltext.Encoder, v reflect.Value) error {
	if fn.typ.Kind() == reflect.Pointer || fn.typ.Kind() == reflect.Interface && !v.Type().Implements(fn.typ) {
		v = addressable(v).Addr()
	}
	return fn.fn(enc, v)
}

// applies reports whether fn unmarshals the values of type t, which is not a
// pointer.
func (fn *unmarshalFunc) applies(t reflect.Type) bool {
	if fn.typ.Kind() == reflect.Interface {
		return reflect.PointerTo(t).Implements(fn.typ)
	}
	return reflect.PointerTo(t) == fn.typ
}

// call calls fn for v, which is addressable and of a type it applies to,
// passing a pointer to v.
func (fn *unmarshalFunc) call(dec *kdltext.Decoder, v reflect.Value) error {
	return fn.fn(dec, v.Addr())
}

// lookup returns the functions of m that apply to t, in order.
func (m *Marshalers) lookup(t reflect.Type) []*marshalFunc {
	if m == nil || len(m.fns) == 0 {
		return nil
	}
	if fns, ok := m.cache.Load(t); ok {
		return fns.([]*marshalFunc)
	}
	var fns []*marshalFunc
	for _, fn := range m.fns {
		if fn.applies(t) {
			fns = append(fns, fn)
		}
	}
	m.cache.Store(t, fns)
	return fns
}

// lookup returns the functions of u that apply to t, in order.
func (u *Unmarshalers) lookup(t reflect.Type) []*unmarshalFunc {
	if u == nil || len(u.fns) == 0 {
		return nil
	}
	if fns, ok := u.cache.Load(t); ok {
		return fns.([]*unmarshalFunc)
	}
	var fns []*unmarshalFunc
	for _, fn := range u.fns {
		if fn.applies(t) {
			fns = append(fns, fn)
		}
	}
	u.cache.Store(t, fns)
	return fns
}

// funcs are the functions of the options of a call, from WithMarshalers and
// WithUnmarshalers. They don't change how types are classified: a function
// for a value type reads or writes its single value, and one for any other
// type the contents of its node. The *funcs of a call is nil if it has no
// functions.
type funcs struct {
	m *Marshalers
	u *Unmarshalers
}

// newFuncs returns the funcs of the options o, or nil if they have none.
func newFuncs(o Options) *funcs {
	s, _ := o.(*kdlopts.Struct)
	if s == nil {
		return nil
	}
	m, _ := s.Marshalers.(*Marshalers)
	u, _ := s.Unmarshalers.(*Unmarshalers)
	if (m == nil || len(m.fns) == 0) && (u == nil || len(u.fns) == 0) {
		return nil
	}
	return &funcs{m: m, u: u}
}

// marshalers are the functions and MarshalKDL method that marshal a value,
// in order of precedence.
type marshalers struct {
	fns    []*marshalFunc
	v      reflect.Value // the value for fns, with its pointers and interfaces followed
	method Marshaler     // or nil
}

func (ms *marshalers) len() int {
	if ms.method != nil {
		return len(ms.fns) + 1
	}
	return len(ms.fns)
}

func (ms *marshalers) at(i int) marshalCall {
	if i < len(ms.fns) {
		return marshalCall{fn: ms.fns[i], v: ms.v}
	}
	return marshalCall{method: ms.method}
}

// marshalCall is a MarshalKDL method, or a function for the value v.
type marshalCall struct {
	method Marshaler
	fn     *marshalFunc
	v      reflect.Value
}

// name names the method or function in errors.
func (c marshalCall) name() string {
	if c.fn != nil {
		return "the MarshalFunc for " + c.fn.typ.String()
	}
	return "MarshalKDL"
}

// unmarshalers are the functions and UnmarshalKDL method that unmarshal a
// value, in order of precedence.
type unmarshalers struct {
	fns    []*unmarshalFunc
	v      reflect.Value // the value for fns, which is addressable
	method Unmarshaler   // or nil
}

func (us *unmarshalers) len() int {
	if us.method != nil {
		return len(us.fns) + 1
	}
	return len(us.fns)
}

func (us *unmarshalers) at(i int) unmarshalCall {
	if i < len(us.fns) {
		return unmarshalCall{fn: us.fns[i], v: us.v}
	}
	return unmarshalCall{method: us.method}
}

// unmarshalCall is an UnmarshalKDL method, or a function for the value v.
type unmarshalCall struct {
	method Unmarshaler
	fn     *unmarshalFunc
	v      reflect.Value
}

// name names the method or function in errors.
func (c unmarshalCall) name() string {
	if c.fn != nil {
		return "the UnmarshalFunc for " + c.fn.typ.String()
	}
	return "UnmarshalKDL"
}
