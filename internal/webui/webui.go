// Package webui serves the embedded static Web UI (go:embed, offline, zero external assets).
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"time"
)

//go:embed web/*
var webFiles embed.FS

// webRoot is the embedded web/ directory exposed as an fs.FS.
var webRoot = mustSub(webFiles, "web")

// mustSub returns fs.Sub(fsys, dir), panicking on the static embed failure.
func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

// assetModTime is the build time of this binary, used as the Last-Modified
// stamp for embedded assets. go:embed files report a zero modtime, which
// disables http.FileServer's conditional-request support entirely (no
// Last-Modified header, no 304s) — browsers then re-download every asset in
// full on each page load. Stamping with the executable's mtime restores
// revalidation: an unchanged binary yields 304s, and a rebuilt binary (new
// mtime) yields fresh assets on the first request after deploy.
var assetModTime = executableModTime()

func executableModTime() time.Time {
	if exe, err := os.Executable(); err == nil {
		if fi, err := os.Stat(exe); err == nil {
			return fi.ModTime()
		}
	}
	return time.Now()
}

// stampedFS overlays a fixed ModTime on every file it opens so that
// http.FileServer emits Last-Modified and honors If-Modified-Since.
type stampedFS struct {
	fs.FS
	modTime time.Time
}

func (s stampedFS) Open(name string) (fs.File, error) {
	f, err := s.FS.Open(name)
	if err != nil {
		return nil, err
	}
	return stampedFile{File: f, modTime: s.modTime}, nil
}

type stampedFile struct {
	fs.File
	modTime time.Time
}

func (f stampedFile) Stat() (fs.FileInfo, error) {
	fi, err := f.File.Stat()
	if err != nil {
		return nil, err
	}
	return stampedInfo{fi, f.modTime}, nil
}

type stampedInfo struct {
	fs.FileInfo
	modTime time.Time
}

func (i stampedInfo) ModTime() time.Time { return i.modTime }

// staticHandler serves the stamped embedded tree; built once at init.
var staticHandler = http.FileServer(http.FS(stampedFS{webRoot, assetModTime}))

// FileServer returns an http.Handler serving the embedded UI (index.html,
// watchlist.html, results.html, data.html, admin.html, style.css, app.js,
// favicon.svg). Every response carries Cache-Control:
// no-cache plus Last-Modified (binary build time), so browsers revalidate on
// each load instead of ever serving a stale asset after a rebuild/deploy —
// and unchanged assets round-trip as cheap 304s.
func FileServer() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		staticHandler.ServeHTTP(w, r)
	})
}
