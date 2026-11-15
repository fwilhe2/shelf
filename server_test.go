package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

type testEnv struct {
	t    *testing.T
	ts   *httptest.Server
	dirs map[string]string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	base := t.TempDir()
	dirs := map[string]string{}
	for _, n := range []string{"files", "media", "private", "outside"} {
		dirs[n] = filepath.Join(base, n)
		if err := os.Mkdir(dirs[n], 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(dirs["media"], "song.mp3"), []byte("la la"), 0o644)
	os.WriteFile(filepath.Join(dirs["outside"], "secret.txt"), []byte("secret"), 0o644)

	hash, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
	cfg := &Config{
		Listen: ":0",
		Users: []User{
			{Name: "alice", PasswordHash: string(hash)},
			{Name: "bob", PasswordHash: string(hash)},
		},
		Shares: []Share{
			{Name: "files", Path: dirs["files"]},
			{Name: "media", Path: dirs["media"], ReadOnly: true},
			{Name: "private", Path: dirs["private"], Users: []string{"bob"}},
		},
	}
	cfgPath := filepath.Join(base, "config.json")
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &testEnv{t: t, ts: ts, dirs: dirs}
}

func (e *testEnv) do(method, path, user string, body io.Reader, hdr ...string) (*http.Response, string) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.ts.URL+path, body)
	if user != "" {
		req.SetBasicAuth(user, "pw")
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestDAVRequiresAuth(t *testing.T) {
	e := newTestEnv(t)
	resp, _ := e.do("PROPFIND", "/dav/", "", nil, "Depth", "1")
	if resp.StatusCode != 401 || resp.Header.Get("WWW-Authenticate") == "" {
		t.Fatalf("got %d, want 401 with WWW-Authenticate", resp.StatusCode)
	}
	req, _ := http.NewRequest("GET", e.ts.URL+"/dav/files/", nil)
	req.SetBasicAuth("alice", "wrong")
	r2, _ := http.DefaultClient.Do(req)
	if r2.StatusCode != 401 {
		t.Fatalf("wrong password: got %d", r2.StatusCode)
	}
}

func TestPutGetDelete(t *testing.T) {
	e := newTestEnv(t)
	if resp, _ := e.do("PUT", "/dav/files/hello.txt", "alice", strings.NewReader("hi there")); resp.StatusCode != 201 {
		t.Fatalf("PUT: %d", resp.StatusCode)
	}
	if b, _ := os.ReadFile(filepath.Join(e.dirs["files"], "hello.txt")); string(b) != "hi there" {
		t.Fatalf("file on disk: %q", b)
	}
	if resp, body := e.do("GET", "/dav/files/hello.txt", "alice", nil); resp.StatusCode != 200 || body != "hi there" {
		t.Fatalf("GET: %d %q", resp.StatusCode, body)
	}
	if resp, _ := e.do("MKCOL", "/dav/files/sub", "alice", nil); resp.StatusCode != 201 {
		t.Fatalf("MKCOL: %d", resp.StatusCode)
	}
	if resp, _ := e.do("MOVE", "/dav/files/hello.txt", "alice", nil, "Destination", e.ts.URL+"/dav/files/sub/moved.txt"); resp.StatusCode != 201 {
		t.Fatalf("MOVE: %d", resp.StatusCode)
	}
	if resp, _ := e.do("DELETE", "/dav/files/sub", "alice", nil); resp.StatusCode != 204 {
		t.Fatalf("DELETE: %d", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(e.dirs["files"], "sub")); !os.IsNotExist(err) {
		t.Fatal("sub should be gone")
	}
}

func TestShareRootCannotBeDeleted(t *testing.T) {
	e := newTestEnv(t)
	if resp, _ := e.do("DELETE", "/dav/files", "alice", nil); resp.StatusCode < 400 {
		t.Fatalf("DELETE share root: %d", resp.StatusCode)
	}
	if _, err := os.Stat(e.dirs["files"]); err != nil {
		t.Fatal("share dir was deleted")
	}
}

func TestReadOnlyShare(t *testing.T) {
	e := newTestEnv(t)
	if resp, body := e.do("GET", "/dav/media/song.mp3", "alice", nil); resp.StatusCode != 200 || body != "la la" {
		t.Fatalf("GET: %d", resp.StatusCode)
	}
	for _, m := range []string{"PUT", "DELETE", "MKCOL"} {
		if resp, _ := e.do(m, "/dav/media/song.mp3", "alice", strings.NewReader("x")); resp.StatusCode != 403 {
			t.Errorf("%s on read-only share: %d, want 403", m, resp.StatusCode)
		}
	}
	e.do("PUT", "/dav/files/a.txt", "alice", strings.NewReader("a"))
	if resp, _ := e.do("COPY", "/dav/files/a.txt", "alice", nil, "Destination", e.ts.URL+"/dav/media/a.txt"); resp.StatusCode != 403 {
		t.Errorf("COPY into read-only share: %d", resp.StatusCode)
	}
	if b, _ := os.ReadFile(filepath.Join(e.dirs["media"], "song.mp3")); string(b) != "la la" {
		t.Fatal("read-only file modified")
	}
}

func TestPrivateShare(t *testing.T) {
	e := newTestEnv(t)
	if resp, _ := e.do("PUT", "/dav/private/x.txt", "alice", strings.NewReader("x")); resp.StatusCode < 400 {
		t.Fatalf("alice wrote to bob's share: %d", resp.StatusCode)
	}
	if resp, _ := e.do("PUT", "/dav/private/x.txt", "bob", strings.NewReader("x")); resp.StatusCode != 201 {
		t.Fatalf("bob PUT: %d", resp.StatusCode)
	}
	_, body := e.do("PROPFIND", "/dav/", "alice", nil, "Depth", "1")
	if strings.Contains(body, "private") || !strings.Contains(body, "files") {
		t.Fatalf("alice's root listing wrong: %s", body)
	}
}

func TestNoEscapeFromShare(t *testing.T) {
	e := newTestEnv(t)
	// A symlink inside a share pointing outside must not be followed.
	os.Symlink(e.dirs["outside"], filepath.Join(e.dirs["files"], "link"))
	os.Symlink(filepath.Join(e.dirs["outside"], "secret.txt"), filepath.Join(e.dirs["files"], "s.txt"))
	for _, p := range []string{"/dav/files/link/secret.txt", "/dav/files/s.txt", "/dav/files/%2e%2e/outside/secret.txt"} {
		if resp, body := e.do("GET", p, "alice", nil); resp.StatusCode == 200 || strings.Contains(body, "secret") {
			t.Errorf("GET %s escaped the share: %d %q", p, resp.StatusCode, body)
		}
	}
	fsys := newNASFS([]Share{{Name: "files", Path: e.dirs["files"]}})
	defer fsys.Close()
	if _, err := fsys.OpenFile(context.Background(), "/files/../../outside/secret.txt", os.O_RDONLY, 0); err == nil {
		t.Error("path traversal via FS succeeded")
	}
}

func TestWebLoginListAndZip(t *testing.T) {
	e := newTestEnv(t)
	e.do("PUT", "/dav/files/a.txt", "alice", strings.NewReader("aaa"))
	e.do("MKCOL", "/dav/files/dir", "alice", nil)
	e.do("PUT", "/dav/files/dir/b.txt", "alice", strings.NewReader("bbb"))

	resp, _ := http.Post(e.ts.URL+"/api/login", "application/json", strings.NewReader(`{"username":"alice","password":"pw"}`))
	if resp.StatusCode != 200 {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	cookie := resp.Cookies()[0]
	get := func(path string) (*http.Response, []byte) {
		req, _ := http.NewRequest("GET", e.ts.URL+path, nil)
		req.AddCookie(cookie)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r, b
	}

	r, body := get("/api/list?path=/files")
	var list struct {
		Entries []entry `json:"entries"`
	}
	json.Unmarshal(body, &list)
	if r.StatusCode != 200 || len(list.Entries) != 2 || list.Entries[0].Name != "dir" || !list.Entries[0].Dir {
		t.Fatalf("list: %d %s", r.StatusCode, body)
	}
	if r, _ := get("/api/list?path=/private"); r.StatusCode != 404 {
		t.Fatalf("alice listing private: %d", r.StatusCode)
	}

	r, body = get("/api/zip?path=/files")
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if r.StatusCode != 200 || err != nil {
		t.Fatalf("zip: %d %v", r.StatusCode, err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names["a.txt"] || !names["dir/b.txt"] {
		t.Fatalf("zip contents: %v", names)
	}

	if r, _ := get("/api/shares"); r.StatusCode != 200 {
		t.Fatalf("shares: %d", r.StatusCode)
	}
	resp, _ = http.Post(e.ts.URL+"/api/login", "application/json", strings.NewReader(`{"username":"alice","password":"nope"}`))
	if resp.StatusCode != 401 {
		t.Fatalf("bad login: %d", resp.StatusCode)
	}
}
