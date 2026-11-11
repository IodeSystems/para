package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Real minisign output (from github.com/jedisct1/go-minisign's tests): the
// verifier agrees with the minisign CLI on both signature kinds.
func TestMinisign_RealVectors(t *testing.T) {
	pk, err := ParsePublicKey("RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3")
	if err != nil {
		t.Fatal(err)
	}
	for _, sig := range []string{
		"untrusted comment: signature from minisign secret key\nRWQf6LRCGA9i59SLOFxz6NxvASXDJeRtuZykwQepbDEGt87ig1BNpWaVWuNrm73YiIiJbq71Wi+dP9eKL8OC351vwIasSSbXxwA=\ntrusted comment: timestamp:1635442742\tfile:test\n0YteLgV960ia80vnA/fHbvkyjl/IoP/HNOCaZfrF0CdhAlp7ok+Tpkya+VpWPX5C/Is3q8a/kEDSY7fBmmgJCg==\n",
		"untrusted comment: signature from minisign secret key\nRUQf6LRCGA9i559r3g7V1qNyJDApGip8MfqcadIgT9CuhV3EMhHoN1mGTkUidF/z7SrlQgXdy8ofjb7bNJJylDOocrCo8KLzZwo=\ntrusted comment: timestamp:1635443258\tfile:test\thashed\n/cj37GK60vryibFn+ftOgbCvW9NKhKYgjVpFFQUcWPAnjO23wrvVDTt7cloNC06maoBli9q6qwZDXXoaxweICQ==\n",
	} {
		if _, err := VerifyAny([]byte("test"), sig, []PublicKey{pk}); err != nil {
			t.Errorf("real signature refused: %v", err)
		}
		if _, err := VerifyAny([]byte("tesT"), sig, []PublicKey{pk}); err == nil {
			t.Error("a signature over other bytes verified")
		}
		// The trusted comment is covered by the global signature.
		forged := strings.Replace(sig, "timestamp:16354", "timestamp:26354", 1)
		if _, err := VerifyAny([]byte("test"), forged, []PublicKey{pk}); err == nil {
			t.Error("a changed trusted comment verified")
		}
	}
	if pk.IDString() != "E7620F1842B4E81F" {
		t.Errorf("key id %s", pk.IDString())
	}
}

// The placeholder that release/minisign.pub held before the owner's key is
// no key: a release build carrying it cannot verify anything. The committed
type testKey struct {
	pub  PublicKey
	priv ed25519.PrivateKey
}

func newKey(t *testing.T) testKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var id [8]byte
	_, _ = rand.Read(id[:])
	return testKey{PublicKey{ID: id, Key: pub}, priv}
}

func (k testKey) sign(msg []byte) string {
	return Sign(k.priv, k.pub.ID, msg, "timestamp:1\tfile:para-manifest.json\thashed")
}

// tarball is a tar.gz with the given entries (name → content); a name
// starting "link:" is a symlink.
func tarball(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if n, ok := strings.CutPrefix(name, "link:"); ok {
			hdr = &tar.Header{Name: n, Linkname: body, Typeflag: tar.TypeSymlink}
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg {
			_, _ = tw.Write([]byte(body))
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func fileFor(url string, b []byte) File {
	h := sha256.Sum256(b)
	return File{URL: url, SHA256: hex.EncodeToString(h[:]), Size: int64(len(b))}
}

// server serves path → body; it counts requests.
type server struct {
	*httptest.Server
	mu    sync.Mutex
	files map[string][]byte
	hits  map[string]int
}

func newServer(t *testing.T) *server {
	s := &server{files: map[string][]byte{}, hits: map[string]int{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		b, ok := s.files[r.URL.Path]
		s.hits[r.URL.Path]++
		s.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *server) put(path string, b []byte) string {
	s.mu.Lock()
	s.files[path] = b
	s.mu.Unlock()
	return s.URL + path
}

var fetch = &Fetcher{AllowHTTP: true}

func TestInstall(t *testing.T) {
	srv := newServer(t)
	dir := t.TempDir()
	dst := filepath.Join(dir, "para")
	good := tarball(t, map[string]string{"para": "#!/bin/sh\necho hi\n", "LICENSE": "MIT"})
	f := fileFor(srv.put("/good.tar.gz", good), good)

	if _, err := fetch.Install(context.Background(), f, "para", dst); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dst)
	st, _ := os.Stat(dst)
	if err != nil || string(b) != "#!/bin/sh\necho hi\n" || st.Mode().Perm() != 0o755 {
		t.Fatalf("installed %q %v %v", b, st.Mode(), err)
	}

	bad := func(name string, f File) {
		t.Helper()
		os.WriteFile(dst, []byte("old"), 0o755)
		_, err := fetch.Install(context.Background(), f, "para", dst)
		if err == nil || !IsVerify(err) {
			t.Errorf("%s: want a refused check, got %v", name, err)
		}
		if b, _ := os.ReadFile(dst); string(b) != "old" {
			t.Errorf("%s: the target changed", name)
		}
		left, _ := os.ReadDir(dir)
		if len(left) != 1 {
			t.Errorf("%s: temp files left: %v", name, left)
		}
	}
	wrong := f
	wrong.SHA256 = strings.Repeat("0", 64)
	bad("wrong sha256", wrong)
	trunc := good[:len(good)-10]
	short := fileFor(srv.put("/short.tar.gz", trunc), good) // the manifest's size and hash, a short body
	bad("truncated download", short)
	long := f
	long.Size--
	bad("longer than the manifest", long)
	for name, entries := range map[string]map[string]string{
		"path traversal":  {"../para": "x"},
		"absolute path":   {"/usr/bin/para": "x"},
		"nested traverse": {"a/../../para": "x"},
		"symlink":         {"link:para": "/bin/sh"},
		"missing":         {"other": "x"},
	} {
		tb := tarball(t, entries)
		bad(name, fileFor(srv.put("/"+strings.ReplaceAll(name, " ", "-"), tb), tb))
	}
	// Plain HTTP is refused outside tests.
	if _, err := (&Fetcher{}).Install(context.Background(), f, "para", dst); err == nil || !strings.Contains(err.Error(), "https") {
		t.Errorf("http download: %v", err)
	}
	// A binary in one directory (name_os_arch/para) is found.
	nested := tarball(t, map[string]string{"para_linux_amd64/para": "nested"})
	if _, err := fetch.Install(context.Background(), fileFor(srv.put("/nested", nested), nested), "para", dst); err != nil {
		t.Fatal(err)
	}
}

func TestVersion(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"v0.3.0", "v0.4.0", -1}, {"v1.10.0", "v1.9.9", 1}, {"v1.0.0", "v1.0.0", 0},
		{"v1.0.0-rc1", "v1.0.0", -1}, {"v1.0.0+meta", "v1.0.0", 0},
	} {
		a, _ := ParseVersion(c.a)
		b, _ := ParseVersion(c.b)
		if got := a.Compare(b); got != c.want {
			t.Errorf("%s vs %s = %d", c.a, c.b, got)
		}
	}
	for _, s := range []string{"dev", "0.3.0", "v0.3", "v01.2.3"} {
		if _, err := ParseVersion(s); err == nil {
			t.Errorf("%q parsed", s)
		}
	}
}

// The committed key is dun's release key, one line, ID 2CC27A171DE03D42.
func TestReleaseKey(t *testing.T) {
	b, err := os.ReadFile("../../release/minisign.pub")
	if err != nil {
		t.Fatal(err)
	}
	keys, err := ParseKeys(string(b))
	if err != nil || len(keys) != 1 || keys[0].IDString() != "2CC27A171DE03D42" {
		t.Fatalf("release/minisign.pub: %v %v", keys, err)
	}
}

// manifestFor is a manifest of product at version with one platform.
func manifestFor(t *testing.T, product, version, published string, plats map[string]File) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"product": product, "version": version, "published": published, "summary": "s", "platforms": plats})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// rel is a release on the test server: the archive and a manifest signed by
// k with trusted, at /latest/para-manifest.json.
func rel(t *testing.T, srv *server, k testKey, product, version, published, trusted string) {
	t.Helper()
	tb := tarball(t, map[string]string{"para": "para " + version})
	f := fileFor(srv.put("/"+version+"/para_linux_amd64.tar.gz", tb), tb)
	body := manifestFor(t, product, version, published, map[string]File{"linux_amd64": f})
	srv.put("/latest/para-manifest.json", body)
	srv.put("/latest/para-manifest.json.minisig", []byte(Sign(k.priv, k.pub.ID, body, trusted)))
}

func upgrader(srv *server, k testKey, dir, current string) *Upgrader {
	return &Upgrader{
		Product: "para", ManifestURL: srv.URL + "/latest/para-manifest.json", Keys: []PublicKey{k.pub},
		Current: current, Exe: filepath.Join(dir, "bin", "para"), StatePath: StateFile(dir),
		Platform: "linux_amd64", Fetch: fetch,
	}
}

func TestUpgraderInstallsANewerRelease(t *testing.T) {
	srv, k, dir := newServer(t), newKey(t), t.TempDir()
	rel(t, srv, k, "para", "v0.4.0", "2026-11-10T00:00:00Z", "para v0.4.0 manifest")
	u := upgrader(srv, k, dir, "v0.3.0")
	res, err := u.Run(context.Background())
	if err != nil || !res.Installed || res.To != "v0.4.0" {
		t.Fatalf("%+v %v", res, err)
	}
	if b, _ := os.ReadFile(u.Exe); string(b) != "para v0.4.0" {
		t.Fatalf("installed %q", b)
	}
	if st := LoadState(u.StatePath); st.Version != "v0.4.0" {
		t.Fatalf("state %+v", st)
	}
	// The same version again installs nothing.
	if res, err := upgrader(srv, k, dir, "v0.4.0").Run(context.Background()); err != nil || res.Installed {
		t.Fatalf("current: %+v %v", res, err)
	}
}

// A manifest that fails a check is refused and leaves no state and no binary.
func TestUpgraderRefuses(t *testing.T) {
	other := newKey(t)
	for name, c := range map[string]struct {
		product, trusted string
		signer           *testKey
	}{
		"bad signature":                 {"para", "para v0.4.0 manifest", &other},
		"dun manifest (shared key)":     {"dun", "dun v0.4.0 manifest", nil},
		"product para, dun comment":     {"para", "dun v0.4.0 manifest", nil},
		"product dun, para comment":     {"dun", "para v0.4.0 manifest", nil},
		"comment names another version": {"para", "para v0.3.9 manifest", nil},
		"timestamp comment":             {"para", "timestamp:1\tfile:para-manifest.json\thashed", nil},
	} {
		srv, k, dir := newServer(t), newKey(t), t.TempDir()
		signer := k
		if c.signer != nil {
			signer = *c.signer
		}
		rel(t, srv, signer, c.product, "v0.4.0", "2026-11-10T00:00:00Z", c.trusted)
		u := upgrader(srv, k, dir, "v0.3.0")
		if _, err := u.Run(context.Background()); err == nil || !IsVerify(err) {
			t.Errorf("%s: want a refused check, got %v", name, err)
		}
		if _, err := os.Stat(u.StatePath); err == nil {
			t.Errorf("%s: state written", name)
		}
		if _, err := os.Stat(u.Exe); err == nil {
			t.Errorf("%s: binary installed", name)
		}
	}
}

// A dun manifest is refused even when the bytes verify: the signature is
// good, the product and the trusted comment are dun's.
func TestVerifyManifestRefusesDun(t *testing.T) {
	k := newKey(t)
	f := File{URL: "https://example.invalid/dun_linux_amd64.tar.gz", SHA256: strings.Repeat("a", 64), Size: 1}
	body := manifestFor(t, "dun", "v0.4.0", "2026-11-10T00:00:00Z", map[string]File{"linux_amd64": f})
	sig := Sign(k.priv, k.pub.ID, body, "dun v0.4.0 manifest")
	if _, err := VerifyAny(body, sig, []PublicKey{k.pub}); err != nil {
		t.Fatalf("the signature itself should verify: %v", err)
	}
	if _, err := VerifyManifest(body, sig, []PublicKey{k.pub}, "dun"); err != nil {
		t.Fatalf("as dun: %v", err)
	}
	if _, err := VerifyManifest(body, sig, []PublicKey{k.pub}, "para"); err == nil {
		t.Fatal("a dun manifest verified as para")
	}
}

func TestUpgraderRefusesDowngradeAndReplay(t *testing.T) {
	srv, k, dir := newServer(t), newKey(t), t.TempDir()
	rel(t, srv, k, "para", "v0.3.0", "2026-11-01T00:00:00Z", "para v0.3.0 manifest")
	u := upgrader(srv, k, dir, "v0.4.0")
	if _, err := u.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "downgrade") {
		t.Fatalf("downgrade: %v", err)
	}
	if _, err := os.Stat(u.Exe); err == nil {
		t.Fatal("a downgrade installed")
	}
	u.Force = true
	if res, err := u.Run(context.Background()); err != nil || !res.Installed {
		t.Fatalf("forced downgrade: %+v %v", res, err)
	}

	// Seen v0.5.0; an older manifest served later is a replay, even forced.
	rel(t, srv, k, "para", "v0.5.0", "2026-11-05T00:00:00Z", "para v0.5.0 manifest")
	if _, err := upgrader(srv, k, dir, "v0.4.0").Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	rel(t, srv, k, "para", "v0.4.1", "2026-11-03T00:00:00Z", "para v0.4.1 manifest")
	u = upgrader(srv, k, dir, "v0.3.0")
	u.Force = true
	if _, err := u.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "replay") {
		t.Fatalf("replay: %v", err)
	}
	if st := LoadState(u.StatePath); st.Version != "v0.5.0" {
		t.Fatalf("state after a replay: %+v", st)
	}
	// A manifest of the same version published earlier is a replay too.
	rel(t, srv, k, "para", "v0.5.0", "2026-11-04T00:00:00Z", "para v0.5.0 manifest")
	if _, err := upgrader(srv, k, dir, "v0.4.0").Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "replay") {
		t.Fatalf("earlier publication: %v", err)
	}
}

// key_next in a manifest is ignored: para never trusts a key from one.
func TestKeyNextIgnored(t *testing.T) {
	k, next := newKey(t), newKey(t)
	f := File{URL: "https://example.invalid/para_linux_amd64.tar.gz", SHA256: strings.Repeat("a", 64), Size: 1}
	body, _ := json.Marshal(map[string]any{"product": "para", "version": "v0.4.0", "published": "2026-11-10T00:00:00Z",
		"platforms": map[string]File{"linux_amd64": f},
		"key_next":  map[string]string{"key": next.pub.Line(), "signature": Sign(k.priv, k.pub.ID, []byte(next.pub.Line()), "endorse")}})
	if _, err := VerifyManifest(body, Sign(k.priv, k.pub.ID, body, "para v0.4.0 manifest"), []PublicKey{k.pub}, "para"); err != nil {
		t.Fatal(err)
	}
	body2 := manifestFor(t, "para", "v0.5.0", "2026-11-11T00:00:00Z", map[string]File{"linux_amd64": f})
	if _, err := VerifyManifest(body2, Sign(next.priv, next.pub.ID, body2, "para v0.5.0 manifest"), []PublicKey{k.pub}, "para"); err == nil {
		t.Fatal("a manifest signed by the next key verified")
	}
}
