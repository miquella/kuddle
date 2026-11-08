// Package kdltest provides the vendored KDL conformance test cases.
package kdltest

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	v1 "github.com/miquella/kuddle/internal/kdltest/v1"
	v2 "github.com/miquella/kuddle/internal/kdltest/v2"
)

// Case is a conformance test case.
type Case struct {
	Name     string // input file name, e.g. "all_escapes.kdl"
	Input    []byte
	Expected []byte // the input after parsing and printing; nil if Fail
	Fail     bool   // whether the input must fail to parse
}

// Cases returns the test cases for KDL version 1 or 2.
func Cases(version int) ([]Case, error) {
	var cases fs.FS
	switch version {
	case 1:
		cases = v1.Files
	case 2:
		cases = v2.Files
	default:
		return nil, fmt.Errorf("kdltest: no tests for KDL version %d", version)
	}
	dir := "testdata/test_cases"
	entries, err := fs.ReadDir(cases, path.Join(dir, "input"))
	if err != nil {
		return nil, err
	}

	// Failing cases have no expected output. The v2 suite also gives them a
	// _fail suffix, but the v1 suite predates that convention.
	var cs []Case
	for _, e := range entries {
		c := Case{Name: e.Name()}
		if c.Input, err = fs.ReadFile(cases, path.Join(dir, "input", c.Name)); err != nil {
			return nil, err
		}
		c.Expected, err = fs.ReadFile(cases, path.Join(dir, "expected_kdl", c.Name))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			c.Fail = true
		case err != nil:
			return nil, err
		case strings.HasSuffix(c.Name, "_fail.kdl"):
			return nil, fmt.Errorf("kdltest: failing case %s has expected output", c.Name)
		}
		cs = append(cs, c)
	}
	return cs, nil
}
