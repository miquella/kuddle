package lexer_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/miquella/kuddle/internal/kdltest"
	"github.com/miquella/kuddle/internal/lexer"
)

// TestConformance lexes the valid inputs and expected outputs of the KDL
// conformance tests. Failing inputs are left to the parser's tests, since
// most of them are rejected by the parser rather than the lexer. Cases that
// contradict the spec are skipped.
func TestConformance(t *testing.T) {
	for _, v := range []lexer.Version{lexer.V1, lexer.V2} {
		cases, err := kdltest.Cases(int(v))
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			if c.Fail {
				continue
			}
			t.Run(fmt.Sprintf("v%d/%s", v, c.Name), func(t *testing.T) {
				if c.Divergence != "" {
					t.Skip(c.Divergence)
				}
				for part, doc := range map[string][]byte{"input": c.Input, "expected": c.Expected} {
					if _, _, err := lexAll(bytes.NewReader(doc), v); err != nil {
						t.Errorf("%s: %v", part, err)
					}
					if _, got, err := lexAll(bytes.NewReader(doc), lexer.Undecided); err != nil {
						t.Errorf("%s (undecided): %v", part, err)
					} else if got != v && got != lexer.Undecided {
						t.Errorf("%s (undecided): detected v%d", part, got)
					}
				}
			})
		}
	}
}
