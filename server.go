package main

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/net/webdav"
)

//go:embed web/index.html
var indexHTML []byte

const (
	davPrefix     = "/dav"
	sessionCookie = "shelf_session"
	sessionTTL    = 30 * 24 * time.Hour
	authCacheTTL  = 10 * time.Minute
)

// Server serves the web UI, a small JSON API and WebDAV.
type Server struct {
	cfgPath string

	mu     sync.RWMutex
	cfg    *Config
	cfgMod time.Time

	locks     webdav.LockSystem
	sessions  sessionStore
	authCache sync.Map // cache key -> expiry (time.Time); avoids bcrypt on every WebDAV request
}

func NewServer(cfgPath string) (*Server, error) {
	s := &Server{
		cfgPath:  cfgPath,
		locks:    webdav.NewMemLS(),
		sessions: sessionStore{m: map[string]session{}},
	}
	if err := s.reload(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) config() *Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Server) reload() error {
	st, err := os.Stat(s.cfgPath)
	if err != nil {
		return err
	}
	cfg, err := LoadConfig(s.cfgPath)
	if err != nil {
		return err
	}
	for _, sh := range cfg.Shares {
		if fi, err := os.Stat(sh.Path); err != nil || !fi.IsDir() {
			log.Printf("warning: share %q: %s is not an accessible directory", sh.Name, sh.Path)
		}
	}
	s.mu.Lock()
	s.cfg, s.cfgMod = cfg, st.ModTime()
	s.mu.Unlock()
	return nil
}

// watchConfig reloads the config when the file changes, so that
// 'shelf user add' etc. take effect without a restart.
func (s *Server) watchConfig(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		st, err := os.Stat(s.cfgPath)
		if err != nil {
			continue
		}
		s.mu.RLock()
		changed := !st.ModTime().Equal(s.cfgMod)
		s.mu.RUnlock()
		if !changed {
			continue
		}
		if err := s.reload(); err != nil {
			log.Printf("config reload failed, keeping old config: %v", err)
		} else {
			log.Printf("config reloaded")
		}
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("OPTIONS /{$}", handleRootOptions) // Windows probes the server root
	mux.HandleFunc("GET /ca.crt", s.handleCACert)
	mux.HandleFunc("GET "+officePrefix, s.handleOffice)
	mux.HandleFunc("GET /vendor/", handleStatic)
	mux.HandleFunc("GET /licenses/", handleStatic)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	mux.HandleFunc("GET /api/me", s.api(s.handleMe))
	mux.HandleFunc("GET /api/shares", s.api(s.handleShares))
	mux.HandleFunc("GET /api/list", s.api(s.handleList))
	mux.HandleFunc("GET /api/zip", s.api(s.handleZip))
	mux.HandleFunc("GET /api/search", s.api(s.handleSearch))
	mux.HandleFunc(davPrefix+"/", s.handleDAV)
	mux.HandleFunc(davPrefix, func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = davPrefix + "/"
		s.handleDAV(w, r)
	})
	return mux
}

// ---------------------------------------------------------------- auth

var dummyHash = sync.OnceValue(func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcrypt.DefaultCost)
	return h
})

func (s *Server) checkPassword(name, pass string) bool {
	u := s.config().User(name)
	hash := dummyHash() // compare anyway, so unknown users take as long as known ones
	if u != nil {
		hash = []byte(u.PasswordHash)
	}
	sum := sha256.Sum256([]byte(name + "\x00" + pass + "\x00" + string(hash)))
	key := hex.EncodeToString(sum[:])
	if exp, ok := s.authCache.Load(key); ok && time.Now().Before(exp.(time.Time)) {
		return u != nil
	}
	ok := bcrypt.CompareHashAndPassword(hash, []byte(pass)) == nil && u != nil
	if ok {
		s.authCache.Store(key, time.Now().Add(authCacheTTL))
	}
	return ok
}

// currentUser returns the authenticated user name (session cookie or HTTP
// basic auth), or "" if the request is not authenticated.
func (s *Server) currentUser(r *http.Request) string {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if name, ok := s.sessions.get(c.Value); ok && s.config().User(name) != nil {
			return name
		}
	}
	if name, pass, ok := r.BasicAuth(); ok && s.checkPassword(name, pass) {
		return name
	}
	return ""
}

type session struct {
	user    string
	expires time.Time
}

type sessionStore struct {
	mu sync.Mutex
	m  map[string]session
}

func (st *sessionStore) create(user string) string {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)
	st.mu.Lock()
	defer st.mu.Unlock()
	now := time.Now()
	for k, v := range st.m {
		if now.After(v.expires) {
			delete(st.m, k)
		}
	}
	st.m[token] = session{user: user, expires: now.Add(sessionTTL)}
	return token
}

func (st *sessionStore) get(token string) (string, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	v, ok := st.m[token]
	if !ok || time.Now().After(v.expires) {
		delete(st.m, token)
		return "", false
	}
	return v.user, true
}

func (st *sessionStore) delete(token string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.m, token)
}

// ---------------------------------------------------------------- web UI + API

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Write(indexHTML)
}

func handleRootOptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("DAV", "1, 2")
	w.Header().Set("MS-Author-Via", "DAV")
	w.Header().Set("Allow", "OPTIONS, GET, HEAD, PROPFIND")
}

type apiFunc func(w http.ResponseWriter, r *http.Request, user string, fsys *nasFS)

func (s *Server) api(h apiFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := s.currentUser(r)
		if user == "" {
			jsonError(w, http.StatusUnauthorized, "not logged in")
			return
		}
		fsys := newNASFS(s.config().SharesFor(user))
		defer fsys.Close()
		h(w, r, user, fsys)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func fsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, os.ErrNotExist):
		jsonError(w, http.StatusNotFound, "not found")
	case errors.Is(err, os.ErrPermission):
		jsonError(w, http.StatusForbidden, "permission denied")
	default:
		log.Printf("error: %v", err)
		jsonError(w, http.StatusInternalServerError, err.Error())
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "bad request")
		return
	}
	if !s.checkPassword(req.Username, req.Password) {
		log.Printf("failed login for %q from %s", req.Username, r.RemoteAddr)
		time.Sleep(time.Second)
		jsonError(w, http.StatusUnauthorized, "wrong username or password")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    s.sessions.create(req.Username),
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode, // also our CSRF protection
	})
	writeJSON(w, map[string]string{"user": req.Username})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.sessions.delete(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1})
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request, user string, _ *nasFS) {
	var office []string // file extensions the office page opens; none when it is off
	if s.officeDir() != "" {
		office = officeExts
	}
	writeJSON(w, map[string]any{"user": user, "ca": s.config().TLSCA != "", "office": office, "search": s.search() != nil})
}

// handleCACert offers the CA certificate, so devices can trust it. It is
// public by nature and needed before logging in, so no authentication.
func (s *Server) handleCACert(w http.ResponseWriter, r *http.Request) {
	p := s.config().TLSCA
	if p == "" {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		log.Printf("error: %v", err)
		http.Error(w, "CA certificate not available", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-x509-ca-cert")
	w.Header().Set("Content-Disposition", `attachment; filename="shelf-ca.crt"`)
	w.Write(data)
}

type shareInfo struct {
	Name      string `json:"name"`
	ReadOnly  bool   `json:"read_only"`
	Available bool   `json:"available"`
	Total     uint64 `json:"total"`
	Free      uint64 `json:"free"`
}

func (s *Server) handleShares(w http.ResponseWriter, r *http.Request, _ string, fsys *nasFS) {
	out := []shareInfo{}
	for _, sh := range fsys.list {
		info := shareInfo{Name: sh.Name, ReadOnly: sh.ReadOnly}
		var st syscall.Statfs_t
		if fi, err := os.Stat(sh.Path); err == nil && fi.IsDir() && syscall.Statfs(sh.Path, &st) == nil {
			info.Available = true
			info.Total = st.Blocks * uint64(st.Bsize)
			info.Free = st.Bavail * uint64(st.Bsize)
		}
		out = append(out, info)
	}
	writeJSON(w, out)
}

type entry struct {
	Name     string    `json:"name"`
	Dir      bool      `json:"dir"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request, _ string, fsys *nasFS) {
	p := path.Clean("/" + r.URL.Query().Get("path"))
	ctx := r.Context()
	fi, err := fsys.Stat(ctx, p)
	if err != nil {
		fsError(w, err)
		return
	}
	if !fi.IsDir() {
		jsonError(w, http.StatusBadRequest, "not a directory")
		return
	}
	f, err := fsys.OpenFile(ctx, p, os.O_RDONLY, 0)
	if err != nil {
		fsError(w, err)
		return
	}
	infos, err := f.Readdir(-1)
	f.Close()
	if err != nil {
		fsError(w, err)
		return
	}
	entries := make([]entry, 0, len(infos))
	for _, fi := range infos {
		if isTempName(fi.Name()) { // an upload still on its way
			continue
		}
		e := entry{Name: fi.Name(), Dir: fi.IsDir(), Modified: fi.ModTime()}
		if !e.Dir {
			e.Size = fi.Size()
		}
		entries = append(entries, e)
	}
	slices.SortFunc(entries, func(a, b entry) int {
		if a.Dir != b.Dir {
			if a.Dir {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	writeJSON(w, map[string]any{
		"path":      p,
		"read_only": fsys.readOnly(p),
		"entries":   entries,
	})
}

// handleZip streams a folder as an (uncompressed) zip archive.
func (s *Server) handleZip(w http.ResponseWriter, r *http.Request, _ string, fsys *nasFS) {
	p := path.Clean("/" + r.URL.Query().Get("path"))
	if p == "/" {
		jsonError(w, http.StatusBadRequest, "choose a folder")
		return
	}
	fi, err := fsys.Stat(r.Context(), p)
	if err != nil {
		fsError(w, err)
		return
	}
	if !fi.IsDir() {
		jsonError(w, http.StatusBadRequest, "not a directory")
		return
	}
	name := path.Base(p) + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	zw := zip.NewWriter(w)
	if err := zipDir(r.Context(), zw, fsys, p, ""); err != nil {
		log.Printf("zip %s: %v", p, err)
		return // the response is already partially written; the client sees a broken download
	}
	zw.Close()
}

func zipDir(ctx context.Context, zw *zip.Writer, fsys webdav.FileSystem, dir, prefix string) error {
	f, err := fsys.OpenFile(ctx, dir, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	infos, err := f.Readdir(-1)
	f.Close()
	if err != nil {
		return err
	}
	for _, fi := range infos {
		if err := ctx.Err(); err != nil {
			return err
		}
		p, zp := path.Join(dir, fi.Name()), prefix+fi.Name()
		switch {
		case fi.IsDir():
			if _, err := zw.Create(zp + "/"); err != nil {
				return err
			}
			if err := zipDir(ctx, zw, fsys, p, zp+"/"); err != nil {
				return err
			}
		case fi.Mode().IsRegular():
			hdr, err := zip.FileInfoHeader(fi)
			if err != nil {
				return err
			}
			hdr.Name, hdr.Method = zp, zip.Store // most big files (photos, videos) are compressed already
			zf, err := zw.CreateHeader(hdr)
			if err != nil {
				return err
			}
			src, err := fsys.OpenFile(ctx, p, os.O_RDONLY, 0)
			if err != nil {
				return err
			}
			_, err = io.Copy(zf, src)
			src.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------- WebDAV

var davWriteMethods = []string{"PUT", "DELETE", "MKCOL", "MOVE", "PROPPATCH", "LOCK"}

func (s *Server) handleDAV(w http.ResponseWriter, r *http.Request) {
	user := s.currentUser(r)
	if user == "" {
		// Our own web UI sets X-Requested-With and must not trigger the browser's login popup.
		if r.Header.Get("X-Requested-With") == "" {
			w.Header().Set("WWW-Authenticate", `Basic realm="shelf", charset="UTF-8"`)
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// Uploaded HTML must not be able to run scripts on our origin.
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	fsys := newNASFS(s.config().SharesFor(user))
	defer fsys.Close()

	// Reject writes to read-only shares up front, so clients get a clean 403.
	src := strings.TrimPrefix(r.URL.Path, davPrefix)
	if slices.Contains(davWriteMethods, r.Method) && fsys.readOnly(src) {
		http.Error(w, "share is read-only", http.StatusForbidden)
		return
	}
	if r.Method == "MOVE" || r.Method == "COPY" {
		if u, err := url.Parse(r.Header.Get("Destination")); err == nil &&
			fsys.readOnly(strings.TrimPrefix(u.Path, davPrefix)) {
			http.Error(w, "destination share is read-only", http.StatusForbidden)
			return
		}
	}

	if r.Method == "PUT" {
		r = watchUpload(r)
	}
	h := &webdav.Handler{
		Prefix:     davPrefix,
		FileSystem: fsys,
		LockSystem: s.locks,
		Logger: func(r *http.Request, err error) {
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				log.Printf("webdav %s %s (%s): %v", r.Method, r.URL.Path, user, err)
			}
		},
	}
	h.ServeHTTP(w, r)
}
