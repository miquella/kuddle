package kdltext

import (
	"fmt"
	"io"
	"math/rand/v2"
	"strings"
	"testing"
	"unsafe"
)

func sameString(a, b string) bool {
	return len(a) == len(b) && unsafe.StringData(a) == unsafe.StringData(b)
}

func TestStringCacheLengths(t *testing.T) {
	var c stringCache
	for n := 0; n <= maxCachedLen+2; n++ {
		b := []byte(strings.Repeat("x", n))
		first, second := c.makeString(b), c.makeString(b)
		if first != string(b) || second != string(b) {
			t.Fatalf("length %d: got %q and %q", n, first, second)
		}
		cached := n >= minCachedLen && n <= maxCachedLen
		// The runtime shares the strings of one byte itself.
		if n > 1 && sameString(first, second) != cached {
			t.Errorf("length %d: shared %v, want %v", n, sameString(first, second), cached)
		}
	}
	var nilCache *stringCache
	if got := nilCache.makeString([]byte("abc")); got != "abc" {
		t.Errorf("nil cache: got %q", got)
	}
}

// The cache keeps one string for each slot, so strings that collide replace
// each other, and must never be mistaken for each other.
func TestStringCacheCollisions(t *testing.T) {
	var c stringCache
	rng := rand.New(rand.NewPCG(1, 2))
	words := make([]string, 2000)
	for i := range words {
		b := make([]byte, 2+rng.IntN(40))
		for j := range b {
			b[j] = "ab"[rng.IntN(2)] // a small alphabet makes many near-equal strings
		}
		words[i] = string(b)
	}
	for range 5 {
		for _, w := range words {
			if got := c.makeString([]byte(w)); got != w {
				t.Fatalf("got %q, want %q", got, w)
			}
		}
	}
	// A string that is repeated straight away is shared.
	b := []byte("repeat")
	if a, b := c.makeString(b), c.makeString(b); !sameString(a, b) {
		t.Error("an immediate repeat is not shared")
	}
}

// Every length of string must reach a slot in range, from the bytes it has.
func TestCacheSlotShortInputs(t *testing.T) {
	for n := minCachedLen; n <= 9; n++ {
		_ = cacheSlot([]byte(strings.Repeat("z", n)))
	}
}

// A Decoder shares the names a document repeats, with its own cache.
func TestDecoderSharesStrings(t *testing.T) {
	var doc strings.Builder
	for i := range 100 {
		fmt.Fprintf(&doc, "service name%d { port 80; env %q; label \"quoted name\" }\n", i, "VALUE")
	}
	d := NewDecoder(strings.NewReader(doc.String()))
	seen := map[string]string{}
	var shared, total int
	for {
		tok, err := d.ReadToken()
		if err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if tok.Kind != KindNode && tok.Kind != KindProp {
			continue
		}
		name := tok.Name()
		if prev, ok := seen[name]; ok {
			total++
			if sameString(prev, name) {
				shared++
			}
		}
		seen[name] = name
	}
	// A repeat is compared with the name before it. A name read before the
	// cache is made isn't shared with the first one after, and collisions
	// may evict others, but nearly all repeats are shared.
	if total == 0 || shared*10 < total*8 {
		t.Errorf("shared %d of %d repeated names", shared, total)
	}

	// Another Decoder has a cache of its own: nothing is shared between them.
	d2 := NewDecoder(strings.NewReader(doc.String()))
	if d2.strs != nil {
		t.Error("a new Decoder starts with a cache")
	}
}
