package main

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// An upload that breaks off leaves the old file as it was, and nothing else.
func TestInterruptedUploadKeepsOldFile(t *testing.T) {
	e := newTestEnv(t)
	p := filepath.Join(e.dirs["files"], "report.txt")
	if err := os.WriteFile(p, []byte("the old report"), 0o640); err != nil {
		t.Fatal(err)
	}

	u, _ := url.Parse(e.ts.URL)
	c, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(c, "PUT /dav/files/report.txt HTTP/1.1\r\nHost: x\r\nAuthorization: Basic YWxpY2U6cHc=\r\nContent-Length: 1000\r\n\r\nthe new rep")
	time.Sleep(100 * time.Millisecond)
	c.Close()

	deadline := time.Now().Add(5 * time.Second)
	for {
		entries, _ := os.ReadDir(e.dirs["files"])
		if len(entries) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("left behind: %v", entries)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if b, _ := os.ReadFile(p); string(b) != "the old report" {
		t.Fatalf("old file is now %q", b)
	}
}

// A finished upload replaces the file and keeps its mode.
func TestUploadReplacesFileKeepingMode(t *testing.T) {
	e := newTestEnv(t)
	p := filepath.Join(e.dirs["files"], "notes.txt")
	os.WriteFile(p, []byte("old"), 0o600)
	os.Chmod(p, 0o600)
	if resp, _ := e.do("PUT", "/dav/files/notes.txt", "alice", strings.NewReader("new")); resp.StatusCode != 201 && resp.StatusCode != 204 {
		t.Fatalf("PUT: %d", resp.StatusCode)
	}
	if b, _ := os.ReadFile(p); string(b) != "new" {
		t.Fatalf("file is %q", b)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, want 0600", fi.Mode().Perm())
	}
	if entries, _ := os.ReadDir(e.dirs["files"]); len(entries) != 1 {
		t.Fatalf("left behind: %v", entries)
	}
	// Not onto a directory.
	os.Mkdir(filepath.Join(e.dirs["files"], "dir"), 0o755)
	if resp, _ := e.do("PUT", "/dav/files/dir", "alice", strings.NewReader("x")); resp.StatusCode < 400 {
		t.Fatalf("PUT onto a directory: %d", resp.StatusCode)
	}
}
