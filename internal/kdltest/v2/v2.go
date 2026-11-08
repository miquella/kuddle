// Package v2 embeds the KDL v2 conformance tests, vendored in testdata from
// github.com/kdl-org/kdl, unmodified. testdata/SOURCE records the commit; go
// generate re-vendors them.
//
// The tests are at the commit of draft-marchan-kdl2.md that spec/SPEC_v2.md
// was copied from.
package v2

import "embed"

//go:generate go run ../vendor.go -ref 89c1087d5e7f530de328f18b6a0fad54ca8ea227

// Files holds the test cases, in testdata/test_cases.
//
//go:embed testdata/test_cases
var Files embed.FS
