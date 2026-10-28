package kuddle_test

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/miquella/kuddle"
	"github.com/miquella/kuddle/kdltext"
)

type benchConfig struct {
	Name     string         `kdl:"name"`
	Debug    bool           `kdl:"debug"`
	Services []benchService `kdl:"service"`
}

type benchService struct {
	Name     string            `kdl:",arg"`
	Image    string            `kdl:"image,prop"`
	Replicas int               `kdl:"replicas,prop"`
	Ports    []int             `kdl:"port"`
	Env      map[string]string `kdl:"env"`
	Command  []string          `kdl:"command"`
	Health   *benchHealth      `kdl:"health"`
}

type benchHealth struct {
	Path     string  `kdl:",arg"`
	Interval float64 `kdl:"interval,prop"`
}

// benchInput returns a document of n services, each with arguments,
// properties, and children.
func benchInput(n int) []byte {
	var b strings.Builder
	b.WriteString("name \"bench\"\ndebug #true\n")
	for i := range n {
		fmt.Fprintf(&b, "service \"svc-%d\" image=\"registry.example.com/svc:%d\" replicas=%d {\n", i, i, i%5+1)
		fmt.Fprintf(&b, "    port %d %d\n", 8000+i, 9000+i)
		b.WriteString("    env {\n        HOME \"/home/svc\"\n        LOG_LEVEL debug\n        \"quoted key\" \"a \\\"quoted\\\" value\"\n    }\n")
		b.WriteString("    command \"/bin/svc\" --config \"/etc/svc.kdl\" -v\n")
		b.WriteString("    health \"/healthz\" interval=2.5\n")
		b.WriteString("}\n")
	}
	return []byte(b.String())
}

var benchData = benchInput(100)

// BenchmarkDecodeTokens reads the tokens of the document, without building
// anything from them: the cost of parsing alone.
func BenchmarkDecodeTokens(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(benchData)))
	for b.Loop() {
		dec := kdltext.NewDecoder(bytes.NewReader(benchData))
		for {
			if _, err := dec.ReadToken(); err == io.EOF {
				break
			} else if err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkUnmarshal(b *testing.B) {
	b.Run("Document", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(benchData)))
		for b.Loop() {
			var doc kuddle.Document
			if err := kuddle.Unmarshal(benchData, &doc); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Struct", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(benchData)))
		for b.Loop() {
			var cfg benchConfig
			if err := kuddle.Unmarshal(benchData, &cfg); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkMarshal(b *testing.B) {
	var doc kuddle.Document
	var cfg benchConfig
	if err := kuddle.Unmarshal(benchData, &doc); err != nil {
		b.Fatal(err)
	}
	if err := kuddle.Unmarshal(benchData, &cfg); err != nil {
		b.Fatal(err)
	}
	for _, bb := range []struct {
		name string
		in   any
	}{{"Document", &doc}, {"Struct", &cfg}} {
		b.Run(bb.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(benchData)))
			for b.Loop() {
				if err := kuddle.MarshalWrite(io.Discard, bb.in); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
