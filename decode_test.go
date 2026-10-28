package kuddle_test

import (
	"errors"
	"fmt"
	"math"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/miquella/kuddle"
	"github.com/miquella/kuddle/kdltext"
)

type Listener struct {
	Addr netip.AddrPort `kdl:",arg"`
	TLS  bool           `kdl:"tls,prop"`
}

type Route struct {
	Path    string            `kdl:",arg"`
	Methods []string          `kdl:",args"`
	Headers map[string]string `kdl:",props"`
	Backend *string           `kdl:"backend"`
}

type Common struct {
	Debug bool `kdl:"debug"`
}

type Config struct {
	Common
	Name      string            `kdl:"name"`
	Ports     []int             `kdl:"port"`
	Listeners []Listener        `kdl:"listen"`
	Routes    []Route           `kdl:"route"`
	Env       map[string]string `kdl:"env"`
	Limits    [2]float64        `kdl:"limits"`
	Data      []byte            `kdl:"data"`
	Extra     any               `kdl:"extra"`
	Other     []kuddle.Node     `kdl:",children"`
	Ignored   string            `kdl:"-"`
}

func TestUnmarshal(t *testing.T) {
	input := `
name "web"
debug #true
port 80 443
port 8080
listen "127.0.0.1:80"
listen "[::1]:443" tls=#true
route "/" GET POST X-A="" {
	backend "app"
}
route "/health"
env { HOME "/root"; USER root }
env SHELL=sh
limits 1.5 2
data "aGk="
extra 1 2
mystery 1
`
	want := Config{
		Common: Common{Debug: true},
		Name:   "web",
		Ports:  []int{80, 443, 8080},
		Listeners: []Listener{
			{Addr: netip.MustParseAddrPort("127.0.0.1:80")},
			{Addr: netip.MustParseAddrPort("[::1]:443"), TLS: true},
		},
		Routes: []Route{
			{Path: "/", Methods: []string{"GET", "POST"}, Headers: map[string]string{"X-A": ""}, Backend: new("app")},
			{Path: "/health"},
		},
		Env:    map[string]string{"HOME": "/root", "USER": "root", "SHELL": "sh"},
		Limits: [2]float64{1.5, 2},
		Data:   []byte("hi"),
		Extra: &kuddle.Node{Name: "extra", Entries: []kuddle.Entry{
			kuddle.Arg(num("1")), kuddle.Arg(num("2")),
		}},
		Other: []kuddle.Node{{Name: "mystery", Entries: []kuddle.Entry{kuddle.Arg(num("1"))}}},
	}

	var got Config
	if err := kuddle.Unmarshal([]byte(input), &got); err != nil {
		t.Fatal(err)
	}
	gv, wv := reflect.ValueOf(got), reflect.ValueOf(want)
	for i := range gv.NumField() {
		if g, w := gv.Field(i).Interface(), wv.Field(i).Interface(); !reflect.DeepEqual(g, w) {
			t.Errorf("%s:\ngot  %#v\nwant %#v", gv.Type().Field(i).Name, g, w)
		}
	}
}

func TestUnmarshalValues(t *testing.T) {
	var v struct {
		I8   int8           `kdl:"i8"`
		U    uint           `kdl:"u"`
		F32  float32        `kdl:"f32"`
		Inf  float64        `kdl:"neg-inf"`
		Any  []any          `kdl:"any"`
		Ptr  *int           `kdl:"ptr"`
		Null *int           `kdl:"nil"`
		Val  kdltext.Value  `kdl:"val"`
		Node kuddle.Node    `kdl:"node"`
		Map  map[string]any `kdl:"map"`
	}
	v.Null = new(1)
	input := `
i8 -0x80
u 0b1010
f32 1.5e3
neg-inf #-inf
any "s" #true #null 3 1.5 0xffffffffffffffff
ptr 7
nil #null
val (u8)200
(t)node a=1
map a=1 { b 2; c 3 4 }
`
	if err := kuddle.Unmarshal([]byte(input), &v); err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprintf("%d %d %g %g %v %d %v %s:%s %s/%s/%d %v %v %v",
		v.I8, v.U, v.F32, v.Inf, v.Any, *v.Ptr, v.Null, *v.Val.Type, v.Val,
		*v.Node.Type, v.Node.Name, len(v.Node.Entries), v.Map["a"], v.Map["b"], v.Map["c"].(*kuddle.Node).Args())
	want := "-128 10 1500 -Inf [s true <nil> 3 1.5 1.8446744073709552e+19] 7 <nil> u8:200 t/node/1 1 2 [3 4]"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if !math.IsInf(v.Inf, -1) {
		t.Errorf("inf = %v", v.Inf)
	}
}

func TestUnmarshalTopLevel(t *testing.T) {
	input := []byte("a 1\nb 2\n")

	var m map[string]int
	if err := kuddle.Unmarshal(input, &m); err != nil || m["a"] != 1 || m["b"] != 2 {
		t.Errorf("map: %v, %v", m, err)
	}
	var s []int
	if err := kuddle.Unmarshal(input, &s); err != nil || fmt.Sprint(s) != "[1 2]" {
		t.Errorf("slice: %v, %v", s, err)
	}
	var a any
	if err := kuddle.Unmarshal(input, &a); err != nil {
		t.Errorf("any: %v", err)
	} else if doc, ok := a.(*kuddle.Document); !ok || len(doc.Nodes) != 2 {
		t.Errorf("any: got %#v, want a *Document", a)
	}
	var i int
	if err := kuddle.Unmarshal(input, &i); err == nil {
		t.Error("int: no error")
	}
	if err := kuddle.Unmarshal(input, i); err == nil {
		t.Error("non-pointer: no error")
	}
}

// readValue reads a KindValue token from dec.
func readValue(dec *kdltext.Decoder) (kdltext.Value, error) {
	t, err := dec.ReadToken()
	if err == nil && t.Kind != kdltext.KindValue {
		err = fmt.Errorf("want a value, not %s", t.Kind)
	}
	return t.Value, err
}

// upper is a string decoded in upper case, from a single value: an argument
// or property, or the single argument of a node.
type upper string

func (u *upper) UnmarshalKDL(dec *kdltext.Decoder) error {
	v, err := readValue(dec)
	*u = upper(strings.ToUpper(v.String()))
	return err
}

// csv is a slice decoded from a single comma-separated string, by its method.
type csv []string

func (c *csv) UnmarshalKDL(dec *kdltext.Decoder) error {
	v, err := readValue(dec)
	*c = strings.Split(v.String(), ",")
	return err
}

// pair is decoded from the two arguments of a node.
type pair struct{ k, v string }

func (p *pair) UnmarshalKDL(dec *kdltext.Decoder) error {
	var args []string
	for dec.PeekKind() == kdltext.KindValue {
		v, _ := readValue(dec)
		args = append(args, v.String())
	}
	if len(args) != 2 {
		return errors.New("want two arguments")
	}
	p.k, p.v = args[0], args[1]
	return nil
}

// maybe is an int, or -1 for "none". Its method decodes "none", and leaves
// numbers to the default rules.
type maybe int

func (m *maybe) UnmarshalKDL(dec *kdltext.Decoder) error {
	t, err := dec.PeekToken()
	if err != nil {
		return err
	}
	if t.Kind != kdltext.KindValue || t.Value.Kind() != kdltext.ValueString || t.Value.String() != "none" {
		return errors.ErrUnsupported
	}
	dec.ReadToken()
	*m = -1
	return nil
}

// listen is decoded by the default rules, through UnmarshalDecode, and then
// checked.
type listen struct {
	Name string `kdl:",name"`
	Host string `kdl:",arg"`
	Port int    `kdl:"port,prop"`
	TLS  bool   `kdl:"tls"`
}

func (l *listen) UnmarshalKDL(dec *kdltext.Decoder) error {
	type plain listen
	if err := kuddle.UnmarshalDecode(dec, (*plain)(l)); err != nil {
		return err
	}
	if l.Port == 0 {
		return errors.New("no port")
	}
	return nil
}

func TestUnmarshalMethods(t *testing.T) {
	type wrap struct {
		N struct {
			U    upper `kdl:"u,prop"`
			Tags csv   `kdl:"tags,prop"`
			M    maybe `kdl:"m,prop"`
			M2   maybe `kdl:"m2,prop"`
		} `kdl:"n"`
		U      upper    `kdl:"u"`
		Tags   csv      `kdl:"tags"`
		M      maybe    `kdl:"m"`
		M2     maybe    `kdl:"m2"`
		P      pair     `kdl:"p"`
		Pairs  []pair   `kdl:"pairs"`
		Listen []listen `kdl:"listen"`
	}
	input := `
n u=abc tags="a,b" m=none m2=3
u def
tags "c,d,e"
m none
m2 4
p a b
pairs c d
pairs e f
listen "::1" port=80 { tls #true; }
listen "0.0.0.0" port=443
`
	var w wrap
	if err := kuddle.Unmarshal([]byte(input), &w); err != nil {
		t.Fatal(err)
	}
	if w.N.U != "ABC" || !slices.Equal(w.N.Tags, csv{"a", "b"}) || w.N.M != -1 || w.N.M2 != 3 {
		t.Errorf("properties: got %+v", w.N)
	}
	if w.U != "DEF" || !slices.Equal(w.Tags, csv{"c", "d", "e"}) || w.M != -1 || w.M2 != 4 {
		t.Errorf("nodes: got %+v %+v %+v %+v", w.U, w.Tags, w.M, w.M2)
	}
	if w.P != (pair{"a", "b"}) || !slices.Equal(w.Pairs, []pair{{"c", "d"}, {"e", "f"}}) {
		t.Errorf("pairs: got %+v %+v", w.P, w.Pairs)
	}
	if want := []listen{{"listen", "::1", 80, true}, {"listen", "0.0.0.0", 443, false}}; !slices.Equal(w.Listen, want) {
		t.Errorf("listen: got %+v, want %+v", w.Listen, want)
	}

	tests := []struct{ input, want string }{
		{"\np a", `kuddle: cannot unmarshal node p (line 2, column 1) into Go kuddle_test.pair: want two arguments`},
		{"listen x", `kuddle: cannot unmarshal node listen[0] (line 1, column 1) into Go kuddle_test.listen: no port`},
		{"listen x port=y", `kuddle: cannot unmarshal property "port" of node listen[0] (line 1, column 1) into Go int: expected a number, found string y`},
		{"listen x port=1 {\n  tls 1\n}", `kuddle: cannot unmarshal argument 1 of node listen[0]/tls (line 2, column 3) into Go bool: expected #true or #false, found number 1`},
	}
	for _, tt := range tests {
		err := kuddle.Unmarshal([]byte(tt.input), &w)
		if err == nil || err.Error() != tt.want {
			t.Errorf("%q:\ngot  %v\nwant %s", tt.input, err, tt.want)
		}
	}
}

// reader reads n tokens, and returns err.
type reader struct {
	n   int
	err error
}

func (r *reader) UnmarshalKDL(dec *kdltext.Decoder) error {
	for range r.n {
		if _, err := dec.ReadToken(); err != nil {
			return err
		}
	}
	return r.err
}

func TestUnmarshalMethodErrors(t *testing.T) {
	type wrap struct {
		R reader `kdl:"r"`
		A struct {
			R reader `kdl:"r,prop"`
		} `kdl:"a"`
	}
	tests := []struct {
		input string
		r     reader
		want  string
	}{
		{"r 1 2", reader{1, nil}, "node r (line 1, column 1) into Go kuddle_test.reader: UnmarshalKDL did not read all of the node"},
		{"r 1", reader{2, nil}, "node r (line 1, column 1) into Go kuddle_test.reader: UnmarshalKDL read past the end of the node"},
		{"r 1\nx", reader{3, nil}, "node r (line 1, column 1) into Go kuddle_test.reader: UnmarshalKDL read past the end of the node"},
		{"r 1", reader{1, errors.ErrUnsupported}, "UnmarshalKDL returned errors.ErrUnsupported after reading input"},
		{"r 1", reader{0, errors.New("bad")}, "node r (line 1, column 1) into Go kuddle_test.reader: bad"},
		{"r 1", reader{0, errors.ErrUnsupported}, "argument 1 of node r (line 1, column 1) into Go kuddle_test.reader: unknown to the Go type"},
		{"r", reader{0, errors.ErrUnsupported}, ""},
		{"a r=1", reader{0, nil}, `property "r" of node a (line 1, column 1) into Go kuddle_test.reader: UnmarshalKDL did not read the value`},
		{"a r=1 2", reader{2, nil}, `property "r" of node a (line 1, column 1) into Go kuddle_test.reader: UnmarshalKDL read more than the value`},
		{"a r=1", reader{1, errors.ErrUnsupported}, `property "r" of node a (line 1, column 1) into Go kuddle_test.reader: UnmarshalKDL returned errors.ErrUnsupported after reading input`},
		{"a r=1", reader{0, errors.ErrUnsupported}, `property "r" of node a (line 1, column 1) into Go kuddle_test.reader: kuddle_test.reader can't be decoded from a single value`},
	}
	for _, tt := range tests {
		var w wrap
		w.R, w.A.R = tt.r, tt.r
		err := kuddle.Unmarshal([]byte(tt.input), &w)
		switch {
		case tt.want == "" && err != nil:
			t.Errorf("%q with %+v: %v", tt.input, tt.r, err)
		case tt.want != "" && (err == nil || !strings.HasSuffix(err.Error(), tt.want)):
			t.Errorf("%q with %+v:\ngot  %v\nwant ...%s", tt.input, tt.r, err, tt.want)
		}
	}
}

// valueKind is the kind of a value, decoded from an argument or property,
// or from the single argument of a node.
type valueKind kdltext.ValueKind

func (k *valueKind) UnmarshalKDL(dec *kdltext.Decoder) error {
	v, err := readValue(dec)
	*k = valueKind(v.Kind())
	return err
}

// unsupported, unsupportedInt and unsupportedMap have methods that return
// errors.ErrUnsupported, for the default rules to apply instead.
type unsupported struct {
	A int `kdl:",arg"`
	B int `kdl:"b,prop"`
}

type unsupportedInt int

type unsupportedMap map[string]int

func (u *unsupported) UnmarshalKDL(*kdltext.Decoder) error    { return errors.ErrUnsupported }
func (u *unsupportedInt) UnmarshalKDL(*kdltext.Decoder) error { return errors.ErrUnsupported }
func (u *unsupportedMap) UnmarshalKDL(*kdltext.Decoder) error { return errors.ErrUnsupported }

type unsupportedValues struct {
	Arg   unsupportedInt            `kdl:",arg"`
	Args  []unsupportedInt          `kdl:",args"`
	Prop  unsupportedInt            `kdl:"p,prop"`
	Props map[string]unsupportedInt `kdl:",props"`
}

type unsupportedNodes struct {
	Values unsupportedValues      `kdl:"values"`
	Node   unsupported            `kdl:"node"`
	Int    unsupportedInt         `kdl:"int"`
	Map    unsupportedMap         `kdl:"map"`
	List   []unsupported          `kdl:"list"`
	Nodes  map[string]unsupported `kdl:"nodes"`
	Rest   []unsupported          `kdl:",children"`
}

// unsupportedKDL is the document for unsupportedWant.
const unsupportedKDL = `values 1 2 3 p=4 q=5
node 6 b=7
int 8
map x=9
list 10 b=11
list 12 b=0
nodes {
    y 13 b=14
}
- 15 b=16
`

var unsupportedWant = unsupportedNodes{
	Values: unsupportedValues{1, []unsupportedInt{2, 3}, 4, map[string]unsupportedInt{"q": 5}},
	Node:   unsupported{6, 7},
	Int:    8,
	Map:    unsupportedMap{"x": 9},
	List:   []unsupported{{10, 11}, {12, 0}},
	Nodes:  map[string]unsupported{"y": {13, 14}},
	Rest:   []unsupported{{15, 16}},
}

// TestUnmarshalUnsupported decodes values whose methods return
// errors.ErrUnsupported by the default rules, from arguments, properties,
// nodes and the document.
func TestUnmarshalUnsupported(t *testing.T) {
	var got unsupportedNodes
	if err := kuddle.Unmarshal([]byte(unsupportedKDL), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, unsupportedWant) {
		t.Errorf("got  %+v\nwant %+v", got, unsupportedWant)
	}

	var m unsupportedMap
	if err := kuddle.Unmarshal([]byte("x 1\ny 2"), &m); err != nil || !reflect.DeepEqual(m, unsupportedMap{"x": 1, "y": 2}) {
		t.Errorf("document: got %v, %v", m, err)
	}
}

func TestUnmarshalNull(t *testing.T) {
	type server struct {
		Host *string `kdl:",arg"`
		Port int     `kdl:"port,prop"`
	}
	var v struct {
		Val  kdltext.Value `kdl:"val"`
		Prop struct {
			Typed kdltext.Value `kdl:"typed,prop"`
			Kind  valueKind     `kdl:"kind,prop"`
			PKind *valueKind    `kdl:"pkind,prop"`
		} `kdl:"prop"`
		Kind   valueKind  `kdl:"kind"`
		PKind  *valueKind `kdl:"pkind"`
		PInt   *int       `kdl:"pint"`
		Server *server    `kdl:"server"`
		Addr   netip.Addr `kdl:"addr"`
		Int    int        `kdl:"int"`
	}
	v.Prop.PKind, v.PInt = new(valueKind(kdltext.ValueString)), new(1)
	v.Addr, v.Int = netip.MustParseAddr("::1"), 1
	input := "val #null\nprop typed=(t)#null kind=#null pkind=#null\nkind #null\npkind #null\npint #null\nserver #null port=80\naddr #null\nint #null\n"
	if err := kuddle.Unmarshal([]byte(input), &v); err != nil {
		t.Fatal(err)
	}
	if !v.Val.Equal(kdltext.Null()) {
		t.Errorf("val = %v, want #null", v.Val)
	}
	typed := kdltext.Null()
	typed.Type = new("t")
	if !v.Prop.Typed.Equal(typed) {
		t.Errorf("typed = %v, want (t)#null", v.Prop.Typed)
	}
	if v.Kind != valueKind(kdltext.ValueNull) {
		t.Errorf("kind = %v, want %v", kdltext.ValueKind(v.Kind), kdltext.ValueNull)
	}
	if v.Prop.Kind != valueKind(kdltext.ValueNull) {
		t.Errorf("prop kind = %v, want %v", kdltext.ValueKind(v.Prop.Kind), kdltext.ValueNull)
	}
	// As a value, #null sets a pointer to nil, without calling its method.
	// A node with a #null argument is decoded into a pointer as any node is,
	// but for a pointer to a value type, which is decoded from the argument.
	if v.Prop.PKind != nil || v.PInt != nil {
		t.Errorf("prop pkind = %v, pint = %v, want nil", v.Prop.PKind, v.PInt)
	}
	if v.PKind == nil || *v.PKind != valueKind(kdltext.ValueNull) {
		t.Errorf("pkind = %v, want a pointer to %v", v.PKind, kdltext.ValueNull)
	}
	if v.Server == nil || v.Server.Host != nil || v.Server.Port != 80 {
		t.Errorf("server = %+v, want no host and port 80", v.Server)
	}
	if v.Addr.IsValid() || v.Int != 0 {
		t.Errorf("addr = %v, int = %d, want zero values", v.Addr, v.Int)
	}
}

func TestUnmarshalErrors(t *testing.T) {
	type inner struct {
		Port int8 `kdl:"port,prop"`
	}
	type addr struct {
		Host string `kdl:",arg"`
	}
	type outer struct {
		Server inner `kdl:"server"`
		Addr   addr  `kdl:"addr"`
		Name   string
		List   [1]int            `kdl:"list"`
		Lists  map[string][1]int `kdl:"lists"`
		Tags   []string          `kdl:"tags"`
	}
	tests := []struct {
		input  string
		ignore bool
		want   string
	}{
		{"server port=300", false,
			`kuddle: cannot unmarshal property "port" of node server (line 1, column 1) into Go int8: 300 overflows int8`},
		{"server port=\"x\"", false,
			`kuddle: cannot unmarshal property "port" of node server (line 1, column 1) into Go int8: expected a number, found string x`},
		{"\nName 1 2", false,
			`kuddle: cannot unmarshal node Name (line 2, column 1) into Go string: node has 2 arguments, but string takes a single argument`},
		{"Name 1", false,
			`kuddle: cannot unmarshal argument 1 of node Name (line 1, column 1) into Go string: expected a string, found number 1`},
		{"list 1 2", false,
			`kuddle: cannot unmarshal argument 2 of node list (line 1, column 1) into Go [1]int: too many elements for an array of 1`},
		{"lists a=1", false,
			`kuddle: cannot unmarshal property "a" of node lists (line 1, column 1) into Go [1]int: [1]int can't be decoded from a single value`},
		{"server port=1 {\n  extra\n}", false,
			`kuddle: cannot unmarshal node server/extra (line 2, column 3) into Go kuddle_test.inner: unknown to the Go type`},
		{"server port=1 {\n  extra\n}", true, ""},
		{"server x=1", false,
			`kuddle: cannot unmarshal property "x" of node server (line 1, column 1) into Go kuddle_test.inner: unknown to the Go type`},
		{"server x=1", true, ""},
		{"\nother 1", false,
			`kuddle: cannot unmarshal node other (line 2, column 1) into Go kuddle_test.outer: unknown to the Go type`},
		{"\nother 1", true, ""},
		{"server 1", false,
			`kuddle: cannot unmarshal argument 1 of node server (line 1, column 1) into Go kuddle_test.inner: unknown to the Go type`},
		{"server 1", true, ""},
		{"addr a b", false,
			`kuddle: cannot unmarshal argument 2 of node addr (line 1, column 1) into Go kuddle_test.addr: unknown to the Go type`},
		{"addr a b", true, ""},
		{"lists 1", true,
			`kuddle: cannot unmarshal argument 1 of node lists (line 1, column 1) into Go map[string][1]int: map[string][1]int takes no arguments`},
		{"list 1 x=2", true,
			`kuddle: cannot unmarshal property "x" of node list (line 1, column 1) into Go [1]int: [1]int takes no properties`},
		{"tags a x=1", true,
			`kuddle: cannot unmarshal property "x" of node tags (line 1, column 1) into Go []string: []string takes no properties`},
		{"tags a {\n  b\n}", true,
			`kuddle: cannot unmarshal node tags/b (line 2, column 3) into Go []string: []string takes no children`},
	}
	for _, tt := range tests {
		var v outer
		err := kuddle.Unmarshal([]byte(tt.input), &v, kuddle.DiscardUnknown(tt.ignore))
		if tt.want == "" {
			if err != nil {
				t.Errorf("%q: %v", tt.input, err)
			}
			continue
		}
		if err == nil || err.Error() != tt.want {
			t.Errorf("%q:\ngot  %v\nwant %s", tt.input, err, tt.want)
		}
		if unknown := strings.HasSuffix(tt.want, "unknown to the Go type"); errors.Is(err, kuddle.ErrUnknown) != unknown {
			t.Errorf("%q: errors.Is(err, ErrUnknown) = %t, want %t", tt.input, !unknown, unknown)
		}
	}
}

func TestUnmarshalErrorPath(t *testing.T) {
	type listen struct {
		Port int `kdl:"port,prop"`
	}
	var v struct {
		Routes  []listen `kdl:"route"`
		Servers map[string]struct {
			Listen listen `kdl:"listen"`
		} `kdl:"server"`
		Lists map[string][]listen `kdl:"lists"`
		Rest  []listen            `kdl:",children"`
	}
	tests := []struct{ input, path string }{
		{"route port=1\nroute port=2\nroute port=x", "route[2]"},
		{`server { "a/b" { listen port=x; }; }`, `server/"a/b"/listen`},
		{`server { "true" { listen port=x; }; }`, `server/"true"/listen`},
		{`server { "a\tb" { listen port=x; }; }`, `server/"a\tb"/listen`},
		{"lists { x { a; b port=x; }; }", "lists/x/b[1]"},
		{"other port=1\nother port=x", "other[1]"},
	}
	for _, tt := range tests {
		err := kuddle.Unmarshal([]byte(tt.input), &v)
		se, ok := errors.AsType[*kuddle.SemanticError](err)
		if !ok || se.Path != tt.path {
			t.Errorf("%q: err = %v, want path %s", tt.input, err, tt.path)
		}
	}
}

func TestUnmarshalTextKeys(t *testing.T) {
	var v struct {
		Hosts struct {
			Names map[netip.Addr]string `kdl:",props"`
		} `kdl:"hosts"`
		Ports map[netip.Addr]int `kdl:"ports"`
	}
	input := "hosts \"10.0.0.1\"=a\nports { \"::1\" 1; \"127.0.0.1\" 2; }"
	if err := kuddle.Unmarshal([]byte(input), &v); err != nil {
		t.Fatal(err)
	}
	addr := netip.MustParseAddr
	if want := map[netip.Addr]string{addr("10.0.0.1"): "a"}; !reflect.DeepEqual(v.Hosts.Names, want) {
		t.Errorf("hosts = %v, want %v", v.Hosts.Names, want)
	}
	if want := map[netip.Addr]int{addr("::1"): 1, addr("127.0.0.1"): 2}; !reflect.DeepEqual(v.Ports, want) {
		t.Errorf("ports = %v, want %v", v.Ports, want)
	}

	err := kuddle.Unmarshal([]byte("ports { bad 1; }"), &v)
	se, ok := errors.AsType[*kuddle.SemanticError](err)
	if !ok || se.Path != "ports/bad" || se.GoType != reflect.TypeFor[netip.Addr]() {
		t.Errorf("bad key: err = %v", err)
	}
}

func TestUnmarshalSkipsUnknown(t *testing.T) {
	var v struct {
		Name string `kdl:"name"`
	}
	input := "extra 1 { deep { deeper 2 }; also }\nname x\nmore { y }\n"
	if err := kuddle.Unmarshal([]byte(input), &v, kuddle.DiscardUnknown(true)); err != nil {
		t.Fatal(err)
	}
	if v.Name != "x" {
		t.Errorf("Name = %q, want x", v.Name)
	}
}

func TestUnmarshalDecodeStruct(t *testing.T) {
	dec := kdltext.NewDecoder(strings.NewReader("a { b 1; c { d } }\ne"))
	if tok, err := dec.ReadToken(); err != nil || tok.Name() != "a" {
		t.Fatalf("ReadToken() = %v, %v", tok, err)
	}
	var v struct {
		B int `kdl:"b"`
	}
	if err := kuddle.UnmarshalDecode(dec, &v, kuddle.DiscardUnknown(true)); err != nil {
		t.Fatal(err)
	}
	if v.B != 1 {
		t.Errorf("B = %d, want 1", v.B)
	}
	for _, want := range []kdltext.Kind{kdltext.KindEnd, kdltext.KindNode} {
		if tok, err := dec.ReadToken(); err != nil || tok.Kind != want {
			t.Errorf("ReadToken() = %v, %v; want %v", tok, err, want)
		}
	}
}

func TestBadTags(t *testing.T) {
	tests := []struct {
		v    any
		want string
	}{
		{&struct {
			A int `kdl:",bogus"`
		}{}, `unknown kdl tag option "bogus"`},
		{&struct {
			A int `kdl:",arg,prop"`
		}{}, "more than one of"},
		{&struct {
			A int `kdl:"x,arg"`
		}{}, `the name "x" in the kdl tag is not used with the arg option`},
		{&struct {
			A string `kdl:"x,omitzero,name"`
		}{}, `is not used with the name option`},
		{&struct {
			A []int `kdl:",args"`
			B []int `kdl:",args"`
		}{}, `more than one field is tagged "args"`},
		{&struct {
			A int `kdl:"x"`
			B int `kdl:"x"`
		}{}, `child node name "x" is also used by field A`},
		{&struct {
			A struct{} `kdl:",arg"`
		}{}, "an arg field must be a value type"},
		{&struct {
			A int `kdl:",name"`
		}{}, "a name field must be a string"},
		{&struct {
			A []kdltext.Value `kdl:",entries"`
		}{}, "an entries field must be a slice of kuddle.Entry"},
		{&struct {
			A []kuddle.Entry `kdl:",entries"`
			B map[string]int `kdl:",props"`
		}{}, "an entries field can't be combined with an args or props field"},
		{&struct {
			A kuddle.Document `kdl:",arg"`
		}{}, "an arg field can't be a kuddle.Document"},
		{&struct {
			A []*kuddle.Document `kdl:",args"`
		}{}, "an args field can't be a []*kuddle.Document"},
		{&struct {
			A map[string]kuddle.Document `kdl:",props"`
		}{}, "a props field can't be a map[string]kuddle.Document"},
		{&struct {
			A kuddle.Entry `kdl:"a"`
		}{}, "a child field can't be a kuddle.Entry: an Entry is an argument"},
		{&struct {
			A map[string][]kuddle.Entry `kdl:",children"`
		}{}, "a children field can't be a map[string][]kuddle.Entry"},
	}
	for _, tt := range tests {
		err := kuddle.Unmarshal([]byte("a"), tt.v)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%T: err = %v, want it to contain %q", tt.v, err, tt.want)
		}
	}
}

func TestUnmarshalEntries(t *testing.T) {
	type entries struct {
		First int            `kdl:",arg"`
		P     string         `kdl:"p,prop"`
		Rest  []kuddle.Entry `kdl:",entries"`
		Kid   int            `kdl:"kid"`
	}
	var v struct {
		N entries `kdl:"n"`
	}
	v.N.Rest = []kuddle.Entry{kuddle.Arg(kdltext.String("stale"))}
	if err := kuddle.Unmarshal([]byte("n 1 a=2 p=x (t)3 a=4 {\n    kid 5\n}"), &v); err != nil {
		t.Fatal(err)
	}
	typed := kdltext.Int(3)
	typed.Type = new("t")
	want := []kuddle.Entry{
		kuddle.Prop("a", kdltext.Int(2)),
		kuddle.Arg(typed),
		kuddle.Prop("a", kdltext.Int(4)),
	}
	if v.N.First != 1 || v.N.P != "x" || v.N.Kid != 5 || !equalEntries(v.N.Rest, want) {
		t.Errorf("got  %+v\nwant entries %+v", v.N, want)
	}

	// With no entries, the field is empty.
	if err := kuddle.Unmarshal([]byte("n 1"), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.N.Rest) != 0 {
		t.Errorf("got %+v, want no entries", v.N.Rest)
	}
}

func equalEntries(a, b []kuddle.Entry) bool {
	return slices.EqualFunc(a, b, kuddle.Entry.Equal)
}

func TestUnmarshalNameAndType(t *testing.T) {
	type item struct {
		Name string `kdl:",name"`
		Type string `kdl:",type"`
		V    int    `kdl:",arg"`
	}
	var v struct {
		Items []item `kdl:",children"`
	}
	if err := kuddle.Unmarshal([]byte("(x)a 1\nb 2"), &v); err != nil {
		t.Fatal(err)
	}
	if want := []item{{"a", "x", 1}, {"b", "", 2}}; !reflect.DeepEqual(v.Items, want) {
		t.Errorf("got %+v, want %+v", v.Items, want)
	}

	// A *string type field tells an empty annotation from none.
	type typed struct {
		Type *string `kdl:",type"`
	}
	var p struct {
		Items []typed `kdl:",children"`
	}
	if err := kuddle.Unmarshal([]byte("(x)a\n(\"\")b\nc"), &p); err != nil {
		t.Fatal(err)
	}
	if want := []typed{{new("x")}, {new("")}, {nil}}; !reflect.DeepEqual(p.Items, want) {
		t.Errorf("got %+v, want %+v", p.Items, want)
	}

	var bad struct {
		Type int `kdl:",type"`
	}
	if err := kuddle.Unmarshal([]byte("a"), &bad); err == nil || !strings.Contains(err.Error(), "a type field must be a string or *string") {
		t.Errorf("int type field: err = %v", err)
	}
}

func TestUnmarshalDecodeContents(t *testing.T) {
	dec := kdltext.NewDecoder(strings.NewReader("a 1 k=2 { b 3; }\nc"))
	if tok, err := dec.ReadToken(); err != nil || tok.Name() != "a" {
		t.Fatalf("ReadToken() = %v, %v", tok, err)
	}
	var v struct {
		A int `kdl:",arg"`
		K int `kdl:"k,prop"`
		B int `kdl:"b"`
	}
	if err := kuddle.UnmarshalDecode(dec, &v); err != nil {
		t.Fatal(err)
	}
	if v.A != 1 || v.K != 2 || v.B != 3 {
		t.Errorf("got %+v, want {A:1 K:2 B:3}", v)
	}
	if tok, err := dec.ReadToken(); err != nil || tok.Kind != kdltext.KindEnd {
		t.Errorf("ReadToken() = %v, %v; want the end of a", tok, err)
	}

	// A value type is decoded from the node's single argument.
	dec = kdltext.NewDecoder(strings.NewReader("a 1"))
	dec.ReadToken()
	var i int
	if err := kuddle.UnmarshalDecode(dec, &i); err != nil || i != 1 {
		t.Errorf("int: got %d, %v; want 1", i, err)
	}
}

// recursive is a Go type that holds nodes nested to any depth.
type recursive map[string]recursive

// TestUnmarshalNestingDepth checks that Unmarshal into a recursive Go type
// stops at the Decoder's limit on nesting, rather than running out of stack.
func TestUnmarshalNestingDepth(t *testing.T) {
	const n = 5_000_000
	in := strings.Repeat("a{", n) + strings.Repeat("}", n)
	var v recursive
	err := kuddle.Unmarshal([]byte(in), &v)
	if _, ok := errors.AsType[*kdltext.SyntacticError](err); !ok || !strings.Contains(err.Error(), "nested deeper than 10000") {
		t.Errorf("Unmarshal: err = %v", err)
	}
}

// delegating decodes its node by the default rules, through UnmarshalDecode.
type delegating struct {
	Host string `kdl:",arg"`
}

func (l *delegating) UnmarshalKDL(dec *kdltext.Decoder) error {
	type plain delegating
	return kuddle.UnmarshalDecode(dec, (*plain)(l))
}

func TestUnmarshalDecodeOptions(t *testing.T) {
	in := []byte("listen localhost bogus=1\n")
	var v struct {
		Listen delegating `kdl:"listen"`
	}
	if err := kuddle.Unmarshal(in, &v, kuddle.DiscardUnknown(true)); err != nil {
		t.Fatalf("Unmarshal with DiscardUnknown: %v", err)
	}
	err := kuddle.Unmarshal(in, &v)
	if !errors.Is(err, kuddle.ErrUnknown) {
		t.Errorf("Unmarshal: err = %v, want ErrUnknown", err)
	}
}

// TestUnmarshalSelfPointer checks that Unmarshal into a pointer type defined
// in terms of itself reports a cycle, rather than allocating forever.
func TestUnmarshalSelfPointer(t *testing.T) {
	type selfPointer *selfPointer
	var v struct {
		P selfPointer `kdl:"p"`
	}
	err := kuddle.Unmarshal([]byte("p 1\n"), &v)
	if _, ok := errors.AsType[*kuddle.SemanticError](err); !ok || !strings.Contains(err.Error(), "encountered a cycle") {
		t.Errorf("err = %v", err)
	}
}

// TestUnmarshalRepeatedInterfaces decodes the nodes of a repeated []any field:
// the arguments of each node, and then its children.
func TestUnmarshalRepeatedInterfaces(t *testing.T) {
	var out struct {
		Items []any `kdl:"item"`
	}
	if err := kuddle.Unmarshal([]byte("item 1 x {\n    - {\n        a b=1\n    }\n}\n"), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 3 || out.Items[0] != int64(1) || out.Items[1] != "x" {
		t.Fatalf("got %#v", out.Items)
	}
	if _, ok := out.Items[2].(*kuddle.Node); !ok {
		t.Errorf("got %#v, want a *kuddle.Node", out.Items[2])
	}

	// Elements of other value types take only the arguments.
	var ints struct {
		Items []int `kdl:"item"`
	}
	if err := kuddle.Unmarshal([]byte("item 1 3\n"), &ints); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ints.Items, []int{1, 3}) {
		t.Errorf("Unmarshal into []int: got %v, want [1 3]", ints.Items)
	}
}
