package kuddle_test

import (
	"errors"
	"net/netip"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/miquella/kuddle"
	"github.com/miquella/kuddle/kdltext"
)

// errText returns the text of err, or "" for nil, without the positions
// that To has none of.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return regexp.MustCompile(` \(line \d+, column \d+\)`).ReplaceAllString(err.Error(), "")
}

// checkFrom checks that Document.From makes of in what Marshal and Unmarshal
// make of it, through KDL text.
func checkFrom(t *testing.T, in any, opts ...kuddle.Options) {
	t.Helper()
	var want kuddle.Document
	text, errWant := kuddle.Marshal(in, opts...)
	if errWant == nil {
		errWant = kuddle.Unmarshal(text, &want, opts...)
	}
	var got kuddle.Document
	err := got.From(in, opts...)
	switch {
	case errText(err) != errText(errWant):
		t.Errorf("From(%T): error\ngot  %v\nwant %v", in, err, errWant)
	case err == nil && !got.Equal(&want):
		t.Errorf("From(%T):\ngot  %+v\nwant %+v", in, got.Nodes, want.Nodes)
	}
}

// checkTo checks that Document.To decodes the Document of input into a T as
// Unmarshal decodes input.
func checkTo[T any](t *testing.T, input string, opts ...kuddle.Options) {
	t.Helper()
	var doc kuddle.Document
	if err := kuddle.Unmarshal([]byte(input), &doc); err != nil {
		t.Fatal(err)
	}
	var want, got T
	errWant := kuddle.Unmarshal([]byte(input), &want, opts...)
	err := doc.To(&got, opts...)
	switch {
	case errText(err) != errText(errWant):
		t.Errorf("To(%T) of %q: error\ngot  %v\nwant %v", &got, input, err, errWant)
	case err == nil && !reflect.DeepEqual(got, want):
		t.Errorf("To(%T) of %q:\ngot  %+v\nwant %+v", &got, input, got, want)
	}
	if se, ok := errors.AsType[*kuddle.SemanticError](err); ok && se.Pos != (kdltext.Position{}) {
		t.Errorf("To(%T) of %q: error has position %v", &got, input, se.Pos)
	}
}

// fromToNode has fields with all of the tag options.
type fromToNode struct {
	Name     string            `kdl:",name"`
	Type     *string           `kdl:",type"`
	Comment  string            `kdl:",comment"`
	Arg      string            `kdl:",arg"`
	Args     []any             `kdl:",args"`
	Prop     float64           `kdl:"p,prop"`
	Props    map[string]any    `kdl:",props"`
	Child    *fromToNode       `kdl:"child"`
	Repeated []int             `kdl:"r"`
	Zero     int               `kdl:"zero,omitzero"`
	Rest     map[string]string `kdl:",children"`
}

// fromToEntries has the tag options that fromToNode can't have alongside
// its own: an entries field, and comment fields of kdltext.Comment.
type fromToEntries struct {
	Comment kdltext.Comment `kdl:",comment"`
	Line    kdltext.Comment `kdl:",linecomment"`
	Arg     int             `kdl:",arg"`
	Rest    []kuddle.Entry  `kdl:",entries"`
}

type fromToValues struct {
	Node     fromToNode             `kdl:"node"`
	Nodes    []fromToNode           `kdl:"nodes"`
	Any      any                    `kdl:"any"`
	Anys     []any                  `kdl:"anys"`
	Map      map[string]any         `kdl:"map"`
	Keys     map[netip.Addr]int     `kdl:"keys"`
	Value    kdltext.Value          `kdl:"value"`
	Null     *int                   `kdl:"null"`
	NilNode  *fromToNode            `kdl:"nil"`
	Pointers map[string]*int        `kdl:"pointers"`
	Doc      kuddle.Document        `kdl:"doc"`
	DocPtr   *kuddle.Document       `kdl:"docptr"`
	DocAny   any                    `kdl:"docany"`
	KDLNode  kuddle.Node            `kdl:"kdlnode"`
	KDLNodes map[string]kuddle.Node `kdl:"kdlnodes"`
}

func fromToTestValues() fromToValues {
	doc := kuddle.Document{Nodes: []kuddle.Node{
		{Kind: kuddle.NodeComment, Comment: kdltext.Comment{Kind: kdltext.CommentLine, Text: "a comment"}},
		{Name: "x", Type: new("t"), Entries: []kuddle.Entry{kuddle.Arg(kdltext.Int(1)), kuddle.Prop("y", kdltext.String("z"))}, Children: []kuddle.Node{}},
	}}
	return fromToValues{
		Node: fromToNode{
			Name: "ignored", Type: new("t"), Comment: "The node.\nTwo lines.",
			Arg: "a", Args: []any{int64(1), "b", nil, true}, Prop: 2.5,
			Props:    map[string]any{"q": "r", "n": nil},
			Child:    &fromToNode{Arg: "c", Repeated: []int{1, 2}},
			Repeated: []int{3}, Rest: map[string]string{"s": "t"},
		},
		Nodes:    []fromToNode{{Arg: "d"}, {Arg: "e", Zero: 1}},
		Any:      map[string]any{"k": int64(1)},
		Anys:     []any{int64(1), "two", []any{int64(3)}},
		Map:      map[string]any{"a": int64(1), "b": map[string]any{"c": "d"}, "e": nil},
		Keys:     map[netip.Addr]int{netip.MustParseAddr("::1"): 1, netip.MustParseAddr("10.0.0.1"): 2},
		Value:    kdltext.Null(),
		Pointers: map[string]*int{"nil": nil, "one": new(1)},
		Doc:      doc,
		DocPtr:   doc.Clone(),
		DocAny:   doc.Clone(),
		KDLNode:  doc.Nodes[1],
		KDLNodes: map[string]kuddle.Node{"m": doc.Nodes[1]},
	}
}

// fromToMethods has fields of types with methods, as nodes and as values.
type fromToMethods struct {
	Props struct {
		U    upper `kdl:"u,prop"`
		Tags csv   `kdl:"tags,prop"`
		M    maybe `kdl:"m,prop"`
		M2   maybe `kdl:"m2,prop"`
		Hex  hexInt
	} `kdl:"props"`
	Args struct {
		U upper   `kdl:",arg"`
		M []maybe `kdl:",args"`
	} `kdl:"args"`
	U      upper             `kdl:"u"`
	Tags   csv               `kdl:"tags"`
	M      maybe             `kdl:"m"`
	M2     maybe             `kdl:"m2"`
	P      pair              `kdl:"p"`
	Pairs  []pair            `kdl:"pairs"`
	Listen []listen          `kdl:"listen"`
	Map    map[string]listen `kdl:"map"`
	Doc    kuddle.Document   `kdl:"doc"`
}

func (h *hexInt) UnmarshalKDL(dec *kdltext.Decoder) error {
	v, err := readValue(dec)
	if err == nil {
		var i int64
		i, err = v.Int()
		*h = hexInt(i)
	}
	return err
}

func fromToTestMethods() fromToMethods {
	var m fromToMethods
	m.Props.U, m.Props.Tags, m.Props.M, m.Props.M2, m.Props.Hex = "A B", csv{"a", "b"}, -1, 3, 255
	m.Args.U, m.Args.M = "X", []maybe{-1, 1}
	m.U, m.Tags, m.M, m.M2 = "C", csv{"c", "d"}, -1, 4
	m.P, m.Pairs = pair{"a", "b"}, []pair{{"c", "d"}, {"e", "f"}}
	m.Listen = []listen{{"listen", "localhost", 80, true}, {"listen", "example.com", 443, false}}
	m.Map = map[string]listen{"l": {"l", "host", 1, false}}
	m.Doc = kuddle.Document{Nodes: []kuddle.Node{{Name: "x", Entries: []kuddle.Entry{kuddle.Arg(kdltext.Int(1))}}}}
	return m
}

func TestDocumentFrom(t *testing.T) {
	values := fromToTestValues()
	for _, in := range []any{
		testConfig(),
		values,
		&values,
		fromToTestMethods(),
		unsupportedWant,
		unsupportedMap{"x": 1, "y": 2},
		map[string]any{"a": int64(1), "b": []any{"c", nil}, "d": nil},
		[]fromToNode{{Arg: "a"}, {Name: "b", Arg: "c"}},
		values.Doc,
		&values.Doc,
		values.KDLNode,
		new(any),
	} {
		checkFrom(t, in)
	}
	checkFrom(t, fromToTestMethods(), kdltext.WithVersion(kdltext.V1))
	checkFrom(t, map[string]fromToEntries{"n": {
		Comment: kdltext.Comment{Kind: kdltext.CommentBlock, Text: "c"},
		Line:    kdltext.Comment{Kind: kdltext.CommentLine, Text: "line"},
		Arg:     1,
		Rest: []kuddle.Entry{
			kuddle.Prop("a", kdltext.Int(2)),
			{Kind: kuddle.EntryComment, Comment: kdltext.Comment{Kind: kdltext.CommentBlock, Text: "c"}},
			kuddle.Arg(kdltext.Int(3)),
		},
	}})

	// A Document can't be a value.
	doc := fromToTestValues().Doc
	err := new(kuddle.Document).From(map[string]struct {
		D kuddle.Document `kdl:"d,prop"`
	}{"a": {doc}})
	if want := `a prop field can't be a kuddle.Document: a Document holds only nodes, not arguments or properties`; err == nil || !strings.HasSuffix(err.Error(), want) {
		t.Errorf("Document property: got %v, want ...%s", err, want)
	}
	checkFrom(t, map[string]struct {
		D kuddle.Document `kdl:",arg"`
	}{"a": {doc}})
	checkTo[struct {
		A struct {
			D kuddle.Document `kdl:"d,prop"`
		} `kdl:"a"`
	}](t, "a d=1")
}

func TestDocumentTo(t *testing.T) {
	text := func(in any) string {
		b, err := kuddle.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	values := text(fromToTestValues())
	checkTo[Config](t, text(testConfig()))
	checkTo[map[string]fromToEntries](t, "/* c */\nn 1 a=2 /* c */ (t)3 // line\n")
	checkTo[fromToValues](t, values)
	checkTo[*fromToValues](t, values)
	checkTo[map[string]any](t, values)
	checkTo[[]any](t, values)
	checkTo[any](t, values)
	checkTo[kuddle.Document](t, values)
	checkTo[fromToMethods](t, text(fromToTestMethods()))
	checkTo[unsupportedNodes](t, unsupportedKDL)
	checkTo[unsupportedMap](t, "x 1\ny 2")
	checkTo[struct {
		P *int        `kdl:"p"`
		I any         `kdl:"i"`
		N *fromToNode `kdl:"n"`
		S string      `kdl:"s"`
		V upper       `kdl:"v"`
	}](t, "p #null\ni #null\nn #null\ns #null\nv #null")
	checkTo[fromToValues](t, "nil 1\nnope")
	checkTo[fromToValues](t, "nil 1\nnope", kuddle.DiscardUnknown(true))
	checkTo[fromToValues](t, "node 1 2 p=x")
	checkTo[fromToMethods](t, "map {\n    l host port=x\n}")
	checkTo[fromToMethods](t, "map {\n    l host\n}")
	checkTo[fromToMethods](t, "props u=1")
	checkTo[int](t, "x 1")
	checkTo[struct {
		L []listen `kdl:"l"`
	}](t, "l a port=1\nl b port=x")

	var doc kuddle.Document
	if err := doc.To(doc); err == nil {
		t.Error("non-pointer: no error")
	}
}

// TestDocumentFromToMethodErrors checks that the methods of values are held
// to the same rules by From and To as by Marshal and Unmarshal.
func TestDocumentFromToMethodErrors(t *testing.T) {
	type node struct {
		W writer `kdl:"w"`
		R reader `kdl:"r"`
	}
	type prop struct {
		A struct {
			W writer `kdl:"w,prop"`
			R reader `kdl:"r,prop"`
		} `kdl:"a"`
	}
	one := kdltext.Token{Kind: kdltext.KindValue, Value: kdltext.Int(1)}
	for _, w := range []writer{
		{[]kdltext.Token{{Kind: kdltext.KindNode, Value: kdltext.String("x")}}, nil},
		{[]kdltext.Token{{Kind: kdltext.KindEnd}}, nil},
		{[]kdltext.Token{one}, errors.ErrUnsupported},
		{[]kdltext.Token{one}, nil},
		{[]kdltext.Token{one, one}, nil},
		{[]kdltext.Token{{Kind: kdltext.KindProp, Value: kdltext.String("p")}, one, {Kind: kdltext.KindNode, Value: kdltext.String("c")}, {Kind: kdltext.KindEnd}}, nil},
		{nil, errors.New("bad")},
		{nil, errors.ErrUnsupported},
		{nil, nil},
	} {
		checkFrom(t, node{W: w})
		var p prop
		p.A.W = w
		checkFrom(t, p)
	}
	// The text that To gives a method ends with the method's node, where
	// Unmarshal would read on, so the methods here read no further than the
	// node's KindEnd token.
	for _, input := range []string{"r 1 2", "r 1", "r 1\nx", "r 1 { c; }", "a r=1", "a r=1 2"} {
		for _, r := range []reader{{0, nil}, {1, nil}, {2, nil}, {0, errors.ErrUnsupported}, {1, errors.ErrUnsupported}, {0, errors.New("bad")}} {
			var doc kuddle.Document
			if err := kuddle.Unmarshal([]byte(input), &doc); err != nil {
				t.Fatal(err)
			}
			var want, got node
			var wantP, gotP prop
			want.R, got.R, wantP.A.R, gotP.A.R = r, r, r, r
			errWant := kuddle.Unmarshal([]byte(input), &want)
			if err := doc.To(&got); errText(err) != errText(errWant) {
				t.Errorf("%q with %+v:\ngot  %v\nwant %v", input, r, err, errWant)
			}
			errWant = kuddle.Unmarshal([]byte(input), &wantP)
			if err := doc.To(&gotP); errText(err) != errText(errWant) {
				t.Errorf("%q with %+v as a property:\ngot  %v\nwant %v", input, r, err, errWant)
			}
		}
	}
}

// TestDocumentFromToErrorPath checks that errors have the paths of their
// nodes, within the methods of values too.
func TestDocumentFromToErrorPath(t *testing.T) {
	bad := func() {}
	var doc kuddle.Document
	err := doc.From(map[string]map[string]any{"server": {"a/b": map[string]any{"port": bad}}})
	if se, ok := errors.AsType[*kuddle.SemanticError](err); !ok || se.Path != `server/"a/b"/port` {
		t.Errorf("From: err = %v", err)
	}
	if doc.Nodes != nil {
		t.Errorf("From: after an error, d = %+v", doc.Nodes)
	}
	err = doc.From(map[string]map[string]listen{"a": {"l": {}}})
	if se, ok := errors.AsType[*kuddle.SemanticError](err); !ok || se.Path != "a/l" {
		t.Errorf("From with a method: err = %v", err)
	}

	if err := kuddle.Unmarshal([]byte("a {\n    l x port=y\n}"), &doc); err != nil {
		t.Fatal(err)
	}
	var v struct {
		A map[string]listen `kdl:"a"`
	}
	err = doc.To(&v)
	se, ok := errors.AsType[*kuddle.SemanticError](err)
	if !ok || se.Path != "a/l" || se.Entry != `property "port"` || se.Pos != (kdltext.Position{}) {
		t.Errorf("To with a method: err = %v", err)
	}
}

// TestDocumentFromRepeatedProperties checks that From keeps the repeated
// properties of a Document, as Marshal does, and reports the same error as
// Marshal for a props field that repeats the property of a prop field.
func TestDocumentFromRepeatedProperties(t *testing.T) {
	var doc kuddle.Document
	err := kuddle.Unmarshal([]byte(`n b=1 x /* c */ a=2 b=3 /- d=0 y a=4 c=5 {
    m a=1 1 a=2 // end
}
`), &doc)
	if err != nil {
		t.Fatal(err)
	}
	type props struct {
		X string         `kdl:",arg"`
		A int            `kdl:"a,prop"`
		P map[string]int `kdl:",props"`
		Y string         `kdl:",arg"`
	}
	for _, in := range []any{
		doc,
		map[string]props{"p": {"x", 1, map[string]int{"b": 2, "c": 3}, "y"}},
		map[string]props{"p": {"x", 1, map[string]int{"a": 2, "b": 3}, "y"}},
	} {
		checkFrom(t, in)
	}

	var got kuddle.Document
	if err := got.From(doc); err != nil {
		t.Fatal(err)
	}
	if n := got.Nodes[0]; len(n.Entries) != 9 || len(n.Children[0].Entries) != 4 {
		t.Errorf("From: got %+v", got.Nodes)
	}
	if err := got.From(map[string]props{"p": {A: 1, P: map[string]int{"a": 2}}}); err == nil {
		t.Errorf("From with a repeated property: got %+v, want error", got.Nodes)
	}
}

// TestDocumentFromToComments checks that comments kept in a Document go
// into the comment fields of structs, and back.
func TestDocumentFromToComments(t *testing.T) {
	var doc kuddle.Document
	if err := kuddle.Unmarshal([]byte(commented), &doc); err != nil {
		t.Fatal(err)
	}
	type listen struct {
		Comment string `kdl:",comment"`
		Addr    string `kdl:",arg"`
	}
	var v struct {
		Server struct {
			Comment string   `kdl:",comment"`
			Name    string   `kdl:",arg"`
			Listen  []listen `kdl:"listen"`
			TLS     bool     `kdl:"tls"`
		} `kdl:"server"`
	}
	if err := doc.To(&v); err != nil {
		t.Fatal(err)
	}
	if v.Server.Comment != "Detached.\n\nThe server." || len(v.Server.Listen) != 1 || v.Server.Listen[0].Comment != "Where to listen,\nin order." {
		t.Fatalf("To: got %+v", v)
	}
	checkTo[struct {
		Server struct {
			Comment string   `kdl:",comment"`
			Listen  []listen `kdl:"listen"`
		} `kdl:"server"`
	}](t, commented)

	var back kuddle.Document
	if err := back.From(v); err != nil {
		t.Fatal(err)
	}
	// The detached comment is written as a comment node above the server.
	if len(back.Nodes) != 2 || !back.Nodes[0].IsComment() || back.Nodes[0].Comment != doc.Nodes[0].Comment {
		t.Fatalf("From: got %+v", back.Nodes)
	}
	server := back.Nodes[1]
	if want := doc.Nodes[1].Comment; server.Comment != want {
		t.Errorf("From: server comment = %+v, want %+v", server.Comment, want)
	}
	if want := doc.Nodes[1].Children[0].Comment; server.Children[0].Comment != want {
		t.Errorf("From: listen comment = %+v, want %+v", server.Children[0].Comment, want)
	}
	checkFrom(t, v)

	var any kuddle.Document
	if err := doc.To(&any); err != nil || !any.Equal(&doc) || !strings.Contains(any.Nodes[0].Comment.Text, "Detached") {
		t.Errorf("To a Document: got %+v, %v", any.Nodes, err)
	}
}

// A Document keeps all comments, and the fields take them from it, and put
// them back, as they do for text.
func TestCommentFieldsDocument(t *testing.T) {
	var doc kuddle.Document
	if err := kuddle.Unmarshal([]byte(edge), &doc); err != nil {
		t.Fatal(err)
	}
	var v edgeConfig
	if err := doc.To(&v); err != nil {
		t.Fatal(err)
	}
	var fromText edgeConfig
	if err := kuddle.Unmarshal([]byte(edge), &fromText); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v, fromText) {
		t.Errorf("To: got %+v, want %+v", v, fromText)
	}

	var back kuddle.Document
	if err := back.From(v); err != nil {
		t.Fatal(err)
	}
	out, err := kuddle.Marshal(back)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != edgeWritten {
		t.Errorf("From: got\n%s\nwant\n%s", out, edgeWritten)
	}
}

// deepDocument returns a Document of nodes nested n deep, each with a
// children block.
func deepDocument(n int) *kuddle.Document {
	node := kuddle.Node{Name: "a", Children: []kuddle.Node{}}
	for range n - 1 {
		node = kuddle.Node{Name: "a", Children: []kuddle.Node{node}}
	}
	return &kuddle.Document{Nodes: []kuddle.Node{node}}
}

// TestDocumentNestingDepth checks that To reads a Document with children
// blocks nested as deep as a Decoder reads them, and stops at the block past
// that, as Marshal does, rather than running out of stack.
func TestDocumentNestingDepth(t *testing.T) {
	const limit = 10000
	var v recursive
	if err := deepDocument(limit).To(&v); err != nil {
		t.Errorf("To, %d deep: %v", limit, err)
	}
	if _, err := kuddle.Marshal(deepDocument(limit), kdltext.WithIndent("")); err != nil {
		t.Errorf("Marshal, %d deep: %v", limit, err)
	}
	if err := deepDocument(limit + 1).To(&v); err == nil || !strings.Contains(err.Error(), "kuddle: children blocks nested deeper than 10000") {
		t.Errorf("To, %d deep: err = %v", limit+1, err)
	}
	if _, err := kuddle.Marshal(deepDocument(limit+1), kdltext.WithIndent("")); err == nil || !strings.Contains(err.Error(), "nested deeper than 10000") {
		t.Errorf("Marshal, %d deep: err = %v", limit+1, err)
	}

	// From copies a Document's nodes directly, but stops where an Encoder
	// would: at a child, or an empty children block, past the limit.
	var doc kuddle.Document
	if err := doc.From(deepDocument(limit)); err != nil {
		t.Errorf("From, %d deep: %v", limit, err)
	}
	if err := doc.From(deepDocument(limit + 1)); err == nil || !strings.Contains(err.Error(), "nested deeper than 10000") {
		t.Errorf("From, %d deep with an empty block: err = %v", limit+1, err)
	}
	noBlock := deepDocument(limit + 1)
	innermost := &noBlock.Nodes[0]
	for len(innermost.Children) > 0 {
		innermost = &innermost.Children[0]
	}
	innermost.Children = nil
	if err := doc.From(noBlock); err != nil {
		t.Errorf("From, %d nodes in %d blocks: %v", limit+1, limit, err)
	}
	if err := doc.From(&kuddle.Document{Nodes: []kuddle.Node{{Name: "a", Children: noBlock.Nodes}}}); err == nil || !strings.Contains(err.Error(), "nested deeper than 10000") {
		t.Errorf("From, %d nodes: err = %v", limit+2, err)
	}
}

// TestDocumentFromCycle checks that From reports a cycle, as Marshal does,
// rather than running out of stack.
func TestDocumentFromCycle(t *testing.T) {
	c := &cycle{}
	c.Next = c
	var doc kuddle.Document
	if err := doc.From(c); err == nil || !strings.Contains(err.Error(), "encountered a cycle") {
		t.Errorf("From: err = %v", err)
	}
}
