package ops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/iodesystems/para/internal/egress"
	"github.com/iodesystems/para/internal/model"
)

// Attachments are files the owner adds to an item (model.Attachment): a photo,
// a recording, a PDF. The owner adds them (attachment_add, a person op); an
// agent reads them (attachment_read), and each read by an agent logs an
// egress event with the path.

// Vault setting and limits of attachments.
const (
	attachMaxConfig  = "attachments.max_mb"
	DefaultAttachMax = 25 // MB
	// readTextMax and readImageMax bound what attachment_read returns inline.
	readTextMax  = 256 << 10
	readImageMax = 5 << 20
)

// AttachMaxBytes is the largest attachment the vault takes, from
// attachments.max_mb.
func AttachMaxBytes(d Deps) (int64, error) {
	v, err := d.Store.Config(attachMaxConfig)
	if err != nil {
		return 0, err
	}
	mb := DefaultAttachMax
	if s := strings.TrimSpace(v); s != "" {
		if mb, err = strconv.Atoi(s); err != nil || mb < 1 {
			return 0, fmt.Errorf("%s=%q: want a whole number of MB, 1 or more", attachMaxConfig, v)
		}
	}
	return int64(mb) << 20, nil
}

// AttachmentAddInput is a file to add to an item. Name is the file name the
// person gave; Mime is the type the browser declared, used only when the name
// has no allowed extension. Data is the content (base64 in JSON).
type AttachmentAddInput struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Mime string `json:"mime,omitempty"`
	Data []byte `json:"data"`
}

// AttachmentAddOutput is the stored file. Embed is the Obsidian embed to put
// in a body or notes.
type AttachmentAddOutput struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Embed  string `json:"embed"`
	SHA256 string `json:"sha256"`
	Size   int    `json:"size"`
	Mime   string `json:"mime"`
}

// activeStarts begin content a browser runs or renders as a page. A file
// that starts with one is refused, whatever its name says.
var activeStarts = []string{"<!doctype", "<html", "<head", "<body", "<script", "<iframe", "<?xml", "<svg"}

// active reports whether data starts, after blanks and a byte order mark,
// with markup a browser would run.
func active(data []byte) bool {
	head := data[:min(len(data), 512)]
	head = bytes.TrimPrefix(head, []byte("\xef\xbb\xbf"))
	lower := strings.ToLower(strings.TrimLeft(string(head), " \t\r\n\f"))
	return slices.ContainsFunc(activeStarts, func(p string) bool { return strings.HasPrefix(lower, p) })
}

// AttachmentAdd stores a file for an item.
var AttachmentAdd = definePerson("attachment_add", "Add a file to an item: an image, audio, video, PDF, or text file, stored in the vault as attachments/<id>/<time>-<name>. Returns the path and the embed (![description](path)). People only", func(ctx context.Context, d Deps, in AttachmentAddInput) (AttachmentAddOutput, error) {
	id := normID(in.ID)
	if id == "" {
		return AttachmentAddOutput{}, errors.New("id is empty")
	}
	if _, err := d.Store.Get(ctx, id); err != nil {
		return AttachmentAddOutput{}, linkErr("id", id, err)
	}
	if len(in.Data) == 0 {
		return AttachmentAddOutput{}, errors.New("the file is empty")
	}
	max, err := AttachMaxBytes(d)
	if err != nil {
		return AttachmentAddOutput{}, err
	}
	if int64(len(in.Data)) > max {
		return AttachmentAddOutput{}, fmt.Errorf("the file is %d bytes, over the limit of %d MB (%s)", len(in.Data), max>>20, attachMaxConfig)
	}
	name := model.SafeAttachmentName(in.Name, in.Mime)
	if name == "" {
		return AttachmentAddOutput{}, fmt.Errorf("%q (%s): only images, audio, video, PDF, and plain or markdown text can be attached", in.Name, firstOf(in.Mime, "no type"))
	}
	if active(in.Data) {
		return AttachmentAddOutput{}, fmt.Errorf("%q holds HTML, XML, or SVG, which cannot be attached", in.Name)
	}
	stamp := d.Store.Now().Format("20060102-150405")
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	var a model.Attachment
	for n := 1; ; n++ {
		file := stamp + "-" + name
		if n > 1 {
			file = stamp + "-" + stem + "-" + strconv.Itoa(n) + ext
		}
		a, err = d.Store.AttachmentAdd(ctx, id, file, in.Data)
		if err == nil {
			break
		}
		if !errors.Is(err, model.ErrExists) || n >= 9 {
			return AttachmentAddOutput{}, err
		}
	}
	return AttachmentAddOutput{ID: id, Path: a.Path, Embed: a.Embed(), SHA256: a.SHA256, Size: int(a.Size), Mime: a.Mime}, nil
})

// AttachmentReadInput names a stored file by its vault path. The field is
// file, not path: no tool input reaches outside the vault by a path, and this
// one takes only attachments/<id>/<file>.
type AttachmentReadInput struct {
	File string `json:"file" query:"file" required:"true"` // attachments/<id>/<file>
}

// AttachmentReadOutput is a stored file. Kind is image, audio, video, text,
// or other. Text holds a text file; Data holds an image (base64 in JSON; over
// MCP it is image content). Note says what was left out and why.
type AttachmentReadOutput struct {
	Path   string `json:"path"`
	ID     string `json:"id"`
	Mime   string `json:"mime"`
	Kind   string `json:"kind"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
	Text   string `json:"text,omitempty"`
	Data   []byte `json:"data,omitempty"`
	Note   string `json:"note,omitempty"`
}

// MCPImage is the image an MCP client shows as image content, and the Output
// without it. ok is false when the Output holds no image.
func (o AttachmentReadOutput) MCPImage() (mime string, data []byte, rest any, ok bool) {
	if len(o.Data) == 0 {
		return "", nil, o, false
	}
	r := o
	r.Data = nil
	return o.Mime, o.Data, r, true
}

// Imaging is an Output that carries an image. The MCP server sends the image
// as image content and the rest as text.
type Imaging interface {
	MCPImage() (mime string, data []byte, rest any, ok bool)
}

const readElsewhere = "Read the file at this path of the vault with your own tools."

// AttachmentRead returns a stored file: a text file as text, an image as
// data, anything else as what it is and where. A read by an agent logs an
// egress event with the path.
var AttachmentRead = defineRead("attachment_read", "Read a file attached to an item, by its vault path (attachments/<id>/<file>, the path in an embed ![description](path)): an image comes back as an image, a text file as text, audio, video, and PDF as their type and size only (read the vault file with your own tools). Logs the read", func(ctx context.Context, d Deps, in AttachmentReadInput) (AttachmentReadOutput, error) {
	p := strings.TrimSpace(in.File)
	p = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(p, "!"), "[["), "]]")
	a, err := d.Store.AttachmentStat(ctx, p)
	if err != nil {
		return AttachmentReadOutput{}, err
	}
	out := AttachmentReadOutput{Path: a.Path, ID: a.Doc, Mime: a.Mime, Kind: model.AttachmentKind(a.Mime), Size: int(a.Size)}
	inline := out.Kind == "text" && out.Size <= readTextMax || out.Kind == "image" && out.Size <= readImageMax
	if !inline {
		switch out.Kind {
		case "text", "image":
			out.Note = "The file is too large to return here. " + readElsewhere
		default:
			out.Note = "Para returns no " + out.Kind + " content. " + readElsewhere
		}
	}
	if agentSurface(d) {
		dec, err := d.Store.Egress(ctx, a.Path, egress.DestAgent+d.Store.Info().Surface)
		if err != nil {
			return AttachmentReadOutput{}, err
		}
		if !dec.Allowed {
			return AttachmentReadOutput{}, fmt.Errorf("egress to agents is not allowed in this vault")
		}
	}
	got, data, err := d.Store.AttachmentRead(ctx, a.Path)
	if err != nil {
		return AttachmentReadOutput{}, err
	}
	out.Size, out.SHA256 = int(got.Size), got.SHA256
	if inline {
		if out.Kind == "text" {
			out.Text = string(data)
		} else {
			out.Data = data
		}
	}
	return out, nil
})
