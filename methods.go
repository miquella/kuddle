package kuddle

import (
	"encoding"
	"errors"
	"reflect"
	"sync"

	"github.com/miquella/kuddle/kdltext"
)

// Unmarshaler is implemented by types that unmarshal themselves from KDL.
// UnmarshalKDL reads from the Decoder the contents of the value's node, or,
// for an arg, args, prop or props field, its single value. It may return
// errors.ErrUnsupported without reading anything, for the value to be
// unmarshaled by the default rules, or, for a node, delegate the node's
// contents to the default rules by calling UnmarshalDecode, which is for a
// node's contents only, not a single value. The package documentation
// describes the rules in full, under Custom marshaling.
type Unmarshaler interface {
	UnmarshalKDL(*kdltext.Decoder) error
}

var (
	unmarshalerType     = reflect.TypeFor[Unmarshaler]()
	textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()

	valueType    = reflect.TypeFor[kdltext.Value]()
	nodeType     = reflect.TypeFor[Node]()
	entryType    = reflect.TypeFor[Entry]()
	documentType = reflect.TypeFor[Document]()
)

// maxIndirections is the most pointers and interfaces followed in a row.
// Only a value that leads back to itself through them, such as p after
// p = &p, or one of a pointer type defined in terms of itself, such as
// type P *P, is likely to need more, and it is reported as a cycle.
const maxIndirections = 1000

// errCycle is the error for a value or type that leads back to itself.
var errCycle = errors.New("encountered a cycle")

// typeInfo is what unmarshaling needs to know about a type, which typeInfoOf
// computes once per type.
type typeInfo struct {
	methods      bool // see hasMethods
	value        bool // see isValueType
	entry        bool // see isEntryType
	defaultValue bool // see isDefaultValueType
	key          bool // see isKeyType
	unmarshaler  bool // whether *t implements Unmarshaler
}

var typeInfos sync.Map // reflect.Type -> *typeInfo

func typeInfoOf(t reflect.Type) *typeInfo {
	if ti, ok := typeInfos.Load(t); ok {
		return ti.(*typeInfo)
	}
	// Pointers are represented as what they point to.
	elem := t
	for n := 0; elem.Kind() == reflect.Pointer && n < maxIndirections; n++ {
		elem = elem.Elem()
	}
	elemMethods := implements(elem, unmarshalerType)
	ti := &typeInfo{
		methods:      implements(t, unmarshalerType),
		value:        !elemMethods && defaultValueType(elem),
		entry:        elemMethods || defaultValueType(elem),
		defaultValue: defaultValueType(t),
		key:          t.Kind() == reflect.String || implements(t, textUnmarshalerType),
		unmarshaler:  reflect.PointerTo(t).Implements(unmarshalerType),
	}
	typeInfos.Store(t, ti)
	return ti
}

// hasMethods reports whether t or *t implements Unmarshaler, which makes it
// marshaled as a node, except in the fields that hold arguments and
// properties.
func hasMethods(t reflect.Type) bool { return typeInfoOf(t).methods }

// isValueType reports whether t is represented by a single KDL value, rather
// than by a node. Pointers are represented as what they point to.
func isValueType(t reflect.Type) bool { return typeInfoOf(t).value }

// isEntryType reports whether t can be held by an arg or prop field: whether
// it is a value type or has methods, which then decode it from a value.
func isEntryType(t reflect.Type) bool { return typeInfoOf(t).entry }

// isDefaultValueType reports whether t, which is not a pointer, is
// represented by a single KDL value by the default rules, those for a type
// without methods.
func isDefaultValueType(t reflect.Type) bool { return typeInfoOf(t).defaultValue }

// isKeyType reports whether t can be the key type of a map holding
// properties or children: whether it is a string type, or implements
// encoding.TextUnmarshaler.
func isKeyType(t reflect.Type) bool { return typeInfoOf(t).key }

// defaultValueType computes isDefaultValueType.
func defaultValueType(t reflect.Type) bool {
	if t == valueType || implements(t, textUnmarshalerType) {
		return true
	}
	switch t.Kind() {
	case reflect.Bool, reflect.String, reflect.Interface,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return true
	case reflect.Slice:
		return t.Elem().Kind() == reflect.Uint8 // []byte, as base64
	}
	return false
}

// implements reports whether t or *t implements any of the interfaces.
func implements(t reflect.Type, ifaces ...reflect.Type) bool {
	pt := reflect.PointerTo(t)
	for _, i := range ifaces {
		if t.Implements(i) || pt.Implements(i) {
			return true
		}
	}
	return false
}
