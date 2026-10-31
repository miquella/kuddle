package kuddle_test

import (
	"errors"
	"fmt"
	"math"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miquella/kuddle"
	"github.com/miquella/kuddle/kdltext"
)

func TestMarshalDocument(t *testing.T) {
	input := `(t)a 1 k=(u8)2 "x y" k=3 {
    b #true
    c
}
e
("")f ("")1 {}
`
	var doc kuddle.Document
	if err := kuddle.Unmarshal([]byte(input), &doc); err != nil {
		t.Fatal(err)
	}
	got, err := kuddle.Marshal(&doc)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != input {
		t.Errorf("got\n%s\nwant\n%s", got, input)
	}

	got, err = kuddle.Marshal(doc, kdltext.WithVersion(kdltext.V1), kdltext.WithIndent("  "))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.NewReplacer("    ", "  ", "#true", "true").Replace(input)
	if string(got) != want {
		t.Errorf("v1: got\n%s\nwant\n%s", got, want)
	}
}

func TestMarshalEmptyDocument(t *testing.T) {
	for _, doc := range []kuddle.Document{{}, {Nodes: []kuddle.Node{}}} {
		got, err := kuddle.Marshal(doc)
		if err != nil || string(got) != "\n" {
			t.Errorf("%#v: got %q, %v; want a newline", doc, got, err)
		}
	}

	// A document of comments is empty without them.
	doc := kuddle.Document{Nodes: []kuddle.Node{{Kind: kuddle.NodeComment, Comment: kdltext.Comment{Kind: kdltext.CommentLine, Text: "c"}}}}
	got, err := kuddle.Marshal(doc, kdltext.DiscardComments(true))
	if err != nil || string(got) != "\n" {
		t.Errorf("comments omitted: got %q, %v; want a newline", got, err)
	}
}

func TestMarshalEncodeChildren(t *testing.T) {
	var b strings.Builder
	enc := kdltext.NewEncoder(&b)
	enc.WriteToken(kdltext.Token{Kind: kdltext.KindNode, Value: kdltext.String("parent")})
	doc := kuddle.Document{Nodes: []kuddle.Node{{Name: "child"}}}
	if err := kuddle.MarshalEncode(enc, doc); err != nil {
		t.Fatal(err)
	}
	enc.WriteToken(kdltext.Token{Kind: kdltext.KindEnd})
	if want := "parent {\n    child\n}\n"; b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}

func testConfig() Config {
	return Config{
		Common: Common{Debug: true},
		Name:   "web",
		Ports:  []int{80, 443},
		Listeners: []Listener{
			{Addr: netip.MustParseAddrPort("127.0.0.1:80")},
			{Addr: netip.MustParseAddrPort("[::1]:443"), TLS: true},
		},
		Routes: []Route{
			{Path: "/", Methods: []string{"GET"}, Headers: map[string]string{"X-B": "2", "X-A": "1"}, Backend: new("app")},
		},
		Env:    map[string]string{"HOME": "/root"},
		Limits: [2]float64{1.5, 2},
		Data:   []byte("hi"),
		Extra:  &kuddle.Node{Name: "extra", Entries: []kuddle.Entry{kuddle.Arg(kdltext.Int(1)), kuddle.Arg(kdltext.Int(2))}},
		Other:  []kuddle.Node{{Name: "mystery"}},
	}
}

func TestMarshal(t *testing.T) {
	in := testConfig()
	got, err := kuddle.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	want := `debug #true
name web
port 80 443
listen "127.0.0.1:80" tls=#false
listen "[::1]:443" tls=#true
route "/" GET X-A="1" X-B="2" {
    backend app
}
env HOME="/root"
limits 1.5 2.0
data "aGk="
extra 1 2
mystery
`
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}

	var out Config
	if err := kuddle.Unmarshal(got, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Errorf("round trip:\ngot  %+v\nwant %+v", out, in)
	}
}

// writeValue writes v to enc as a KindValue token.
func writeValue(enc *kdltext.Encoder, v kdltext.Value) error {
	return enc.WriteToken(kdltext.Token{Kind: kdltext.KindValue, Value: v})
}

// hexInt is an int written in hexadecimal.
type hexInt int

func (h hexInt) MarshalKDL(enc *kdltext.Encoder) error {
	v, err := kdltext.Number(fmt.Sprintf("0x%x", int(h)))
	if err != nil {
		return err
	}
	return writeValue(enc, v)
}

func (u upper) MarshalKDL(enc *kdltext.Encoder) error {
	return writeValue(enc, kdltext.String(string(u)))
}

func (c csv) MarshalKDL(enc *kdltext.Encoder) error {
	return writeValue(enc, kdltext.String(strings.Join(c, ",")))
}

func (p pair) MarshalKDL(enc *kdltext.Encoder) error {
	if err := writeValue(enc, kdltext.String(p.k)); err != nil {
		return err
	}
	return writeValue(enc, kdltext.String(p.v))
}

func (m maybe) MarshalKDL(enc *kdltext.Encoder) error {
	if m != -1 {
		return errors.ErrUnsupported
	}
	return writeValue(enc, kdltext.String("none"))
}

func (l listen) MarshalKDL(enc *kdltext.Encoder) error {
	if l.Port == 0 {
		return errors.New("no port")
	}
	type plain listen
	return kuddle.MarshalEncode(enc, plain(l))
}

// TestMarshalMethods marshals values with methods and unmarshals them back,
// through the same methods, as nodes and as properties.
func TestMarshalMethods(t *testing.T) {
	type props struct {
		U    upper `kdl:"u,prop"`
		Tags csv   `kdl:"tags,prop"`
		M    maybe `kdl:"m,prop"`
		M2   maybe `kdl:"m2,prop"`
	}
	type methods struct {
		N      props           `kdl:"n"`
		U      upper           `kdl:"u"`
		Tags   csv             `kdl:"tags"`
		M      maybe           `kdl:"m"`
		M2     maybe           `kdl:"m2"`
		P      pair            `kdl:"p"`
		Pairs  []pair          `kdl:"pairs"`
		Listen []listen        `kdl:"listen"`
		Doc    kuddle.Document `kdl:"doc"`
	}
	in := methods{
		N:      props{"A B", csv{"a", "b"}, -1, 3},
		U:      "C",
		Tags:   csv{"c", "d"},
		M:      -1,
		M2:     4,
		P:      pair{"a", "b"},
		Pairs:  []pair{{"c", "d"}, {"e", "f"}},
		Listen: []listen{{"listen", "localhost", 80, true}, {"listen", "example.com", 443, false}},
		Doc:    kuddle.Document{Nodes: []kuddle.Node{{Name: "x", Entries: []kuddle.Entry{kuddle.Arg(kdltext.Int(1))}}}},
	}
	want := `n u="A B" tags=a,b m=none m2=3
u C
tags c,d
m none
m2 4
p a b
pairs c d
pairs e f
listen localhost port=80 {
    tls #true
}
listen example.com port=443 {
    tls #false
}
doc {
    x 1
}
`
	got, err := kuddle.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	var out methods
	if err := kuddle.Unmarshal(got, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Errorf("round trip:\ngot  %+v\nwant %+v", out, in)
	}

	_, err = kuddle.Marshal(map[string]listen{"l": {}})
	if want := "kuddle: cannot marshal node l from Go kuddle_test.listen: no port"; err == nil || err.Error() != want {
		t.Errorf("no port:\ngot  %v\nwant %s", err, want)
	}
}

// writer writes toks, and returns err.
type writer struct {
	toks []kdltext.Token
	err  error
}

func (w writer) MarshalKDL(enc *kdltext.Encoder) error {
	for _, t := range w.toks {
		if err := enc.WriteToken(t); err != nil {
			return err
		}
	}
	return w.err
}

func TestMarshalMethodErrors(t *testing.T) {
	type node struct {
		W writer `kdl:"w"`
	}
	type prop struct {
		A struct {
			W writer `kdl:"w,prop"`
		} `kdl:"a"`
	}
	one := kdltext.Token{Kind: kdltext.KindValue, Value: kdltext.Int(1)}
	tests := []struct {
		prop bool
		w    writer
		want string
	}{
		{false, writer{[]kdltext.Token{{Kind: kdltext.KindNode, Value: kdltext.String("x")}}, nil}, "node w from Go kuddle_test.writer: MarshalKDL started a node it did not end"},
		{false, writer{[]kdltext.Token{{Kind: kdltext.KindEnd}}, nil}, "node w from Go kuddle_test.writer: MarshalKDL ended the node it was writing"},
		{false, writer{[]kdltext.Token{one}, errors.ErrUnsupported}, "node w from Go kuddle_test.writer: MarshalKDL returned errors.ErrUnsupported after writing output"},
		{false, writer{nil, errors.New("bad")}, "node w from Go kuddle_test.writer: bad"},
		{false, writer{nil, errors.ErrUnsupported}, ""},
		{true, writer{nil, nil}, `property "w" of node a from Go kuddle_test.writer: MarshalKDL did not write a single value`},
		{true, writer{[]kdltext.Token{one, one}, nil}, `property "w" of node a from Go kuddle_test.writer: MarshalKDL did not write a single value`},
		{true, writer{[]kdltext.Token{one}, errors.ErrUnsupported}, `property "w" of node a from Go kuddle_test.writer: MarshalKDL returned errors.ErrUnsupported after writing output`},
		{true, writer{nil, errors.ErrUnsupported}, `property "w" of node a from Go kuddle_test.writer: kuddle_test.writer can't be marshaled as a single value`},
	}
	for _, tt := range tests {
		var in any = node{tt.w}
		if tt.prop {
			var p prop
			p.A.W = tt.w
			in = p
		}
		_, err := kuddle.Marshal(in)
		switch {
		case tt.want == "" && err != nil:
			t.Errorf("%+v: %v", tt.w, err)
		case tt.want != "" && (err == nil || !strings.HasSuffix(err.Error(), tt.want)):
			t.Errorf("%+v:\ngot  %v\nwant ...%s", tt.w, err, tt.want)
		}
	}
}

func (u unsupported) MarshalKDL(*kdltext.Encoder) error    { return errors.ErrUnsupported }
func (u unsupportedInt) MarshalKDL(*kdltext.Encoder) error { return errors.ErrUnsupported }
func (u unsupportedMap) MarshalKDL(*kdltext.Encoder) error { return errors.ErrUnsupported }

// TestMarshalUnsupported encodes values whose methods return
// errors.ErrUnsupported by the default rules, as arguments, properties, nodes
// and the document.
func TestMarshalUnsupported(t *testing.T) {
	got, err := kuddle.Marshal(unsupportedWant)
	if err != nil || string(got) != unsupportedKDL {
		t.Errorf("got\n%s%v\nwant\n%s", got, err, unsupportedKDL)
	}

	got, err = kuddle.Marshal(unsupportedMap{"x": 1, "y": 2})
	if want := "x 1\ny 2\n"; err != nil || string(got) != want {
		t.Errorf("document: got %q, %v; want %q", got, err, want)
	}
}

func TestMarshalEncodeContents(t *testing.T) {
	var b strings.Builder
	enc := kdltext.NewEncoder(&b)
	enc.WriteToken(kdltext.Token{Kind: kdltext.KindNode, Value: kdltext.String("a")})
	v := struct {
		A int `kdl:",arg"`
		K int `kdl:"k,prop"`
		B int `kdl:"b"`
	}{1, 2, 3}
	if err := kuddle.MarshalEncode(enc, v); err != nil {
		t.Fatal(err)
	}
	enc.WriteToken(kdltext.Token{Kind: kdltext.KindEnd})
	if want := "a 1 k=2 {\n    b 3\n}\n"; b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}

func TestMarshalValues(t *testing.T) {
	type item struct {
		Name string `kdl:",name"`
		V    int    `kdl:",arg"`
	}
	v := struct {
		F32     float32           `kdl:"f32"`
		NaN     float64           `kdl:"nan"`
		Big     uint64            `kdl:"big"`
		Nil     *int              `kdl:"nil"`
		Omitted *int              `kdl:"omitted,omitzero"`
		Time    time.Time         `kdl:"time,omitzero"`
		AnyTime any               `kdl:"anytime,omitzero"`
		Hex     hexInt            `kdl:"hex"`
		Pair    pair              `kdl:"pair"`
		Pairs   []pair            `kdl:"pairs"`
		Empty   []pair            `kdl:"empty"`
		Any     []any             `kdl:"any"`
		Typed   kdltext.Value     `kdl:"typed"`
		Items   []item            `kdl:",children"`
		Props   map[string]any    `kdl:",props"`
		Arg     string            `kdl:",arg"`
		Nested  map[string][]int  `kdl:"nested"`
		Unused  map[string]string `kdl:"unused,omitzero"`
	}{
		F32:     0.1,
		NaN:     math.NaN(),
		Big:     math.MaxUint64,
		AnyTime: time.Time{},
		Hex:     255,
		Pair:    pair{"a", "b"},
		Pairs:   []pair{{"c", "d"}, {"e", "f"}},
		Any:     []any{"s", nil, 1.5, []int{1}},
		Typed:   typed("u8", kdltext.Int(3)),
		Items:   []item{{"x", 1}, {"", 2}},
		Props:   map[string]any{"p": true},
		Arg:     "arg",
		Nested:  map[string][]int{"a": {1, 2}},
	}
	got, err := kuddle.Marshal(struct {
		V any `kdl:"v"`
	}{&v})
	if err != nil {
		t.Fatal(err)
	}
	want := `v arg p=#true {
    f32 0.1
    "nan" #nan
    big 18446744073709551615
    nil #null
    hex 0xff
    pair a b
    pairs c d
    pairs e f
    any s #null 1.5 {
        - 1
    }
    typed (u8)3
    nested {
        a 1 2
    }
    x 1
    - 2
}
`
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestMarshalIntegralFloats checks that a float with an integral value is
// written with a fraction, so that it unmarshals back into an empty interface
// as a float64, not an int64.
func TestMarshalIntegralFloats(t *testing.T) {
	in := map[string]any{"x": 1.0, "y": float32(2), "z": int64(3)}
	got, err := kuddle.Marshal(in)
	if want := "x 1.0\ny 2.0\nz 3\n"; err != nil || string(got) != want {
		t.Fatalf("Marshal: got %q, %v; want %q", got, err, want)
	}
	var out map[string]any
	if err := kuddle.Unmarshal(got, &out); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"x": 1.0, "y": 2.0, "z": int64(3)}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("Unmarshal: got %#v, want %#v", out, want)
	}
}

// TestMarshalEmptyNodeName checks that a Node keeps its name even when it is
// empty, where a struct without a name falls back to "-".
func TestMarshalEmptyNodeName(t *testing.T) {
	in := []byte("\"\" 1\nn 2\n")
	var nodes []kuddle.Node
	if err := kuddle.Unmarshal(in, &nodes); err != nil {
		t.Fatal(err)
	}
	got, err := kuddle.Marshal(nodes)
	if err != nil || string(got) != string(in) {
		t.Errorf("Marshal: got %q, %v; want %q", got, err, in)
	}
}

func TestMarshalTypes(t *testing.T) {
	type str struct {
		Type string `kdl:",type"`
	}
	type ptr struct {
		Type *string `kdl:",type"`
	}
	v := struct {
		A str         `kdl:"a"`
		B str         `kdl:"b"`
		C ptr         `kdl:"c"`
		D ptr         `kdl:"d"`
		E ptr         `kdl:"e"`
		F kuddle.Node `kdl:"f"`
	}{
		A: str{"t"},
		C: ptr{new("t")},
		D: ptr{new("")},
		F: kuddle.Node{Type: new(""), Children: []kuddle.Node{}},
	}
	got, err := kuddle.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	want := "(t)a\nb\n(t)c\n(\"\")d\ne\n(\"\")f {}\n"
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestMarshalTopLevel(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{
		{map[string]int{"b": 2, "a": 1}, "a 1\nb 2\n"},
		{[]any{1, "x", map[string]int{"k": 1}}, "- 1\n- x\n- k=1\n"},
		{kuddle.Node{Name: "n"}, "n\n"},
		{&kuddle.Document{}, "\n"},
		{map[string]any{"n": kuddle.Node{Name: "x", Entries: []kuddle.Entry{kuddle.Arg(kdltext.Int(1))}}}, "n 1\n"},
		{[]any{kuddle.Node{Name: "x"}, kuddle.Document{Nodes: []kuddle.Node{{Name: "y"}}}, &kuddle.Document{}}, "x\n- {\n    y\n}\n-\n"},
	}
	for _, tt := range tests {
		got, err := kuddle.Marshal(tt.in)
		if err != nil || string(got) != tt.want {
			t.Errorf("%#v: got %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
}

type cycle struct {
	Next *cycle `kdl:"next"`
}

func TestMarshalErrors(t *testing.T) {
	c := &cycle{}
	c.Next = c
	tests := []struct {
		in   any
		want string
	}{
		{nil, "kuddle: cannot marshal nil"},
		{1, "kuddle: cannot marshal the document from Go int: a document can't be marshaled from a value type"},
		{struct {
			A int `kdl:",arg"`
		}{}, "kuddle: cannot marshal the document from Go struct { A int \"kdl:\\\",arg\\\"\" }: a document can't have arguments or properties"},
		{struct {
			C chan int `kdl:"c"`
		}{}, `kuddle: cannot marshal node c from Go chan int: unsupported type`},
		{struct {
			C []chan int `kdl:",arg"`
		}{}, "an arg field must be a value type"},
		{struct {
			A struct {
				B struct {
					F func() `kdl:",prop"`
				} `kdl:"b"`
			} `kdl:"a"`
		}{}, `kuddle: cannot marshal node a/b from Go struct { F func() "kdl:\",prop\"" }: struct field struct { F func() "kdl:\",prop\"" }.F: a prop field must be a value type, or have a MarshalKDL or UnmarshalKDL method, not func()`},
		{map[string]struct {
			F any `kdl:",prop"`
		}{"n": {F: func() {}}}, `kuddle: cannot marshal property "F" of node n from Go interface {}: func() can't be marshaled as a single value`},
		{map[string]struct {
			F []any `kdl:",args"`
		}{"n": {F: []any{map[string]int{}}}}, `kuddle: cannot marshal argument 1 of node n from Go interface {}: map[string]int can't be marshaled as a single value`},
		{c, "encountered a cycle"},
		{struct {
			N struct {
				V kdltext.Value `kdl:",arg"`
			} `kdl:"n"`
		}{}, `kuddle: cannot marshal argument 1 of node n from Go kdltext.Value: the zero kdltext.Value can't be marshaled`},
		{map[string]any{"n": kuddle.Node{Entries: []kuddle.Entry{kuddle.Arg(kdltext.Int(1)), kuddle.Arg(kdltext.Value{})}}},
			`kuddle: cannot marshal argument 2 of node n from Go kuddle.Node: kdltext: invalid value`},
	}
	for _, tt := range tests {
		_, err := kuddle.Marshal(tt.in)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%T: got %v, want %s", tt.in, err, tt.want)
		}
	}

	_, err := kuddle.Marshal(map[string]float64{"a": math.Inf(1)}, kdltext.WithVersion(kdltext.V1))
	if err == nil || !strings.Contains(err.Error(), "#inf can't be written in KDL v1") {
		t.Errorf("inf in v1: got %v", err)
	}

	in := map[string]map[string]float64{"a": {"x": math.NaN()}}
	_, err = kuddle.Marshal(in, kdltext.WithVersion(kdltext.V1))
	se, ok := errors.AsType[*kuddle.SemanticError](err)
	if !ok || se.Path != "a" || se.Entry != `property "x"` || se.GoType != reflect.TypeFor[float64]() {
		t.Fatalf("NaN in v1: got %#v, want a *SemanticError for property \"x\" of a", err)
	}
	if want := `kuddle: cannot marshal property "x" of node a from Go float64: kdltext: #nan can't be written in KDL v1`; err.Error() != want {
		t.Errorf("NaN in v1: got %q, want %q", err, want)
	}
	if errors.Unwrap(err) == nil {
		t.Errorf("NaN in v1: %v does not wrap the encoder's error", err)
	}
}

// TestMarshalTakenNames checks that Marshal reports an error for a props or
// children field that holds a property or child node with the name of a prop
// or child field, which Unmarshal would decode into that field instead.
func TestMarshalEntries(t *testing.T) {
	type entries struct {
		First int            `kdl:",arg"`
		P     string         `kdl:"p,prop"`
		Rest  []kuddle.Entry `kdl:",entries"`
		Kid   int            `kdl:"kid"`
	}
	typed := kdltext.Int(3)
	typed.Type = new("t")
	in := map[string]entries{"n": {First: 1, P: "x", Kid: 5, Rest: []kuddle.Entry{
		kuddle.Prop("a", kdltext.Int(2)),
		kuddle.Arg(typed),
		kuddle.Prop("a", kdltext.Int(4)),
	}}}
	out, err := kuddle.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	// The entries follow the arg fields, and the prop fields follow them.
	if want := "n 1 a=2 (t)3 a=4 p=x {\n    kid 5\n}\n"; string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}

	// The document has no arguments or properties.
	_, err = kuddle.Marshal(struct {
		Rest []kuddle.Entry `kdl:",entries"`
	}{[]kuddle.Entry{kuddle.Arg(kdltext.Int(1))}})
	if err == nil || !strings.Contains(err.Error(), "a document can't have arguments or properties") {
		t.Errorf("document: got %v", err)
	}
}

func TestMarshalTakenNames(t *testing.T) {
	type props struct {
		Port int            `kdl:"port,prop,omitzero"`
		Rest map[string]int `kdl:",props"`
	}
	type childMap struct {
		Log  string            `kdl:"log"`
		Rest map[string]string `kdl:",children"`
	}
	type childList struct {
		Log  string        `kdl:"log"`
		Rest []kuddle.Node `kdl:",children"`
	}
	type entries struct {
		Port int            `kdl:"port,prop,omitzero"`
		Rest []kuddle.Entry `kdl:",entries"`
	}
	node := func(name string) kuddle.Node {
		return kuddle.Node{Name: name, Entries: []kuddle.Entry{kuddle.Arg(kdltext.Int(1))}}
	}
	tests := []struct {
		in   any
		want string // the output, or the error if it starts with "kuddle:"
	}{
		{map[string]props{"n": {1, map[string]int{"host": 2}}}, "n port=1 host=2\n"},
		{map[string]props{"n": {1, map[string]int{"port": 2}}},
			`kuddle: cannot marshal property "port" of node n from Go map[string]int: property "port" has the name of field Port`},
		// Unmarshal would decode it into the field, even one left out.
		{map[string]props{"n": {0, map[string]int{"port": 2}}},
			`kuddle: cannot marshal property "port" of node n from Go map[string]int: property "port" has the name of field Port`},
		{map[string]entries{"n": {1, []kuddle.Entry{kuddle.Prop("host", kdltext.Int(2))}}}, "n host=2 port=1\n"},
		{map[string]entries{"n": {0, []kuddle.Entry{kuddle.Prop("port", kdltext.Int(2))}}},
			`kuddle: cannot marshal property "port" of node n from Go []kuddle.Entry: property "port" has the name of field Port`},
		{childMap{"debug", map[string]string{"level": "x"}}, "log debug\nlevel x\n"},
		{childMap{"debug", map[string]string{"log": "x"}},
			`kuddle: cannot marshal node log: child node "log" has the name of field Log`},
		{childList{"debug", []kuddle.Node{node("level")}}, "log debug\nlevel 1\n"},
		{childList{"debug", []kuddle.Node{node("level"), node("log")}},
			`kuddle: cannot marshal node log[1]: child node "log" has the name of field Log`},
	}
	for _, tt := range tests {
		b, err := kuddle.Marshal(tt.in)
		got := string(b)
		if err != nil {
			got = err.Error()
			if _, ok := errors.AsType[*kuddle.SemanticError](err); !ok {
				t.Errorf("%+v: err = %#v, want a *SemanticError", tt.in, err)
			}
		}
		if got != tt.want {
			t.Errorf("%+v:\ngot  %s\nwant %s", tt.in, got, tt.want)
		}
	}
}

func TestMarshalErrorPath(t *testing.T) {
	type listen struct {
		Port any `kdl:"port,prop"`
	}
	type named struct {
		Name string `kdl:",name"`
		Port any    `kdl:"port,prop"`
	}
	bad := func() {}
	tests := []struct {
		in   any
		path string
	}{
		{struct {
			Routes []listen `kdl:"route"`
		}{[]listen{{1}, {2}, {bad}}}, "route[2]"},
		{map[string]map[string]listen{"server": {"a/b": {bad}}}, `server/"a/b"`},
		{map[string]map[string]listen{"server": {"null": {bad}}}, `server/"null"`},
		{map[string][]any{"list": {1, named{"x", 1}, named{"y", bad}}}, "list/y[2]"},
		{struct {
			Rest []any `kdl:",children"`
		}{[]any{named{"a", 1}, listen{bad}}}, "-[1]"},
	}
	for _, tt := range tests {
		_, err := kuddle.Marshal(tt.in)
		se, ok := errors.AsType[*kuddle.SemanticError](err)
		if !ok || se.Path != tt.path {
			t.Errorf("%T: err = %v, want path %s", tt.in, err, tt.path)
		}
	}
}

func TestMarshalTextKeys(t *testing.T) {
	addr := netip.MustParseAddr
	in := map[string]any{
		"hosts": map[netip.Addr]string{addr("10.0.0.2"): "b", addr("10.0.0.1"): "a"},
		"ports": map[netip.Addr][]int{addr("::1"): {1}},
	}
	got, err := kuddle.Marshal(in)
	want := "hosts \"10.0.0.1\"=a \"10.0.0.2\"=b\nports {\n    ::1 1\n}\n"
	if err != nil || string(got) != want {
		t.Errorf("got %q, %v; want %q", got, err, want)
	}

	_, err = kuddle.Marshal(map[string]map[errKey]int{"m": {"bad": 1}})
	se, ok := errors.AsType[*kuddle.SemanticError](err)
	if !ok || se.Path != "m" || se.GoType != reflect.TypeFor[errKey]() {
		t.Errorf("bad key: err = %v", err)
	}
}

// errKey is a map key that fails to marshal as "bad".
type errKey string

func (k errKey) MarshalText() ([]byte, error) {
	if k == "bad" {
		return nil, errors.New("bad key")
	}
	return []byte(k), nil
}

func TestMarshalNullValue(t *testing.T) {
	var v struct {
		N kdltext.Value `kdl:"n"`
	}
	if err := kuddle.Unmarshal([]byte("n #null\n"), &v); err != nil {
		t.Fatal(err)
	}
	got, err := kuddle.Marshal(v)
	if want := "n #null\n"; err != nil || string(got) != want {
		t.Errorf("got %q, %v; want %q", got, err, want)
	}
}

func TestMarshalNil(t *testing.T) {
	type server struct {
		Host string `kdl:",arg"`
	}
	in := struct {
		Int     *int               `kdl:"int"`
		Any     any                `kdl:"any"`
		Server  *server            `kdl:"server"`
		InAny   any                `kdl:"inany"`
		Writer  *writer            `kdl:"writer"`
		Servers map[string]*server `kdl:"servers"`
		Props   struct {
			W *writer `kdl:"w,prop"`
			I *int    `kdl:"i,prop"`
			A any     `kdl:"a,prop"`
		} `kdl:"props"`
	}{
		InAny:   (*server)(nil),
		Servers: map[string]*server{"a": nil, "b": {"x"}},
	}
	in.Props.A = (*writer)(nil)
	got, err := kuddle.Marshal(in)
	want := "int #null\nany #null\nservers {\n    b x\n}\nprops w=#null i=#null a=#null\n"
	if err != nil || string(got) != want {
		t.Errorf("got %q, %v; want %q", got, err, want)
	}

	for _, in := range []any{
		[]*server{nil},
		[]any{(*server)(nil)},
		struct {
			S []*server `kdl:"s"`
		}{[]*server{nil}},
		struct {
			S []any `kdl:",children"`
		}{[]any{(*kuddle.Document)(nil)}},
	} {
		_, err := kuddle.Marshal(in)
		if want := "a nil pointer to a node can't be marshaled as an element"; err == nil || !strings.HasSuffix(err.Error(), want) {
			t.Errorf("%#v: got %v, want ...%s", in, err, want)
		}
	}
	if _, err := kuddle.Marshal((*server)(nil)); err == nil || err.Error() != "kuddle: cannot marshal nil" {
		t.Errorf("nil *server: got %v, want kuddle: cannot marshal nil", err)
	}
}

// TestMarshalNilInterfaces marshals nil interfaces whose types have methods,
// which can't be called, as #null, or as no map key.
func TestMarshalNilInterfaces(t *testing.T) {
	type texter interface{ MarshalText() ([]byte, error) }
	type zeroer interface{ IsZero() bool }
	type node struct {
		Arg   texter            `kdl:",arg"`
		Prop  texter            `kdl:"p,prop"`
		M     kuddle.Marshaler  `kdl:"m,prop"`
		Z     zeroer            `kdl:"z,prop,omitzero"`
		Child texter            `kdl:"c"`
		List  []texter          `kdl:"l"`
		Map   map[string]texter `kdl:"map"`
	}
	in := map[string]node{"n": {List: []texter{nil}, Map: map[string]texter{"k": nil}}}
	got, err := kuddle.Marshal(in)
	want := "n #null p=#null m=#null {\n    c #null\n    l #null\n    map k=#null\n}\n"
	if err != nil || string(got) != want {
		t.Errorf("got %q, %v; want %q", got, err, want)
	}

	_, err = kuddle.Marshal(map[texter]int{nil: 1})
	if want := "a nil map key can't be marshaled"; err == nil || !strings.HasSuffix(err.Error(), want) {
		t.Errorf("nil key: got %v, want ...%s", err, want)
	}
}

func TestMarshalRepeatedInterfaces(t *testing.T) {
	type items struct {
		Items []any `kdl:"item"`
	}
	in := items{Items: []any{1, "x", map[string]any{"a": map[string]any{"b": 1}}}}
	want := "item 1 x {\n    - {\n        a b=1\n    }\n}\n"
	got, err := kuddle.Marshal(in)
	if err != nil || string(got) != want {
		t.Fatalf("Marshal: got %q, %v; want %q", got, err, want)
	}

	var out items
	if err := kuddle.Unmarshal(got, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 3 || out.Items[0] != int64(1) || out.Items[1] != "x" {
		t.Fatalf("Unmarshal: got %#v", out.Items)
	}
	if _, ok := out.Items[2].(*kuddle.Node); !ok {
		t.Errorf("Unmarshal: got %#v, want a *kuddle.Node", out.Items[2])
	}
	if again, err := kuddle.Marshal(out); err != nil || string(again) != want {
		t.Errorf("Marshal of Unmarshal: got %q, %v; want %q", again, err, want)
	}
}

// TestNameQuoting checks that the paths of errors write names as the Encoder
// writes the names of nodes, and that a name that is not valid UTF-8 can be
// in a path but not in a document.
func TestNameQuoting(t *testing.T) {
	type port struct {
		Port any `kdl:"port,prop"`
	}
	tests := []struct{ name, want string }{
		{"plain", "plain"},
		{"a b", `"a b"`},
		{"true", `"true"`},
		{"a/b", `"a/b"`},
		{"\b\f", `"\b\f"`},
		{"\u00a0", "\"\u00a0\""},
		{"\u0085", `"\u{85}"`},
		{"\u200e", `"\u{200e}"`},
		{"a\xffb", "\"a\ufffdb\""},
	}
	for _, tt := range tests {
		_, err := kuddle.Marshal(map[string]port{tt.name: {make(chan int)}})
		if se, ok := errors.AsType[*kuddle.SemanticError](err); !ok || se.Path != tt.want {
			t.Errorf("%q: path of %v, want %s", tt.name, err, tt.want)
		}

		b, err := kuddle.Marshal(map[string]int{tt.name: 1})
		if strings.ContainsRune(tt.want, '\ufffd') {
			if err == nil || !strings.Contains(err.Error(), "invalid UTF-8 in "+tt.want) {
				t.Errorf("%q: Marshal = %q, %v, want an invalid UTF-8 error", tt.name, b, err)
			}
		} else if err != nil || string(b) != tt.want+" 1\n" {
			t.Errorf("%q: Marshal = %q, %v, want %s 1", tt.name, b, err, tt.want)
		}
	}
}

// TestMarshalNestingDepth checks that Marshal writes values nested as
// deeply as the Encoder allows, and reports an error for one nested deeper,
// and that it reports a cycle long before that.
func TestMarshalNestingDepth(t *testing.T) {
	const limit = 10000
	// list returns n cycle nodes, each the next of the one before.
	list := func(n int) *cycle {
		var head *cycle
		for range n {
			head = &cycle{Next: head}
		}
		return head
	}
	in := struct {
		Next *cycle `kdl:"next"`
	}{list(limit + 1)}
	if _, err := kuddle.Marshal(in, kdltext.WithIndent("")); err != nil {
		t.Errorf("%d deep: %v", limit+1, err)
	}
	in.Next = list(limit + 2)
	if _, err := kuddle.Marshal(in, kdltext.WithIndent("")); err == nil || !strings.Contains(err.Error(), "children blocks nested deeper than 10000") {
		t.Errorf("%d deep: err = %v", limit+2, err)
	}

	c := &cycle{}
	c.Next = c
	_, err := kuddle.Marshal(c)
	se, ok := errors.AsType[*kuddle.SemanticError](err)
	if !ok || !strings.Contains(err.Error(), "encountered a cycle") {
		t.Fatalf("cycle: err = %v", err)
	}
	if n := strings.Count(se.Path, "/") + 1; n > 1100 {
		t.Errorf("cycle: reported %d nodes deep, want soon after 1000", n)
	}
}

func TestMarshalInvalidNodeName(t *testing.T) {
	type inner struct {
		X int `kdl:"x"`
	}
	_, err := kuddle.Marshal(map[string]inner{"a\xff": {X: 1}})
	se, ok := errors.AsType[*kuddle.SemanticError](err)
	if !ok {
		t.Fatalf("err = %v, want a *SemanticError", err)
	}
	if want := "\"a�\""; se.Path != want {
		t.Errorf("Path = %s, want %s", se.Path, want)
	}
}

// selfPointer is a pointer type defined in terms of itself.
type selfPointer *selfPointer

// TestMarshalSelfReference checks that Marshal reports a cycle for a pointer
// or interface that leads back to itself, rather than following it forever.
func TestMarshalSelfReference(t *testing.T) {
	var x any
	x = &x
	var p selfPointer
	p = &p
	for _, tt := range []struct {
		name string
		in   any
	}{
		{"node of an interface", &x},
		{"node of a pointer type", p},
		{"property", struct {
			X any `kdl:"x,prop"`
		}{x}},
		{"argument", struct {
			X []any `kdl:",args"`
		}{[]any{x}}},
		{"element", map[string][]any{"a": {x}}},
	} {
		_, err := kuddle.Marshal(map[string]any{"n": tt.in})
		if _, ok := errors.AsType[*kuddle.SemanticError](err); !ok || !strings.Contains(err.Error(), "encountered a cycle") {
			t.Errorf("%s: err = %v", tt.name, err)
		}
	}
}
