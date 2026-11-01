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

func TestOnly(t *testing.T) {
	s := Join(
		&Struct{Present: Version, Version: 1},
		Bool(DiscardUnknown, true),
		Bool(DiscardUnknown, true),
		&Struct{Present: Newline, Newline: "\r\n"},
	)
	want := Struct{Present: DiscardUnknown | Version, Flags: DiscardUnknown, Version: 1}
	if got := *s.Only(DiscardUnknown | Version | Indent); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
