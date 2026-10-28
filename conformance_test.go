package kuddle_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/miquella/kuddle"
	"github.com/miquella/kuddle/internal/kdltest"
	"github.com/miquella/kuddle/kdltext"
)

// TestConformance runs the KDL conformance tests through Unmarshal and
// Marshal. For each valid input, it checks that:
//
//   - the input, unmarshaled into a Document and marshaled in the suite's
//     format (see suiteOptions and uniqueProps), is the expected output;
//   - the Document marshals, by default, to a document that unmarshals to
//     the same Document, comments included; and
//   - the expected output is unchanged by unmarshaling and marshaling.
//
// Invalid inputs must fail to unmarshal.
//
// The outputs are compared with the expected outputs byte for byte: they are
// made to match by the options of Unmarshal and Marshal, and by leaving out
// repeated properties, without changing the text.
func TestConformance(t *testing.T) {
	for _, v := range []kdltext.Version{kdltext.V1, kdltext.V2} {
		cases, err := kdltest.Cases(int(v))
		if err != nil {
			t.Fatal(err)
		}
		suite := suiteOptions(v)
		for _, c := range cases {
			t.Run(fmt.Sprintf("v%d/%s", v, c.Name), func(t *testing.T) {
				if c.Divergence != "" {
					t.Skip(c.Divergence)
				}
				var doc kuddle.Document
				err := kuddle.Unmarshal(c.Input, &doc, suite)
				if c.Fail {
					if err == nil {
						t.Fatalf("unmarshaled, want error; got %+v", doc)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}

				out, err := kuddle.Marshal(doc, kdltext.WithVersion(v))
				if err != nil {
					t.Fatal(err)
				}
				var again kuddle.Document
				if err := kuddle.Unmarshal(out, &again, suite); err != nil {
					t.Fatalf("unmarshaling the marshaled input: %v\n%s", err, out)
				}
				if !again.Equal(&doc) {
					t.Errorf("round trip changed the document:\n%s", out)
				}

				if got, err := kuddle.Marshal(suiteFormat(uniqueProps(&doc)), suite); err != nil {
					t.Fatal(err)
				} else if !bytes.Equal(got, c.Expected) {
					t.Errorf("input: got\n%s\nwant\n%s", got, c.Expected)
				}

				var exp kuddle.Document
				if err := kuddle.Unmarshal(c.Expected, &exp, suite); err != nil {
					t.Fatalf("unmarshaling the expected output: %v", err)
				}
				if got, err := kuddle.Marshal(suiteFormat(uniqueProps(&exp)), suite); err != nil {
					t.Fatal(err)
				} else if !bytes.Equal(got, c.Expected) {
					t.Errorf("expected output: got\n%s\nwant\n%s", got, c.Expected)
				}
			})
		}
	}
}

// suiteOptions returns the options that read and write documents as the
// suite for version v does. Following its translation rules, documents are
// written with properties sorted after the arguments, and
// with numbers in canonical form. The rules also leave out repeated
// properties, which uniqueProps does.
func suiteOptions(v kdltext.Version) kuddle.Options {
	return kuddle.JoinOptions(
		kdltext.WithVersion(v),
	)
}

// suiteFormat formats doc in the suite's canonical style.
func suiteFormat(doc *kuddle.Document) *kuddle.Document {
	doc.Format(
		kuddle.FormatSortProperties(true),
		kuddle.FormatNumbers(kuddle.NumberDecimal),
		kuddle.FormatEmptyBlocks(kuddle.EmptyBlockOmitted),
		kuddle.FormatNormalizeStrings(true),
	)
	return doc
}

// uniqueProps returns a copy of doc in which each node has only the last of
// its properties of each name, the one that takes precedence.
func uniqueProps(doc *kuddle.Document) *kuddle.Document {
	c := doc.Clone()
	var unique func(nodes []kuddle.Node)
	unique = func(nodes []kuddle.Node) {
		for i := range nodes {
			n := &nodes[i]
			last := map[string]int{}
			for j, e := range n.Entries {
				if e.IsProp() {
					last[e.Name] = j
				}
			}
			entries := n.Entries[:0]
			for j, e := range n.Entries {
				if !e.IsProp() || last[e.Name] == j {
					entries = append(entries, e)
				}
			}
			n.Entries = entries
			unique(n.Children)
		}
	}
	unique(c.Nodes)
	return c
}
