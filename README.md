# kuddle

[![Go Reference](https://pkg.go.dev/badge/github.com/miquella/kuddle.svg)](https://pkg.go.dev/github.com/miquella/kuddle)

kuddle reads and writes [KDL](https://kdl.dev) documents in Go. Its API takes
some inspiration from `encoding/json/v2`, so it may look familiar.

## Features

- **Simple API**: `Marshal` and `Unmarshal` convert between KDL and Go values,
  and `MarshalEncode` and `UnmarshalDecode` do the same for a stream.
- **KDL v1 and v2**: reads either version, detecting which one it's given, and
  writes v2 by default. Both official conformance suites pass.
- **Struct tags**: map struct fields to a node's arguments, properties and
  children, as well as its name, type annotation and comments.
- **Lossless documents**: `Document` preserves node order, repeated properties
  and comments, and converts directly to and from Go values.
- **Marshaler methods**: a type can implement `MarshalKDL` and `UnmarshalKDL`
  to control its own encoding, and fall back to the default behavior when it
  wants to.
- **Type marshaler functions**: supply marshaling functions for any type,
  including ones you don't own, with `WithMarshalers` and `WithUnmarshalers`.
- **Type annotations**: optional built-in support for the spec's
  `(duration)`, `(date-time)` and `(date)` annotations.
- **Streaming**: the `kdltext` package reads and writes KDL as a stream of
  tokens.

## Installation

```sh
go get github.com/miquella/kuddle
```

## Usage

Take this configuration file:

```kdl
log-level info
server web port=8080 {
    routes "/" "/static"
}
server api port=8443 tls=#true
```

Define Go types that mirror it, then unmarshal it:

```go
type Server struct {
	Name   string   `kdl:",arg"`
	Port   int      `kdl:"port,prop"`
	TLS    bool     `kdl:"tls,prop,omitzero"`
	Routes []string `kdl:"routes"`
}

type Config struct {
	LogLevel string   `kdl:"log-level"`
	Servers  []Server `kdl:"server"`
}

var cfg Config
if err := kuddle.Unmarshal(data, &cfg); err != nil {
	return err
}
fmt.Println(cfg.Servers[0].Routes) // [/ /static]

out, err := kuddle.Marshal(cfg) // the same document
```

A few things in the tags are worth pointing out:

- `kdl:",arg"` has no name because arguments go by position, not by name. The
  first argument of a `server` node, `web` or `api`, is its `Name`.
- `kdl:"port,prop"` is the property `port=8080`. A field with no kind, like
  `kdl:"routes"`, is a child node.
- `Routes` is a slice of strings, so the one `routes` node holds all of them as
  arguments. It doesn't take a child node per route. `Servers`, a slice of
  structs, is the opposite: one `server` node per element.
- `omitzero` on `TLS` only matters when marshaling. Without it, `web` would
  come back out as `tls=#false` instead of leaving the property off, as it was
  in the input.

### Documents

A `Document` holds a KDL document as it was written, comments and all.
`Document.To` and `Document.From` convert it to and from Go values using the
same struct tags and rules as `Unmarshal` and `Marshal`, without a round trip
through KDL text. That makes it easy to, for example, layer local overrides on
top of a base configuration:

```go
var doc, local kuddle.Document
if err := kuddle.Unmarshal(baseData, &doc); err != nil {
	return err
}
if err := kuddle.Unmarshal(localData, &local); err != nil {
	return err
}
doc.Nodes = append(doc.Nodes, local.Nodes...)

// A later log-level replaces an earlier one; servers accumulate.
var cfg Config
err := doc.To(&cfg)
```

### Struct tags

A struct field's tag looks like `kdl:"name,kind,modifiers"`, and all of it is
optional:

- The **kind** says what the field holds, such as an argument, a property or a
  child node. A field with no kind is a child node.
- The **name** says which property or child node the field holds, and defaults
  to the field's Go name. Only `child` and `prop` fields have a name, so giving
  one to any other kind is an error. To give a kind without a name, start with
  the comma: `kdl:",arg"`.
- **Modifiers** change how the field is handled, and can be added to any kind.

For example, `kdl:"port,prop,omitzero"` is the property `port`, and is left out
when `Marshal` finds it zero.

| Kind            | Field holds                                                    |
| --------------- | -------------------------------------------------------------- |
| `child`         | the child node with the name (the default)                     |
| `arg`           | the next argument                                              |
| `args`          | the remaining arguments, as a slice                            |
| `prop`          | the property with the name                                     |
| `props`         | the remaining properties, as a map                             |
| `entries`       | the remaining arguments, properties and comments, as `[]Entry` |
| `children`      | the remaining child nodes, as a slice or map                   |
| `name`          | the node's name                                                |
| `type`          | the node's type annotation                                     |
| `comment`       | the comments above the node (see Comments)                     |
| `headercomment` | the comments before the node's first child (see Comments)      |
| `linecomment`   | the line comment at the end of the node's line                 |

| Modifier   | Effect                                                              |
| ---------- | ------------------------------------------------------------------- |
| `omitzero` | `Marshal` leaves the field out when it's zero (`Unmarshal` ignores it) |

`omitzero` goes best with `child` and `prop` fields, to keep optional values
out of the output. Be careful with it on `arg`: an argument that's left out
moves the ones after it up a place, so it's only safe on the last one.

An `entries` field keeps a node's arguments, properties and comments in their
original order, as a `Node`'s entries do, and takes the place of `args` and
`props`. Fields can also hold a `Node` or `Document`, but don't embed a
`Document` in a struct: its methods would be promoted, and the struct would
decode as a `Document`.

By default, `Unmarshal` returns an error when the document has an argument,
property or child node with no matching field. Pass
`kuddle.DiscardUnknown(true)` to skip those instead, so a struct only needs
fields for the parts it cares about. A value the Go type can't hold at all, such
as an argument decoded into a map, is always an error.

The [package documentation](https://pkg.go.dev/github.com/miquella/kuddle#hdr-Struct_tags)
has the full rules for every kind, and for how each Go type is mapped.

### Comments

A struct can keep a document's comments in `comment`, `headercomment` and
`linecomment` fields. Comments are kept in blocks. A block is a run of `//`
lines on consecutive lines, or one `/* */` comment. A blank line, or a change
from one kind of comment to the other, starts a new block.

Here is a document with a comment in each of the three places:

```kdl
// Settings for the web servers.

// The main server.
server a.example.com { // on the line
    // These are its options.

    // It needs TLS.
    tls #true
}
```

And the structs that keep them:

```go
type Config struct {
	Header string `kdl:",headercomment"` // "Settings for the web servers."
	Server Server `kdl:"server"`
}

type Server struct {
	Host    string `kdl:",arg"`
	Comment string `kdl:",comment"`       // "The main server."
	Line    string `kdl:",linecomment"`   // "on the line"
	Options string `kdl:",headercomment"` // "These are its options."
	TLS     TLS    `kdl:"tls"`
}

type TLS struct {
	On      bool   `kdl:",arg"`
	Comment string `kdl:",comment"` // "It needs TLS."
}
```

- `comment` holds every block above the node.
- `headercomment` holds the blocks at the head of the struct's own contents:
  for the document, the ones before its first node, and for a node, the ones
  inside its braces before its first child. A block that sits right on top of
  the first child, with no blank line between them, belongs to that child.
- `linecomment` holds the comment on the node's own line.

A block goes to only one field. If a struct has a `headercomment` field, its
first child's `comment` field doesn't get the blocks at the head. That way, the
head of a document stays put when its first node moves.

A `comment` or `headercomment` field is a `string` (the blocks, with a blank
line between them), a `[]string` (one element for each block), or a
`[]kdltext.Comment` (each block as it was written, with its kind). A single
`kdltext.Comment` is an error, since there can be more than one block.
`Marshal` writes the blocks back. Comments after the last child of a node have
no node below them, so they aren't kept. The
[package documentation](https://pkg.go.dev/github.com/miquella/kuddle#hdr-Comments)
has the details.

## Options

Every function accepts options from both `kuddle` and `kdltext`. Later options
override earlier ones, and options that don't apply are ignored, so the same
set can be passed when decoding and encoding.

```go
out, err := kuddle.Marshal(cfg, kdltext.WithVersion(kdltext.V1), kdltext.WithIndent("\t"))
```

Commonly used options:

| Option                              | Effect                                                   |
| ----------------------------------- | -------------------------------------------------------- |
| `kuddle.DiscardUnknown`             | ignore args, props and children with no struct field     |
| `kdltext.WithVersion`               | read or write only v1 or v2 (detected / v2 by default)   |
| `kdltext.WithIndent`                | indentation of children (four spaces by default)         |
| `kdltext.WithNewline`               | line endings (`\n` by default)                           |
| `kdltext.DiscardComments`           | leave out comments when reading or writing               |
| `kdltext.RejectDuplicateProperties` | error for a repeated property                            |
| `kdltext.WithMaxTokenSize`          | limit the size of a single token, for untrusted input    |

See the [`kdltext` documentation](https://pkg.go.dev/github.com/miquella/kuddle/kdltext#hdr-Options)
for the full list.

To change a document's style, call `Document.Format` between reading and
writing it. By default it only tidies comments, keeping numbers and strings as
they were written.

| Format option                  | Effect                                                     |
| ------------------------------ | ---------------------------------------------------------- |
| `kuddle.FormatNumbers`         | write numbers in canonical form or decimal                 |
| `kuddle.FormatEmptyBlocks`     | keep or remove empty children blocks                       |
| `kuddle.FormatSortProperties`  | move properties after arguments, sorted by name            |
| `kuddle.FormatQuoteValues`     | write string values quoted                                 |
| `kuddle.FormatNormalizeStrings`| write strings bare where possible, quoted otherwise        |
| `kuddle.FormatLiteralComments` | leave comments exactly as they are                         |
| `kuddle.FormatIndent`          | indentation used for comments of several lines             |
| `kuddle.FormatNewline`         | line ending inside block comments and slash-dashed parts   |
| `kuddle.FormatStringNewlines`  | also rewrite the newlines in strings; can change v1 values |

```go
doc.Format(kuddle.FormatSortProperties(true), kuddle.FormatNumbers(kuddle.NumberCanonical))
out, err := kuddle.Marshal(doc)
```

`FormatIndent` and `FormatNewline` should match `kdltext.WithIndent` and
`kdltext.WithNewline`. Setting `kdltext.WithNewline` also applies to line and
block comments when writing; the text of strings and slash-dashed parts is only
changed by `Format`.

## Type marshaler functions

When you can't add methods to a type, or want different behavior for one call,
pass marshaling functions as options. Build them with `MarshalFunc` and
`UnmarshalFunc`, and pass them with `WithMarshalers` and `WithUnmarshalers`.
Functions run before a type's own `MarshalKDL` and `UnmarshalKDL` methods. A
function can return `errors.ErrUnsupported` to hand the value on to the next
function, the type's methods, or the default rules.

Functions don't change the shape of the document. A function for a
single-value type like `time.Duration` reads and writes just that value,
wherever it appears.

For example, a `time.Duration` is written as an integer number of nanoseconds
by default. To use strings like `"1m30s"` instead:

```go
durations := kuddle.JoinOptions(
	kuddle.WithMarshalers(kuddle.MarshalFunc(func(enc *kdltext.Encoder, d time.Duration) error {
		return enc.WriteToken(kdltext.Token{Kind: kdltext.KindValue, Value: kdltext.String(d.String())})
	})),
	kuddle.WithUnmarshalers(kuddle.UnmarshalFunc(func(dec *kdltext.Decoder, d *time.Duration) error {
		t, err := dec.ReadToken()
		if err != nil {
			return err
		}
		*d, err = time.ParseDuration(t.Value.String())
		return err
	})),
)

type Check struct {
	Interval time.Duration   `kdl:"interval,prop"`
	Timeout  time.Duration   `kdl:"timeout"`
	Backoff  []time.Duration `kdl:"backoff"`
}
```

With these options, a `Check` looks like this:

```kdl
check interval="30s" {
    timeout "5s"
    backoff "1s" "10s" "1m0s"
}
```

Use `JoinMarshalers` and `JoinUnmarshalers` to combine several functions into
one option.

### Type annotations

The KDL spec reserves some type annotations for common formats.
`AnnotationUnmarshalers` provides built-in unmarshalers for the ones you name:
`(date-time)` and `(date)` decode into `time.Time`, and `(duration)` (an ISO
8601 duration, such as `"PT1H30M"`) into `time.Duration`. Values without a
matching annotation are unaffected.

```go
annotations := kuddle.AnnotationUnmarshalers("date", "duration")
err := kuddle.Unmarshal(data, &cfg, kuddle.WithUnmarshalers(annotations))
```

`AllAnnotationUnmarshalers` returns every built-in unmarshaler. That set may
grow in later versions, so name the ones you need if you want behavior that
won't change.

## Packages

- [`kuddle`](https://pkg.go.dev/github.com/miquella/kuddle) maps between KDL
  and Go values, and provides the `Document` type.
- [`kuddle/kdltext`](https://pkg.go.dev/github.com/miquella/kuddle/kdltext)
  handles KDL syntax: a streaming token `Decoder` and `Encoder`, plus
  formatting options.

## Conformance

kuddle passes the official
[KDL test suites](https://github.com/kdl-org/kdl/tree/main/tests) for both v1
and v2. Each case is checked by the lexer, the `kdltext` decoder, and
`Unmarshal`/`Marshal`. For the last of these, the output must match the
expected output byte for byte, and each input must round-trip without loss,
comments included.

The suites are vendored in `internal/kdltest`.

```sh
go test -run Conformance ./...           # run the conformance tests
go generate ./internal/kdltest/...       # update the vendored suites
```

## License

kuddle is available under a BSD-style license; see [LICENSE](LICENSE).

The KDL specification in `spec/` and the conformance tests in
`internal/kdltest/` come from the [KDL project](https://github.com/kdl-org/kdl)
and keep their own license.
