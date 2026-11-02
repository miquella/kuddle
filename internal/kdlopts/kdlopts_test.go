package kdlopts

import "testing"

func TestJoin(t *testing.T) {
	s := Join(
		&Struct{Present: Version, Version: 1},
		Bool(DiscardUnknown, true),
		Join(&Struct{Present: Version, Version: 2}, &Struct{Present: Indent, Indent: "\t"}),
		Bool(DiscardUnknown, false),
		&Struct{Present: Newline, Newline: "\r\n"},
		&Struct{Present: MaxTokenSize, MaxTokenSize: 8},
		nil,
	)
	want := Struct{Present: Version | Indent | DiscardUnknown | Newline | MaxTokenSize, Version: 2, Indent: "\t", Newline: "\r\n", MaxTokenSize: 8}
	if *s != want {
		t.Errorf("got %+v, want %+v", *s, want)
	}
}

func TestJoinMarshalers(t *testing.T) {
	m1, m2, u := new(int), new(int), new(int)
	s := Join(
		&Struct{Present: Marshalers, Marshalers: m1},
		&Struct{Present: Unmarshalers, Unmarshalers: u},
		&Struct{Present: Marshalers, Marshalers: m2},
		Bool(DiscardUnknown, true),
	)
	if s.Marshalers != m2 || s.Unmarshalers != u {
		t.Errorf("got %+v, want Marshalers %p and Unmarshalers %p", *s, m2, u)
	}
	if o := s.Only(Semantic); o.Marshalers != m2 || o.Unmarshalers != u {
		t.Errorf("Only(Semantic): got %+v", *o)
	}
}

func TestOnly(t *testing.T) {
	s := Join(
		&Struct{Present: Version, Version: 1},
		Bool(DiscardUnknown, true),
		Bool(QuoteIdentifiers, true),
		&Struct{Present: Newline, Newline: "\r\n"},
	)
	want := Struct{Present: DiscardUnknown | Version, Flags: DiscardUnknown, Version: 1}
	if got := *s.Only(DiscardUnknown | Version | Indent); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
