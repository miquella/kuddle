package kdltext

import (
	"encoding/binary"
	"math/bits"
)

// A stringCache shares the strings a Decoder makes from its input, so that
// the names and values a document repeats are one string each, not one for
// every time they are written. It is a small direct-mapped cache, as in
// encoding/json/v2: a string is kept in the slot its hash picks, and replaces
// whatever was there. That makes a lookup a hash of a few bytes and a
// comparison, with no allocation, locking or growth, and bounds its size, at
// the cost of forgetting strings that collide.
type stringCache [256]string

const (
	// minCachedLen is the shortest string that is cached: a string of one
	// byte doesn't allocate.
	minCachedLen = 2

	// maxCachedLen is the longest string that is cached: long enough for
	// the names and values that are repeated, such as UUIDs and paths.
	maxCachedLen = 256

	// cacheAfter is the number of strings a Decoder reads before it makes a
	// cache, so that reading a short document doesn't pay for one.
	cacheAfter = 16
)

// makeString returns the string form of b. It returns an earlier string of
// the same text from c if it has one, and otherwise makes a string and keeps
// it in c. A nil c, or a b of an uncached length, always makes a string.
func (c *stringCache) makeString(b []byte) string {
	if c == nil || len(b) < minCachedLen || len(b) > maxCachedLen {
		return string(b)
	}
	slot := &c[cacheSlot(b)]
	if *slot == string(b) {
		return *slot
	}
	s := string(b)
	*slot = s
	return s
}

// cacheSlot returns the slot of the cache for b, which has at least
// minCachedLen bytes, from a hash of its first and last few bytes. Hashing
// only those makes the cost the same for a string of any length.
func cacheSlot(b []byte) uint8 {
	const (
		prime1 = 0x9e3779b97f4a7c15
		prime2 = 0xc2b2ae3d27d4eb4f
		prime3 = 0x165667b19e3779f9
	)
	var lo, hi uint64
	switch n := len(b); {
	case n >= 8:
		lo = binary.LittleEndian.Uint64(b)
		hi = binary.LittleEndian.Uint64(b[n-8:])
	case n >= 4:
		lo = uint64(binary.LittleEndian.Uint32(b))
		hi = uint64(binary.LittleEndian.Uint32(b[n-4:]))
	default:
		lo = uint64(binary.LittleEndian.Uint16(b))
		hi = uint64(b[n-1])
	}
	h := (lo*prime1 ^ bits.RotateLeft64(hi*prime2, 29) ^ uint64(len(b))) * prime3
	return uint8(h >> 56) // the high bits are the best mixed
}

// shareString returns b as a string, shared with an earlier one of the same
// text if it is short enough. The Decoder makes its cache once it has read
// enough strings to use it.
func (d *Decoder) shareString(b []byte) string {
	if d.strs == nil {
		if d.strsRead++; d.strsRead < cacheAfter {
			return string(b)
		}
		d.strs = new(stringCache)
	}
	return d.strs.makeString(b)
}
