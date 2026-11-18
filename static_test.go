package main

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

func TestStaticFiles(t *testing.T) {
	e := newTestEnv(t)

	resp, body := e.do("GET", "/vendor/pdfjs-"+pdfjsVersion(t)+"/legacy/build/pdf.min.mjs", "", nil)
	if resp.StatusCode != 200 || len(body) < 1000 {
		t.Fatalf("pdf.js: %d, %d bytes", resp.StatusCode, len(body))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Errorf("pdf.js Content-Type %q", ct) // module scripts are refused without a JavaScript type
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("vendored files should be cached for good, got %q", cc)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff")
	}

	resp, body = e.do("GET", "/licenses/go.txt", "", nil)
	if resp.StatusCode != 200 || !strings.Contains(body, "The Go Authors") {
		t.Fatalf("licence: %d %q", resp.StatusCode, body)
	}

	for _, p := range []string{
		"/vendor/",
		"/vendor/pdfjs-" + pdfjsVersion(t) + "/",
		"/licenses/",
		"/vendor/missing.js",
		"/vendor/%2e%2e/static.go",
		"/licenses/../static.go",
	} {
		if resp, _ := e.do("GET", p, "", nil); resp.StatusCode != 404 {
			t.Errorf("GET %s: %d, want 404", p, resp.StatusCode)
		}
	}
}

// pdfjsVersion is the PDF.js version web/index.html loads.
func pdfjsVersion(t *testing.T) string {
	t.Helper()
	m := regexp.MustCompile(`const PDFJS_VERSION = '([^']+)'`).FindSubmatch(indexHTML)
	if m == nil {
		t.Fatal("PDFJS_VERSION not found in web/index.html")
	}
	return string(m[1])
}

// Every vendored file the web UI names, scripts and licence texts, must be
// embedded: after updating PDF.js, this catches a path that moved.
func TestIndexNamesEmbeddedFiles(t *testing.T) {
	dir := "vendor/pdfjs-" + pdfjsVersion(t) + "/"
	if _, err := fs.Stat(staticFS, dir+"LICENSE"); err != nil {
		t.Fatal(err)
	}
	refs := regexp.MustCompile(`(?:PDFJS|P) \+ '([^']+)'`).FindAllSubmatch(indexHTML, -1)
	if len(refs) < 10 {
		t.Fatalf("found only %d references to PDF.js files", len(refs))
	}
	for _, m := range refs {
		name := dir + string(m[1])
		if strings.HasSuffix(name, "/") {
			name = strings.TrimSuffix(name, "/")
		}
		if _, err := fs.Stat(staticFS, name); err != nil {
			t.Errorf("web/index.html names %s: %v", m[1], err)
		}
	}
	for _, m := range regexp.MustCompile(`'(/licenses/[^']+)'`).FindAllSubmatch(indexHTML, -1) {
		if _, err := fs.Stat(staticFS, strings.TrimPrefix(string(m[1]), "/")); err != nil {
			t.Errorf("web/index.html names %s: %v", m[1], err)
		}
	}
}
