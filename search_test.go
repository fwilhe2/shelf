package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeClerk writes a stand-in for clerk that records its arguments in
// args.txt and answers find with the lines in find.jsonl, and turns search on.
func fakeClerk(t *testing.T, cfg *Config, find []string) (dir string) {
	t.Helper()
	dir = t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > %[1]s/args.txt\n"+
		"case \"$3\" in find) cat %[1]s/find.jsonl ;; esac\n", dir)
	if err := os.WriteFile(filepath.Join(dir, "clerk"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "find.jsonl"), []byte(strings.Join(find, "\n")+"\n"), 0o644)
	idx := filepath.Join(dir, "index.sqlite")
	os.WriteFile(idx, nil, 0o600)
	cfg.Search = &SearchConfig{Index: idx, Clerk: filepath.Join(dir, "clerk")}
	return dir
}

func hit(path, context string) string {
	ctx := "null"
	if context != "" {
		ctx = fmt.Sprintf("%q", context)
	}
	return fmt.Sprintf(`{"path":%q,"hash":"00","size":3,"context":%s}`, path, ctx)
}

type searchResult struct {
	Results []struct {
		Path    string
		Size    int64
		Context *string
	}
	Truncated bool
}

func (e *testEnv) search(user, q string) (int, searchResult) {
	e.t.Helper()
	resp, body := e.do("GET", "/api/search?q="+q, user, nil)
	var r searchResult
	if resp.StatusCode == 200 {
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			e.t.Fatalf("%s: %v", body, err)
		}
	}
	return resp.StatusCode, r
}

func paths(r searchResult) []string {
	var out []string
	for _, h := range r.Results {
		out = append(out, h.Path)
	}
	return out
}

func TestSearchOnlyInTheUsersShares(t *testing.T) {
	var clerkDir string
	e := newTestEnvWith(t, func(cfg *Config, d map[string]string) {
		os.WriteFile(filepath.Join(d["files"], "a.txt"), []byte("abc"), 0o644)
		os.Mkdir(filepath.Join(d["files"], "sub dir"), 0o755)
		os.WriteFile(filepath.Join(d["files"], "sub dir", "b.pdf"), []byte("abc"), 0o644)
		os.WriteFile(filepath.Join(d["private"], "p.txt"), []byte("abc"), 0o644)
		os.Symlink(filepath.Join(d["outside"], "secret.txt"), filepath.Join(d["files"], "link.txt"))
		clerkDir = fakeClerk(t, cfg, []string{
			hit(filepath.Join(d["files"], "a.txt"), "the [word] here"),
			hit(filepath.Join(d["files"], "sub dir", "b.pdf"), ""),
			hit(filepath.Join(d["private"], "p.txt"), "[word]"),
			hit(filepath.Join(d["outside"], "secret.txt"), "[word]"),
			hit(filepath.Join(d["files"], "link.txt"), "[word]"),
			hit(filepath.Join(d["files"], "gone.txt"), "[word]"),
			hit(d["files"], ""),
			"not json",
		})
	})

	code, r := e.search("alice", "w%C3%B6rd+-x")
	if code != 200 {
		t.Fatalf("search: %d", code)
	}
	if got, want := strings.Join(paths(r), ","), "/files/a.txt,/files/sub dir/b.pdf"; got != want {
		t.Fatalf("alice found %s, want %s", got, want)
	}
	if c := r.Results[0].Context; c == nil || *c != "the [word] here" || r.Results[0].Size != 3 {
		t.Fatalf("first hit: %+v", r.Results[0])
	}
	if r.Results[1].Context != nil {
		t.Fatalf("found by name, so no context: %+v", r.Results[1])
	}
	args, _ := os.ReadFile(filepath.Join(clerkDir, "args.txt"))
	want := fmt.Sprintf("--index\n%s\nfind\n--json\n--\nwörd\n-x\n", filepath.Join(clerkDir, "index.sqlite"))
	if string(args) != want {
		t.Fatalf("clerk ran with\n%s\nwant\n%s", args, want)
	}

	_, r = e.search("bob", "word")
	if got, want := strings.Join(paths(r), ","), "/files/a.txt,/files/sub dir/b.pdf,/private/p.txt"; got != want {
		t.Fatalf("bob found %s, want %s", got, want)
	}

	if code, _ := e.search("alice", "+"); code != 400 {
		t.Fatalf("no words: %d, want 400", code)
	}
	if code, _ := e.search("", "word"); code != 401 {
		t.Fatalf("not logged in: %d, want 401", code)
	}
	_, body := e.do("GET", "/api/me", "alice", nil)
	if !strings.Contains(body, `"search":true`) {
		t.Fatalf("me: %s", body)
	}
}

func TestSearchTruncated(t *testing.T) {
	e := newTestEnvWith(t, func(cfg *Config, d map[string]string) {
		var lines []string
		for i := range searchMaxHits + 5 {
			p := filepath.Join(d["files"], fmt.Sprintf("f%03d.txt", i))
			os.WriteFile(p, []byte("abc"), 0o644)
			lines = append(lines, hit(p, ""))
		}
		fakeClerk(t, cfg, lines)
	})
	_, r := e.search("alice", "f")
	if len(r.Results) != searchMaxHits || !r.Truncated {
		t.Fatalf("%d results, truncated %v", len(r.Results), r.Truncated)
	}
}

func TestSearchOffOrNotIndexed(t *testing.T) {
	e := newTestEnv(t)
	if code, _ := e.search("alice", "word"); code != 404 {
		t.Fatalf("search off: %d, want 404", code)
	}
	_, body := e.do("GET", "/api/me", "alice", nil)
	if !strings.Contains(body, `"search":false`) {
		t.Fatalf("me: %s", body)
	}

	e = newTestEnvWith(t, func(cfg *Config, _ map[string]string) {
		fakeClerk(t, cfg, nil)
		os.Remove(cfg.Search.Index)
	})
	if code, _ := e.search("alice", "word"); code != 503 {
		t.Fatalf("not indexed: %d, want 503", code)
	}
}

func TestSearchIndexNotInAShare(t *testing.T) {
	cfg := &Config{Listen: ":0", Shares: []Share{{Name: "files", Path: "/srv/files"}}}
	for idx, ok := range map[string]bool{
		"/var/lib/shelf/clerk.sqlite": true,
		"/srv/files-index.sqlite":     true,
		"/srv/files/.clerk.sqlite":    false,
		"clerk.sqlite":                false,
	} {
		cfg.Search = &SearchConfig{Index: idx}
		if err := cfg.Validate(); (err == nil) != ok {
			t.Errorf("index %s: %v", idx, err)
		}
	}
}

func TestIndexScansEveryShare(t *testing.T) {
	var dir, cfgPath string
	newTestEnvWith(t, func(cfg *Config, d map[string]string) {
		dir = fakeClerk(t, cfg, nil)
		cfg.Search.Index = filepath.Join(dir, "state", "clerk.sqlite")
		cfg.Shares = append(cfg.Shares, Share{Name: "gone", Path: filepath.Join(d["outside"], "unmounted")})
		cfgPath = filepath.Join(filepath.Dir(d["files"]), "config.json")
	})
	if err := cmdIndex([]string{"-c", cfgPath}); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args.txt"))
	base := filepath.Dir(cfgPath)
	want := fmt.Sprintf("--index\n%s\nscan\n--\n%s\n%s\n%s\n", filepath.Join(dir, "state", "clerk.sqlite"),
		filepath.Join(base, "files"), filepath.Join(base, "media"), filepath.Join(base, "private"))
	if string(args) != want {
		t.Fatalf("clerk ran with\n%s\nwant\n%s", args, want)
	}
}

func TestSearchFromTheEnvironment(t *testing.T) {
	t.Setenv("SHELF_SEARCH_INDEX", "/var/lib/shelf/clerk.sqlite")
	t.Setenv("SHELF_CLERK", "/opt/clerk")
	sc := searchConfig(&Config{})
	if sc == nil || sc.Index != "/var/lib/shelf/clerk.sqlite" || sc.Clerk != "/opt/clerk" {
		t.Fatalf("%+v", sc)
	}
	own := &SearchConfig{Index: "/srv/index.sqlite"}
	if searchConfig(&Config{Search: own}) != own {
		t.Fatal("the config's search comes first")
	}
}
