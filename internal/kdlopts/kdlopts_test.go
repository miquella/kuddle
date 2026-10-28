package kdlopts

import "testing"

func TestJoin(t *testing.T) {
	s := Join(
		&Struct{Present: Version, Version: 1},
		Bool(DiscardUnknown, true),
		Join(&Struct{Present: Version, Version: 2}),
		Bool(DiscardUnknown, false),
		&Struct{Present: MaxTokenSize, MaxTokenSize: 8},
		nil,
	)
	want := Struct{Present: Version | DiscardUnknown | MaxTokenSize, Version: 2, MaxTokenSize: 8}
	if *s != want {
		t.Errorf("got %+v, want %+v", *s, want)
	}
}
