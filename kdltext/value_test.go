package kdltext_test

import (
	"math"
	"testing"

	"github.com/miquella/kuddle/kdltext"
)

func TestValue(t *testing.T) {
	big, err := kdltext.Number("0xABCDEF0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		v    kdltext.Value
		kind kdltext.ValueKind
		str  string
	}{
		{kdltext.String("hi"), kdltext.ValueString, "hi"},
		{kdltext.Int(-3), kdltext.ValueNumber, "-3"},
		{kdltext.Uint(math.MaxUint64), kdltext.ValueNumber, "18446744073709551615"},
		{kdltext.Float(1.5), kdltext.ValueNumber, "1.5"},
		{kdltext.Float(math.Inf(-1)), kdltext.ValueNumber, "#-inf"},
		{kdltext.Bool(true), kdltext.ValueBool, "#true"},
		{kdltext.Null(), kdltext.ValueNull, "#null"},
		{big, kdltext.ValueNumber, "0xABCDEF0123456789abcdef"},
		{kdltext.Value{}, kdltext.ValueInvalid, "<invalid>"},
	}
	for _, tt := range tests {
		if tt.v.Kind() != tt.kind || tt.v.String() != tt.str {
			t.Errorf("got %v %q, want %v %q", tt.v.Kind(), tt.v, tt.kind, tt.str)
		}
	}

	for _, s := range []string{"", "1.", "x", "1 2", "#true", `"1"`} {
		if _, err := kdltext.Number(s); err == nil {
			t.Errorf("Number(%q) succeeded, want error", s)
		}
	}
	if _, err := big.Int(); err == nil {
		t.Error("Int() of a huge number succeeded")
	}
	if _, err := kdltext.String("1").Int(); err == nil {
		t.Error("Int() of a string succeeded")
	}
}

func typed(v kdltext.Value, typ string) kdltext.Value {
	v.Type = new(typ)
	return v
}

func TestValueEqual(t *testing.T) {
	one, err := kdltext.Number("1")
	if err != nil {
		t.Fatal(err)
	}
	onePointZero, err := kdltext.Number("1.0")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		a, b kdltext.Value
		want bool
	}{
		{kdltext.Int(1), one, true},
		{kdltext.Int(1), onePointZero, false},
		{kdltext.Int(1), kdltext.String("1"), false},
		{kdltext.Bool(true), kdltext.Bool(false), false},
		{kdltext.Null(), kdltext.Null(), true},
		{typed(kdltext.Int(1), "u8"), typed(kdltext.Int(1), "u8"), true},
		{typed(kdltext.Int(1), "u8"), typed(kdltext.Int(1), "i8"), false},
		{typed(kdltext.Int(1), ""), kdltext.Int(1), false},
		{typed(kdltext.Int(1), ""), typed(kdltext.Int(1), ""), true},
	}
	for _, tt := range tests {
		if got := tt.a.Equal(tt.b); got != tt.want {
			t.Errorf("%v.Equal(%v) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
		if got := tt.b.Equal(tt.a); got != tt.want {
			t.Errorf("%v.Equal(%v) = %v, want %v", tt.b, tt.a, got, tt.want)
		}
	}
}

func TestClone(t *testing.T) {
	v := typed(kdltext.Int(1), "u8")
	c := v.Clone()
	if !c.Equal(v) || c.Type == v.Type {
		t.Errorf("Value.Clone() = %v with type %p, want an equal value with a new type than %p", c, c.Type, v.Type)
	}

	tok := kdltext.Token{Kind: kdltext.KindValue, Value: v.WithType(new("t"))}
	ct := tok.Clone()
	if !ct.Equal(tok) || ct.Value.Type == tok.Value.Type {
		t.Errorf("Token.Clone() shares a type annotation with the original")
	}
	*ct.Value.Type = "changed"
	if *tok.Value.Type != "t" {
		t.Errorf("changing the clone changed the original: %q", *tok.Value.Type)
	}
	if (kdltext.Token{}).Clone().Value.Type != nil {
		t.Error("Clone() of a token without a type annotation has one")
	}
}

func TestTokenEqual(t *testing.T) {
	node := kdltext.Token{Kind: kdltext.KindNode, Value: kdltext.String("n")}
	val := kdltext.Token{Kind: kdltext.KindValue, Value: kdltext.Int(1)}
	prop := kdltext.Token{Kind: kdltext.KindProp, Value: kdltext.String("k")}
	tests := []struct {
		a, b kdltext.Token
		want bool
	}{
		{node, node, true},
		{node, kdltext.Token{Kind: kdltext.KindNode, Value: kdltext.String("m")}, false},
		{kdltext.Token{Kind: kdltext.KindEnd}, kdltext.Token{Kind: kdltext.KindEnd, Children: true}, false},
		{node, kdltext.Token{Kind: kdltext.KindNode, Value: kdltext.String("n").WithType(new(""))}, false},
		{kdltext.Token{Kind: kdltext.KindNode, Value: kdltext.String("n").WithType(new("t"))}, kdltext.Token{Kind: kdltext.KindNode, Value: kdltext.String("n").WithType(new("t"))}, true},
		{val, val, true},
		{val, kdltext.Token{Kind: kdltext.KindValue, Value: kdltext.Int(2)}, false},
		{val, kdltext.Token{Kind: kdltext.KindProp, Value: kdltext.Int(1)}, false},
		{prop, prop, true},
		{prop, kdltext.Token{Kind: kdltext.KindProp, Value: kdltext.String("j")}, false},
		{kdltext.Token{Kind: kdltext.KindEnd}, kdltext.Token{Kind: kdltext.KindEnd}, true},
	}
	for _, tt := range tests {
		if got := tt.a.Equal(tt.b); got != tt.want {
			t.Errorf("%+v.Equal(%+v) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}
