package main

import (
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
)

// officePrefix is where grind's web page (an office suite in WebAssembly) is
// served, when an office directory is configured. See docs/office.md.
const officePrefix = "/office/"

// officeDir returns the directory holding grind's built web page (its dist/),
// or "" when office documents are turned off.
func (s *Server) officeDir() string {
	if d := s.config().OfficeDir; d != "" {
		return d
	}
	return os.Getenv("SHELF_OFFICE_DIR")
}

// officeExts are the files the page can open. It saves ODF only, so
// .xlsx and .docx are for viewing; edits can be downloaded as ODF.
var officeExts = []string{".ods", ".odt", ".fods", ".fodt", ".xlsx", ".docx"}

// handleOffice serves grind's page. It is public: the page is static code
// without any data, and the documents it opens come through /dav, which
// requires a login. A precompressed "name.gz" next to a file is preferred
// when the browser accepts gzip; a file that only exists as "name.gz" is
// decompressed for browsers that don't.
func (s *Server) handleOffice(w http.ResponseWriter, r *http.Request) {
	dir := s.officeDir()
	if dir == "" {
		http.NotFound(w, r)
		return
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		log.Printf("office: %v", err)
		http.NotFound(w, r)
		return
	}
	defer root.Close()

	name := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(r.URL.Path, officePrefix)), "/")
	if name == "" {
		name = "index.html"
	}
	if strings.HasSuffix(name, ".gz") {
		http.NotFound(w, r) // only ever served as an encoding of the real name
		return
	}

	w.Header().Set("Cache-Control", "no-cache") // grind's file names carry no hash
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Vary", "Accept-Encoding")
	// The web UI shows the page in a frame; nobody else may.
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'self'")
	ctype := mime.TypeByExtension(path.Ext(name))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)

	if acceptsGzip(r) {
		if f, fi, ok := openFile(root, name+".gz"); ok {
			defer f.Close()
			w.Header().Set("Content-Encoding", "gzip")
			http.ServeContent(w, r, name, fi.ModTime(), f)
			return
		}
	}
	if f, fi, ok := openFile(root, name); ok {
		defer f.Close()
		http.ServeContent(w, r, name, fi.ModTime(), f)
		return
	}
	if f, _, ok := openFile(root, name+".gz"); ok {
		defer f.Close()
		zr, err := gzip.NewReader(f)
		if err != nil {
			http.Error(w, "broken file", http.StatusInternalServerError)
			return
		}
		if r.Method != http.MethodHead {
			io.Copy(w, zr)
		}
		return
	}
	w.Header().Del("Content-Type")
	http.NotFound(w, r)
}

// openFile opens a regular file in root.
func openFile(root *os.Root, name string) (*os.File, fs.FileInfo, bool) {
	f, err := root.Open(name)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("office: %v", err)
		}
		return nil, nil, false
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		f.Close()
		return nil, nil, false
	}
	return f, fi, true
}

func acceptsGzip(r *http.Request) bool {
	for part := range strings.SplitSeq(r.Header.Get("Accept-Encoding"), ",") {
		enc, q, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(enc), "gzip") && strings.ReplaceAll(q, " ", "") != "q=0" {
			return true
		}
	}
	return false
}
