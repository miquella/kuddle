// Package kdlopts implements the options shared by the kuddle and kdltext
// packages, in the manner of encoding/json/v2: a single Options type that
// either package accepts, with each package ignoring options that do not
// apply to it.
package kdlopts

// Options is the type of the options accepted by kuddle and kdltext. It can
// only be implemented by this package.
type Options interface {
	KDLOptions(NotForPublicUse)
}

// NotForPublicUse prevents other packages from implementing Options.
type NotForPublicUse struct{}

// MaxNestingDepth is the deepest nesting of children blocks allowed in a
// document.
const MaxNestingDepth = 10000

// Bits is a set of options.
type Bits uint32

const (
	Version Bits = 1 << iota
	MaxTokenSize
)

// Struct holds a set of options. Each option function returns a Struct with
// a single option present.
type Struct struct {
	Present Bits // options that are set

	Version      int
	MaxTokenSize int
}

func (*Struct) KDLOptions(NotForPublicUse) {}

// Join sets the options present in opts, in order, so that later options take
// precedence.
func (s *Struct) Join(opts ...Options) {
	for _, o := range opts {
		src, ok := o.(*Struct)
		if !ok || src == nil {
			continue
		}
		s.Present |= src.Present
		if src.Present&Version != 0 {
			s.Version = src.Version
		}
		if src.Present&MaxTokenSize != 0 {
			s.MaxTokenSize = src.MaxTokenSize
		}
	}
}

// Join returns the options in opts joined into a single Struct.
func Join(opts ...Options) *Struct {
	s := new(Struct)
	s.Join(opts...)
	return s
}

// Clone returns a copy of s.
func (s *Struct) Clone() *Struct {
	c := *s
	return &c
}
