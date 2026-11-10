package kdltext_test

import (
	"io"
	"strings"
	"testing"

	"github.com/miquella/kuddle/internal/kdltest"
	"github.com/miquella/kuddle/kdltext"
)

// tokensOf reads all the tokens of input, and the error that ended them, if
// it wasn't io.EOF.
func tokensOf(input string, opts ...kdltext.Options) ([]kdltext.Token, error) {
	d := kdltext.NewDecoder(strings.NewReader(input), opts...)
	var toks []kdltext.Token
	for {
		t, err := d.ReadToken()
		if err == io.EOF {
			return toks, nil
		} else if err != nil {
			return toks, err
		}
		toks = append(toks, t.Clone())
	}
}

// withoutComments returns toks without comment tokens, or the comments of
// nodes.
func withoutComments(toks []kdltext.Token) []kdltext.Token {
	var out []kdltext.Token
	for _, t := range toks {
		switch t.Kind {
		case kdltext.KindComment, kdltext.KindEntryComment:
			continue
		}
		t.Comment = kdltext.Comment{}
		out = append(out, t)
	}
	return out
}

// TestDecodeDiscardComments checks that a Decoder that discards comments
// reads the same document, minus its comments, as one that doesn't, on every
// conformance case and on documents that are full of comments.
func TestDecodeDiscardComments(t *testing.T) {
	type doc struct{ name, input string }
	var docs []doc
	for _, v := range []int{1, 2} {
		cases, err := kdltest.Cases(v)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			docs = append(docs, doc{c.Name, string(c.Input)})
		}
	}
	docs = append(docs,
		doc{"line", "// a\n// b\nnode 1 // c\n\n// d\nother\n"},
		doc{"block", "/* a */ node /* b */ 1 /* c */ { /* d */ x /* e */ } /* f */\n"},
		doc{"slashdash", "/- gone 1\nnode /- 1 2 key=3 /- { child } 4 { /- skipped\nkept }\n"},
		doc{"slashdash block", "node /-{ a } { b } /-{ c }\nnext\n"},
		doc{"semicolon", "a; // x\nb /* y */; c\n"},
		doc{"nested", "/* outer /* inner */ still */ node // end\n"},
		doc{"slashdash space", "node /- 1 // x\n /* y */ 2 \\\n // z\n 3\n"},
		doc{"unterminated block", "node /* never ends\n"},
		doc{"bad", "node { // x\n"},
	)
	for _, d := range docs {
		t.Run(d.name, func(t *testing.T) {
			want, wantErr := tokensOf(d.input)
			want = withoutComments(want)
			got, gotErr := tokensOf(d.input, kdltext.DiscardComments(true))
			if (wantErr == nil) != (gotErr == nil) || (wantErr != nil && wantErr.Error() != gotErr.Error()) {
				t.Fatalf("error %v, want %v", gotErr, wantErr)
			}
			if len(got) != len(want) {
				t.Fatalf("got %d tokens, want %d", len(got), len(want))
			}
			for i := range got {
				if !got[i].Equal(want[i]) {
					t.Fatalf("token %d: got %+v, want %+v", i, got[i], want[i])
				}
			}
		})
	}
}
