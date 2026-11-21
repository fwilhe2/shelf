package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The server keeps the search index current by itself: it runs `shelf
// index` (runIndex) a while after something was changed through it, every
// "every" (an hour by default, for files changed on the disk directly), and
// when asked to in the web UI. GET /api/index tells how current it is.

const (
	defaultIndexEvery = time.Hour
	indexAfterStart   = time.Minute
	indexAfterChange  = 30 * time.Second
)

var errIndexBusy = errors.New("another index run is under way")

// every is how often the server indexes by itself; 0 for never.
func (sc *SearchConfig) every() (time.Duration, error) {
	s := sc.Every
	if s == "" {
		s = os.Getenv("SHELF_INDEX_EVERY")
	}
	switch s {
	case "":
		return defaultIndexEvery, nil
	case "off", "0":
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < time.Minute {
		return 0, fmt.Errorf("search.every must be a duration of at least 1m, or \"off\", got %q", s)
	}
	return d, nil
}

// runIndex has clerk read every share into the index, writing its report to
// out. While another run is under way, it waits for it with wait, else it
// returns errIndexBusy.
func runIndex(cfg *Config, out io.Writer, wait bool) error {
	sc := searchConfig(cfg)
	if sc == nil {
		return errors.New(`search is off: set "search": {"index": "/var/lib/shelf/clerk.sqlite"} in the config (see docs/search.md)`)
	}
	if err := sc.validate(cfg.Shares); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(sc.Index), 0o750); err != nil {
		return err
	}
	lock, err := os.OpenFile(sc.Index+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := flock(lock, false); err != nil {
		if !wait {
			return errIndexBusy
		}
		log.Printf("another index run is under way (shelf serve indexes by itself); waiting for it")
		if err := flock(lock, true); err != nil {
			return err
		}
	}

	var dirs []string
	for _, sh := range cfg.Shares {
		if fi, err := os.Stat(sh.Path); err != nil || !fi.IsDir() {
			log.Printf("warning: share %q: %s is not an accessible directory, not indexed", sh.Name, sh.Path)
			continue
		}
		dirs = append(dirs, sh.Path)
	}
	if len(dirs) == 0 {
		return errors.New("no share to index")
	}
	var stderr bytes.Buffer
	cmd := sc.command(context.Background(), append([]string{"scan", "--"}, dirs...)...)
	cmd.Stdout, cmd.Stderr = out, io.MultiWriter(os.Stderr, &stderr)
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("clerk: %s", msg)
		}
		return fmt.Errorf("clerk: %w", err)
	}
	return nil
}

type indexer struct {
	mu                sync.Mutex
	running, queued   bool
	started, finished time.Time
	next              time.Time
	err               string
	reports           map[string]string // share directory -> clerk's report of the last run

	kick chan struct{}
	soon *time.Timer
}

// indexNow asks the loop to index as soon as it can.
func (s *Server) indexNow() {
	s.idx.mu.Lock()
	s.idx.queued = true
	s.idx.mu.Unlock()
	select {
	case s.idx.kick <- struct{}{}:
	default:
	}
}

// indexSoon indexes a while after the last change.
func (s *Server) indexSoon() {
	if s.search() == nil {
		return
	}
	s.idx.mu.Lock()
	defer s.idx.mu.Unlock()
	if s.idx.soon == nil {
		s.idx.soon = time.AfterFunc(indexAfterChange, s.indexNow)
	} else {
		s.idx.soon.Reset(indexAfterChange)
	}
	if t := time.Now().Add(indexAfterChange); s.idx.next.IsZero() || t.Before(s.idx.next) {
		s.idx.next = t
	}
}

// indexLoop runs until ctx ends.
func (s *Server) indexLoop(ctx context.Context) {
	wait := indexAfterStart
	for {
		var timer <-chan time.Time // none: only when asked to
		next := time.Time{}
		if wait > 0 {
			timer = time.After(wait)
			next = time.Now().Add(wait)
		}
		s.idx.mu.Lock()
		// A run after a change during the last run is due sooner.
		if soon := s.idx.next; soon.After(time.Now()) && (next.IsZero() || soon.Before(next)) {
			next = soon
		}
		s.idx.next = next
		s.idx.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-timer:
		case <-s.idx.kick:
		}
		sc := s.search()
		if sc == nil { // search is off; look again later, the config may change
			wait = time.Minute
			continue
		}
		s.runIndexed()
		var err error
		if wait, err = sc.every(); err != nil {
			log.Printf("warning: %v; indexing every %v", err, defaultIndexEvery)
			wait = defaultIndexEvery
		}
	}
}

var reportRe = regexp.MustCompile(`^(.+): (\d+ files, .*)$`)

func (s *Server) runIndexed() {
	s.idx.mu.Lock()
	s.idx.running, s.idx.queued = true, false
	s.idx.started, s.idx.next = time.Now(), time.Time{}
	s.idx.mu.Unlock()

	var out bytes.Buffer
	err := runIndex(s.config(), &out, false)
	reports := map[string]string{}
	lines := bufio.NewScanner(&out)
	var last string
	for lines.Scan() {
		l := lines.Text()
		if m := reportRe.FindStringSubmatch(l); m != nil {
			last = filepath.Clean(m[1])
			reports[last] = m[2]
		} else if last != "" && strings.HasPrefix(l, "  ") {
			reports[last] += ";" + strings.TrimPrefix(l, " ") // "  123 with text"
		}
	}
	if err != nil {
		log.Printf("index: %v", err)
	} else {
		log.Printf("index: done in %v", time.Since(s.idx.started).Round(time.Second))
	}

	s.idx.mu.Lock()
	defer s.idx.mu.Unlock()
	s.idx.running, s.idx.finished, s.idx.err = false, time.Now(), ""
	if err != nil {
		s.idx.err = err.Error()
	}
	if len(reports) > 0 {
		s.idx.reports = reports
	}
}

// clerkStatus is what `clerk status` says.
type clerkStatus struct {
	files, documents, withText, awaitingOCR int
	folders                                 map[string]clerkFolder
}

type clerkFolder struct {
	files   int
	scanned *time.Time
}

func (sc *SearchConfig) status(ctx context.Context) (*clerkStatus, error) {
	if _, err := os.Stat(sc.Index); err != nil {
		return nil, err
	}
	out, err := sc.command(ctx, "status").Output()
	if err != nil {
		return nil, err
	}
	st := &clerkStatus{folders: map[string]clerkFolder{}}
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Split(l, "\t")
		num := func(i int) int {
			if i >= len(f) {
				return 0
			}
			n, _ := strconv.Atoi(strings.TrimSuffix(f[i], " files"))
			return n
		}
		switch f[0] {
		case "files":
			st.files = num(1)
		case "documents":
			st.documents = num(1)
		case "with text":
			st.withText = num(1)
		case "awaiting OCR":
			st.awaitingOCR = num(1)
		case "folder":
			if len(f) < 4 {
				continue
			}
			cf := clerkFolder{files: num(2)}
			if t, err := time.Parse("2006-01-02 15:04 MST", strings.TrimPrefix(f[3], "scanned ")); err == nil {
				cf.scanned = &t
			}
			st.folders[filepath.Clean(f[1])] = cf
		}
	}
	return st, nil
}

func timeOrNil(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

type indexShare struct {
	Name    string     `json:"name"`
	Files   *int       `json:"files"`   // nil when never indexed
	Scanned *time.Time `json:"scanned"` // when clerk last read it
	Report  string     `json:"report,omitempty"`
}

// handleIndexStatus: how current the index is, for the user's shares.
func (s *Server) handleIndexStatus(w http.ResponseWriter, r *http.Request, user string, fsys *nasFS) {
	sc := s.search()
	if sc == nil {
		jsonError(w, http.StatusNotFound, "search is off")
		return
	}
	every, everyErr := sc.every()
	if everyErr != nil {
		every = defaultIndexEvery
	}
	ctx, cancel := context.WithTimeout(r.Context(), searchTimeout)
	defer cancel()
	st, err := sc.status(ctx)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("index status: %v", err)
	}

	s.idx.mu.Lock()
	resp := map[string]any{
		"running":  s.idx.running,
		"queued":   s.idx.queued,
		"started":  timeOrNil(s.idx.started),
		"finished": timeOrNil(s.idx.finished),
		"next":     timeOrNil(s.idx.next),
		"error":    s.idx.err,
		"every":    int(every.Seconds()),
	}
	reports := s.idx.reports
	s.idx.mu.Unlock()

	shares := []indexShare{}
	for _, sh := range fsys.list {
		is := indexShare{Name: sh.Name}
		dir := filepath.Clean(sh.Path)
		if st != nil {
			if f, ok := st.folders[dir]; ok {
				is.Files, is.Scanned = &f.files, f.scanned
			}
		}
		is.Report = reports[dir]
		shares = append(shares, is)
	}
	resp["shares"] = shares
	// The totals include every share, so only for who may see them all.
	if st != nil && len(fsys.list) == len(s.config().Shares) {
		resp["totals"] = map[string]int{
			"files": st.files, "documents": st.documents,
			"with_text": st.withText, "awaiting_ocr": st.awaitingOCR,
		}
	}
	writeJSON(w, resp)
}

// handleIndexStart starts indexing now.
func (s *Server) handleIndexStart(w http.ResponseWriter, r *http.Request, _ string, _ *nasFS) {
	if s.search() == nil {
		jsonError(w, http.StatusNotFound, "search is off")
		return
	}
	s.indexNow()
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]bool{"queued": true})
}
