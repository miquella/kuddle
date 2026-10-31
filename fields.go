package kuddle

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/miquella/kuddle/internal/lexer"
)

// fieldKind is what a struct field corresponds to in a node.
type fieldKind int

const (
	fieldChild    fieldKind = iota // the child nodes with the field's name
	fieldArg                       // the next argument
	fieldArgs                      // the remaining arguments
	fieldProp                      // the property with the field's name
	fieldProps                     // the remaining properties
	fieldEntries                   // the remaining arguments, properties and comments
	fieldChildren                  // the remaining child nodes
	fieldName                      // the node's name
	fieldType                      // the node's type annotation
)

var fieldKinds = map[string]fieldKind{
	"child":    fieldChild,
	"arg":      fieldArg,
	"args":     fieldArgs,
	"prop":     fieldProp,
	"props":    fieldProps,
	"entries":  fieldEntries,
	"children": fieldChildren,
	"name":     fieldName,
	"type":     fieldType,
}

// field is a struct field that corresponds to part of a node.
type field struct {
	id       int // index in structFields.all
	goName   string
	name     string // the child or property name
	kind     fieldKind
	index    []int // for reflect.Value.FieldByIndex
	typ      reflect.Type
	omitzero bool

	// repeated is set for a child field that holds every child node with its
	// name: a slice that is not itself a value type, and has no methods.
	repeated bool
}

// structFields describes how a struct type corresponds to a node.
type structFields struct {
	all []*field // in declaration order

	args      []*field
	restArgs  *field
	props     map[string]*field
	restProps *field
	entries   *field
	children  map[string]*field
	restNodes *field
	name, typ *field
}

var fieldCache sync.Map // reflect.Type -> fieldsResult

type fieldsResult struct {
	fields *structFields
	err    error
}

func cachedFields(t reflect.Type) (*structFields, error) {
	if r, ok := fieldCache.Load(t); ok {
		r := r.(fieldsResult)
		return r.fields, r.err
	}
	fs, err := makeFields(t)
	fieldCache.Store(t, fieldsResult{fs, err})
	return fs, err
}

func makeFields(t reflect.Type) (*structFields, error) {
	fs := &structFields{props: map[string]*field{}, children: map[string]*field{}}
	if err := fs.add(t, nil); err != nil {
		return nil, err
	}
	for _, f := range fs.all {
		if err := fs.index(f); err != nil {
			return nil, fmt.Errorf("struct field %s.%s: %w", t, f.goName, err)
		}
	}
	if f := fs.entries; f != nil && (fs.restArgs != nil || fs.restProps != nil) {
		return nil, fmt.Errorf("struct field %s.%s: an entries field can't be combined with an args or props field, whose arguments or properties it would hold", t, f.goName)
	}
	return fs, nil
}

// add adds the fields of the struct type t, whose fields are reached through
// index, inlining embedded structs.
func (fs *structFields) add(t reflect.Type, index []int) error {
	for i := range t.NumField() {
		sf := t.Field(i)
		tag, hasTag := sf.Tag.Lookup("kdl")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		idx := append(index[:len(index):len(index)], i)

		if sf.Anonymous && name == "" && opts == "" {
			et := sf.Type
			if et.Kind() == reflect.Pointer {
				et = et.Elem()
			}
			if et.Kind() == reflect.Struct {
				// A pointer to an unexported type can't be allocated.
				if sf.Type.Kind() == reflect.Pointer && !sf.IsExported() {
					continue
				}
				if err := fs.add(et, idx); err != nil {
					return err
				}
				continue
			}
		}
		if !sf.IsExported() {
			continue
		}

		f := &field{id: len(fs.all), goName: sf.Name, name: sf.Name, index: idx, typ: sf.Type}
		if hasTag && name != "" {
			f.name = name
		}
		kindSet := false
		for opt := range strings.SplitSeq(opts, ",") {
			if opt == "" {
				continue
			}
			if opt == "omitzero" {
				f.omitzero = true
				continue
			}
			k, ok := fieldKinds[opt]
			if !ok {
				return fmt.Errorf("struct field %s.%s: unknown kdl tag option %q", t, sf.Name, opt)
			}
			if kindSet {
				return fmt.Errorf("struct field %s.%s: more than one of %s in kdl tag", t, sf.Name, kindOptions)
			}
			f.kind, kindSet = k, true
		}
		if hasTag && name != "" && f.kind != fieldChild && f.kind != fieldProp {
			return fmt.Errorf("struct field %s.%s: the name %s in the kdl tag is not used with the %s option, only child and prop fields have names", t, sf.Name, lexer.Quote(name), kindName(f.kind))
		}
		f.repeated = f.kind == fieldChild && f.typ.Kind() == reflect.Slice && !isValueType(f.typ) && !hasMethods(f.typ)
		fs.all = append(fs.all, f)
	}
	return nil
}

const kindOptions = "child, arg, args, prop, props, entries, children, name and type"

// index records f in the lookup fields of fs, checking that its type suits
// its kind.
func (fs *structFields) index(f *field) error {
	t := f.typ
	single := func(p **field) error {
		if *p != nil {
			return fmt.Errorf("more than one field is tagged %q (also %s)", kindName(f.kind), (*p).goName)
		}
		*p = f
		return nil
	}
	named := func(m map[string]*field, what string) error {
		if other, ok := m[f.name]; ok {
			return fmt.Errorf("%s name %s is also used by field %s", what, lexer.Quote(f.name), other.goName)
		}
		m[f.name] = f
		return nil
	}

	switch f.kind {
	case fieldChild:
		if err := checkNode(t); err != nil {
			return fmt.Errorf("a child field can't be a %s: %w", t, err)
		}
		return named(fs.children, "child node")
	case fieldArg:
		if !isEntryType(t) {
			return fmt.Errorf("an arg field must be a value type, or have a MarshalKDL or UnmarshalKDL method, not %s", t)
		}
		if err := checkValue(t); err != nil {
			return fmt.Errorf("an arg field can't be a %s: %w", t, err)
		}
		fs.args = append(fs.args, f)
	case fieldArgs:
		if t.Kind() != reflect.Slice || isValueType(t) || hasMethods(t) || !isEntryType(t.Elem()) {
			return fmt.Errorf("an args field must be a slice of a value type, or of a type with a MarshalKDL or UnmarshalKDL method, not %s", t)
		}
		if err := checkValue(t.Elem()); err != nil {
			return fmt.Errorf("an args field can't be a %s: %w", t, err)
		}
		return single(&fs.restArgs)
	case fieldProp:
		if !isEntryType(t) {
			return fmt.Errorf("a prop field must be a value type, or have a MarshalKDL or UnmarshalKDL method, not %s", t)
		}
		if err := checkValue(t); err != nil {
			return fmt.Errorf("a prop field can't be a %s: %w", t, err)
		}
		return named(fs.props, "property")
	case fieldProps:
		if t.Kind() != reflect.Map || hasMethods(t) || !isKeyType(t.Key()) || !isEntryType(t.Elem()) {
			return fmt.Errorf("a props field must be a map from string or text keys to a value type, or to a type with a MarshalKDL or UnmarshalKDL method, not %s", t)
		}
		if err := checkValue(t.Elem()); err != nil {
			return fmt.Errorf("a props field can't be a %s: %w", t, err)
		}
		return single(&fs.restProps)
	case fieldEntries:
		if t.Kind() != reflect.Slice || hasMethods(t) || t.Elem() != entryType {
			return fmt.Errorf("an entries field must be a slice of kuddle.Entry, not %s", t)
		}
		return single(&fs.entries)
	case fieldChildren:
		if hasMethods(t) || t.Kind() != reflect.Slice && (t.Kind() != reflect.Map || !isKeyType(t.Key())) {
			return fmt.Errorf("a children field must be a slice or a map from string or text keys, not %s", t)
		}
		if err := checkNode(t.Elem()); err != nil {
			return fmt.Errorf("a children field can't be a %s: %w", t, err)
		}
		return single(&fs.restNodes)
	case fieldName:
		if t.Kind() != reflect.String {
			return fmt.Errorf("a name field must be a string, not %s", t)
		}
		return single(&fs.name)
	case fieldType:
		if t.Kind() != reflect.String && t != reflect.TypeFor[*string]() {
			return fmt.Errorf("a type field must be a string or *string, not %s", t)
		}
		return single(&fs.typ)
	}
	return nil
}

// checkValue reports an error if t, the type of an argument or property, is
// one of the package's types that can't hold a single value.
func checkValue(t reflect.Type) error {
	for n := 0; t.Kind() == reflect.Pointer && n < maxIndirections; n++ {
		t = t.Elem()
	}
	if t == documentType {
		return errors.New("a Document holds only nodes, not arguments or properties")
	}
	return nil
}

// checkNode reports an error if t, the type of a node, is one of the
// package's types that can't hold a node, or is a slice, array or map with
// such elements in place of nodes.
func checkNode(t reflect.Type) error {
	for range maxIndirections {
		switch {
		case t == entryType:
			return errors.New("an Entry is an argument, property or comment, not a node, and belongs in an entries field")
		case t == commentType:
			return errors.New("a kdltext.Comment is a comment, not a node")
		case t.Kind() == reflect.Pointer:
			t = t.Elem()
		case isValueType(t) || hasMethods(t):
			return nil
		case t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map:
			t = t.Elem()
		default:
			return nil
		}
	}
	return nil
}

func kindName(k fieldKind) string {
	for name, kind := range fieldKinds {
		if kind == k {
			return name
		}
	}
	return "invalid"
}

// fieldByIndex returns the field of v at index, allocating any nil embedded
// struct pointers on the way.
func fieldByIndex(v reflect.Value, index []int) reflect.Value {
	for i, x := range index {
		if i > 0 && v.Kind() == reflect.Pointer {
			if v.IsNil() {
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.Field(x)
	}
	return v
}
