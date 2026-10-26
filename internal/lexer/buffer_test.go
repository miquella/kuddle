package lexer_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/miquella/kuddle/internal/lexer"
)

// letters returns n pseudo-random lowercase letters drawn from rng. Unlike a
// repeated byte, the result detects data that was copied to the wrong place.
func letters(rng *rand.ChaCha8, n int) []byte {
	p := make([]byte, n)
	rng.Read(p)
	for i := range p {
		p[i] = 'a' + p[i]%26
	}
	return p
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

// dataThenErr returns a reader whose first Read returns data together with
// err, and whose later Reads return only err.
func dataThenErr(data string, err error) io.Reader {
	done := false
	return readerFunc(func(p []byte) (int, error) {
		if done {
			return 0, err
		}
		done = true
		return copy(p, data), err
	})
}

// drain calls More until it returns an error, checking that every successful
// call added at least one pending byte.
func drain(t *testing.T, b *lexer.Buffer) error {
	t.Helper()
	for {
		before := len(b.Pending())
		if err := b.More(); err != nil {
			return err
		}
		if after := len(b.Pending()); after <= before {
			t.Fatalf("More returned nil, but pending went from %d to %d bytes", before, after)
		}
	}
}

func TestPendingBeforeMore(t *testing.T) {
	b := lexer.NewBuffer(strings.NewReader("abc"), 0)
	if p := b.Pending(); len(p) != 0 {
		t.Fatalf("pending = %q before More, want empty", p)
	}
}

func TestMoreOneByteAtATime(t *testing.T) {
	b := lexer.NewBuffer(iotest.OneByteReader(strings.NewReader("abc")), 0)
	for _, want := range []string{"a", "ab", "abc"} {
		if err := b.More(); err != nil {
			t.Fatal(err)
		}
		if got := string(b.Pending()); got != want {
			t.Fatalf("pending = %q, want %q", got, want)
		}
	}
	for range 2 {
		if err := b.More(); err != io.EOF {
			t.Fatalf("err = %v, want io.EOF", err)
		}
		if got := string(b.Pending()); got != "abc" {
			t.Fatalf("pending = %q after EOF, want %q", got, "abc")
		}
	}
	b.Consume(1)
	if got := string(b.Pending()); got != "bc" {
		t.Fatalf("pending = %q after Consume(1), want %q", got, "bc")
	}
	if err := b.More(); err != io.EOF {
		t.Fatalf("err = %v after Consume at EOF, want io.EOF", err)
	}
}

func TestConsume(t *testing.T) {
	b := lexer.NewBuffer(bytes.NewBuffer([]byte("abcdef")), 0)
	for _, step := range []struct {
		n    int
		want string
	}{{0, "abcdef"}, {2, "cdef"}, {0, "cdef"}, {4, ""}, {0, ""}} {
		b.Consume(step.n)
		if got := string(b.Pending()); got != step.want {
			t.Fatalf("pending = %q after Consume(%d), want %q", got, step.n, step.want)
		}
	}
}

func TestConsumeOutOfRange(t *testing.T) {
	for _, n := range []int{-1, math.MinInt, 4, math.MaxInt} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			b := lexer.NewBuffer(bytes.NewBuffer([]byte("xabc")), 0)
			b.Consume(1) // so that consumed+n can overflow
			defer func() {
				if recover() == nil {
					t.Fatalf("Consume(%d) with 3 bytes pending did not panic", n)
				}
			}()
			b.Consume(n)
		})
	}
}

func TestErrorAfterData(t *testing.T) {
	errBoom := errors.New("boom")
	for _, want := range []error{io.EOF, errBoom} {
		t.Run(want.Error(), func(t *testing.T) {
			b := lexer.NewBuffer(dataThenErr("hi", want), 0)
			if err := b.More(); err != nil {
				t.Fatalf("err = %v, want nil with data", err)
			}
			if got := string(b.Pending()); got != "hi" {
				t.Fatalf("pending = %q, want %q", got, "hi")
			}
			for range 2 {
				if err := b.More(); err != want {
					t.Fatalf("err = %v, want %v", err, want)
				}
			}
			b.Consume(2)
			if err := b.More(); err != want {
				t.Fatalf("err = %v after Consume, want %v", err, want)
			}
		})
	}
}

func TestStickyError(t *testing.T) {
	errBoom := errors.New("boom")
	b := lexer.NewBuffer(io.MultiReader(strings.NewReader("hi"), iotest.ErrReader(errBoom)), 0)
	if err := b.More(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := b.More(); err != errBoom {
			t.Fatalf("err = %v, want boom", err)
		}
		if got := string(b.Pending()); got != "hi" {
			t.Fatalf("pending = %q after error, want %q", got, "hi")
		}
	}
}

func TestNoProgress(t *testing.T) {
	stuck := readerFunc(func([]byte) (int, error) { return 0, nil })
	b := lexer.NewBuffer(stuck, 0)
	for range 2 {
		if err := b.More(); err != io.ErrNoProgress {
			t.Fatalf("err = %v, want io.ErrNoProgress", err)
		}
	}
}

func TestToleratesEmptyReads(t *testing.T) {
	empty, inner := 3, strings.NewReader("x")
	r := readerFunc(func(p []byte) (int, error) {
		if empty > 0 {
			empty--
			return 0, nil
		}
		return inner.Read(p)
	})
	b := lexer.NewBuffer(r, 0)
	if err := b.More(); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if got := string(b.Pending()); got != "x" {
		t.Fatalf("pending = %q, want %q", got, "x")
	}
	if err := b.More(); err != io.EOF {
		t.Fatalf("err = %v, want io.EOF", err)
	}
}

func TestNoLimit(t *testing.T) {
	input := letters(rand.NewChaCha8([32]byte{}), 20_000)
	for _, maxSize := range []int{0, -1} {
		t.Run(strconv.Itoa(maxSize), func(t *testing.T) {
			b := lexer.NewBuffer(iotest.OneByteReader(bytes.NewReader(input)), maxSize)
			if err := drain(t, b); err != io.EOF {
				t.Fatalf("err = %v, want io.EOF", err)
			}
			if !bytes.Equal(b.Pending(), input) {
				t.Fatalf("pending (%d bytes) differs from input (%d bytes)", len(b.Pending()), len(input))
			}
		})
	}
}

// TestMaxSize checks that exactly maxSize bytes can be pending, no matter how
// the reader chunks its data. The sizes straddle likely internal growth steps.
func TestMaxSize(t *testing.T) {
	readers := []struct {
		name string
		wrap func(io.Reader) io.Reader
	}{
		{"greedy", func(r io.Reader) io.Reader { return r }},
		{"half", iotest.HalfReader},
		{"onebyte", iotest.OneByteReader},
	}
	sizes := []int{1, 2, 3, 100, 4095, 4096, 4097, 5000, 8191, 8192, 8193}
	for _, r := range readers {
		for _, size := range sizes {
			t.Run(fmt.Sprintf("%s/%d", r.name, size), func(t *testing.T) {
				input := letters(rand.NewChaCha8([32]byte{}), size+1)
				b := lexer.NewBuffer(r.wrap(bytes.NewReader(input)), size)
				if err := drain(t, b); !errors.Is(err, lexer.ErrTooLarge) {
					t.Fatalf("err = %v, want ErrTooLarge", err)
				}
				if !bytes.Equal(b.Pending(), input[:size]) {
					t.Fatalf("pending = %d bytes, want the first %d bytes of input", len(b.Pending()), size)
				}
				// Nothing was consumed, so the next call fails the same way.
				if err := b.More(); !errors.Is(err, lexer.ErrTooLarge) {
					t.Fatalf("second err = %v, want ErrTooLarge", err)
				}
				if !bytes.Equal(b.Pending(), input[:size]) {
					t.Fatal("pending changed by a failing More")
				}
			})
		}
	}
}

// TestMaxSizeExactFit documents that input of exactly maxSize bytes is too
// large: More must read a byte to learn whether the input has ended, and there
// is no room for it.
func TestMaxSizeExactFit(t *testing.T) {
	const size = 100
	input := letters(rand.NewChaCha8([32]byte{}), size)
	b := lexer.NewBuffer(bytes.NewReader(input), size)
	if err := drain(t, b); !errors.Is(err, lexer.ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if !bytes.Equal(b.Pending(), input) {
		t.Fatal("pending differs from input")
	}
}

// TestTooLargeRecovers checks that ErrTooLarge is not sticky: consuming a
// single byte makes room for the next read.
func TestTooLargeRecovers(t *testing.T) {
	const size = 8
	input := letters(rand.NewChaCha8([32]byte{}), 1000)
	b := lexer.NewBuffer(iotest.HalfReader(bytes.NewReader(input)), size)

	var got []byte
	for {
		err := drain(t, b)
		if err == io.EOF {
			break
		}
		if !errors.Is(err, lexer.ErrTooLarge) {
			t.Fatal(err)
		}
		if n := len(b.Pending()); n != size {
			t.Fatalf("pending = %d bytes at ErrTooLarge, want %d", n, size)
		}
		got = append(got, b.Pending()[0])
		b.Consume(1)
	}
	got = append(got, b.Pending()...)
	if !bytes.Equal(got, input) {
		t.Fatal("reassembled data differs from input")
	}
}

// TestReusesConsumedSpace streams data much larger than maxSize through the
// buffer. Without reuse of consumed space the limit would be hit.
func TestReusesConsumedSpace(t *testing.T) {
	const size = 16
	input := letters(rand.NewChaCha8([32]byte{}), 100_000)
	consumers := []struct {
		name    string
		consume func(pending int) int
	}{
		{"all", func(n int) int { return n }},
		{"half", func(n int) int { return max(n/2, 1) }},
	}
	for _, c := range consumers {
		t.Run(c.name, func(t *testing.T) {
			b := lexer.NewBuffer(iotest.HalfReader(bytes.NewReader(input)), size)
			var got []byte
			for {
				err := b.More()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("after %d bytes: %v", len(got), err)
				}
				n := c.consume(len(b.Pending()))
				got = append(got, b.Pending()[:n]...)
				b.Consume(n)
			}
			got = append(got, b.Pending()...)
			if !bytes.Equal(got, input) {
				t.Fatal("reassembled data differs from input")
			}
		})
	}
}

// TestLexerLoop drives the buffer the way the lexer does: it looks for a
// complete token in the pending data, consumes it, and calls More only when
// the token is incomplete. The reader's final error arrives alongside data.
//
// Newline-terminated runs of letters stand in for KDL tokens. They are long
// enough to span growth steps, so pending data is carried across both
// compaction and growth.
func TestLexerLoop(t *testing.T) {
	rng := rand.NewChaCha8([32]byte{})
	var tokens []string
	var input []byte
	longest := 0
	for i := range 500 {
		n := i * 131 % 5000
		tok := letters(rng, n)
		tokens = append(tokens, string(tok))
		input = append(append(input, tok...), '\n')
		longest = max(longest, n)
	}

	lex := func(maxSize int) ([]string, error) {
		r := iotest.DataErrReader(iotest.HalfReader(bytes.NewReader(input)))
		b := lexer.NewBuffer(r, maxSize)
		var got []string
		for {
			p := b.Pending()
			if i := bytes.IndexByte(p, '\n'); i >= 0 {
				got = append(got, string(p[:i]))
				b.Consume(i + 1)
				continue
			}
			if err := b.More(); err != nil {
				return got, err
			}
		}
	}

	for _, maxSize := range []int{0, longest + 1} {
		t.Run(strconv.Itoa(maxSize), func(t *testing.T) {
			got, err := lex(maxSize)
			if err != io.EOF {
				t.Fatalf("err = %v, want io.EOF", err)
			}
			if !slices.Equal(got, tokens) {
				t.Fatalf("got %d tokens, want %d (or contents differ)", len(got), len(tokens))
			}
		})
	}
	// The longest token needs longest+1 bytes pending to find its newline.
	if _, err := lex(longest); !errors.Is(err, lexer.ErrTooLarge) {
		t.Fatalf("err = %v with maxSize %d, want ErrTooLarge", err, longest)
	}
}

func TestBytesBuffer(t *testing.T) {
	input := []byte("hello, world")
	// maxSize is smaller than the input, but does not apply to a bytes.Buffer.
	b := lexer.NewBuffer(bytes.NewBuffer(input), 1)

	if !bytes.Equal(b.Pending(), input) {
		t.Fatalf("pending = %q before More, want %q", b.Pending(), input)
	}
	for range 2 {
		if err := b.More(); err != io.EOF {
			t.Fatalf("err = %v, want io.EOF", err)
		}
	}
	b.Consume(7)
	if got := string(b.Pending()); got != "world" {
		t.Fatalf("pending = %q, want %q", got, "world")
	}
	// The contents are used without copying.
	input[7] = 'W'
	if got := string(b.Pending()); got != "World" {
		t.Fatalf("pending = %q after writing to input, want %q", got, "World")
	}
}

func TestEmptyBytesBuffer(t *testing.T) {
	b := lexer.NewBuffer(new(bytes.Buffer), 0)
	if p := b.Pending(); len(p) != 0 {
		t.Fatalf("pending = %q, want empty", p)
	}
	if err := b.More(); err != io.EOF {
		t.Fatalf("err = %v, want io.EOF", err)
	}
}

func BenchmarkScan(b *testing.B) {
	for _, tokLen := range []int{8, 100, 10000} {
		input := strings.Repeat(strings.Repeat("x", tokLen-1)+"\n", 1<<20/tokLen+1)
		b.Run(strconv.Itoa(tokLen), func(b *testing.B) {
			b.SetBytes(int64(len(input)))
			for b.Loop() {
				buf := lexer.NewBuffer(strings.NewReader(input), 0)
				for {
					p := buf.Pending()
					if i := bytes.IndexByte(p, '\n'); i >= 0 {
						buf.Consume(i + 1)
						continue
					}
					if err := buf.More(); err != nil {
						break
					}
				}
			}
		})
	}
}
