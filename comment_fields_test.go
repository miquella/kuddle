package kuddle_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/miquella/kuddle"
	"github.com/miquella/kuddle/kdltext"
)

// edge is a document with comments in every place a struct can keep them.
const edge = `// Acme edge configuration
// Maintained by the platform team

// The public listener.
listen :443

// Rolled out in March; see the runbook.

// Backends for the app tier.
upstream app {
    // Keep this list sorted by host.

    // Primary.
    server a.example.com
    server b.example.com
}
`

// edgeWritten is edge as Marshal writes it: a comment goes right below the node
// before it, so the blank line before "Rolled out" is not kept.
var edgeWritten = strings.Replace(edge, "listen :443\n\n// Rolled out", "listen :443\n// Rolled out", 1)

type edgeConfig struct {
	Header   []string     `kdl:",headercomment"`
	Listen   edgeListener `kdl:"listen"`
	Upstream edgeUpstream `kdl:"upstream"`
}

type edgeListener struct {
	Addr    string `kdl:",arg"`
	Comment string `kdl:",comment"`
}

type edgeUpstream struct {
	Name    string       `kdl:",arg"`
	Above   []string     `kdl:",comment"`
	Opening []string     `kdl:",headercomment"`
	Servers []edgeServer `kdl:"server"`
}

type edgeServer struct {
	Host    string `kdl:",arg"`
	Comment string `kdl:",comment"`
}

func TestCommentFieldsEdge(t *testing.T) {
	want := edgeConfig{
		Header: []string{"Acme edge configuration\nMaintained by the platform team"},
		Listen: edgeListener{Addr: ":443", Comment: "The public listener."},
		Upstream: edgeUpstream{
			Name:    "app",
			Above:   []string{"Rolled out in March; see the runbook.", "Backends for the app tier."},
			Opening: []string{"Keep this list sorted by host."},
			Servers: []edgeServer{
				{Host: "a.example.com", Comment: "Primary."},
				{Host: "b.example.com"},
			},
		},
	}
	var got edgeConfig
	if err := kuddle.Unmarshal([]byte(edge), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}

	// Every comment is written back where it was. The one thing that isn't
	// kept is the blank line before a comment that is between two nodes: a
	// comment is written right below the node before it.
	out, err := kuddle.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != edgeWritten {
		t.Errorf("got\n%s\nwant\n%s", out, edgeWritten)
	}
	var again edgeConfig
	if err := kuddle.Unmarshal(out, &again); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again, want) {
		t.Errorf("read again: got %+v, want %+v", again, want)
	}

	// A comment travels with its node, and the head of the document and of
	// the children stay where they are.
	got.Upstream.Servers[0], got.Upstream.Servers[1] = got.Upstream.Servers[1], got.Upstream.Servers[0]
	out, err = kuddle.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	moved := strings.Replace(edgeWritten, "    // Primary.\n    server a.example.com\n    server b.example.com\n",
		"    server b.example.com\n    // Primary.\n    server a.example.com\n", 1)
	if string(out) != moved {
		t.Errorf("swapped servers: got\n%s\nwant\n%s", out, moved)
	}
}

// Without a headercomment field, the comments at the head of a document go
// above its first node, with that node's own comment.
func TestCommentFieldsNoHeaderField(t *testing.T) {
	type config struct {
		Listen edgeListener `kdl:"listen"`
	}
	var got config
	if err := kuddle.Unmarshal([]byte(edge), &got, kuddle.DiscardUnknown(true)); err != nil {
		t.Fatal(err)
	}
	want := "Acme edge configuration\nMaintained by the platform team\n\nThe public listener."
	if got.Listen.Comment != want {
		t.Errorf("got %q, want %q", got.Listen.Comment, want)
	}
}

// A struct that only wants the head of the document doesn't get the comment
// of its first node.
func TestCommentFieldsHeaderOnly(t *testing.T) {
	type config struct {
		Header string `kdl:",headercomment"`
	}
	var got config
	if err := kuddle.Unmarshal([]byte(edge), &got, kuddle.DiscardUnknown(true)); err != nil {
		t.Fatal(err)
	}
	if want := "Acme edge configuration\nMaintained by the platform team"; got.Header != want {
		t.Errorf("got %q, want %q", got.Header, want)
	}
}

func TestCommentFieldsShapes(t *testing.T) {
	const in = "// A.\n// A2.\n\n/* B */\n\n// C.\nn\n"
	line := func(text string) kdltext.Comment { return kdltext.Comment{Kind: kdltext.CommentLine, Text: text} }
	block := func(text string) kdltext.Comment { return kdltext.Comment{Kind: kdltext.CommentBlock, Text: text} }

	type shapes struct {
		Text   string
		Items  []string
		Blocks []kdltext.Comment
		Array  [2]string
		Short  [5]kdltext.Comment
	}
	var got shapes
	for _, f := range []struct {
		name   string
		decode func() error
	}{
		{"string", func() error {
			var x struct {
				N struct {
					C string `kdl:",comment"`
				} `kdl:"n"`
			}
			err := kuddle.Unmarshal([]byte(in), &x)
			got.Text = x.N.C
			return err
		}},
		{"[]string", func() error {
			var x struct {
				N struct {
					C []string `kdl:",comment"`
				} `kdl:"n"`
			}
			err := kuddle.Unmarshal([]byte(in), &x)
			got.Items = x.N.C
			return err
		}},
		{"[]kdltext.Comment", func() error {
			var x struct {
				N struct {
					C []kdltext.Comment `kdl:",comment"`
				} `kdl:"n"`
			}
			err := kuddle.Unmarshal([]byte(in), &x)
			got.Blocks = x.N.C
			return err
		}},
		{"[2]string", func() error {
			var x struct {
				N struct {
					C [2]string `kdl:",comment"`
				} `kdl:"n"`
			}
			err := kuddle.Unmarshal([]byte(in), &x)
			got.Array = x.N.C
			return err
		}},
		{"[5]kdltext.Comment", func() error {
			var x struct {
				N struct {
					C [5]kdltext.Comment `kdl:",comment"`
				} `kdl:"n"`
			}
			err := kuddle.Unmarshal([]byte(in), &x)
			got.Short = x.N.C
			return err
		}},
	} {
		if err := f.decode(); err != nil {
			t.Fatalf("%s: %v", f.name, err)
		}
	}

	if want := "A.\nA2.\n\nB\n\nC."; got.Text != want {
		t.Errorf("string: got %q, want %q", got.Text, want)
	}
	if want := []string{"A.\nA2.", "B", "C."}; !reflect.DeepEqual(got.Items, want) {
		t.Errorf("[]string: got %q, want %q", got.Items, want)
	}
	wantBlocks := []kdltext.Comment{line(" A.\n A2."), block(" B "), line(" C.")}
	if !reflect.DeepEqual(got.Blocks, wantBlocks) {
		t.Errorf("[]kdltext.Comment: got %+v, want %+v", got.Blocks, wantBlocks)
	}
	// An array takes as many as it has room for, and leaves the rest empty.
	if want := [2]string{"A.\nA2.", "B"}; got.Array != want {
		t.Errorf("[2]string: got %q, want %q", got.Array, want)
	}
	if want := [5]kdltext.Comment{line(" A.\n A2."), block(" B "), line(" C.")}; got.Short != want {
		t.Errorf("[5]kdltext.Comment: got %+v, want %+v", got.Short, want)
	}
}

// Writing: one block for each element of a slice, and for each run of lines
// in a string.
func TestCommentFieldsMarshal(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want string
	}{
		{"string", &struct {
			N struct {
				C string `kdl:",comment"`
			} `kdl:"n"`
		}{N: struct {
			C string `kdl:",comment"`
		}{"One.\nTwo.\n\nThree."}}, "// One.\n// Two.\n\n// Three.\nn\n"},
		{"more blank lines", &struct {
			N struct {
				C string `kdl:",comment"`
			} `kdl:"n"`
		}{N: struct {
			C string `kdl:",comment"`
		}{"\n\nOne.\n\n\n\nTwo.\n\n"}}, "// One.\n\n// Two.\nn\n"},
		{"slice", &struct {
			N struct {
				C []string `kdl:",comment"`
			} `kdl:"n"`
		}{N: struct {
			C []string `kdl:",comment"`
		}{[]string{"One.\nTwo.", "", "Three."}}}, "// One.\n// Two.\n\n// Three.\nn\n"},
		{"empty line kept in a slice", &struct {
			N struct {
				C []string `kdl:",comment"`
			} `kdl:"n"`
		}{N: struct {
			C []string `kdl:",comment"`
		}{[]string{"One.\n\nTwo."}}}, "// One.\n//\n// Two.\nn\n"},
		{"comments", &struct {
			N struct {
				C []kdltext.Comment `kdl:",comment"`
			} `kdl:"n"`
		}{N: struct {
			C []kdltext.Comment `kdl:",comment"`
		}{[]kdltext.Comment{
			{Kind: kdltext.CommentLine, Text: " One."},
			{},
			{Kind: kdltext.CommentBlock, Text: " Two. "},
		}}}, "// One.\n/* Two. */\nn\n"},
		{"array", &struct {
			N struct {
				C [3]string `kdl:",comment"`
			} `kdl:"n"`
		}{N: struct {
			C [3]string `kdl:",comment"`
		}{[3]string{"One.", "", "Two."}}}, "// One.\n\n// Two.\nn\n"},
		{"none", &struct {
			N struct {
				C []string `kdl:",comment"`
			} `kdl:"n"`
		}{}, "n\n"},
		{"slash-dashed last", &struct {
			N struct {
				C []kdltext.Comment `kdl:",comment"`
			} `kdl:"n"`
		}{N: struct {
			C []kdltext.Comment `kdl:",comment"`
		}{[]kdltext.Comment{{Kind: kdltext.CommentSlashdash, Text: " old"}}}}, "/- old\nn\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := kuddle.Marshal(tt.v)
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != tt.want {
				t.Errorf("got\n%s\nwant\n%s", out, tt.want)
			}
		})
	}
}

// Slash-dashed parts are not comments in a comment field.
func TestCommentFieldsSlashdash(t *testing.T) {
	var v struct {
		N struct {
			C []string `kdl:",comment"`
		} `kdl:"n"`
		M struct {
			C string `kdl:",comment"`
		} `kdl:"m"`
	}
	in := "/- old 1\n\n// About n.\nn\n\n/- old 2\n/* About m. */\nm\n"
	if err := kuddle.Unmarshal([]byte(in), &v); err != nil {
		t.Fatal(err)
	}
	if want := []string{"About n."}; !reflect.DeepEqual(v.N.C, want) {
		t.Errorf("n: got %q, want %q", v.N.C, want)
	}
	if v.M.C != "About m." {
		t.Errorf("m: got %q", v.M.C)
	}
}

// A block with no children, other than comments, has them as its head.
func TestCommentFieldsEmptyBlock(t *testing.T) {
	type node struct {
		Head []string `kdl:",headercomment"`
	}
	var v struct {
		A node `kdl:"a"`
		B node `kdl:"b"`
		C node `kdl:"c"`
	}
	in := "a {\n    // One.\n\n    // Two.\n}\nb\nc {\n}\n"
	if err := kuddle.Unmarshal([]byte(in), &v); err != nil {
		t.Fatal(err)
	}
	if want := []string{"One.", "Two."}; !reflect.DeepEqual(v.A.Head, want) {
		t.Errorf("a: got %+v", v.A)
	}
	if v.B.Head != nil || v.C.Head != nil {
		t.Errorf("b and c: got %+v and %+v, want none", v.B, v.C)
	}
}

// The comments after the last child belong to no node, and are dropped.
func TestCommentFieldsTrailing(t *testing.T) {
	type node struct {
		Head []string `kdl:",headercomment"`
		Kids []struct {
			Comment string `kdl:",comment"`
		} `kdl:"k"`
	}
	var v struct {
		N node `kdl:"n"`
		M struct {
			Comment string `kdl:",comment"`
		} `kdl:"m"`
	}
	in := "n {\n    k\n    // Last.\n}\n// For m.\nm\n"
	if err := kuddle.Unmarshal([]byte(in), &v); err != nil {
		t.Fatal(err)
	}
	if v.N.Head != nil || len(v.N.Kids) != 1 || v.N.Kids[0].Comment != "" {
		t.Errorf("n: got %+v", v.N)
	}
	if v.M.Comment != "For m." {
		t.Errorf("m: got %q, want only its own comment", v.M.Comment)
	}
}

// Comments in nested nodes, and in nodes that are skipped, don't leak into
// the comments of the nodes next to them.
func TestCommentFieldsNesting(t *testing.T) {
	type leaf struct {
		Comment []string `kdl:",comment"`
		Name    string   `kdl:",arg"`
	}
	type branch struct {
		Comment []string `kdl:",comment"`
		Head    []string `kdl:",headercomment"`
		Leaves  []leaf   `kdl:"leaf"`
	}
	var v struct {
		Branches []branch `kdl:"branch"`
	}
	in := `// B1.
branch {
    // Head 1.

    // L1.
    leaf one {
        // Inside a skipped node.

        // And another.
        skipped
        // Dropped.
    }
    // L2a.

    // L2b.
    leaf two
    // Dropped.
}

// B2a.

// B2b.
branch {
    // Head 2.

    // L3.
    leaf three
}
`
	if err := kuddle.Unmarshal([]byte(in), &v, kuddle.DiscardUnknown(true)); err != nil {
		t.Fatal(err)
	}
	want := []branch{
		{
			Comment: []string{"B1."},
			Head:    []string{"Head 1."},
			Leaves: []leaf{
				{Comment: []string{"L1."}, Name: "one"},
				{Comment: []string{"L2a.", "L2b."}, Name: "two"},
			},
		},
		{
			Comment: []string{"B2a.", "B2b."},
			Head:    []string{"Head 2."},
			Leaves:  []leaf{{Comment: []string{"L3."}, Name: "three"}},
		},
	}
	if !reflect.DeepEqual(v.Branches, want) {
		t.Errorf("got  %+v\nwant %+v", v.Branches, want)
	}
}

// A struct that is decoded into again has its comment fields set afresh.
func TestCommentFieldsReset(t *testing.T) {
	var v edgeConfig
	v.Header = []string{"stale"}
	v.Listen.Comment = "stale"
	v.Upstream.Above = []string{"stale"}
	v.Upstream.Opening = []string{"stale"}
	if err := kuddle.Unmarshal([]byte("listen :80\nupstream x\n"), &v); err != nil {
		t.Fatal(err)
	}
	if v.Header != nil || v.Listen.Comment != "" || v.Upstream.Above != nil || v.Upstream.Opening != nil {
		t.Errorf("got %+v", v)
	}
}

func TestCommentFieldsErrors(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want string
	}{
		{"single Comment", &struct {
			C kdltext.Comment `kdl:",comment"`
		}{}, "a comment field can't be a single kdltext.Comment"},
		{"single Comment in a header", &struct {
			C kdltext.Comment `kdl:",headercomment"`
		}{}, "a headercomment field can't be a single kdltext.Comment"},
		{"int", &struct {
			C int `kdl:",comment"`
		}{}, "a comment field must be a string, a []string or a []kdltext.Comment"},
		{"[]int", &struct {
			C []int `kdl:",headercomment"`
		}{}, "a headercomment field must be a string, a []string or a []kdltext.Comment"},
		{"two comments", &struct {
			A string `kdl:",comment"`
			B string `kdl:",comment"`
		}{}, `more than one field is tagged "comment"`},
		{"two headers", &struct {
			A string   `kdl:",headercomment"`
			B []string `kdl:",headercomment"`
		}{}, `more than one field is tagged "headercomment"`},
		{"a name", &struct {
			A string `kdl:"name,headercomment"`
		}{}, "the name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := kuddle.Marshal(tt.v)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Marshal: got %v, want an error with %q", err, tt.want)
			}
			err = kuddle.Unmarshal([]byte("n\n"), tt.v)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Unmarshal: got %v, want an error with %q", err, tt.want)
			}
		})
	}
}

// delegatedEdge is decoded and encoded by the default rules, through
// UnmarshalDecode and MarshalEncode, and so are its comment fields.
type delegatedEdge edgeUpstream

func (d *delegatedEdge) UnmarshalKDL(dec *kdltext.Decoder) error {
	return kuddle.UnmarshalDecode(dec, (*edgeUpstream)(d))
}

func (d delegatedEdge) MarshalKDL(enc *kdltext.Encoder) error {
	return kuddle.MarshalEncode(enc, edgeUpstream(d))
}

func TestCommentFieldsMethods(t *testing.T) {
	var v struct {
		Upstream delegatedEdge `kdl:"upstream"`
	}
	in := "// Above.\nupstream app {\n    // Head.\n\n    server a\n}\n"
	if err := kuddle.Unmarshal([]byte(in), &v); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v.Upstream.Above, []string{"Above."}) || !reflect.DeepEqual(v.Upstream.Opening, []string{"Head."}) {
		t.Errorf("got %+v", v.Upstream)
	}
	out, err := kuddle.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != in {
		t.Errorf("got\n%s\nwant\n%s", out, in)
	}
}
