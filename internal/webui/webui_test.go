package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func fsys() fstest.MapFS {
	return fstest.MapFS{
		"index.html":           {Data: []byte("<html>shell</html>")},
		"assets/app-abc.js":    {Data: []byte("console.log(1)")},
		"manifest.webmanifest": {Data: []byte("{}")},
		"sw.js":                {Data: []byte("// sw")},
	}
}

func get(h http.Handler, method, path string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestCaching(t *testing.T) {
	h := Handler(fsys())
	w := get(h, http.MethodGet, "/assets/app-abc.js", nil)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	for _, p := range []string{"/", "/index.html", "/sw.js", "/manifest.webmanifest", "/deep/link"} {
		if cc := get(h, http.MethodGet, p, nil).Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("%s: %q", p, cc)
		}
	}
}

// Embedded files have a zero mod time, so the ETag is what gives a 304.
func TestETagRevalidates(t *testing.T) {
	h := Handler(fsys())
	etag := get(h, http.MethodGet, "/", nil).Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	if w := get(h, http.MethodGet, "/", map[string]string{"If-None-Match": etag}); w.Code != http.StatusNotModified {
		t.Errorf("%d", w.Code)
	}
}

func TestContentTypes(t *testing.T) {
	h := Handler(fsys())
	if ct := get(h, http.MethodGet, "/manifest.webmanifest", nil).Header().Get("Content-Type"); ct != "application/manifest+json" {
		t.Errorf("%q", ct)
	}
	if ct := get(h, http.MethodGet, "/", nil).Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("%q", ct)
	}
}

func TestSPAFallback(t *testing.T) {
	h := Handler(fsys())
	for _, p := range []string{"/", "/index.html", "/project/x"} {
		w := get(h, http.MethodGet, p, nil)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "shell") {
			t.Errorf("%s: %d %s", p, w.Code, w.Body)
		}
	}
	if w := get(h, http.MethodGet, "/assets/gone-123.js", nil); w.Code != 404 {
		t.Errorf("a missing asset is not a route: %d", w.Code)
	}
	if w := get(h, http.MethodPost, "/", nil); w.Code != 405 {
		t.Errorf("POST: %d", w.Code)
	}
}

func TestNotBuilt(t *testing.T) {
	for _, h := range []http.Handler{Handler(nil), Handler(fstest.MapFS{})} {
		if w := get(h, http.MethodGet, "/", nil); w.Code != 503 {
			t.Errorf("%d", w.Code)
		}
	}
}
