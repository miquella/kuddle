package kuddle_test

import (
	"testing"

	"github.com/miquella/kuddle"
	"github.com/miquella/kuddle/kdltext"
)

func format(t *testing.T, in string, opts ...kuddle.FormatOption) string {
	t.Helper()
	var doc kuddle.Document
	if err := kuddle.Unmarshal([]byte(in), &doc); err != nil {
		t.Fatal(err)
	}
	doc.Format(opts...)
	out, err := kuddle.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestFormatOptions(t *testing.T) {
	const in = "n b=1 \"a\" a=0x0A_bc #\"r\"# {}\n"
	for _, tt := range []struct {
		name string
		opts []kuddle.FormatOption
		want string
	}{
		{"sort", []kuddle.FormatOption{kuddle.FormatSortProperties(true)}, "n \"a\" #\"r\"# a=0x0A_bc b=1 {}\n"},
		{"canonical", []kuddle.FormatOption{kuddle.FormatNumbers(kuddle.NumberCanonical)}, "n b=1 \"a\" a=0xabc #\"r\"# {}\n"},
		{"decimal", []kuddle.FormatOption{kuddle.FormatNumbers(kuddle.NumberDecimal)}, "n b=1 \"a\" a=2748 #\"r\"# {}\n"},
		{"omit", []kuddle.FormatOption{kuddle.FormatEmptyBlocks(kuddle.EmptyBlockOmitted)}, "n b=1 \"a\" a=0x0A_bc #\"r\"#\n"},
		{"quote", []kuddle.FormatOption{kuddle.FormatQuoteValues(true)}, "n b=1 \"a\" a=0x0A_bc \"r\" {}\n"},
		{"normalize", []kuddle.FormatOption{kuddle.FormatNormalizeStrings(true)}, "n b=1 a a=0x0A_bc r {}\n"},
	} {
		if got := format(t, in, tt.opts...); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestFormatStringNewlines(t *testing.T) {
	const in = "n \"a\nb\"\n"
	v1 := kdltext.WithVersion(kdltext.V1)
	for _, tt := range []struct {
		name string
		opts []kuddle.FormatOption
		want string
	}{
		{"not by default", []kuddle.FormatOption{kuddle.FormatNewline("\r\n")}, in},
		{"on request", []kuddle.FormatOption{kuddle.FormatNewline("\r\n"), kuddle.FormatStringNewlines(true)}, "n \"a\r\nb\"\n"},
	} {
		var doc kuddle.Document
		if err := kuddle.Unmarshal([]byte(in), &doc, v1); err != nil {
			t.Fatal(err)
		}
		doc.Format(tt.opts...)
		if got, err := kuddle.Marshal(doc, v1); err != nil || string(got) != tt.want {
			t.Errorf("%s: got %q, %v; want %q", tt.name, got, err, tt.want)
		}
	}
}

// TestEncoderNewlineLeavesStrings checks that only the Encoder's own line
// endings change: a v1 string holding a newline would otherwise change value.
func TestEncoderNewlineLeavesStrings(t *testing.T) {
	v1 := kdltext.WithVersion(kdltext.V1)
	var doc kuddle.Document
	if err := kuddle.Unmarshal([]byte("n \"a\nb\"\n"), &doc, v1); err != nil {
		t.Fatal(err)
	}
	out, err := kuddle.Marshal(doc, v1, kdltext.WithNewline("\r\n"))
	if want := "n \"a\nb\"\r\n"; err != nil || string(out) != want {
		t.Errorf("got %q, %v; want %q", out, err, want)
	}
}

func TestEncoderNewline(t *testing.T) {
	out, err := kuddle.Marshal(map[string]int{"a": 1}, kdltext.WithNewline("\r\n"))
	if want := "a 1\r\n"; err != nil || string(out) != want {
		t.Errorf("got %q, %v; want %q", out, err, want)
	}
}
