package lexer

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// eof is returned by peek at the end of the input.
const eof rune = -1

// Version is a KDL language version.
type Version int

const (
	// Undecided lets the lexer detect the version (see Lexer).
	Undecided Version = 0
	V1        Version = 1
	V2        Version = 2
)

// ErrVersionMismatch is wrapped by the error returned when a document's
// version marker conflicts with the lexer's version.
var ErrVersionMismatch = errors.New("version marker does not match lexer version")

// Position is a location in the input.
type Position struct {
	Offset int64 // byte offset, starting at 0
	Line   int   // line number, starting at 1
	Column int   // column number in code points, starting at 1
}

// advance returns the position after the text b, which starts at p. A CRLF
// pair is a single newline, so a '\n' that follows a '\r' is not counted.
func (p Position) advance(b []byte, v Version) Position {
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		crlf := r == '\n' && i > 0 && b[i-1] == '\r'
		switch {
		case crlf:
		case IsNewline(r) && !(r == '\v' && v == V1):
			p.Line++
			p.Column = 1
		default:
			p.Column++
		}
		i += size
	}
	p.Offset += int64(len(b))
	return p
}

// SyntaxError reports invalid input.
type SyntaxError struct {
	Position
	Msg string
	Err error // underlying error, if any
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("lexer: syntax error at line %d, column %d: %s", e.Line, e.Column, e.Msg)
}

func (e *SyntaxError) Unwrap() error { return e.Err }

// Lexer splits KDL input into tokens.
//
// Unlike the spec, comments (including slash-dash comments) are emitted as
// tokens rather than treated as whitespace.
//
// A version marker (/- kdl-version N) at the start of the document sets the
// version of an undecided lexer, and is an ErrVersionMismatch for a lexer
// whose version differs. The marker is otherwise emitted as normal tokens.
//
// Without a marker, an undecided lexer commits to a version the first time it
// meets syntax specific to that version. Syntax that is valid in only one
// version decides it; where a construct is valid (but different) in both,
// v2 is preferred: a token starting with '#', and a string starting with """.
type Lexer struct {
	buf *Buffer
	pos Position // position of the start of the pending data
	tok Position // position of the last token returned by NextToken

	version Version
	started bool
	queue   []queued // tokens read ahead by checkVersionMarker
}

type queued struct {
	tok Token
	pos Position
}

// New returns a lexer for KDL version v, or Undecided to detect it.
func New(r io.Reader, v Version) *Lexer {
	return &Lexer{buf: NewBuffer(r, 0), pos: Position{Line: 1, Column: 1}, version: v}
}

// Version returns the lexer's version, which is Undecided until the input
// shows which version it is.
func (l *Lexer) Version() Version { return l.version }

// Use reports whether syntax specific to version v is allowed, committing an
// undecided lexer to v. It must only be called once that syntax is present in
// the input.
func (l *Lexer) Use(v Version) bool {
	if l.version == Undecided {
		l.version = v
	}
	return l.version == v
}

// Pos returns the position of the start of the last token returned by
// NextToken.
func (l *Lexer) Pos() Position { return l.tok }

// NextToken returns the next token. The token's raw bytes are backed by the
// lexer's buffer and are only valid until the next call to NextToken.
func (l *Lexer) NextToken() (Token, error) {
	if !l.started {
		l.started = true
		toks, err := l.checkVersionMarker()
		if err != nil {
			return Token{}, err
		}
		l.queue = toks
	}
	if len(l.queue) > 0 {
		q := l.queue[0]
		l.queue = l.queue[1:]
		l.tok = q.pos
		return q.tok, nil
	}
	l.tok = l.pos
	return l.scan()
}

func (l *Lexer) scan() (Token, error) {
	kind, n, err := l.scanToken()
	if err != nil {
		return Token{}, err
	}
	raw := l.buf.Pending()[:n:n]
	l.consume(n)
	return FromRaw(kind, raw), nil
}

// checkVersionMarker reads tokens from the start of the document for as long
// as they match a version marker, setting the version if they form one:
//
//	bom? '/-' unicode-space* 'kdl-version' unicode-space+ ('1' | '2') unicode-space* newline
//
// It returns the tokens it read, to be emitted before any others.
func (l *Lexer) checkVersionMarker() ([]queued, error) {
	var toks []queued
	var v Version
	var start Position
	for step := 0; step < 4; {
		// Reading the next token invalidates the raw bytes of the last one.
		if n := len(toks); n > 0 {
			toks[n-1].tok = toks[n-1].tok.Clone()
		}
		pos := l.pos
		t, err := l.scan()
		if err != nil {
			// Nothing was consumed, so the error is reported again when the
			// token is read normally.
			return toks, nil
		}
		toks = append(toks, queued{t, pos})

		raw := string(t.Raw())
		matched := false
		switch {
		case t.Kind == Whitespace:
			// Only a BOM may precede the slash-dash.
			matched = step > 0 || (len(toks) == 1 && raw == string(BOM))
		case step == 0:
			matched = t.Kind == Slashdash
			start = pos
		case step == 1:
			matched = t.Kind == Ident && raw == "kdl-version"
		case step == 2:
			switch {
			case t.Kind == Number && raw == "1":
				v, matched = V1, true
			case t.Kind == Number && raw == "2":
				v, matched = V2, true
			}
		case step == 3:
			matched = t.Kind == Newline
		}
		if !matched {
			return toks, nil
		}
		if t.Kind != Whitespace {
			step++
		}
	}

	if l.version != Undecided && l.version != v {
		return nil, &SyntaxError{
			Position: start,
			Msg:      fmt.Sprintf("document is marked as KDL v%d, but lexing KDL v%d", v, l.version),
			Err:      ErrVersionMismatch,
		}
	}
	l.version = v
	return toks, nil
}

func (l *Lexer) consume(n int) {
	l.pos = l.pos.advance(l.buf.Pending()[:n], l.version)
	l.buf.Consume(n)
}

func (l *Lexer) errorf(off int, format string, args ...any) error {
	return &SyntaxError{
		Position: l.pos.advance(l.buf.Pending()[:off], l.version),
		Msg:      fmt.Sprintf(format, args...),
	}
}

// peek returns the rune at offset i of the pending data and its size, reading
// more input as needed. It returns eof at the end of the input.
//
// Nothing is consumed while a token is being scanned, so offsets into the
// pending data remain valid across reads.
func (l *Lexer) peek(i int) (rune, int, error) {
	for {
		p := l.buf.Pending()
		if i < len(p) && (p[i] < utf8.RuneSelf || utf8.FullRune(p[i:])) {
			r, size := rune(p[i]), 1
			if r >= utf8.RuneSelf {
				r, size = utf8.DecodeRune(p[i:])
				if r == utf8.RuneError && size == 1 {
					return 0, 0, l.errorf(i, "invalid UTF-8")
				}
			}
			// A BOM is allowed at the start of the document, and anywhere in v1.
			atStart := l.pos.Offset+int64(i) == 0
			if IsDisallowed(r) && !(r == BOM && atStart) && !l.Use(V1) {
				return 0, 0, l.errorf(i, "disallowed character %U", r)
			}
			return r, size, nil
		}
		if err := l.buf.More(); err == io.EOF {
			if i < len(l.buf.Pending()) {
				return 0, 0, l.errorf(i, "invalid UTF-8")
			}
			return eof, 0, nil
		} else if err != nil {
			return 0, 0, err
		}
	}
}

// matchAt reports whether the pending data at offset i starts with the ASCII
// string s.
func (l *Lexer) matchAt(i int, s string) (bool, error) {
	for j := range len(s) {
		c, _, err := l.peek(i + j)
		if err != nil || c != rune(s[j]) {
			return false, err
		}
	}
	return true, nil
}

// isWhitespace reports whether r is non-newline whitespace. A BOM is
// whitespace wherever peek allows it.
func isWhitespace(r rune) bool { return IsSpace(r) || r == BOM }

// isNewline reports whether r is a newline. VT is a newline only in v2. CRLF
// is handled by the caller.
func (l *Lexer) isNewline(r rune) bool {
	if r == '\v' {
		return l.Use(V2)
	}
	return IsNewline(r)
}

// isIdentChar reports whether r may appear in a bare identifier.
func (l *Lexer) isIdentChar(r rune) bool {
	// v1 excludes code points up to 0x20 (and eof); in v2 they are all
	// whitespace, newlines or disallowed.
	if r <= 0x20 {
		return false
	}
	switch r {
	case '\\', '/', '(', ')', '{', '}', ';', '[', ']', '=', '"', BOM:
		return false
	case '<', '>', ',':
		return l.Use(V2)
	case '#':
		return l.Use(V1)
	}
	return !IsSpace(r) && !l.isNewline(r)
}

// scanToken determines the type and length of the token at the start of the
// pending data.
func (l *Lexer) scanToken() (Kind, int, error) {
	c, size, err := l.peek(0)
	if err != nil {
		return Invalid, 0, err
	}
	switch c {
	case eof:
		return EOF, 0, nil
	case '(':
		return LParen, 1, nil
	case ')':
		return RParen, 1, nil
	case '{':
		return LBrace, 1, nil
	case '}':
		return RBrace, 1, nil
	case '=':
		return Equals, 1, nil
	case ';':
		return Semicolon, 1, nil
	case '\\':
		return Escline, 1, nil
	case '"':
		return l.scanString()
	case '/':
		return l.scanSlash()
	case '\r':
		if crlf, err := l.matchAt(0, "\r\n"); err != nil {
			return Invalid, 0, err
		} else if crlf {
			return Newline, 2, nil
		}
		return Newline, 1, nil
	case '+', '-':
		c, _, err := l.peek(1)
		if err != nil {
			return Invalid, 0, err
		}
		if isDigit(c) {
			return l.scanNumber()
		}
	case 'r':
		if raw, err := l.isRawStringStart(1); err != nil {
			return Invalid, 0, err
		} else if raw && l.Use(V1) {
			return l.scanRawString(V1)
		}
	case '#':
		if l.Use(V2) {
			return l.scanHash()
		}
	}

	switch {
	case isWhitespace(c):
		return l.scanWhitespace()
	case l.isNewline(c):
		return Newline, size, nil
	case isDigit(c):
		return l.scanNumber()
	case l.isIdentChar(c):
		return l.scanIdent()
	}
	return Invalid, 0, l.errorf(0, "unexpected character %q", c)
}

func (l *Lexer) scanWhitespace() (Kind, int, error) {
	i := 0
	for {
		c, size, err := l.peek(i)
		if err != nil {
			return Invalid, 0, err
		}
		if !isWhitespace(c) {
			return Whitespace, i, nil
		}
		i += size
	}
}

// scanIdentChars returns the offset of the first non-identifier character at
// or after start.
func (l *Lexer) scanIdentChars(start int) (int, error) {
	i := start
	for {
		c, size, err := l.peek(i)
		if err != nil {
			return 0, err
		}
		if !l.isIdentChar(c) {
			return i, nil
		}
		i += size
	}
}

func (l *Lexer) scanIdent() (Kind, int, error) {
	n, err := l.scanIdentChars(0)
	if err != nil {
		return Invalid, 0, err
	}
	ident := l.buf.Pending()[:n]

	// true, false and null are keywords in v1. They, inf, -inf and nan are
	// invalid as bare identifiers in v2, where keywords start with '#'.
	kind := Ident
	switch string(ident) {
	case "true":
		kind = True
	case "false":
		kind = False
	case "null":
		kind = Null
	}
	switch string(ident) {
	case "true", "false", "null", "inf", "-inf", "nan":
		if !l.Use(V1) {
			return Invalid, 0, l.errorf(0, "bare keyword %s (use #%s)", Quote(string(ident)), ident)
		}
	}

	// v2 disallows identifiers like .1 that look like numbers.
	rest := ident
	if rest[0] == '+' || rest[0] == '-' {
		rest = rest[1:]
	}
	if len(rest) >= 2 && rest[0] == '.' && isDigit(rune(rest[1])) && !l.Use(V1) {
		return Invalid, 0, l.errorf(0, "invalid identifier %s (looks like a number)", Quote(string(ident)))
	}
	return kind, n, nil
}

// scanHash scans a v2 token starting with '#': a raw string or a keyword.
func (l *Lexer) scanHash() (Kind, int, error) {
	if raw, err := l.isRawStringStart(0); err != nil {
		return Invalid, 0, err
	} else if raw {
		return l.scanRawString(V2)
	}

	n, err := l.scanIdentChars(1)
	if err != nil {
		return Invalid, 0, err
	}
	switch kw := string(l.buf.Pending()[:n]); kw {
	case "#true":
		return True, n, nil
	case "#false":
		return False, n, nil
	case "#null":
		return Null, n, nil
	case "#inf", "#-inf", "#nan":
		return Number, n, nil
	default:
		return Invalid, 0, l.errorf(0, "invalid keyword %s", Quote(kw))
	}
}

// scanNumber scans a number. The whole run of identifier characters is taken
// so that input like "1abc" is reported as an invalid number.
func (l *Lexer) scanNumber() (Kind, int, error) {
	n, err := l.scanIdentChars(0)
	if err != nil {
		return Invalid, 0, err
	}
	if b := l.buf.Pending()[:n]; !validNumber(b) {
		return Invalid, 0, l.errorf(0, "invalid number %s", Quote(string(b)))
	}
	return Number, n, nil
}

// scanString scans a quoted string, or a v2 multi-line string.
func (l *Lexer) scanString() (Kind, int, error) {
	delim := `"`
	if multi, err := l.matchAt(0, `"""`); err != nil {
		return Invalid, 0, err
	} else if multi && l.Use(V2) {
		delim = `"""`
		if err := l.expectNewline(3); err != nil {
			return Invalid, 0, err
		}
	}

	i := len(delim)
	for {
		c, size, err := l.peek(i)
		if err != nil {
			return Invalid, 0, err
		}
		if c == '"' {
			if end, err := l.matchAt(i, delim); err != nil {
				return Invalid, 0, err
			} else if end {
				return l.finishString(String, i+len(delim), delim == `"""`)
			}
		}
		switch {
		case c == eof:
			return Invalid, 0, l.errorf(0, "unterminated string")
		case c == '\\':
			if size, err = l.scanEscape(i); err != nil {
				return Invalid, 0, err
			}
		case delim == `"` && l.isNewline(c) && !l.Use(V1):
			return Invalid, 0, l.errorf(i, "newline in single-line string")
		}
		i += size
	}
}

// scanEscape validates the escape sequence at offset i and returns its length.
func (l *Lexer) scanEscape(i int) (int, error) {
	c, _, err := l.peek(i + 1)
	if err != nil {
		return 0, err
	}
	switch c {
	case '"', '\\', 'b', 'f', 'n', 'r', 't':
		return 2, nil
	case '/':
		if l.Use(V1) {
			return 2, nil
		}
	case 's':
		if l.Use(V2) {
			return 2, nil
		}
	case 'u':
		return l.scanUnicodeEscape(i)
	default:
		if (IsSpace(c) || l.isNewline(c)) && l.Use(V2) {
			return l.scanWhitespaceEscape(i)
		}
	}
	return 0, l.errorf(i, "invalid escape")
}

// scanUnicodeEscape validates the \u{...} escape at offset i and returns its
// length.
func (l *Lexer) scanUnicodeEscape(i int) (int, error) {
	if ok, err := l.matchAt(i, `\u{`); err != nil {
		return 0, err
	} else if !ok {
		return 0, l.errorf(i, "invalid unicode escape")
	}
	var r rune
	j := i + 3
	for ; ; j++ {
		c, _, err := l.peek(j)
		if err != nil {
			return 0, err
		}
		if c == '}' {
			break
		}
		d, ok := hexValue(c)
		if !ok || j-(i+3) >= 6 {
			return 0, l.errorf(i, "invalid unicode escape")
		}
		r = r<<4 | d
	}
	if j == i+3 || !utf8.ValidRune(r) {
		return 0, l.errorf(i, "invalid unicode escape")
	}
	return j + 1 - i, nil
}

// scanWhitespaceEscape returns the length of the v2 whitespace escape at
// offset i: a backslash followed by whitespace and newlines.
func (l *Lexer) scanWhitespaceEscape(i int) (int, error) {
	j := i + 1
	for {
		c, size, err := l.peek(j)
		if err != nil {
			return 0, err
		}
		if !IsSpace(c) && !l.isNewline(c) {
			return j - i, nil
		}
		j += size
	}
}

// isRawStringStart reports whether a raw string's hashes and opening quote
// start at offset i.
func (l *Lexer) isRawStringStart(i int) (bool, error) {
	for {
		c, _, err := l.peek(i)
		if err != nil || c != '#' {
			return c == '"', err
		}
		i++
	}
}

// scanRawString scans a v1 raw string (r#"..."#) or a v2 raw string, which may
// be multi-line (#"..."# or #"""..."""#).
func (l *Lexer) scanRawString(v Version) (Kind, int, error) {
	i := 0
	if v == V1 {
		i = 1 // skip 'r'
	}
	// isRawStringStart has already read the hashes and opening quote.
	hashes := bytes.IndexByte(l.buf.Pending()[i:], '"')
	i += hashes

	delim := `"`
	if v == V2 {
		if multi, err := l.matchAt(i, `"""`); err != nil {
			return Invalid, 0, err
		} else if multi {
			delim = `"""`
			if err := l.expectNewline(i + 3); err != nil {
				return Invalid, 0, err
			}
		}
	}
	i += len(delim)

	closing := delim + strings.Repeat("#", hashes)
	for {
		c, size, err := l.peek(i)
		if err != nil {
			return Invalid, 0, err
		}
		if c == '"' {
			if end, err := l.matchAt(i, closing); err != nil {
				return Invalid, 0, err
			} else if end {
				return l.finishString(RawString, i+len(closing), delim == `"""`)
			}
		}
		switch {
		case c == eof:
			return Invalid, 0, l.errorf(0, "unterminated raw string")
		case v == V2 && delim == `"` && l.isNewline(c):
			return Invalid, 0, l.errorf(i, "newline in single-line raw string")
		}
		i += size
	}
}

// expectNewline reports an error if there is no newline at offset i, which
// must follow the opening quotes of a multi-line string.
func (l *Lexer) expectNewline(i int) error {
	c, _, err := l.peek(i)
	if err != nil {
		return err
	}
	if !l.isNewline(c) {
		return l.errorf(i, "multi-line string must start with a newline")
	}
	return nil
}

// finishString returns a string token of length n, validating the indentation
// of a multi-line string.
func (l *Lexer) finishString(kind Kind, n int, multiline bool) (Kind, int, error) {
	if multiline {
		if _, err := DecodeString(string(l.buf.Pending()[:n])); err != nil {
			return Invalid, 0, l.errorf(0, "%v", err)
		}
	}
	return kind, n, nil
}

func (l *Lexer) scanSlash() (Kind, int, error) {
	c, _, err := l.peek(1)
	if err != nil {
		return Invalid, 0, err
	}
	switch c {
	case '-':
		return Slashdash, 2, nil
	case '/':
		return l.scanLineComment()
	case '*':
		return l.scanBlockComment()
	}
	return Invalid, 0, l.errorf(0, "unexpected character '/'")
}

func (l *Lexer) scanLineComment() (Kind, int, error) {
	i := 2
	for {
		c, size, err := l.peek(i)
		if err != nil {
			return Invalid, 0, err
		}
		if c == eof || l.isNewline(c) {
			return LineComment, i, nil
		}
		i += size
	}
}

// scanBlockComment scans a (possibly nested) block comment.
func (l *Lexer) scanBlockComment() (Kind, int, error) {
	i, depth := 2, 1
	for {
		c, size, err := l.peek(i)
		if err != nil {
			return Invalid, 0, err
		}
		if c == eof {
			return Invalid, 0, l.errorf(0, "unterminated block comment")
		}
		i += size
		if c != '*' && c != '/' {
			continue
		}

		next, _, err := l.peek(i)
		if err != nil {
			return Invalid, 0, err
		}
		switch {
		case c == '*' && next == '/':
			i++
			if depth--; depth == 0 {
				return BlockComment, i, nil
			}
		case c == '/' && next == '*':
			i++
			depth++
		}
	}
}

func isDigit(r rune) bool { return '0' <= r && r <= '9' }

func hexValue(r rune) (rune, bool) {
	switch {
	case '0' <= r && r <= '9':
		return r - '0', true
	case 'a' <= r && r <= 'f':
		return r - 'a' + 10, true
	case 'A' <= r && r <= 'F':
		return r - 'A' + 10, true
	}
	return 0, false
}

// validNumber reports whether b is a valid hex, octal, binary or decimal number.
func validNumber(b []byte) bool {
	if len(b) > 0 && (b[0] == '+' || b[0] == '-') {
		b = b[1:]
	}
	for _, p := range []struct {
		prefix string
		digit  func(byte) bool
	}{
		{"0x", func(c byte) bool { _, ok := hexValue(rune(c)); return ok }},
		{"0o", func(c byte) bool { return '0' <= c && c <= '7' }},
		{"0b", func(c byte) bool { return c == '0' || c == '1' }},
	} {
		if rest, ok := bytes.CutPrefix(b, []byte(p.prefix)); ok {
			rest, ok = digits(rest, p.digit)
			return ok && len(rest) == 0
		}
	}

	isDec := func(c byte) bool { return isDigit(rune(c)) }
	b, ok := digits(b, isDec)
	if !ok {
		return false
	}
	if rest, found := bytes.CutPrefix(b, []byte(".")); found {
		if b, ok = digits(rest, isDec); !ok {
			return false
		}
	}
	if len(b) > 0 && (b[0] == 'e' || b[0] == 'E') {
		b = b[1:]
		if len(b) > 0 && (b[0] == '+' || b[0] == '-') {
			b = b[1:]
		}
		if b, ok = digits(b, isDec); !ok {
			return false
		}
	}
	return len(b) == 0
}

// digits consumes a digit followed by any number of digits and underscores.
func digits(b []byte, digit func(byte) bool) ([]byte, bool) {
	if len(b) == 0 || !digit(b[0]) {
		return b, false
	}
	i := 1
	for i < len(b) && (digit(b[i]) || b[i] == '_') {
		i++
	}
	return b[i:], true
}
