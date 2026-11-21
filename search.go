package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Full-text search is clerk's (docs/search.md): `shelf index` has clerk read
// every share into an index kept outside of them, and /api/search asks clerk
// for the files where every word is found, keeping those in the user's shares.

const (
	searchTimeout = 15 * time.Second
	searchMaxHits = 200
	searchMaxWord = 16
)

// SearchConfig turns on full-text search.
type SearchConfig struct {
	// Index is clerk's index file. It must not be inside a share.
	Index string `json:"index"`
	// Clerk is the clerk program; "clerk" on $PATH when empty.
	Clerk string `json:"clerk,omitempty"`
	// Every is how often `shelf serve` indexes by itself ("1h" when empty, or
	// $SHELF_INDEX_EVERY), "off" for never. It also indexes after changes.
	Every string `json:"every,omitempty"`
}

func (sc *SearchConfig) validate(shares []Share) error {
	if !filepath.IsAbs(sc.Index) {
		return fmt.Errorf("search.index must be absolute, got %q", sc.Index)
	}
	for _, sh := range shares {
		if _, ok := within(filepath.Clean(sh.Path), sc.Index); ok {
			return fmt.Errorf("search.index must not be inside share %q", sh.Name)
		}
	}
	return nil
}

func (sc *SearchConfig) command(ctx context.Context, args ...string) *exec.Cmd {
	prog := sc.Clerk
	if prog == "" {
		prog = "clerk"
	}
	return exec.CommandContext(ctx, prog, append([]string{"--index", sc.Index}, args...)...)
}

// within returns p relative to dir, if p is dir or below it.
func within(dir, p string) (string, bool) {
	rel, err := filepath.Rel(dir, filepath.Clean(p))
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return rel, true
}

// cmdIndex has clerk read every share, for search. Only what changed since
// the last run is read again.
func cmdIndex(args []string) error {
	fs, cfgPath := newFlags("index")
	if _, err := parseArgs(fs, args, 0); err != nil {
		return err
	}
	cfg, err := LoadConfig(*cfgPath)
	if err != nil {
		return err
	}
	return runIndex(cfg, os.Stdout, true)
}

func flock(f *os.File, wait bool) error {
	how := syscall.LOCK_EX
	if !wait {
		how |= syscall.LOCK_NB
	}
	return syscall.Flock(int(f.Fd()), how)
}

// searchConfig is the config's "search", or else the one $SHELF_SEARCH_INDEX
// (and $SHELF_CLERK) name, or nil when search is off.
func searchConfig(cfg *Config) *SearchConfig {
	if cfg.Search != nil {
		return cfg.Search
	}
	if idx := os.Getenv("SHELF_SEARCH_INDEX"); idx != "" {
		return &SearchConfig{Index: idx, Clerk: os.Getenv("SHELF_CLERK")}
	}
	return nil
}

func (s *Server) search() *SearchConfig { return searchConfig(s.config()) }

type searchHit struct {
	Path    string  `json:"path"`              // /share/a/b.pdf
	Size    int64   `json:"size"`              // as indexed
	Context *string `json:"context,omitempty"` // where in the text, the words [marked]
}

// handleSearch finds the files in the user's shares where every word is in
// the path or in the text.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request, _ string, fsys *nasFS) {
	sc := s.search()
	if sc == nil {
		jsonError(w, http.StatusNotFound, "search is off")
		return
	}
	words := strings.Fields(r.URL.Query().Get("q"))
	if len(words) == 0 || len(words) > searchMaxWord {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("search for 1 to %d words", searchMaxWord))
		return
	}
	if _, err := os.Stat(sc.Index); errors.Is(err, os.ErrNotExist) {
		jsonError(w, http.StatusServiceUnavailable, "nothing indexed yet: run 'shelf index' on the server")
		return
	}

	// A share's files are known to clerk by the share's path, or the path its
	// symlinks lead to.
	type root struct{ dir, share string }
	var roots []root
	for _, sh := range fsys.list {
		dir := filepath.Clean(sh.Path)
		roots = append(roots, root{dir, sh.Name})
		if real, err := filepath.EvalSymlinks(dir); err == nil && real != dir {
			roots = append(roots, root{real, sh.Name})
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), searchTimeout)
	defer cancel()
	cmd := sc.command(ctx, append([]string{"find", "--json", "--"}, words...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		fsError(w, err)
		return
	}
	if err := cmd.Start(); err != nil {
		log.Printf("search: %v", err)
		jsonError(w, http.StatusInternalServerError, "search is not available")
		return
	}

	hits, truncated := []searchHit{}, false
	seen := map[string]bool{}
	lines := bufio.NewScanner(out)
	lines.Buffer(make([]byte, 64<<10), 1<<20)
	for lines.Scan() && !truncated {
		var f struct {
			Path    string  `json:"path"`
			Size    int64   `json:"size"`
			Context *string `json:"context"`
		}
		if err := json.Unmarshal(lines.Bytes(), &f); err != nil {
			log.Printf("search: clerk said %q: %v", lines.Text(), err)
			continue
		}
		for _, rt := range roots {
			rel, ok := within(rt.dir, f.Path)
			if !ok || rel == "." {
				continue
			}
			p := "/" + rt.share + "/" + filepath.ToSlash(rel)
			if seen[p] {
				continue
			}
			// Through the share's os.Root: the file is still there, and a
			// symlink inside the share does not lead out of it.
			if fi, err := fsys.Stat(ctx, p); err != nil || !fi.Mode().IsRegular() {
				continue
			}
			if len(hits) == searchMaxHits {
				truncated = true
				break
			}
			seen[p] = true
			hits = append(hits, searchHit{Path: p, Size: f.Size, Context: f.Context})
		}
	}
	if truncated {
		cancel()
	}
	err = cmd.Wait()
	if !truncated && err != nil {
		log.Printf("search: clerk: %v: %s", err, strings.TrimSpace(stderr.String()))
		jsonError(w, http.StatusInternalServerError, "search failed")
		return
	}
	writeJSON(w, map[string]any{"results": hits, "truncated": truncated})
}
