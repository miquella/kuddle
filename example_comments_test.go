package kuddle_test

import (
	"fmt"
	"log"

	"github.com/miquella/kuddle"
)

// A struct can keep the comments of a document in comment and headercomment
// fields.
func Example_comments() {
	const input = `// Settings for the web servers.

// The main server.
server a.example.com {
    // These are its options.

    // It needs TLS.
    tls #true
}
`
	type TLS struct {
		On      bool   `kdl:",arg"`
		Comment string `kdl:",comment"` // "It needs TLS."
	}
	type Server struct {
		Host    string `kdl:",arg"`
		Comment string `kdl:",comment"`       // "The main server."
		Options string `kdl:",headercomment"` // "These are its options."
		TLS     TLS    `kdl:"tls"`
	}
	type Config struct {
		Header string `kdl:",headercomment"` // "Settings for the web servers."
		Server Server `kdl:"server"`
	}

	var cfg Config
	if err := kuddle.Unmarshal([]byte(input), &cfg); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%q\n", cfg.Header)
	fmt.Printf("%q\n", cfg.Server.Comment)
	fmt.Printf("%q\n", cfg.Server.Options)
	fmt.Printf("%q\n", cfg.Server.TLS.Comment)

	// Marshal writes them back.
	out, err := kuddle.Marshal(cfg)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(string(out) == input)

	// Output:
	// "Settings for the web servers."
	// "The main server."
	// "These are its options."
	// "It needs TLS."
	// true
}

// A comment field can hold more than one block, as a string, a []string or
// a []kdltext.Comment.
func Example_commentBlocks() {
	const input = `// Check the version.

// Needs a restart.
// Ask first.
server a.example.com
`
	var asString struct {
		Server struct {
			Comment string `kdl:",comment"`
		} `kdl:"server"`
	}
	var asSlice struct {
		Server struct {
			Comment []string `kdl:",comment"`
		} `kdl:"server"`
	}
	if err := kuddle.Unmarshal([]byte(input), &asString, kuddle.DiscardUnknown(true)); err != nil {
		log.Fatal(err)
	}
	if err := kuddle.Unmarshal([]byte(input), &asSlice, kuddle.DiscardUnknown(true)); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%q\n", asString.Server.Comment)
	fmt.Printf("%q\n", asSlice.Server.Comment)

	// Output:
	// "Check the version.\n\nNeeds a restart.\nAsk first."
	// ["Check the version." "Needs a restart.\nAsk first."]
}
