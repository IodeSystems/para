// Package webui serves the built SPA from an fs.FS: embedded in the binary, or
// a directory given by --web-root. Real files are served as they are; any other
// path gets the shell so client-side routes survive a refresh. The server
// mounts it under its own routes, so it never answers an /api path.
package webui

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

// Handler serves fsys. A nil fsys, or one with no index.html, answers 503 with a
// notice, so a build without a UI is obvious.
func Handler(fsys fs.FS) http.Handler {
	if fsys == nil {
		return notBuilt("this binary has no UI: build with -tags embedui or pass --web-root")
	}
	if _, err := fs.Stat(fsys, "index.html"); err != nil {
		return notBuilt("no index.html under --web-root")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if !fs.ValidPath(p) {
			http.NotFound(w, r)
			return
		}
		if serve(w, r, fsys, p) {
			return
		}
		// A missing hashed asset is a stale page, not a client route.
		if strings.HasPrefix(p, "assets/") {
			http.NotFound(w, r)
			return
		}
		serve(w, r, fsys, "index.html")
	})
}

func serve(w http.ResponseWriter, r *http.Request, fsys fs.FS, p string) bool {
	info, err := fs.Stat(fsys, p)
	if err != nil || info.IsDir() {
		return false
	}
	data, err := fs.ReadFile(fsys, p)
	if err != nil {
		return false
	}
	setCacheControl(w, p)
	// Embedded files have a zero mod time, so a content hash is the validator.
	sum := sha256.Sum256(data)
	w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:16])+`"`)
	if ct := contentType(p); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	http.ServeContent(w, r, path.Base(p), time.Time{}, bytes.NewReader(data))
	return true
}

// setCacheControl: vite content-hashes assets/*, so they never change. Every
// other file revalidates on each load, so a new build is seen at once.
func setCacheControl(w http.ResponseWriter, p string) {
	if strings.HasPrefix(p, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
}

// contentType returns the type for a path. Go's table has no .webmanifest, and
// browsers reject a manifest sniffed as text.
func contentType(p string) string {
	if strings.HasSuffix(p, ".webmanifest") {
		return "application/manifest+json"
	}
	return mime.TypeByExtension(path.Ext(p))
}

func notBuilt(reason string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "UI not built: "+reason+"\n")
	})
}
