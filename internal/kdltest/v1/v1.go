// Package v1 embeds the KDL v1 conformance tests, vendored in testdata from
// the last KDL v1 commit of github.com/kdl-org/kdl, unmodified. testdata/SOURCE records
// the commit; go generate re-vendors them.
package v1

import "embed"

//go:generate go run ../vendor.go -ref 05d16e0597214c911f2941b9074b752236bd1f51

// Files holds the test cases, in testdata/test_cases.
//
//go:embed testdata/test_cases
var Files embed.FS
