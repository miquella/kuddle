package kuddle_test

import (
	"strings"
	"testing"

	"github.com/miquella/kuddle"
	"github.com/miquella/kuddle/kdltext"
)

// commented is written as Marshal writes it, so that it round-trips exactly.
const commented = `// Detached.

// The server.
server main /* name */ /- "old" { // the main one
    // Where to listen,
    // in order.
    listen :80 // http
    /- listen :8080
    tls #true
    // More to come.
}
// After the server.

// The end.
`

func TestDocumentRoundTrip(t *testing.T) {
	var doc kuddle.Document
	if err := kuddle.Unmarshal([]byte(commented), &doc); err != nil {
		t.Fatal(err)
	}
	out, err := kuddle.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != commented {
		t.Errorf("got\n%s\nwant\n%s", out, commented)
	}

	var again kuddle.Document
	if err := kuddle.Unmarshal(out, &again); err != nil {
		t.Fatal(err)
	}
	if !again.Equal(&doc) {
		t.Errorf("unmarshaled again:\ngot  %+v\nwant %+v", again, doc)
	}

	// A Node keeps its comments, too.
	var v struct {
		Server kuddle.Node `kdl:"server"`
	}
	if err := kuddle.Unmarshal([]byte(commented), &v); err != nil {
		t.Fatal(err)
	}
	out, err = kuddle.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	start, end := strings.Index(commented, "// The server."), strings.Index(commented, "// After")
	if want := commented[start:end]; string(out) != want {
		t.Errorf("node: got\n%s\nwant\n%s", out, want)
	}
}

func TestNodeComment(t *testing.T) {
	var doc kuddle.Document
	if err := kuddle.Unmarshal([]byte(commented), &doc); err != nil {
		t.Fatal(err)
	}
	server := &doc.Nodes[1]
	if got := server.Comment.Text; got != " The server." {
		t.Errorf("server: got %q", got)
	}
	listen, tls := &server.Children[0], &server.Children[2]
	if got := listen.Comment.Text; got != " Where to listen,\n in order." {
		t.Errorf("listen: got %q", got)
	}
	if got := tls.Comment; got.Kind != 0 {
		t.Errorf("tls: got %+v", got)
	}

	server.Comment.Text = " The web server."
	listen.Comment = kdltext.Comment{}
	tls.Comment = kdltext.Comment{Kind: kdltext.CommentLine, Text: " Serve TLS,\n always."}
	doc.Nodes = append(doc.Nodes, kuddle.Node{Name: "new", Comment: kdltext.Comment{Kind: kdltext.CommentBlock, Text: " Added. "}})
	out, err := kuddle.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	want := `// Detached.

// The web server.
server main /* name */ /- "old" { // the main one
    listen :80 // http
    /- listen :8080
    // Serve TLS,
    // always.
    tls #true
    // More to come.
}
// After the server.

// The end.
/* Added. */
new
`
	if string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}
func TestUnmarshalPassesOverComments(t *testing.T) {
	var v struct {
		P *int   `kdl:"p"`
		N int    `kdl:"n"`
		A any    `kdl:"a"`
		L [3]int `kdl:"l"`
	}
	v.P = new(1)
	input := "// c\np #null // null\nn /* x */ 1 /- 2\na 3 // c\nl 1 /* x */ 2 {\n    // c\n    - 3\n} /- { - 4 }\n"
	if err := kuddle.Unmarshal([]byte(input), &v); err != nil {
		t.Fatal(err)
	}
	if v.P != nil || v.N != 1 || v.A != int64(3) || v.L != [3]int{1, 2, 3} {
		t.Errorf("got %+v", v)
	}
}

func TestStructEntriesComments(t *testing.T) {
	// The entries field keeps the comment entries in order, even a
	// slash-dashed children block after the children, which is written before
	// them, as for a Node.
	type node struct {
		Rest []kuddle.Entry `kdl:",entries"`
		Kids []kuddle.Node  `kdl:",children"`
	}
	in := "n 1 /* b */ 2 /- 3 { // opens\n    - 4\n} /- { y; }\n"
	var v struct {
		N node `kdl:"n"`
	}
	if err := kuddle.Unmarshal([]byte(in), &v); err != nil {
		t.Fatal(err)
	}
	comment := func(k kdltext.CommentKind, text string) kuddle.Entry {
		return kuddle.Entry{Kind: kuddle.EntryComment, Comment: kdltext.Comment{Kind: k, Text: text}}
	}
	want := []kuddle.Entry{
		kuddle.Arg(kdltext.Int(1)),
		comment(kdltext.CommentBlock, " b "),
		kuddle.Arg(kdltext.Int(2)),
		comment(kdltext.CommentSlashdash, " 3"),
		comment(kdltext.CommentLine, " opens"),
		comment(kdltext.CommentSlashdash, " { y; }"),
	}
	if !equalEntries(v.N.Rest, want) {
		t.Errorf("got  %+v\nwant %+v", v.N.Rest, want)
	}
	out, err := kuddle.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if want := "n 1 /* b */ 2 /- 3 \\ // opens\n    /- { y; } {\n    - 4\n}\n"; string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}
