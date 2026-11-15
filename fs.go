package main

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/webdav"
)

// This file implements the file system that WebDAV and the web UI see.
//
//	/                -> virtual directory listing the user's shares
//	/<share>/a/b.txt -> a/b.txt inside the share's directory
//
// Every share is accessed through an os.Root, so neither "..", absolute paths
// nor symlinks can escape the share directory.

const writeFlags = os.O_WRONLY | os.O_RDWR | os.O_CREATE | os.O_TRUNC | os.O_APPEND

var startTime = time.Now()

// relPath converts a slash-separated WebDAV name into a path relative to a root.
func relPath(name string) string {
	name = path.Clean("/" + name)
	if name == "/" {
		return "."
	}
	return name[1:]
}

// splitPath splits "/share/rest/of/path" into "share" and "/rest/of/path".
func splitPath(name string) (share, rest string) {
	name = path.Clean("/" + name)
	if name == "/" {
		return "", "/"
	}
	share, rest, _ = strings.Cut(name[1:], "/")
	return share, "/" + rest
}

// shareFS is a webdav.FileSystem for a single share.
type shareFS struct {
	root     *os.Root
	readOnly bool
}

func (s *shareFS) Mkdir(_ context.Context, name string, perm os.FileMode) error {
	if s.readOnly {
		return os.ErrPermission
	}
	return s.root.Mkdir(relPath(name), perm)
}

func (s *shareFS) OpenFile(_ context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	if s.readOnly && flag&writeFlags != 0 {
		return nil, os.ErrPermission
	}
	f, err := s.root.OpenFile(relPath(name), flag, perm)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (s *shareFS) RemoveAll(_ context.Context, name string) error {
	p := relPath(name)
	if s.readOnly || p == "." {
		return os.ErrPermission
	}
	return s.root.RemoveAll(p)
}

func (s *shareFS) Rename(_ context.Context, oldName, newName string) error {
	o, n := relPath(oldName), relPath(newName)
	if s.readOnly || o == "." || n == "." {
		return os.ErrPermission
	}
	return s.root.Rename(o, n)
}

func (s *shareFS) Stat(_ context.Context, name string) (os.FileInfo, error) {
	return s.root.Stat(relPath(name))
}

// nasFS is the per-request view of all shares a user may access. Share roots
// are opened lazily and closed with Close, so no file descriptors are held
// between requests (disks can be unmounted and remounted freely).
type nasFS struct {
	list   []Share // sorted by name
	shares map[string]Share

	mu   sync.Mutex
	open map[string]*shareFS
}

func newNASFS(shares []Share) *nasFS {
	n := &nasFS{shares: map[string]Share{}, open: map[string]*shareFS{}}
	for _, s := range shares {
		n.list = append(n.list, s)
		n.shares[s.Name] = s
	}
	slices.SortFunc(n.list, func(a, b Share) int { return strings.Compare(a.Name, b.Name) })
	return n
}

func (n *nasFS) Close() {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, s := range n.open {
		s.root.Close()
	}
	clear(n.open)
}

func (n *nasFS) share(name string) (*shareFS, error) {
	sh, ok := n.shares[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if s, ok := n.open[name]; ok {
		return s, nil
	}
	root, err := os.OpenRoot(sh.Path)
	if err != nil {
		return nil, err
	}
	s := &shareFS{root: root, readOnly: sh.ReadOnly}
	n.open[name] = s
	return s, nil
}

// readOnly reports whether writes below name are forbidden.
func (n *nasFS) readOnly(name string) bool {
	share, _ := splitPath(name)
	sh, ok := n.shares[share]
	return !ok || sh.ReadOnly
}

func (n *nasFS) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	share, rest := splitPath(name)
	if share == "" || rest == "/" {
		return os.ErrPermission
	}
	s, err := n.share(share)
	if err != nil {
		return err
	}
	return s.Mkdir(ctx, rest, perm)
}

func (n *nasFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	share, rest := splitPath(name)
	if share == "" {
		if flag&writeFlags != 0 {
			return nil, os.ErrPermission
		}
		return &rootDir{n: n}, nil
	}
	s, err := n.share(share)
	if err != nil {
		return nil, err
	}
	return s.OpenFile(ctx, rest, flag, perm)
}

func (n *nasFS) RemoveAll(ctx context.Context, name string) error {
	share, rest := splitPath(name)
	if share == "" || rest == "/" {
		return os.ErrPermission
	}
	s, err := n.share(share)
	if err != nil {
		return err
	}
	return s.RemoveAll(ctx, rest)
}

func (n *nasFS) Rename(ctx context.Context, oldName, newName string) error {
	oldShare, oldRest := splitPath(oldName)
	newShare, newRest := splitPath(newName)
	if oldShare == "" || oldShare != newShare {
		return os.ErrPermission // moving between shares is not supported
	}
	s, err := n.share(oldShare)
	if err != nil {
		return err
	}
	return s.Rename(ctx, oldRest, newRest)
}

func (n *nasFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	share, rest := splitPath(name)
	if share == "" {
		return dirInfo{name: "/", mod: startTime}, nil
	}
	s, err := n.share(share)
	if err != nil {
		return nil, err
	}
	fi, err := s.Stat(ctx, rest)
	if err != nil {
		return nil, err
	}
	if rest == "/" {
		return namedInfo{fi, share}, nil
	}
	return fi, nil
}

func (n *nasFS) rootInfos() []fs.FileInfo {
	var out []fs.FileInfo
	for _, sh := range n.list {
		s, err := n.share(sh.Name)
		if err != nil {
			continue // e.g. disk not mounted
		}
		fi, err := s.root.Stat(".")
		if err != nil {
			continue
		}
		out = append(out, namedInfo{fi, sh.Name})
	}
	return out
}

// rootDir is the virtual directory "/" containing one entry per share.
type rootDir struct {
	n      *nasFS
	infos  []fs.FileInfo
	loaded bool
}

func (d *rootDir) Close() error { return nil }
func (d *rootDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: "/", Err: syscall.EISDIR}
}
func (d *rootDir) Write([]byte) (int, error)      { return 0, os.ErrPermission }
func (d *rootDir) Seek(int64, int) (int64, error) { return 0, nil }
func (d *rootDir) Stat() (fs.FileInfo, error)     { return dirInfo{name: "/", mod: startTime}, nil }

func (d *rootDir) Readdir(count int) ([]fs.FileInfo, error) {
	if !d.loaded {
		d.infos, d.loaded = d.n.rootInfos(), true
	}
	if count <= 0 {
		out := d.infos
		d.infos = nil
		return out, nil
	}
	if len(d.infos) == 0 {
		return nil, io.EOF
	}
	k := min(count, len(d.infos))
	out := d.infos[:k]
	d.infos = d.infos[k:]
	return out, nil
}

type namedInfo struct {
	fs.FileInfo
	name string
}

func (i namedInfo) Name() string { return i.name }

type dirInfo struct {
	name string
	mod  time.Time
}

func (d dirInfo) Name() string       { return d.name }
func (d dirInfo) Size() int64        { return 0 }
func (d dirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o555 }
func (d dirInfo) ModTime() time.Time { return d.mod }
func (d dirInfo) IsDir() bool        { return true }
func (d dirInfo) Sys() any           { return nil }
