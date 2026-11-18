package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func gz(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	zw.Write([]byte(s))
	zw.Close()
	return b.Bytes()
}

// newOfficeDir creates a fake grind dist/ and turns office documents on.
func newOfficeDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<p>grind</p>"), 0o644)
	os.WriteFile(filepath.Join(dir, "index.html.gz"), gz(t, "<p>grind</p>"), 0o644)
	os.WriteFile(filepath.Join(dir, "app.wasm.gz"), gz(t, "wasm bytes"), 0o644) // only compressed
	os.Mkdir(filepath.Join(dir, "fonts"), 0o755)
	os.WriteFile(filepath.Join(dir, "fonts", "a.ttf"), []byte("font"), 0o644)
	t.Setenv("SHELF_OFFICE_DIR", dir)
	return dir
}

func TestOfficeOffByDefault(t *testing.T) {
	t.Setenv("SHELF_OFFICE_DIR", "")
	e := newTestEnv(t)
	if resp, _ := e.do("GET", "/office/", "", nil); resp.StatusCode != 404 {
		t.Fatalf("GET /office/ without office dir: %d, want 404", resp.StatusCode)
	}
	_, body := e.do("GET", "/api/me", "alice", nil)
	var me struct{ Office []string }
	json.Unmarshal([]byte(body), &me)
	if me.Office != nil {
		t.Fatalf("office should be off: %s", body)
	}
}

func TestOfficeServesPage(t *testing.T) {
	newOfficeDir(t)
	e := newTestEnv(t)

	_, body := e.do("GET", "/api/me", "alice", nil)
	var me struct{ Office []string }
	json.Unmarshal([]byte(body), &me)
	if len(me.Office) == 0 {
		t.Fatalf("office should be on: %s", body)
	}

	// Without gzip: the plain file.
	resp, body := e.do("GET", "/office/", "", nil, "Accept-Encoding", "identity")
	if resp.StatusCode != 200 || body != "<p>grind</p>" || resp.Header.Get("Content-Encoding") != "" {
		t.Fatalf("index: %d %q %q", resp.StatusCode, body, resp.Header.Get("Content-Encoding"))
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("index Content-Type %q", ct)
	}
	// The web UI shows the page in a frame, and nobody else may.
	if resp.Header.Get("X-Frame-Options") != "SAMEORIGIN" || resp.Header.Get("Content-Security-Policy") != "frame-ancestors 'self'" {
		t.Errorf("framing: %q %q", resp.Header.Get("X-Frame-Options"), resp.Header.Get("Content-Security-Policy"))
	}

	// With gzip: the precompressed sibling, with the original's type.
	resp, body = e.do("GET", "/office/index.html", "", nil, "Accept-Encoding", "br, gzip")
	if resp.StatusCode != 200 || resp.Header.Get("Content-Encoding") != "gzip" || body != string(gz(t, "<p>grind</p>")) {
		t.Fatalf("gzip index: %d %q", resp.StatusCode, resp.Header.Get("Content-Encoding"))
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("gzip index Content-Type %q", ct)
	}

	// Only compressed on disk, browser without gzip: decompressed on the fly.
	resp, body = e.do("GET", "/office/app.wasm", "", nil, "Accept-Encoding", "identity")
	if resp.StatusCode != 200 || body != "wasm bytes" || resp.Header.Get("Content-Type") != "application/wasm" {
		t.Fatalf("wasm: %d %q %q", resp.StatusCode, body, resp.Header.Get("Content-Type"))
	}

	if resp, body := e.do("GET", "/office/fonts/a.ttf", "", nil); resp.StatusCode != 200 || body != "font" {
		t.Fatalf("font: %d %q", resp.StatusCode, body)
	}
}

func TestOfficeStaysInsideItsDirectory(t *testing.T) {
	dir := newOfficeDir(t)
	os.WriteFile(filepath.Join(filepath.Dir(dir), "secret.txt"), []byte("secret"), 0o644)
	os.Symlink(filepath.Join(filepath.Dir(dir), "secret.txt"), filepath.Join(dir, "link.txt"))
	e := newTestEnv(t)
	for _, p := range []string{
		"/office/../secret.txt",
		"/office/%2e%2e/secret.txt",
		"/office/link.txt",
		"/office/index.html.gz",
		"/office/fonts/",
		"/office/missing.js",
	} {
		if resp, body := e.do("GET", p, "", nil); resp.StatusCode == 200 || body == "secret" {
			t.Errorf("GET %s: %d %q", p, resp.StatusCode, body)
		}
	}
}
