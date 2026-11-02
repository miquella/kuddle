package kuddle_test

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/miquella/kuddle"
	"github.com/miquella/kuddle/kdltext"
)

func ExampleUnmarshal() {
	type Server struct {
		Name   string   `kdl:",arg"`
		Port   int      `kdl:"port,prop"`
		TLS    bool     `kdl:"tls,prop"`
		Routes []string `kdl:"route"`
	}
	type Config struct {
		LogLevel string   `kdl:"log-level"`
		Servers  []Server `kdl:"server"`
	}

	input := []byte(`
log-level debug
server web port=8080 {
    route "/"
    route "/static"
}
server api port=8443 tls=#true
`)
	var c Config
	if err := kuddle.Unmarshal(input, &c); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%+v\n", c)
	// Output:
	// {LogLevel:debug Servers:[{Name:web Port:8080 TLS:false Routes:[/ /static]} {Name:api Port:8443 TLS:true Routes:[]}]}
}

func ExampleMarshal() {
	type Package struct {
		Name    string   `kdl:",arg"`
		Version string   `kdl:"version,prop"`
		Authors []string `kdl:"authors,omitzero"`
		License string   `kdl:"license,omitzero"`
	}

	type Manifest struct {
		Name string    `kdl:"name"`
		Deps []Package `kdl:"dependency"`
	}

	b, err := kuddle.Marshal(Manifest{
		Name: "app",
		Deps: []Package{
			{Name: "kuddle", Version: "1.2.0", Authors: []string{"Ann", "Bo"}},
			{Name: "lexer", Version: "0.3.1", License: "MIT"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	os.Stdout.Write(b)
	// Output:
	// name app
	// dependency kuddle version="1.2.0" {
	//     authors Ann Bo
	// }
	// dependency lexer version="0.3.1" {
	//     license MIT
	// }
}

// Duration is a time.Duration that is written in KDL as a string, such as
// "1m30s": as a property, or as the single argument of a node.
type Duration time.Duration

func (d Duration) MarshalKDL(enc *kdltext.Encoder) error {
	return enc.WriteToken(kdltext.Token{Kind: kdltext.KindValue, Value: kdltext.String(time.Duration(d).String())})
}

func (d *Duration) UnmarshalKDL(dec *kdltext.Decoder) error {
	t, err := dec.ReadToken()
	if err != nil {
		return err
	}
	if t.Kind != kdltext.KindValue || t.Value.Kind() != kdltext.ValueString {
		return fmt.Errorf("want a duration string, not %v", t.Value)
	}
	td, err := time.ParseDuration(t.Value.String())
	*d = Duration(td)
	return err
}

// The same method unmarshals a Duration from a property and from a node.
func ExampleUnmarshaler() {
	type Check struct {
		URL      string   `kdl:",arg"`
		Interval Duration `kdl:"interval,prop"`
		Timeout  Duration `kdl:"timeout"`
	}

	var checks struct {
		Checks []Check `kdl:"check"`
	}
	input := `check "https://example.com/health" interval="1m30s" { timeout "5s"; }`
	if err := kuddle.Unmarshal([]byte(input), &checks); err != nil {
		log.Fatal(err)
	}
	c := checks.Checks[0]
	fmt.Println(time.Duration(c.Interval), time.Duration(c.Timeout))
	// Output:
	// 1m30s 5s
}

// The same method marshals a Duration as a property and as a node.
func ExampleMarshaler() {
	type Check struct {
		URL      string   `kdl:",arg"`
		Interval Duration `kdl:"interval,prop"`
		Timeout  Duration `kdl:"timeout"`
	}

	checks := struct {
		Checks []Check `kdl:"check"`
	}{
		[]Check{{"https://example.com/health", Duration(90 * time.Second), Duration(10 * time.Second)}},
	}
	b, err := kuddle.Marshal(checks)
	if err != nil {
		log.Fatal(err)
	}
	os.Stdout.Write(b)
	// Output:
	// check "https://example.com/health" interval="1m30s" {
	//     timeout "10s"
	// }
}

// Functions marshal and unmarshal a time.Duration, which has no methods, as a
// string, such as "1m30s", wherever it is: as a property, or as the single
// argument of a node.
func ExampleWithMarshalers() {
	marshal := kuddle.MarshalFunc(func(enc *kdltext.Encoder, d time.Duration) error {
		return enc.WriteToken(kdltext.Token{Kind: kdltext.KindValue, Value: kdltext.String(d.String())})
	})
	unmarshal := kuddle.UnmarshalFunc(func(dec *kdltext.Decoder, d *time.Duration) error {
		t, err := dec.ReadToken()
		if err != nil {
			return err
		}
		if t.Kind != kdltext.KindValue || t.Value.Kind() != kdltext.ValueString {
			return fmt.Errorf("want a duration string, not %v", t.Value)
		}
		*d, err = time.ParseDuration(t.Value.String())
		return err
	})
	opts := kuddle.JoinOptions(kuddle.WithMarshalers(marshal), kuddle.WithUnmarshalers(unmarshal))

	type Check struct {
		URL      string        `kdl:",arg"`
		Interval time.Duration `kdl:"interval,prop"`
		Timeout  time.Duration `kdl:"timeout"`
	}
	var checks struct {
		Checks []Check `kdl:"check"`
	}
	input := `check "https://example.com/health" interval="1m30s" { timeout "5s"; }`
	if err := kuddle.Unmarshal([]byte(input), &checks, opts); err != nil {
		log.Fatal(err)
	}
	c := &checks.Checks[0]
	fmt.Println(c.Interval, c.Timeout)

	c.Timeout = 10 * time.Second
	b, err := kuddle.Marshal(checks, opts)
	if err != nil {
		log.Fatal(err)
	}
	os.Stdout.Write(b)
	// Output:
	// 1m30s 5s
	// check "https://example.com/health" interval="1m30s" {
	//     timeout "10s"
	// }
}

// Port is a TCP port, written in KDL as a number or as the name of a
// service, such as "https". Its method decodes names, and leaves numbers to
// the default rules.
type Port int

var services = map[string]Port{"http": 80, "https": 443}

func (p *Port) UnmarshalKDL(dec *kdltext.Decoder) error {
	t, err := dec.PeekToken()
	if err != nil {
		return err
	}
	if t.Kind != kdltext.KindValue || t.Value.Kind() != kdltext.ValueString {
		return errors.ErrUnsupported
	}
	dec.ReadToken()
	port, ok := services[t.Value.String()]
	if !ok {
		return fmt.Errorf("unknown service %q", t.Value.String())
	}
	*p = port
	return nil
}

// Listen is the address of a listener. Its method decodes it by the default
// rules, and then checks it.
type Listen struct {
	Host string `kdl:",arg"`
	Port Port   `kdl:"port,prop"`
}

func (l *Listen) UnmarshalKDL(dec *kdltext.Decoder) error {
	type plain Listen
	if err := kuddle.UnmarshalDecode(dec, (*plain)(l)); err != nil {
		return err
	}
	if l.Port == 0 {
		return errors.New("no port")
	}
	return nil
}

// The method of Port falls back to the default rules for numbers by
// returning errors.ErrUnsupported, and that of Listen delegates to them by
// calling UnmarshalDecode.
func ExampleUnmarshaler_fallback() {
	var config struct {
		Listen []Listen `kdl:"listen"`
	}
	input := `
listen localhost port=8080
listen "::" port=https
`
	if err := kuddle.Unmarshal([]byte(input), &config); err != nil {
		log.Fatal(err)
	}
	fmt.Println(config.Listen)

	err := kuddle.Unmarshal([]byte("listen localhost"), &config)
	fmt.Println(err)
	// Output:
	// [{localhost 8080} {:: 443}]
	// kuddle: cannot unmarshal node listen[0] (line 1, column 1) into Go kuddle_test.Listen: no port
}

// Env is a list of environment variables, written in KDL as the properties of
// a node, in order.
type Env struct {
	Vars []EnvVar
}

type EnvVar struct {
	Name, Value string
}

func (e Env) MarshalKDL(enc *kdltext.Encoder) error {
	for _, v := range e.Vars {
		if err := enc.WriteToken(kdltext.Token{Kind: kdltext.KindProp, Value: kdltext.String(v.Name)}); err != nil {
			return err
		}
		if err := enc.WriteToken(kdltext.Token{Kind: kdltext.KindValue, Value: kdltext.String(v.Value)}); err != nil {
			return err
		}
	}
	return nil
}

func (e *Env) UnmarshalKDL(dec *kdltext.Decoder) error {
	e.Vars = nil
	for dec.PeekKind() == kdltext.KindProp {
		name, _ := dec.ReadToken()
		val, err := dec.ReadToken()
		if err != nil {
			return err
		}
		if val.Value.Kind() != kdltext.ValueString {
			return fmt.Errorf("want a string for %s, not %v", name.Name(), val.Value)
		}
		e.Vars = append(e.Vars, EnvVar{name.Name(), val.Value.String()})
	}
	if k := dec.PeekKind(); k != kdltext.KindEnd {
		return fmt.Errorf("want only properties, not a %v", k)
	}
	return nil
}

// Env writes and reads the contents of its node: its properties.
func ExampleMarshaler_contents() {
	type Job struct {
		Command string `kdl:"command"`
		Env     Env    `kdl:"env"`
	}

	var job Job
	input := `
command "make test"
env PATH="/usr/bin" GOFLAGS="-race" CGO_ENABLED="1"
`
	if err := kuddle.Unmarshal([]byte(input), &job); err != nil {
		log.Fatal(err)
	}
	fmt.Println(job.Env.Vars)

	job.Env.Vars = job.Env.Vars[:2]
	b, err := kuddle.Marshal(job)
	if err != nil {
		log.Fatal(err)
	}
	os.Stdout.Write(b)
	// Output:
	// [{PATH /usr/bin} {GOFLAGS -race} {CGO_ENABLED 1}]
	// command "make test"
	// env PATH="/usr/bin" GOFLAGS=-race
}

// This example reads a document an order at a time, decoding the children of
// each order node with UnmarshalDecode.
func ExampleUnmarshalDecode() {
	type Item struct {
		Name string `kdl:",arg"`
		Qty  int    `kdl:"qty,prop"`
	}

	input := `
order 1001 {
    item apple qty=2
    item pear qty=1
}
order 1002 {
    item fig qty=12
}
`
	dec := kdltext.NewDecoder(strings.NewReader(input))
	for {
		// The order's KindNode token, and its argument.
		if _, err := dec.ReadToken(); err == io.EOF {
			break
		} else if err != nil {
			log.Fatal(err)
		}
		id, err := dec.ReadToken()
		if err != nil {
			log.Fatal(err)
		}

		var items []Item
		if err := kuddle.UnmarshalDecode(dec, &items); err != nil {
			log.Fatal(err)
		}
		// The order's KindEnd token.
		if _, err := dec.ReadToken(); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%v: %+v\n", id.Value, items)
	}
	// Output:
	// 1001: [{Name:apple Qty:2} {Name:pear Qty:1}]
	// 1002: [{Name:fig Qty:12}]
}
