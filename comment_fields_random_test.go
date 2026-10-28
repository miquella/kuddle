package kuddle_test

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/miquella/kuddle"
	"github.com/miquella/kuddle/kdltext"
)

// randomLevel writes some nodes to b, with comments and blank lines among
// them, at the depth given.
func randomLevel(b *strings.Builder, rng *rand.Rand, depth int) {
	pad := strings.Repeat("    ", depth)
	for range rng.IntN(7) {
		switch rng.IntN(10) {
		case 0, 1:
			fmt.Fprintf(b, "%s// line %d\n", pad, rng.IntN(100))
		case 2:
			fmt.Fprintf(b, "%s/* block %d */\n", pad, rng.IntN(100))
		case 3:
			b.WriteString("\n")
		case 4:
			fmt.Fprintf(b, "%s/- skipped%d\n", pad, rng.IntN(100))
		default:
			fmt.Fprintf(b, "%sn%d", pad, rng.IntN(100))
			if depth < 3 && rng.IntN(2) == 0 {
				b.WriteString(" {")
				if rng.IntN(4) == 0 {
					fmt.Fprintf(b, " // opens %d", rng.IntN(100))
				}
				b.WriteString("\n")
				randomLevel(b, rng, depth+1)
				b.WriteString(pad + "}")
			} else if rng.IntN(4) == 0 {
				fmt.Fprintf(b, " // on the line %d", rng.IntN(100))
			}
			b.WriteString("\n")
		}
	}
}

type randomNode struct {
	Name  string            `kdl:",name"`
	Above []kdltext.Comment `kdl:",comment"`
	Head  []kdltext.Comment `kdl:",headercomment"`
	Nodes []randomNode      `kdl:",children"`
}

type randomRoot struct {
	Head  []kdltext.Comment `kdl:",headercomment"`
	Nodes []randomNode      `kdl:",children"`
}

// TestCommentFieldsRandom decodes documents full of comments, writes what was
// decoded, and decodes that again: the comments must come out as they went
// in.
func TestCommentFieldsRandom(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 12))
	for i := range 3000 {
		var b strings.Builder
		randomLevel(&b, rng, 0)
		text := b.String()

		var first randomRoot
		if err := kuddle.Unmarshal([]byte(text), &first); err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		out, err := kuddle.Marshal(first)
		if err != nil {
			t.Fatalf("%q: Marshal: %v", text, err)
		}
		var second randomRoot
		if err := kuddle.Unmarshal(out, &second); err != nil {
			t.Fatalf("%q: written as %q: %v", text, out, err)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("#%d: %q\nread as    %+v\nwritten as %q\nread again %+v", i, text, first, out, second)
		}
	}
}
