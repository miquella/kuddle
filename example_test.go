package kuddle_test

import (
	"errors"
	"fmt"
	"io"
	"log"
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

// Duration is a time.Duration that is written in KDL as a string, such as
// "1m30s": as a property, or as the single argument of a node.
type Duration time.Duration

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
