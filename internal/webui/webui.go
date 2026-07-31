// Package webui serves the embedded static Web UI (go:embed, offline, zero external assets).
package webui

import (
	"embed"
	"io/fs"
	"net/http"
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

// FileServer returns an http.Handler serving the embedded UI (index.html, admin.html, style.css, app.js).
func FileServer() http.Handler {
	return http.FileServer(http.FS(webRoot))
}
