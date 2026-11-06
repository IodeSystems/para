package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// An item's body may be only a path to a file in the vault. A message must
// hold its text in an outgoing io draft, so the owner edits that one copy: a
// path to an io draft becomes the block's io reference, and a path to any
// other file is refused. Any other kind keeps the path; the review page shows
// the file's text beside the item (read through the store), and an owner's
// edit puts the edited text in the body, so the approved text is the text
// shown. Para reads nothing outside the vault: such a path is refused.

func isPathBody(body string) bool { return model.IsPathBody(body) }

// bodyDoc is the document a path body names, or an error that says why the
// path cannot be used.
func bodyDoc(ctx context.Context, d Deps, body string) (model.Document, error) {
	p := strings.TrimSpace(body)
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return model.Document{}, fmt.Errorf("body %s: %w", p, store.ErrOutsideVault)
		}
		p = filepath.Join(home, rest)
	}
	id, err := d.Store.DocAt(ctx, p)
	switch {
	case errors.Is(err, store.ErrOutsideVault):
		return model.Document{}, fmt.Errorf("body %s is a path outside the vault: para reads nothing outside the vault; put the text in the body, or the file in the vault", strings.TrimSpace(body))
	case errors.Is(err, model.ErrNotFound):
		return model.Document{}, fmt.Errorf("body %s is a path, but no vault document is there", strings.TrimSpace(body))
	case err != nil:
		return model.Document{}, err
	}
	return d.Store.Get(ctx, id)
}

// BodyFile is the vault document an item's body names, for the review page.
type BodyFile struct {
	Path  string `json:"path"`
	Doc   string `json:"doc"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

// bodyFile reads the document a path body names; nil when the body is text
// or the path no longer names a document.
func bodyFile(ctx context.Context, d Deps, it model.ProposalItem) *BodyFile {
	if it.Kind == model.ItemMessage || !isPathBody(it.Body) {
		return nil
	}
	doc, err := bodyDoc(ctx, d, it.Body)
	if err != nil {
		return nil
	}
	return &BodyFile{Path: strings.TrimSpace(it.Body), Doc: doc.ID, Title: doc.Title, Text: doc.Body}
}

// urlNoteText is said beside a page URL when server.base_url is unset.
const urlNoteText = "works only on this machine; set server.base_url (para vault config set server.base_url <url>) for other devices"

// urlNote is urlNoteText when url is set and server.base_url is not.
func urlNote(d Deps, url string) string {
	if url == "" {
		return ""
	}
	if base, err := d.Store.Config(reviewBaseConfig); err != nil || base != "" {
		return ""
	}
	return urlNoteText
}
