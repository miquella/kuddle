// Package v2 embeds the KDL v2 conformance tests, vendored in testdata from
// github.com/kdl-org/kdl, unmodified. testdata/SOURCE records the commit; go
// generate re-vendors them.
//
// The tests are at tag 2.0.0 with the upstream fix to
// vertical_tab_whitespace, which contradicted the spec by treating VT as
// whitespace. The commit adds no other changes to the tests.
package v2

import "embed"

//go:generate go run ../vendor.go -ref 646dafcd353d1cf5ec31422935133911ec0be772

// Files holds the test cases, in testdata/test_cases.
//
//go:embed testdata/test_cases
var Files embed.FS
