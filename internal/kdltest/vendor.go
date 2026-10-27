//go:build ignore

// Vendor replaces testdata, in the current directory, with the conformance
// tests of github.com/kdl-org/kdl: tests/ at the given ref, a tag or commit,
// and the license at that ref, copied verbatim. It records the ref and the
// commit it names in testdata/SOURCE.
//
// It is run by go generate in the package of each KDL version's tests.
package main

import (
	"archive/tar"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const repo = "https://github.com/kdl-org/kdl"

func main() {
	ref := flag.String("ref", "", "tag or commit of the tests")
	flag.Parse()
	log.SetFlags(0)
	log.SetPrefix("vendor: ")
	if *ref == "" {
		log.Fatal("usage: go run vendor.go -ref <tag or commit>")
	}
	if err := vendor(*ref); err != nil {
		log.Fatal(err)
	}
}

// vendor replaces testdata with the tests at ref.
func vendor(ref string) error {
	// Fetch everything before replacing anything.
	tmp, err := os.MkdirTemp(".", ".vendor-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	commit, err := extract(ref, map[string]string{
		"tests":      filepath.Join(tmp, "testdata"),
		"LICENSE.md": filepath.Join(tmp, "testdata", "LICENSE.md"),
	})
	if err != nil {
		return err
	}
	source := fmt.Sprintf("repository: %s\nref: %s\ncommit: %s\n", repo, ref, commit)
	if err := os.WriteFile(filepath.Join(tmp, "testdata", "SOURCE"), []byte(source), 0o644); err != nil {
		return err
	}

	if err := os.RemoveAll("testdata"); err != nil {
		return err
	}
	return os.Rename(filepath.Join(tmp, "testdata"), "testdata")
}

// extract writes each file or directory of the repository at ref named by a
// key of files to the path it maps to, and returns the commit that ref names,
// which the archive records.
func extract(ref string, files map[string]string) (commit string, err error) {
	url := fmt.Sprintf("%s/archive/%s.tar.gz", repo, ref)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: %s", url, resp.Status)
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return "", fmt.Errorf("%s: %v", url, err)
	}
	tr := tar.NewReader(gz)

	found := map[string]bool{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return "", fmt.Errorf("%s: %v", url, err)
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			// git archive records the commit in the global header.
			commit = h.PAXRecords["comment"]
			continue
		}
		// Names start with the archive's top-level directory, kdl-<ref>/.
		_, name, _ := strings.Cut(path.Clean(h.Name), "/")
		for from, to := range files {
			rel, ok := strings.CutPrefix(name, from)
			if !ok || rel != "" && rel[0] != '/' {
				continue
			}
			if !filepath.IsLocal(filepath.FromSlash(name)) {
				return "", fmt.Errorf("%s: unsafe path %q", url, h.Name)
			}
			dst := filepath.Join(to, filepath.FromSlash(rel))
			switch h.Typeflag {
			case tar.TypeDir:
				err = os.MkdirAll(dst, 0o755)
			case tar.TypeReg:
				err = writeFile(dst, tr)
			default:
				err = fmt.Errorf("%s: %s is not a regular file", url, h.Name)
			}
			if err != nil {
				return "", err
			}
			found[from] = true
		}
	}
	for from := range files {
		if !found[from] {
			return "", fmt.Errorf("%s: no %s", url, from)
		}
	}
	if commit == "" {
		return "", fmt.Errorf("%s: the archive does not record its commit", url)
	}
	return commit, nil
}

func writeFile(name string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
