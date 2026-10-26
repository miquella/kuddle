// Package lexer splits KDL v1 and v2 documents into tokens.
package lexer

import (
	"bytes"
	"errors"
	"io"
)

const (
	defaultBufSize = 4096

	maxConsecutiveEmptyReads = 100
)

// ErrTooLarge is returned by More when the unconsumed data would exceed the
// maximum size.
var ErrTooLarge = errors.New("token exceeds the maximum size")

// Buffer reads from an io.Reader into a contiguous buffer, growing as needed.
//
// More reads data into the buffer, which is inspected using Pending, and then
// marked as processed with Consume. More reuses space previously marked as
// consumed, overwriting data in slices previously returned by Pending. Always
// query the current data using Pending after calling More.
type Buffer struct {
	r   io.Reader
	err error // sticky error from r

	buf      []byte
	consumed int

	// maxSize bounds the number of unconsumed bytes; <= 0 means no limit.
	maxSize int
}

// NewBuffer returns a Buffer reading from r. maxSize limits the number of
// unconsumed bytes held at once; use 0 to disable the limit.
//
// If r is a *bytes.Buffer, its contents are used directly without copying (and
// maxSize does not apply). r must not be written to afterward.
func NewBuffer(r io.Reader, maxSize int) *Buffer {
	b := &Buffer{r: r, maxSize: maxSize}
	if bb, ok := r.(*bytes.Buffer); ok {
		n := bb.Len()
		b.buf = bb.Next(n)[:n:n]
		b.err = io.EOF
	}
	return b
}

// Pending returns the unconsumed data. It is valid until the next call to More.
func (b *Buffer) Pending() []byte {
	return b.buf[b.consumed:]
}

// Consume marks the first n pending bytes as consumed.
func (b *Buffer) Consume(n int) {
	if n < 0 || n > len(b.buf)-b.consumed {
		panic("lexer: consume out of range")
	}
	b.consumed += n
}

// More reads at least one more byte into the buffer, invalidating any slice
// returned by Pending. It returns ErrTooLarge if the buffer is full at its
// maximum size, and the reader's error (including io.EOF) once the reader is
// exhausted.
//
// A full buffer is too large even when the reader has no more data: More
// cannot learn that without room to read into. Pending data of exactly maxSize
// bytes is therefore never followed by io.EOF.
//
// Pending data is copied to the front of the buffer on each call, so callers
// should Consume as much as possible first.
func (b *Buffer) More() error {
	if b.err != nil {
		return b.err
	}

	if err := b.makeRoom(); err != nil {
		return err
	}

	for range maxConsecutiveEmptyReads {
		n, err := b.r.Read(b.buf[len(b.buf):cap(b.buf)])
		b.buf = b.buf[:len(b.buf)+n]
		if err != nil {
			b.err = err
			if n > 0 {
				// Return the data now; the error is returned by the next call.
				return nil
			}
			return err
		}
		if n > 0 {
			return nil
		}
	}
	b.err = io.ErrNoProgress
	return b.err
}

// makeRoom ensures there is free space at the end of buf.
func (b *Buffer) makeRoom() error {
	if b.consumed > 0 {
		n := copy(b.buf, b.buf[b.consumed:])
		b.buf = b.buf[:n]
		b.consumed = 0
		return nil
	}

	if len(b.buf) < cap(b.buf) {
		return nil
	}

	return b.grow()
}

// grow moves the pending data to a larger buffer.
func (b *Buffer) grow() error {
	pending := len(b.buf) - b.consumed
	newCap := max(2*cap(b.buf), defaultBufSize)
	if b.maxSize > 0 {
		newCap = min(newCap, b.maxSize)
	}
	if newCap <= pending {
		return ErrTooLarge
	}

	nb := make([]byte, pending, newCap)
	copy(nb, b.buf[b.consumed:])
	b.buf = nb
	b.consumed = 0
	return nil
}
