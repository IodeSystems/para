package vault

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/iodesystems/para/internal/atomicfile"
	"github.com/iodesystems/para/internal/events"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/paramd"
)

// OpAttachmentAdd is the event of a stored attachment. Doc is its vault path,
// from its size in bytes, to its media type ("/" written as ":"), hash its
// sha256. The event holds no content.
const OpAttachmentAdd = "attachment_add"

// mimeToken writes a media type in the event's token alphabet.
func mimeToken(mime string) string { return strings.ReplaceAll(mime, "/", ":") }

// AttachmentAdd stores data as attachments/<doc>/<file>, logs one event, and
// returns what it stored. file must be a stored name
// (model.ValidAttachmentFile); the caller makes it. A file of that name that
// exists is model.ErrExists. Attachments are not documents: the manifest does
// not track them.
func (v *Vault) AttachmentAdd(ctx context.Context, doc, file string, data []byte) (model.Attachment, error) {
	if !paramd.ValidStem(doc) {
		return model.Attachment{}, fmt.Errorf("vault: attachment add: invalid id %q", doc)
	}
	if !model.ValidAttachmentFile(file) {
		return model.Attachment{}, fmt.Errorf("vault: attachment add: invalid file name %q", file)
	}
	a := model.Attachment{Path: model.AttachmentPath(doc, file), Doc: doc, SHA256: events.Hash(data),
		Size: int64(len(data)), Mime: model.AttachmentMime(file)}
	err := v.lock(ctx, func() error {
		if err := v.noLinks(path.Dir(a.Path)); err != nil {
			return err
		}
		dir := v.abs(path.Dir(a.Path))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := v.noLinks(path.Dir(a.Path)); err != nil {
			return err
		}
		abs := v.abs(a.Path)
		if _, err := os.Lstat(abs); err == nil {
			return fmt.Errorf("%w: %s", model.ErrExists, a.Path)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := atomicfile.Write(abs, data, fileMode); err != nil {
			return err
		}
		undo := func() error { return os.Remove(abs) }
		return v.logOrUndo(undo, OpAttachmentAdd, a.Path, strconv.FormatInt(a.Size, 10), mimeToken(a.Mime), a.SHA256)
	})
	if err != nil {
		return model.Attachment{}, fmt.Errorf("vault: attachment add: %w", err)
	}
	return a, nil
}

// AttachmentStat returns what is stored at p, a vault path of an attachment,
// without reading it. A path that is not one, or that runs through a link,
// is refused.
func (v *Vault) AttachmentStat(_ context.Context, p string) (model.Attachment, error) {
	a, err := v.attachmentAt(p)
	if err != nil {
		return a, fmt.Errorf("vault: attachment stat: %w", err)
	}
	st, err := os.Lstat(v.abs(a.Path))
	if err != nil {
		return a, fmt.Errorf("vault: attachment stat: %w", notFound(err, p))
	}
	if !st.Mode().IsRegular() {
		return a, fmt.Errorf("vault: attachment stat: %s is not a regular file", p)
	}
	a.Size = st.Size()
	return a, nil
}

// AttachmentRead returns the stored file at p and its hash.
func (v *Vault) AttachmentRead(ctx context.Context, p string) (model.Attachment, []byte, error) {
	a, err := v.AttachmentStat(ctx, p)
	if err != nil {
		return a, nil, err
	}
	data, err := os.ReadFile(v.abs(a.Path))
	if err != nil {
		return a, nil, fmt.Errorf("vault: attachment read: %w", notFound(err, p))
	}
	a.Size, a.SHA256 = int64(len(data)), events.Hash(data)
	return a, data, nil
}

// attachmentAt checks p and that no folder on the way is a link out of the
// vault.
func (v *Vault) attachmentAt(p string) (model.Attachment, error) {
	doc, file, ok := model.SplitAttachmentPath(p, paramd.ValidStem)
	if !ok {
		return model.Attachment{}, fmt.Errorf("%q is not an attachment path: want %s/<item id>/<yyyymmdd-hhmmss>-<name>", p, model.AttachmentsDir)
	}
	a := model.Attachment{Path: p, Doc: doc, Mime: model.AttachmentMime(file)}
	if err := v.noLinks(path.Dir(p)); err != nil {
		return a, err
	}
	return a, nil
}

// noLinks refuses a folder path, relative to the root, any part of which is a
// symbolic link: a link could lead out of the vault. A part that does not
// exist yet is fine.
func (v *Vault) noLinks(rel string) error {
	cur := v.root
	for _, part := range strings.Split(rel, "/") {
		cur = filepath.Join(cur, part)
		st, err := os.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if st.Mode()&fs.ModeSymlink != 0 || !st.IsDir() {
			return fmt.Errorf("%s is not a folder of the vault", rel)
		}
	}
	return nil
}

func notFound(err error, p string) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %s", model.ErrNotFound, p)
	}
	return err
}
