package lexer

import (
	"bytes"
	"math"
	"testing"
)

func TestNumbers(t *testing.T) {
	tests := []struct {
		raw   string
		i     int64
		f     float64
		isInt bool
	}{
		{"0", 0, 0, true},
		{"-1_000", -1000, -1000, true},
		{"010", 10, 10, true},
		{"0xdead_BEEF", 0xdeadbeef, 0xdeadbeef, true},
		{"-0o17", -0o17, -0o17, true},
		{"+0b1_01", 5, 5, true},
		{"1.5e+3_0", 0, 1.5e30, false},
		{"2E-2", 0, 0.02, false},
	}
	for _, tt := range tests {
		tok := FromRaw(Number, []byte(tt.raw))
		i, err := tok.Int()
		if tt.isInt && (err != nil || i != tt.i) {
			t.Errorf("%s: Int() = %d, %v; want %d", tt.raw, i, err, tt.i)
		} else if !tt.isInt && err == nil {
			t.Errorf("%s: Int() succeeded, want error", tt.raw)
		}
		if f, err := tok.Float(); err != nil || f != tt.f {
			t.Errorf("%s: Float() = %g, %v; want %g", tt.raw, f, err, tt.f)
		}
	}

	for raw, want := range map[string]float64{"#inf": math.Inf(1), "#-inf": math.Inf(-1)} {
		if f, err := FromRaw(Number, []byte(raw)).Float(); err != nil || f != want {
			t.Errorf("%s: Float() = %g, %v", raw, f, err)
		}
	}
	if f, err := FromRaw(Number, []byte("#nan")).Float(); err != nil || !math.IsNaN(f) {
		t.Errorf("#nan: Float() = %g, %v", f, err)
	}

	if _, err := FromRaw(Number, []byte("0x8000_0000_0000_0000")).Int(); err == nil {
		t.Error("overflow: want error")
	}
	if _, err := FromString("1").Int(); err == nil {
		t.Error("String.Int(): want error")
	}
}

func TestConstructed(t *testing.T) {
	tests := []struct {
		tok  Token
		kind Kind
		text string
	}{
		{FromString("a\"b"), String, "a\"b"},
		{FromInt(-42), Number, "-42"},
		{FromFloat(1e100), Number, "1e+100"},
		{FromFloat(2), Number, "2.0"},
		{FromFloat(math.Copysign(0, -1)), Number, "-0.0"},
		{FromFloat(math.Inf(-1)), Number, "#-inf"},
		{FromBool(true), True, "true"},
		{FromBool(false), False, "false"},
		{Token{Kind: Null}, Null, "null"},
		{Token{Kind: LBrace}, LBrace, "{"},
		{Token{Kind: Whitespace}, Whitespace, " "},
	}
	for _, tt := range tests {
		if tt.tok.Raw() != nil {
			t.Errorf("%s: Raw() = %q, want nil", tt.kind, tt.tok.Raw())
		}
		if tt.tok.Kind != tt.kind || tt.tok.String() != tt.text {
			t.Errorf("got %s %q, want %s %q", tt.tok.Kind, tt.tok.String(), tt.kind, tt.text)
		}
	}
	if f, err := FromFloat(1e100).Float(); err != nil || f != 1e100 {
		t.Errorf("FromFloat(1e100).Float() = %g, %v", f, err)
	}
}

func TestDecodeString(t *testing.T) {
	tests := []struct {
		raw, want string
	}{
		{`""`, ""},
		{`"a\tb\u{48}\/\s"`, "a\tbH/ "},
		{"\"a\\ \n\t b\"", "ab"},
		{`r"\n"`, `\n`},
		{`r##"a"#b"##`, `a"#b`},
		{`#"\n"#`, `\n`},
		{"\"\"\"\n\"\"\"", ""},
		{"\"\"\"\n  a\r\n\n    b\\n\n  \"\"\"", "a\n\n  b\n"},
		{"#\"\"\"\n  a\\n\n  \"\"\"#", `a\n`},
	}
	for _, tt := range tests {
		got, err := DecodeString(tt.raw)
		if err != nil || got != tt.want {
			t.Errorf("DecodeString(%q) = %q, %v; want %q", tt.raw, got, err, tt.want)
		}
	}
}

func TestCloneDetach(t *testing.T) {
	raw := []byte(`"a\nb"`)
	tok := FromRaw(String, raw)

	clone := tok.Clone()
	detached := tok.Detach()
	copy(raw, `"xxxx"`)

	if !bytes.Equal(clone.Raw(), []byte(`"a\nb"`)) || clone.String() != "a\nb" {
		t.Errorf("clone = %q (%q)", clone.Raw(), clone.String())
	}
	if detached.Raw() != nil || detached.String() != "a\nb" {
		t.Errorf("detached = %q (%q)", detached.Raw(), detached.String())
	}
	if c := FromString("x").Clone(); c.Raw() != nil || c.String() != "x" {
		t.Errorf("constructed clone = %q (%q)", c.Raw(), c.String())
	}
}
