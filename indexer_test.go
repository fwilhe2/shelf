package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type indexStatus struct {
	Running, Queued bool
	Finished        *time.Time
	Error           string
	Every           int
	Shares          []struct {
		Name    string
		Files   *int
		Scanned *time.Time
		Report  string
	}
	Totals map[string]int
}

func (e *testEnv) indexStatus(user string) indexStatus {
	e.t.Helper()
	resp, body := e.do("GET", "/api/index", user, nil)
	if resp.StatusCode != 200 {
		e.t.Fatalf("GET /api/index: %d %s", resp.StatusCode, body)
	}
	var st indexStatus
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		e.t.Fatal(err)
	}
	return st
}

func TestIndexStatusAndStart(t *testing.T) {
	var dir string
	var d map[string]string
	e := newTestEnvWith(t, func(cfg *Config, dirs map[string]string) {
		d = dirs
		dir = fakeClerk(t, cfg, nil)
	})
	os.WriteFile(filepath.Join(dir, "status.txt"), []byte(fmt.Sprintf(
		"index\t%s\nfiles\t9\ndocuments\t5\nwith text\t4\n"+
			"folder\t%s\t3 files\tscanned 2026-11-20 08:15 UTC\n"+
			"folder\t%s\t6 files\tscanned 2026-11-19 23:00 UTC\n"+
			"folder\t/old/share\t0 files\tscanned never\n",
		filepath.Join(dir, "index.sqlite"), d["files"], d["private"])), 0o644)

	st := e.indexStatus("alice")
	if st.Running || st.Finished != nil || st.Every != 3600 {
		t.Fatalf("before a run: %+v", st)
	}
	names := map[string]bool{}
	for _, sh := range st.Shares {
		names[sh.Name] = true
		switch sh.Name {
		case "files":
			if sh.Files == nil || *sh.Files != 3 || sh.Scanned == nil ||
				!sh.Scanned.Equal(time.Date(2026, 11, 20, 8, 15, 0, 0, time.UTC)) {
				t.Errorf("files: %+v", sh)
			}
		case "media":
			if sh.Files != nil || sh.Scanned != nil {
				t.Errorf("media was never indexed: %+v", sh)
			}
		}
	}
	if names["private"] || !names["files"] || !names["media"] {
		t.Fatalf("alice sees %v", names)
	}
	if st.Totals != nil {
		t.Fatal("totals include bob's share, alice must not see them")
	}
	if e.indexStatus("bob").Totals["with_text"] != 4 {
		t.Fatal("bob sees every share, and the totals")
	}

	if resp, _ := e.do("POST", "/api/index", "alice", nil); resp.StatusCode != 202 {
		t.Fatalf("POST /api/index: %d", resp.StatusCode)
	}
	deadline := time.Now().Add(5 * time.Second)
	for st = e.indexStatus("alice"); st.Finished == nil; st = e.indexStatus("alice") {
		if time.Now().After(deadline) {
			t.Fatalf("no run: %+v", st)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st.Error != "" || st.Running || st.Queued {
		t.Fatalf("after the run: %+v", st)
	}
	for _, sh := range st.Shares {
		if sh.Name == "files" && sh.Report != "2 files, 1 read; 1 added, 0 moved, 0 copied, 0 changed, 0 removed; 1 with text" {
			t.Errorf("report %q", sh.Report)
		}
	}
}

func TestIndexEvery(t *testing.T) {
	for s, want := range map[string]time.Duration{"": time.Hour, "15m": 15 * time.Minute, "off": 0, "0": 0} {
		if d, err := (&SearchConfig{Every: s}).every(); err != nil || d != want {
			t.Errorf("%q: %v %v", s, d, err)
		}
	}
	for _, s := range []string{"10s", "soon"} {
		if _, err := (&SearchConfig{Every: s}).every(); err == nil {
			t.Errorf("%q should be refused", s)
		}
	}
}
