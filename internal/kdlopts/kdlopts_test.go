package kdlopts

import "testing"

func TestJoin(t *testing.T) {
	s := Join(
		&Struct{Present: Version, Version: 1},
		Bool(DiscardUnknown, true),
		Join(&Struct{Present: Version, Version: 2}, &Struct{Present: Indent, Indent: "\t"}),
		Bool(DiscardUnknown, false),
		&Struct{Present: MaxTokenSize, MaxTokenSize: 8},
		nil,
	)
	want := Struct{Present: Version | Indent | DiscardUnknown | MaxTokenSize, Version: 2, Indent: "\t", MaxTokenSize: 8}
	if *s != want {
		t.Errorf("got %+v, want %+v", *s, want)
	}
}
