package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/netguard"
	"github.com/iodesystems/para/internal/ops"
)

// Attachments: the composer sends one file per multipart POST, and a page
// shows a stored file from a GET. Both are raw routes, outside huma: a file
// does not fit a JSON body (base64 and the 16 MB cap). They pass the same
// gate as every route; the upload needs review:decide and runs the person op
// attachment_add, so a bearer key is refused.
const (
	pathAttachUpload = "/api/v1/attachment/upload"
	pathAttachFile   = "/api/v1/attachment/file"
)

// registerAttachments: the ops (JSON, for API parity) and the two raw routes.
func registerAttachments(r *registrar) {
	reg(r, ops.AttachmentAdd, post, netguard.ScopeReviewDecide)
	reg(r, ops.AttachmentRead, get, read)
	reg(r, ops.AgentSend, post, netguard.ScopeReviewDecide)
	reg(r, ops.ItemClose, post, netguard.ScopeReviewDecide)
	// The generic verbs. Each runs the op of the item's type; a caller on a
	// bearer key must hold that op's own scope too (boundVault.deps
	// Authorize), so review:decide alone writes nothing an agent could not.
	reg(r, ops.Close, post, netguard.ScopeReviewDecide)
	reg(r, ops.Later, post, netguard.ScopeReviewDecide)
	reg(r, ops.Reopen, post, netguard.ScopeReviewDecide)
	// bulk runs one of those verbs (or depends_set) per item, each under the
	// same Authorize.
	reg(r, ops.Bulk, post, netguard.ScopeReviewDecide)
	reg(r, ops.SendToAgent, post, netguard.ScopeReviewDecide)
	reg(r, ops.Show, get, read)
	reg(r, ops.List, get, read)
}

// attachmentRoutes are chi routes, like /ws: the gate finds their scope in
// the route table, and they are not huma operations.
func (s *Server) attachmentRoutes() {
	s.routes.add(post, pathAttachUpload, netguard.ScopeReviewDecide)
	s.mux.Post(pathAttachUpload, s.attachUpload)
	s.routes.add(get, pathAttachFile, read)
	s.mux.Get(pathAttachFile, s.attachFile)
}

// writeProblem writes huma's error shape with the detail escaped.
func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"title": http.StatusText(status), "status": status, "detail": detail})
}

func writeOpError(w http.ResponseWriter, err error) {
	var se huma.StatusError
	if errors.As(err, &se) {
		writeProblem(w, se.GetStatus(), se.Error())
		return
	}
	writeProblem(w, http.StatusInternalServerError, "internal error")
}

// attachUpload reads a multipart form: the field id, then the field file.
// The file is read into memory up to the vault's limit; nothing is written
// outside the vault.
func (s *Server) attachUpload(w http.ResponseWriter, r *http.Request) {
	b, err := s.bound(r.Context())
	if err != nil {
		writeOpError(w, err)
		return
	}
	deps := b.deps()
	max, err := ops.AttachMaxBytes(deps)
	if err != nil {
		writeOpError(w, b.t.apiError(err))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, max+1<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "want a multipart form with id and file")
		return
	}
	var in ops.AttachmentAddInput
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeProblem(w, uploadStatus(err), "the form could not be read: "+uploadReason(err))
			return
		}
		switch part.FormName() {
		case "id":
			v, err := io.ReadAll(io.LimitReader(part, 256))
			if err != nil {
				writeProblem(w, http.StatusBadRequest, "the id could not be read")
				return
			}
			in.ID = string(v)
		case "file":
			data, err := io.ReadAll(io.LimitReader(part, max+1))
			if err != nil {
				writeProblem(w, uploadStatus(err), "the file could not be read: "+uploadReason(err))
				return
			}
			in.Name, in.Mime, in.Data = part.FileName(), part.Header.Get("Content-Type"), data
		}
		_ = part.Close()
	}
	if in.Data == nil {
		writeProblem(w, http.StatusBadRequest, "no file in the form")
		return
	}
	out, err := ops.AttachmentAdd.Run(r.Context(), deps, in)
	if err != nil {
		writeOpError(w, b.t.apiError(err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

func uploadStatus(err error) int {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

func uploadReason(err error) string {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return "it is over the vault's attachment limit"
	}
	return "bad multipart data"
}

// attachFile serves a stored file to a page of the vault. Images, audio, and
// video show inline; text shows as plain text; a PDF downloads. The response
// runs nothing: its own CSP sandboxes it.
func (s *Server) attachFile(w http.ResponseWriter, r *http.Request) {
	b, err := s.bound(r.Context())
	if err != nil {
		writeOpError(w, err)
		return
	}
	p := r.URL.Query().Get("path")
	a, data, err := b.t.store.AttachmentRead(r.Context(), p)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "no such attachment")
		return
	}
	h := w.Header()
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	disp := "inline"
	ct := a.Mime
	switch model.AttachmentKind(a.Mime) {
	case "text":
		ct = "text/plain; charset=utf-8"
	case "other":
		disp = "attachment"
	}
	h.Set("Content-Type", ct)
	h.Set("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": path.Base(a.Path)}))
	http.ServeContent(w, r, path.Base(a.Path), time.Time{}, bytes.NewReader(data))
}
