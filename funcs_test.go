package kuddle_test

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/miquella/kuddle"
	"github.com/miquella/kuddle/kdltext"
)

// marshalDuration and unmarshalDuration write and read a time.Duration as a
// string, such as "1m30s".
var (
	marshalDuration = kuddle.MarshalFunc(func(enc *kdltext.Encoder, d time.Duration) error {
		return writeValue(enc, kdltext.String(d.String()))
	})
	unmarshalDuration = kuddle.UnmarshalFunc(func(dec *kdltext.Decoder, d *time.Duration) error {
		v, err := readValue(dec)
		if err != nil {
			return err
		}
		*d, err = time.ParseDuration(v.String())
		return err
	})
	durationFuncs = kuddle.JoinOptions(kuddle.WithMarshalers(marshalDuration), kuddle.WithUnmarshalers(unmarshalDuration))
)

// durations holds time.Durations wherever a value can be.
type durations struct {
	Arg   time.Duration            `kdl:",arg"`
	Args  []time.Duration          `kdl:",args"`
	Prop  time.Duration            `kdl:"p,prop"`
	Props map[string]time.Duration `kdl:",props"`
	Child time.Duration            `kdl:"child"`
	List  []time.Duration          `kdl:"list"`
	Ptr   *time.Duration           `kdl:"ptr"`
	Map   map[string]time.Duration `kdl:"map"`
	Any   any                      `kdl:"any"`
}

type durationsDoc struct {
	D durations `kdl:"d"`
}

// durationsKDL is the document for durationsWant. The functions write and
// read each Duration where the default rules put a value type: a slice of
// them is the arguments of a node, and a map of them its properties.
const durationsKDL = `d "1s" "2s" "3s" p="1m30s" q="4s" {
    child "1m30s"
    list "1s" "2s"
    ptr "5s"
    map a="6s"
    any "7s"
}
`

var durationsWant = durationsDoc{durations{
	Arg:   time.Second,
	Args:  []time.Duration{2 * time.Second, 3 * time.Second},
	Prop:  90 * time.Second,
	Props: map[string]time.Duration{"q": 4 * time.Second},
	Child: 90 * time.Second,
	List:  []time.Duration{time.Second, 2 * time.Second},
	Ptr:   new(5 * time.Second),
	Map:   map[string]time.Duration{"a": 6 * time.Second},
	Any:   "7s",
}}

// TestFuncsDuration marshals and unmarshals time.Durations, which have no
// methods, as strings, through functions.
func TestFuncsDuration(t *testing.T) {
	var got durationsDoc
	if err := kuddle.Unmarshal([]byte(durationsKDL), &got, durationFuncs); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, durationsWant) {
		t.Errorf("Unmarshal:\ngot  %+v\nwant %+v", got, durationsWant)
	}

	in := durationsWant
	in.D.Any = 7 * time.Second // in an interface, as the value itself
	b, err := kuddle.Marshal(in, durationFuncs)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != durationsKDL {
		t.Errorf("Marshal:\ngot\n%s\nwant\n%s", b, durationsKDL)
	}

	// Without the functions, a Duration is an integer.
	b, err = kuddle.Marshal(struct {
		D time.Duration `kdl:"d"`
	}{time.Second})
	if err != nil || string(b) != "d 1000000000\n" {
		t.Errorf("without functions: got %q, %v", b, err)
	}

	checkFrom(t, in, durationFuncs)
	checkTo[durationsDoc](t, durationsKDL, durationFuncs)
}

// TestFuncsKeepShape checks that functions for a value type don't change how
// its values are laid out, even when they leave every value to the default
// rules.
func TestFuncsKeepShape(t *testing.T) {
	opts := kuddle.JoinOptions(
		kuddle.WithMarshalers(kuddle.MarshalFunc(func(*kdltext.Encoder, time.Duration) error {
			return errors.ErrUnsupported
		})),
		kuddle.WithUnmarshalers(kuddle.UnmarshalFunc(func(*kdltext.Decoder, *time.Duration) error {
			return errors.ErrUnsupported
		})),
	)
	type doc struct {
		List []time.Duration          `kdl:"list"`
		Map  map[string]time.Duration `kdl:"map"`
	}
	const input = "list 1 2 3\nmap a=4 b=5\n"
	var got doc
	if err := kuddle.Unmarshal([]byte(input), &got, opts); err != nil {
		t.Fatal(err)
	}
	want := doc{[]time.Duration{1, 2, 3}, map[string]time.Duration{"a": 4, "b": 5}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Unmarshal:\ngot  %+v\nwant %+v", got, want)
	}
	if b, err := kuddle.Marshal(got, opts); err != nil || string(b) != input {
		t.Errorf("Marshal: got %q, %v; want %q", b, err, input)
	}
}

// point is a struct marshaled by functions as the single argument of its
// node, a string such as "1,2".
type point struct{ X, Y int }

// span is a struct marshaled by functions as the two arguments of its node.
type span struct{ From, To int }

var pointFuncs = kuddle.JoinOptions(
	kuddle.WithMarshalers(kuddle.JoinMarshalers(
		kuddle.MarshalFunc(func(enc *kdltext.Encoder, p point) error {
			return writeValue(enc, kdltext.String(strconv.Itoa(p.X)+","+strconv.Itoa(p.Y)))
		}),
		kuddle.MarshalFunc(func(enc *kdltext.Encoder, s *span) error {
			if err := writeValue(enc, kdltext.Int(int64(s.From))); err != nil {
				return err
			}
			return writeValue(enc, kdltext.Int(int64(s.To)))
		}),
	)),
	kuddle.WithUnmarshalers(kuddle.JoinUnmarshalers(
		kuddle.UnmarshalFunc(func(dec *kdltext.Decoder, p *point) error {
			v, err := readValue(dec)
			if err != nil {
				return err
			}
			x, y, _ := strings.Cut(v.String(), ",")
			if p.X, err = strconv.Atoi(x); err != nil {
				return err
			}
			p.Y, err = strconv.Atoi(y)
			return err
		}),
		kuddle.UnmarshalFunc(func(dec *kdltext.Decoder, s *span) error {
			from, err := readValue(dec)
			if err != nil {
				return err
			}
			to, err := readValue(dec)
			if err != nil {
				return err
			}
			f, _ := from.Int()
			t, _ := to.Int()
			s.From, s.To = int(f), int(t)
			return nil
		}),
	)),
)

type shape struct {
	Points []point `kdl:"point"`
	Spans  []span  `kdl:"span"`
	Bounds span    `kdl:"bounds"`
}

// TestFuncsStruct marshals and unmarshals structs through functions, which
// write and read the contents of their nodes.
func TestFuncsStruct(t *testing.T) {
	const input = `shape {
    point "1,2"
    point "3,4"
    span 1 2
    span 3 4
    bounds 0 9
}
`
	var got struct {
		Shape shape `kdl:"shape"`
	}
	if err := kuddle.Unmarshal([]byte(input), &got, pointFuncs); err != nil {
		t.Fatal(err)
	}
	want := shape{[]point{{1, 2}, {3, 4}}, []span{{1, 2}, {3, 4}}, span{0, 9}}
	if !reflect.DeepEqual(got.Shape, want) {
		t.Errorf("Unmarshal:\ngot  %+v\nwant %+v", got.Shape, want)
	}
	b, err := kuddle.Marshal(got, pointFuncs)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != input {
		t.Errorf("Marshal:\ngot\n%s\nwant\n%s", b, input)
	}
	checkFrom(t, got, pointFuncs)
	checkTo[struct {
		Shape shape `kdl:"shape"`
	}](t, input, pointFuncs)

	// Functions don't make a struct a value type, which an argument must be.
	_, err = kuddle.Marshal(struct {
		P point `kdl:",arg"`
	}{}, pointFuncs)
	if want := "an arg field must be a value type"; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("point as an argument: got %v, want ...%s...", err, want)
	}
}

// namer and renamer are interfaces with functions for the types that
// implement them.
type namer interface{ Name() string }

type renamer interface{ Rename(string) error }

// color is written as its name.
type color int

var colorNames = []string{"red", "green", "blue"}

func (c color) Name() string { return colorNames[c] }

func (c *color) Rename(name string) error {
	for i, n := range colorNames {
		if n == name {
			*c = color(i)
			return nil
		}
	}
	return errors.New("unknown color " + name)
}

var namerFuncs = kuddle.JoinOptions(
	kuddle.WithMarshalers(kuddle.MarshalFunc(func(enc *kdltext.Encoder, n namer) error {
		return writeValue(enc, kdltext.String(n.Name()))
	})),
	kuddle.WithUnmarshalers(kuddle.UnmarshalFunc(func(dec *kdltext.Decoder, r renamer) error {
		v, err := readValue(dec)
		if err != nil {
			return err
		}
		return r.Rename(v.String())
	})),
)

// TestFuncsInterface marshals and unmarshals values through the functions
// for interfaces that they, or their pointers, implement.
func TestFuncsInterface(t *testing.T) {
	type colors struct {
		Fill   color   `kdl:"fill,prop"`
		Stroke *color  `kdl:"stroke"`
		All    []color `kdl:"all"`
	}
	const input = `c fill=green {
    stroke blue
    all red blue
}
`
	var got struct {
		C colors `kdl:"c"`
	}
	if err := kuddle.Unmarshal([]byte(input), &got, namerFuncs); err != nil {
		t.Fatal(err)
	}
	want := colors{1, new(color(2)), []color{0, 2}}
	if !reflect.DeepEqual(got.C, want) {
		t.Errorf("Unmarshal:\ngot  %+v\nwant %+v", got.C, want)
	}
	b, err := kuddle.Marshal(got, namerFuncs)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != input {
		t.Errorf("Marshal:\ngot\n%s\nwant\n%s", b, input)
	}

	err = kuddle.Unmarshal([]byte("c fill=pink"), &got, namerFuncs)
	if want := `property "fill" of node c (line 1, column 1) into Go kuddle_test.color: unknown color pink`; err == nil || !strings.HasSuffix(err.Error(), want) {
		t.Errorf("got  %v\nwant ...%s", err, want)
	}
}

// TestFuncsPrecedence checks that functions take precedence over methods,
// in the order they are joined in, and that each may return
// errors.ErrUnsupported for the next, the methods, or the default rules.
func TestFuncsPrecedence(t *testing.T) {
	// maybe's methods write and read -1 as "none", and leave other values to
	// the default rules.
	type values struct {
		M maybe `kdl:"m,prop"`
		N maybe `kdl:"n"`
	}
	type doc struct {
		V []values `kdl:"v"`
	}
	in := doc{[]values{{-1, -1}, {1, 1}, {2, 2}, {3, 3}}}
	writeIf := func(want maybe, s string) *kuddle.Marshalers {
		return kuddle.MarshalFunc(func(enc *kdltext.Encoder, m maybe) error {
			if m != want {
				return errors.ErrUnsupported
			}
			return writeValue(enc, kdltext.String(s))
		})
	}
	readIf := func(s string, want maybe) *kuddle.Unmarshalers {
		return kuddle.UnmarshalFunc(func(dec *kdltext.Decoder, m *maybe) error {
			t, err := dec.PeekToken()
			if err != nil || t.Kind != kdltext.KindValue || t.Value.String() != s {
				return errors.ErrUnsupported
			}
			dec.ReadToken()
			*m = want
			return nil
		})
	}
	opts := kuddle.JoinOptions(
		kuddle.WithMarshalers(kuddle.JoinMarshalers(
			writeIf(1, "one"),
			kuddle.JoinMarshalers(writeIf(1, "uno"), writeIf(2, "two")),
		)),
		kuddle.WithUnmarshalers(kuddle.JoinUnmarshalers(
			readIf("one", 1),
			kuddle.JoinUnmarshalers(readIf("one", -2), readIf("two", 2)),
		)),
	)
	const want = `v m=none {
    n none
}
v m=one {
    n one
}
v m=two {
    n two
}
v m=3 {
    n 3
}
`
	b, err := kuddle.Marshal(in, opts)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != want {
		t.Errorf("Marshal:\ngot\n%s\nwant\n%s", b, want)
	}
	var got doc
	if err := kuddle.Unmarshal(b, &got, opts); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Errorf("Unmarshal:\ngot  %+v\nwant %+v", got, in)
	}
	checkFrom(t, in, opts)
	checkTo[doc](t, want, opts)

	// A function over a method that writes the value otherwise.
	b, err = kuddle.Marshal(doc{[]values{{-1, -1}}}, kuddle.WithMarshalers(writeIf(-1, "nothing")))
	if err != nil || string(b) != "v m=nothing {\n    n nothing\n}\n" {
		t.Errorf("over the method: got %q, %v", b, err)
	}
}

// TestFuncsReplaced checks that a later WithMarshalers or WithUnmarshalers
// replaces an earlier one.
func TestFuncsReplaced(t *testing.T) {
	type d struct {
		D time.Duration `kdl:"d"`
	}
	other := kuddle.MarshalFunc(func(enc *kdltext.Encoder, d time.Duration) error {
		return writeValue(enc, kdltext.Float(d.Seconds()))
	})
	tests := []struct {
		opts []kuddle.Options
		want string
	}{
		{[]kuddle.Options{kuddle.WithMarshalers(marshalDuration), kuddle.WithMarshalers(other)}, "d 90.0\n"},
		{[]kuddle.Options{kuddle.WithMarshalers(other), kuddle.WithMarshalers(marshalDuration)}, "d \"1m30s\"\n"},
		{[]kuddle.Options{kuddle.WithMarshalers(marshalDuration), kuddle.WithMarshalers(nil)}, "d 90000000000\n"},
		{[]kuddle.Options{kuddle.WithMarshalers(kuddle.JoinMarshalers(other, marshalDuration))}, "d 90.0\n"},
	}
	for _, tt := range tests {
		b, err := kuddle.Marshal(d{90 * time.Second}, tt.opts...)
		if err != nil || string(b) != tt.want {
			t.Errorf("got %q, %v; want %q", b, err, tt.want)
		}
	}

	var got d
	err := kuddle.Unmarshal([]byte(`d "1m30s"`), &got, kuddle.WithUnmarshalers(unmarshalDuration), kuddle.WithUnmarshalers(nil))
	if err == nil {
		t.Errorf("WithUnmarshalers(nil): got %v, want an error", got)
	}
}

// TestFuncsErrors checks the errors for functions that write or read too
// much or too little, as for methods.
func TestFuncsErrors(t *testing.T) {
	type wrap struct {
		D time.Duration `kdl:"d"`
		A struct {
			D time.Duration `kdl:"d,prop"`
		} `kdl:"a"`
	}
	one := kdltext.Token{Kind: kdltext.KindValue, Value: kdltext.Int(1)}
	writer := func(toks []kdltext.Token, err error) kuddle.Options {
		return kuddle.WithMarshalers(kuddle.MarshalFunc(func(enc *kdltext.Encoder, d time.Duration) error {
			for _, t := range toks {
				if err := enc.WriteToken(t); err != nil {
					return err
				}
			}
			return err
		}))
	}
	mtests := []struct {
		opts kuddle.Options
		want string
	}{
		{writer([]kdltext.Token{one}, errors.ErrUnsupported), "node d from Go time.Duration: the MarshalFunc for time.Duration returned errors.ErrUnsupported after writing output"},
		{writer([]kdltext.Token{{Kind: kdltext.KindNode, Value: kdltext.String("x")}}, nil), "argument 1 of node d from Go time.Duration: the MarshalFunc for time.Duration did not write a single value"},
		{writer([]kdltext.Token{{Kind: kdltext.KindEnd}}, nil), "argument 1 of node d from Go time.Duration: the MarshalFunc for time.Duration did not write a single value"},
		{writer(nil, errors.New("bad")), "node d from Go time.Duration: bad"},
		{writer(nil, nil), "argument 1 of node d from Go time.Duration: the MarshalFunc for time.Duration did not write a single value"},
		{writer([]kdltext.Token{one, one}, nil), "argument 1 of node d from Go time.Duration: the MarshalFunc for time.Duration did not write a single value"},
	}
	for _, tt := range mtests {
		_, err := kuddle.Marshal(wrap{}, tt.opts)
		if err == nil || !strings.HasSuffix(err.Error(), tt.want) {
			t.Errorf("Marshal:\ngot  %v\nwant ...%s", err, tt.want)
		}
		var doc kuddle.Document
		if err := doc.From(wrap{}, tt.opts); err == nil || !strings.HasSuffix(err.Error(), tt.want) {
			t.Errorf("From:\ngot  %v\nwant ...%s", err, tt.want)
		}
	}

	reader := func(n int, err error) kuddle.Options {
		return kuddle.WithUnmarshalers(kuddle.UnmarshalFunc(func(dec *kdltext.Decoder, d *time.Duration) error {
			for range n {
				if _, err := dec.ReadToken(); err != nil {
					return err
				}
			}
			return err
		}))
	}
	utests := []struct {
		input string
		opts  kuddle.Options
		want  string
	}{
		{"d 1", reader(1, errors.ErrUnsupported), "node d (line 1, column 1) into Go time.Duration: the UnmarshalFunc for *time.Duration returned errors.ErrUnsupported after reading input"},
		{"d 1 2", reader(1, nil), "node d (line 1, column 1) into Go time.Duration: node has 2 arguments, but time.Duration takes a single argument"},
		{"d 1", reader(2, nil), "argument 1 of node d (line 1, column 1) into Go time.Duration: the UnmarshalFunc for *time.Duration read more than the value"},
		{"a d=1", reader(0, nil), `property "d" of node a (line 1, column 1) into Go time.Duration: the UnmarshalFunc for *time.Duration did not read the value`},
		{"a d=1", reader(1, errors.ErrUnsupported), `property "d" of node a (line 1, column 1) into Go time.Duration: the UnmarshalFunc for *time.Duration returned errors.ErrUnsupported after reading input`},
		// Falling through to the default rules.
		{"d 1", reader(0, errors.ErrUnsupported), ""},
		{"a d=1", reader(0, errors.ErrUnsupported), ""},
	}
	for _, tt := range utests {
		var w wrap
		err := kuddle.Unmarshal([]byte(tt.input), &w, tt.opts)
		switch {
		case tt.want == "" && err != nil:
			t.Errorf("%q: %v", tt.input, err)
		case tt.want != "" && (err == nil || !strings.HasSuffix(err.Error(), tt.want)):
			t.Errorf("%q:\ngot  %v\nwant ...%s", tt.input, err, tt.want)
		}
		checkTo[wrap](t, tt.input, tt.opts)
	}
}

// server is checked by functions that delegate it to the default rules, as
// a plainServer, which has no functions.
type server struct {
	Host    string        `kdl:",arg"`
	Port    int           `kdl:"port,prop"`
	Timeout time.Duration `kdl:"timeout,prop"`
}

type plainServer server

// job delegates its node's contents to the default rules from its methods,
// as a type without them, whose Duration is marshaled by functions.
type job struct {
	Name    string        `kdl:",arg"`
	Timeout time.Duration `kdl:"timeout"`
}

func (j job) MarshalKDL(enc *kdltext.Encoder) error {
	type plain job
	return kuddle.MarshalEncode(enc, plain(j))
}

func (j *job) UnmarshalKDL(dec *kdltext.Decoder) error {
	type plain job
	return kuddle.UnmarshalDecode(dec, (*plain)(j))
}

// TestFuncsDelegate checks that functions apply in the values that methods
// and functions delegate to the default rules.
func TestFuncsDelegate(t *testing.T) {
	opts := kuddle.JoinOptions(
		kuddle.WithMarshalers(kuddle.JoinMarshalers(
			kuddle.MarshalFunc(func(enc *kdltext.Encoder, s server) error {
				if s.Port == 0 {
					return errors.New("no port")
				}
				return kuddle.MarshalEncode(enc, plainServer(s))
			}),
			marshalDuration,
		)),
		kuddle.WithUnmarshalers(kuddle.JoinUnmarshalers(
			kuddle.UnmarshalFunc(func(dec *kdltext.Decoder, s *server) error {
				if err := kuddle.UnmarshalDecode(dec, (*plainServer)(s)); err != nil {
					return err
				}
				if s.Port == 0 {
					return errors.New("no port")
				}
				return nil
			}),
			unmarshalDuration,
		)),
	)
	type doc struct {
		Servers []server `kdl:"server"`
		Jobs    []job    `kdl:"job"`
	}
	const input = `server localhost port=80 timeout="5s"
server ::1 port=443 timeout="1m0s"
job build {
    timeout "10m0s"
}
`
	var got doc
	if err := kuddle.Unmarshal([]byte(input), &got, opts); err != nil {
		t.Fatal(err)
	}
	want := doc{
		[]server{{"localhost", 80, 5 * time.Second}, {"::1", 443, time.Minute}},
		[]job{{"build", 10 * time.Minute}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Unmarshal:\ngot  %+v\nwant %+v", got, want)
	}
	b, err := kuddle.Marshal(got, opts)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != input {
		t.Errorf("Marshal:\ngot\n%s\nwant\n%s", b, input)
	}
	checkFrom(t, got, opts)
	checkTo[doc](t, input, opts)

	err = kuddle.Unmarshal([]byte("server localhost"), &got, opts)
	if want := "kuddle: cannot unmarshal node server[0] (line 1, column 1) into Go kuddle_test.server: no port"; err == nil || err.Error() != want {
		t.Errorf("got  %v\nwant %s", err, want)
	}
	_, err = kuddle.Marshal(doc{Servers: []server{{Host: "x"}}}, opts)
	if want := "kuddle: cannot marshal node server[0] from Go kuddle_test.server: no port"; err == nil || err.Error() != want {
		t.Errorf("got  %v\nwant %s", err, want)
	}
}

// TestFuncsMarshalEncodeOptions checks that the functions among the options
// of MarshalEncode and UnmarshalDecode apply.
func TestFuncsMarshalEncodeOptions(t *testing.T) {
	var b strings.Builder
	enc := kdltext.NewEncoder(&b)
	enc.WriteToken(kdltext.Token{Kind: kdltext.KindNode, Value: kdltext.String("n")})
	if err := kuddle.MarshalEncode(enc, []time.Duration{time.Second}, kuddle.WithMarshalers(marshalDuration)); err != nil {
		t.Fatal(err)
	}
	enc.WriteToken(kdltext.Token{Kind: kdltext.KindEnd})
	if want := "n {\n    - \"1s\"\n}\n"; b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}

	dec := kdltext.NewDecoder(strings.NewReader(b.String()))
	dec.ReadToken()
	var got []time.Duration
	if err := kuddle.UnmarshalDecode(dec, &got, kuddle.WithUnmarshalers(unmarshalDuration)); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != time.Second {
		t.Errorf("got %v", got)
	}
}

func TestFuncsInvalid(t *testing.T) {
	type named *int
	panics := func(name string, f func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s: no panic", name)
			}
		}()
		f()
	}
	panics("MarshalFunc of a named pointer", func() {
		kuddle.MarshalFunc(func(*kdltext.Encoder, named) error { return nil })
	})
	panics("UnmarshalFunc of a non-pointer", func() {
		kuddle.UnmarshalFunc(func(*kdltext.Decoder, time.Duration) error { return nil })
	})
	panics("UnmarshalFunc of a named pointer", func() {
		kuddle.UnmarshalFunc(func(*kdltext.Decoder, named) error { return nil })
	})
}
