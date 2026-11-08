package kuddle_test

import (
	"bytes"
	"math"
	"math/big"
	"slices"
	"strings"
	"testing"

	"github.com/miquella/kuddle"
	"github.com/miquella/kuddle/internal/kdltest"
	"github.com/miquella/kuddle/kdltext"
)

// FuzzDocumentRoundTrip checks that any input that unmarshals into a
// Document marshals, after formatting, with the options picked by the bits of opts (see
// fuzzOptions), to a document that:
//
//   - unmarshals, in the version it was written in;
//   - holds the same Document, less what the options leave out or rewrite
//     (see expectDocument and equivalentNodes); and
//   - marshals, with the same options, to the same bytes again.
//
// The Document may fail to marshal only in the other version than the input
// was read as: some values, such as #inf, can only be written in v2, and a
// slash-dashed part keeps the text of the version it was read in. The seeds
// are the inputs of the conformance suites.
func FuzzDocumentRoundTrip(f *testing.F) {
	for _, v := range []int{1, 2} {
		cases, err := kdltest.Cases(v)
		if err != nil {
			f.Fatal(err)
		}
		for i, c := range cases {
			f.Add(c.Input, uint16(0))
			f.Add(c.Input, uint16(i*0x9e37))
		}
	}

	f.Fuzz(func(t *testing.T, data []byte, opts uint16) {
		var doc kuddle.Document
		dec := kdltext.NewDecoder(bytes.NewReader(data))
		if err := kuddle.UnmarshalDecode(dec, &doc); err != nil {
			return
		}

		fo := fuzzOptions(opts)
		doc.Format(fo.format...)
		twice := doc.Clone()
		twice.Format(fo.format...)
		if !twice.Equal(&doc) {
			t.Fatalf("formatting again with %s changed the document:\n%+v\nthen\n%+v", fo, doc, *twice)
		}
		out, err := kuddle.Marshal(&doc, fo.options)
		if err != nil {
			if v := dec.Version(); v != 0 && v != fo.version {
				return
			}
			t.Fatalf("marshaling with %s: %v", fo, err)
		}

		var again kuddle.Document
		if err := kuddle.Unmarshal(out, &again, fo.options); err != nil {
			t.Fatalf("unmarshaling the output of %s: %v\n%s", fo, err, out)
		}
		if want := expectDocument(&doc, fo); !equivalentNodes(want.Nodes, again.Nodes, fo.numbers != kuddle.NumberLiteral) {
			t.Fatalf("round trip with %s changed the document:\n%s", fo, out)
		}

		out2, err := kuddle.Marshal(&again, fo.options)
		if err != nil {
			t.Fatalf("marshaling the round-tripped document with %s: %v", fo, err)
		}
		if !bytes.Equal(out, out2) {
			t.Fatalf("marshaling with %s is not stable:\nfirst\n%s\nthen\n%s", fo, out, out2)
		}
	})
}

// fuzzOpts is a set of format and encoder options for FuzzDocumentRoundTrip.
type fuzzOpts struct {
	options kuddle.Options
	format  []kuddle.FormatOption
	bits    uint16

	version         kdltext.Version
	discardComments bool
	numbers         kuddle.NumberFormat
	crlf            bool // the Encoder is given CRLF newlines
}

func (o fuzzOpts) String() string {
	var names []string
	for i, name := range fuzzOptionNames {
		if o.bits&(1<<i) != 0 {
			names = append(names, name)
		}
	}
	return "options " + strings.Join(names, "|")
}

// fuzzOptionNames names the bits of fuzzOptions.
var fuzzOptionNames = []string{
	"V1", "SortProperties", "DiscardComments",
	"NumberCanonical", "NumberDecimal",
	"EmptyBlockOmitted", "QuoteValues", "QuoteIdentifiers",
	"NormalizeStrings", "LiteralComments", "TabIndent",
	"CRLF", "RewriteStringNewlines",
}

// fuzzOptions returns the options picked by the bits of b, in the order of
// fuzzOptionNames. Of the two bits of a number format, the first one set wins.
func fuzzOptions(b uint16) fuzzOpts {
	b &= 1<<len(fuzzOptionNames) - 1
	bit := func(i int) bool { return b&(1<<i) != 0 }
	o := fuzzOpts{bits: b, version: kdltext.V2}
	if bit(0) {
		o.version = kdltext.V1
	}
	o.discardComments = bit(2)
	switch {
	case bit(3):
		o.numbers = kuddle.NumberCanonical
	case bit(4):
		o.numbers = kuddle.NumberDecimal
	}
	blocks := kuddle.EmptyBlockCompact
	if bit(5) {
		blocks = kuddle.EmptyBlockOmitted
	}
	indent := "    "
	if bit(10) {
		indent = "\t"
	}
	newline := "\n"
	if bit(11) {
		newline = "\r\n"
	}
	o.format = []kuddle.FormatOption{
		kuddle.FormatSortProperties(bit(1)),
		kuddle.FormatNumbers(o.numbers),
		kuddle.FormatEmptyBlocks(blocks),
		kuddle.FormatQuoteValues(bit(6)),
		kuddle.FormatNormalizeStrings(bit(8)),
		kuddle.FormatLiteralComments(bit(9)),
		kuddle.FormatIndent(indent),
		kuddle.FormatNewline(newline),
		kuddle.FormatStringNewlines(bit(12)),
	}
	// The options also unmarshal the output, in the version it was written.
	o.options = kuddle.JoinOptions(
		kdltext.WithVersion(o.version),
		kdltext.DiscardComments(o.discardComments),
		kdltext.QuoteIdentifiers(bit(7)),
		kdltext.WithIndent(indent),
	)
	if bit(11) {
		o.crlf = true
		o.options = kuddle.JoinOptions(o.options, kdltext.WithNewline("\r\n"))
	}
	return o
}

// expectDocument returns the Document that doc, already formatted, should
// round-trip to with the options o: without comments for DiscardComments, and
// with CRLF newlines in block comments if the Encoder is given them.
func expectDocument(doc *kuddle.Document, o fuzzOpts) *kuddle.Document {
	var expect func(nodes []kuddle.Node) []kuddle.Node
	expect = func(nodes []kuddle.Node) []kuddle.Node {
		if nodes == nil {
			return nil
		}
		out := nodes[:0]
		for _, n := range nodes {
			if o.discardComments {
				if n.IsComment() {
					continue
				}
				n.Comment = kdltext.Comment{}
				n.Entries = slices.DeleteFunc(n.Entries, kuddle.Entry.IsComment)
				for i := range n.Entries {
					n.Entries[i].Comment = kdltext.Comment{}
				}
			}
			if o.crlf {
				n.Comment = crlfBlock(n.Comment)
				for i := range n.Entries {
					n.Entries[i].Comment = crlfBlock(n.Entries[i].Comment)
				}
			}
			n.Children = expect(n.Children)
			out = append(out, n)
		}
		return out
	}
	c := doc.Clone()
	c.Nodes = expect(c.Nodes)
	return c
}

// crlfBlock returns c as an Encoder given CRLF newlines writes it.
func crlfBlock(c kdltext.Comment) kdltext.Comment {
	if c.Kind == kdltext.CommentBlock {
		s := strings.ReplaceAll(strings.ReplaceAll(c.Text, "\r\n", "\n"), "\r", "\n")
		c.Text = strings.ReplaceAll(s, "\n", "\r\n")
	}
	return c
}

// equivalentNodes reports whether a and b are equal, as Node.Equal reports,
// but compares numbers by value if byValue is set, for options that rewrite
// their literals.
func equivalentNodes(a, b []kuddle.Node, byValue bool) bool {
	if !byValue {
		return (&kuddle.Document{Nodes: a}).Equal(&kuddle.Document{Nodes: b})
	}
	return slices.EqualFunc(a, b, func(n, m kuddle.Node) bool {
		return n.IsComment() == m.IsComment() && n.Comment == m.Comment &&
			equalTypes(n.Type, m.Type) && n.Name == m.Name &&
			slices.EqualFunc(n.Entries, m.Entries, equivalentEntries) &&
			(n.Children == nil) == (m.Children == nil) && equivalentNodes(n.Children, m.Children, true)
	})
}

func equivalentEntries(e, f kuddle.Entry) bool {
	if e.Kind != f.Kind || e.Name != f.Name || e.Comment != f.Comment {
		return false
	}
	if e.IsComment() {
		return true
	}
	v, w := e.Value, f.Value
	if v.Kind() != kdltext.ValueNumber || w.Kind() != kdltext.ValueNumber {
		return v.Equal(w)
	}
	return equalTypes(v.Type, w.Type) && sameNumber(v, w)
}

// sameNumber reports whether the numbers v and w have the same value: as
// integers if both are in range, and exactly otherwise (see exactNumber).
func sameNumber(v, w kdltext.Value) bool {
	if i, err := v.Int(); err == nil {
		j, err := w.Int()
		return err == nil && i == j
	}
	if i, err := v.Uint(); err == nil {
		j, err := w.Uint()
		return err == nil && i == j
	}
	if x, ok := exactNumber(v); ok {
		y, ok := exactNumber(w)
		return ok && x == y
	}
	// #inf, -#inf and #nan.
	x, errx := v.Float()
	y, erry := w.Float()
	return errx == nil && erry == nil &&
		(math.Float64bits(x) == math.Float64bits(y) || math.IsNaN(x) && math.IsNaN(y))
}

// exactNumber returns the finite number v in a form that is the same for
// numbers of the same value, of any size: its sign, its digits in decimal,
// without leading or trailing zeros, and its exponent, as in -12e3.
func exactNumber(v kdltext.Value) (string, bool) {
	s := strings.ReplaceAll(v.String(), "_", "")
	sign := ""
	if s != "" && (s[0] == '+' || s[0] == '-') {
		sign, s = strings.TrimPrefix(s[:1], "+"), s[1:]
	}
	if strings.HasPrefix(s, "#") {
		return "", false
	}
	digits, exp := new(big.Int), new(big.Int)
	if len(s) > 1 && s[0] == '0' && strings.ContainsRune("box", rune(s[1])) {
		if _, ok := digits.SetString(s, 0); !ok {
			return "", false
		}
	} else {
		mant, e, hasExp := strings.Cut(strings.ToLower(s), "e")
		if hasExp {
			if _, ok := exp.SetString(strings.TrimPrefix(e, "+"), 10); !ok {
				return "", false
			}
		}
		whole, frac, _ := strings.Cut(mant, ".")
		if _, ok := digits.SetString(whole+frac, 10); !ok {
			return "", false
		}
		exp.Sub(exp, big.NewInt(int64(len(frac))))
	}
	d := digits.String()
	if d == "0" {
		return "0", true
	}
	trimmed := strings.TrimRight(d, "0")
	exp.Add(exp, big.NewInt(int64(len(d)-len(trimmed))))
	return sign + trimmed + "e" + exp.String(), true
}

func equalTypes(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
