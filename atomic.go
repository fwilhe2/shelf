package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
)

// A file written whole (a WebDAV PUT, i.e. every upload) goes to a hidden
// temporary file next to it first. Only when all of it has arrived is it
// flushed to the disk and renamed over the old file, so an upload that breaks
// off never leaves half a file, nor destroys the file it was replacing.
// clerk leaves hidden files out of the index.

const uploadTempPrefix = ".shelf-upload-"

// uploadBody watches a request body, so the file written from it can tell
// whether all of it arrived.
type uploadBody struct {
	io.ReadCloser
	want int64 // Content-Length, -1 when unknown

	mu  sync.Mutex
	n   int64
	err error
}

func (b *uploadBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.mu.Lock()
	b.n += int64(n)
	if err != nil && err != io.EOF && b.err == nil {
		b.err = err
	}
	b.mu.Unlock()
	return n, err
}

// complete says whether the whole body was read.
func (b *uploadBody) complete() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.err != nil {
		return b.err
	}
	if b.want >= 0 && b.n != b.want {
		return io.ErrUnexpectedEOF
	}
	return nil
}

type uploadKey struct{}

// watchUpload wraps r's body, for the file written from it.
func watchUpload(r *http.Request) *http.Request {
	b := &uploadBody{ReadCloser: r.Body, want: r.ContentLength}
	r2 := r.WithContext(context.WithValue(r.Context(), uploadKey{}, b))
	r2.Body = b
	return r2
}

// atomicFile is the temporary file; Close puts it in place.
type atomicFile struct {
	*os.File
	ctx       context.Context
	root      *os.Root
	tmp, name string // relative to root
	failed    error
}

func isTempName(name string) bool { return strings.HasPrefix(path.Base(name), uploadTempPrefix) }

func createAtomic(ctx context.Context, root *os.Root, name string, perm os.FileMode) (*atomicFile, error) {
	if fi, err := root.Stat(name); err == nil {
		if fi.IsDir() {
			return nil, &os.PathError{Op: "open", Path: name, Err: errors.New("is a directory")}
		}
		perm = fi.Mode().Perm() // keep the old file's mode
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var rnd [8]byte
	rand.Read(rnd[:])
	tmp := path.Join(path.Dir(name), uploadTempPrefix+hex.EncodeToString(rnd[:]))
	f, err := root.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return nil, err
	}
	// The umask may have taken bits off; keep the old file's mode exactly.
	f.Chmod(perm)
	return &atomicFile{File: f, ctx: ctx, root: root, tmp: tmp, name: name}, nil
}

func (a *atomicFile) Write(p []byte) (int, error) {
	n, err := a.File.Write(p)
	if err != nil && a.failed == nil {
		a.failed = err
	}
	return n, err
}

func (a *atomicFile) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(struct{ io.Writer }{a}, r) // through Write, so errors are seen
}

func (a *atomicFile) Close() error {
	err := a.failed
	if b, ok := a.ctx.Value(uploadKey{}).(*uploadBody); ok && err == nil {
		err = b.complete()
	}
	if err == nil {
		err = a.File.Sync()
	}
	if cerr := a.File.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = a.root.Rename(a.tmp, a.name)
	}
	if err != nil {
		a.root.Remove(a.tmp)
		return err
	}
	// The rename itself is safe on the disk once the directory is.
	if d, derr := a.root.Open(path.Dir(a.name)); derr == nil {
		d.Sync()
		d.Close()
	}
	return nil
}
