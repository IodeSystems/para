package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/netguard"
	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/opsample"
)

// multipartBody is a form with the fields id and file.
func multipartBody(t *testing.T, id, name, ctype string, data []byte) (string, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	if err := w.WriteField("id", id); err != nil {
		t.Fatal(err)
	}
	h := make(map[string][]string)
	h["Content-Disposition"] = []string{`form-data; name="file"; filename="` + name + `"`}
	h["Content-Type"] = []string{ctype}
	part, err := w.CreatePart(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.String(), w.FormDataContentType()
}

func TestAttachmentUpload(t *testing.T) {
	f := newFixture(t)
	tok := f.key(f.keyA, "owner", allScopes...)
	cookie := netguard.CookieName + "=" + f.session(tok)
	body, ctype := multipartBody(t, "task-sample", "board photo.png", "image/png", []byte("\x89PNG\r\n\x1a\n synthetic"))

	// A session of a review:decide key stores the file.
	w := f.do(call{method: post, target: pathAttachUpload, body: body,
		header: map[string]string{"Cookie": cookie, "Origin": "http://" + hostA, "Content-Type": ctype}})
	wantStatus(t, w, 200)
	var out ops.AttachmentAddOutput
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.Path, "attachments/task-sample/") || !strings.HasSuffix(out.Path, "-board-photo.png") || out.Mime != "image/png" {
		t.Fatalf("out = %+v", out)
	}

	// The page reads it back from the file route, sandboxed.
	g := f.do(call{target: pathAttachFile + "?path=" + url.QueryEscape(out.Path), header: map[string]string{"Cookie": cookie}})
	wantStatus(t, g, 200)
	if g.Header().Get("Content-Type") != "image/png" || !strings.Contains(g.Header().Get("Content-Security-Policy"), "sandbox") ||
		!strings.HasPrefix(g.Header().Get("Content-Disposition"), "inline") || !strings.Contains(g.Body.String(), "synthetic") {
		t.Fatalf("file: %v %q", g.Header(), g.Body.String())
	}
	// A text file is served as plain text; a path out of attachments/ is not found.
	g = f.do(call{target: pathAttachFile + "?path=" + url.QueryEscape(opsample.SampleAttachment), token: tok})
	wantStatus(t, g, 200)
	if g.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("text type %q", g.Header().Get("Content-Type"))
	}
	wantStatus(t, f.do(call{target: pathAttachFile + "?path=" + url.QueryEscape("attachments/task-sample/../../.para/vault.properties"), token: tok}), 404)

	// A bearer key is not a person: attachment_add refuses it.
	wantStatus(t, f.do(call{method: post, target: pathAttachUpload, body: body, token: tok, header: map[string]string{"Content-Type": ctype}}), 403)
	// No key, a key without review:decide, and a cross-site post are refused at the gate.
	wantStatus(t, f.do(call{method: post, target: pathAttachUpload, body: body, header: map[string]string{"Content-Type": ctype}}), 401)
	wantStatus(t, f.do(call{target: pathAttachFile + "?path=" + url.QueryEscape(out.Path)}), 401)
	ro := f.key(f.keyA, "ro", netguard.ScopeVaultRead)
	roCookie := netguard.CookieName + "=" + f.session(ro)
	wantStatus(t, f.do(call{method: post, target: pathAttachUpload, body: body,
		header: map[string]string{"Cookie": roCookie, "Origin": "http://" + hostA, "Content-Type": ctype}}), 403)
	wantStatus(t, f.do(call{method: post, target: pathAttachUpload, body: body,
		header: map[string]string{"Cookie": cookie, "Origin": "http://evil.example", "Content-Type": ctype}}), 403)

	// Over the vault's limit: 413, and nothing is stored.
	if err := f.store(t).SetConfig(t.Context(), "attachments.max_mb", "1"); err != nil {
		t.Fatal(err)
	}
	big, bigType := multipartBody(t, "task-sample", "big.txt", "text/plain", bytes.Repeat([]byte("a"), 3<<20))
	w = f.do(call{method: post, target: pathAttachUpload, body: big,
		header: map[string]string{"Cookie": cookie, "Origin": "http://" + hostA, "Content-Type": bigType}})
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("big: %d %s", w.Code, w.Body)
	}
	// A type para does not store: 400 with the reason.
	exe, exeType := multipartBody(t, "task-sample", "run.exe", "application/x-msdownload", []byte("MZ"))
	w = f.do(call{method: post, target: pathAttachUpload, body: exe,
		header: map[string]string{"Cookie": cookie, "Origin": "http://" + hostA, "Content-Type": exeType}})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "can be attached") {
		t.Fatalf("exe: %d %s", w.Code, w.Body)
	}
}
