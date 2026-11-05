package model

import (
	"path"
	"regexp"
	"strings"
)

// Attachments are files the owner adds to an item: a photo, a recording, a
// PDF, a text file. Each lives in the vault at
// attachments/<doc id>/<yyyymmdd-hhmmss>-<name>, so a file-sync tool carries it
// with the vault, and Obsidian shows it from an embed (AttachmentEmbed).
// They are not documents: lint and the manifest do not read them.

// AttachmentsDir is the vault folder of attachments.
const AttachmentsDir = "attachments"

// Attachment is one stored file: its vault path, the item it belongs to, and
// what the event log records of it. It never holds content.
type Attachment struct {
	Path   string `json:"path"`
	Doc    string `json:"doc"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Mime   string `json:"mime"`
}

// Embed is the Markdown embed of the attachment.
func (a Attachment) Embed() string { return AttachmentEmbed(a.Path) }

// AttachmentEmbed is the embed of a stored file: ![<kind> attachment
// <name>](attachments/<id>/<file>). para-md refuses the wikilink embed
// ![[...]], which has no description; Obsidian resolves this path from the
// vault root and shows the file the same way.
func AttachmentEmbed(p string) string {
	file := path.Base(p)
	name := strings.TrimSuffix(file, path.Ext(file))
	if len(name) > 16 && attachNameRe.MatchString(file) {
		name = name[16:] // drop yyyymmdd-hhmmss-
	}
	kind := AttachmentKind(AttachmentMime(file))
	if kind == "other" {
		kind = "file"
	}
	return "![" + strings.ToUpper(kind[:1]) + kind[1:] + " attachment " + name + "](" + p + ")"
}

// attachmentTypes maps a file extension to its media type. Only these are
// stored. SVG and HTML are left out: a browser runs script in them.
var attachmentTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif",
	".webp": "image/webp", ".heic": "image/heic", ".heif": "image/heif", ".avif": "image/avif",
	".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".aac": "audio/aac", ".wav": "audio/wav",
	".ogg": "audio/ogg", ".oga": "audio/ogg", ".opus": "audio/opus", ".flac": "audio/flac", ".weba": "audio/webm",
	".mp4": "video/mp4", ".m4v": "video/mp4", ".mov": "video/quicktime", ".webm": "video/webm", ".ogv": "video/ogg",
	".pdf": "application/pdf",
	".txt": "text/plain", ".md": "text/markdown",
}

// extFor is the extension to give a file of a declared type that has no
// known extension, such as a phone recording named "blob".
var extFor = map[string]string{
	"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp",
	"image/heic": ".heic", "image/heif": ".heif", "image/avif": ".avif",
	"audio/mpeg": ".mp3", "audio/mp4": ".m4a", "audio/x-m4a": ".m4a", "audio/aac": ".aac", "audio/wav": ".wav",
	"audio/x-wav": ".wav", "audio/ogg": ".ogg", "audio/opus": ".opus", "audio/flac": ".flac", "audio/webm": ".weba",
	"video/mp4": ".mp4", "video/quicktime": ".mov", "video/webm": ".webm", "video/ogg": ".ogv",
	"application/pdf": ".pdf", "text/plain": ".txt", "text/markdown": ".md",
}

// AttachmentMime is the media type of a stored file name, or "" when the
// extension is not allowed.
func AttachmentMime(name string) string {
	return attachmentTypes[strings.ToLower(path.Ext(name))]
}

// AttachmentKind is image, audio, video, text, or other (a PDF).
func AttachmentKind(mime string) string {
	major, _, _ := strings.Cut(mime, "/")
	switch major {
	case "image", "audio", "video", "text":
		return major
	}
	return "other"
}

var (
	unsafeRun      = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
	attachNameRe   = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[A-Za-z0-9][A-Za-z0-9_-]*(?:\.[A-Za-z0-9_-]+)*$`)
	maxAttachStem  = 80
	attachmentRoot = AttachmentsDir + "/"
)

// SafeAttachmentName makes a stored name from the name a person gave and
// the type the browser declared. It keeps the base name only (no folder, in
// either slash), maps every other character to "-", and ends in an allowed
// extension: the given one when allowed, else the one of the declared type.
// It returns "" when neither is allowed.
func SafeAttachmentName(given, declared string) string {
	base := given
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	ext := strings.ToLower(path.Ext(base))
	stem := strings.TrimSuffix(base, path.Ext(base))
	if attachmentTypes[ext] == "" {
		major, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(declared)), ";")
		ext = extFor[strings.TrimSpace(major)]
		if ext == "" {
			return ""
		}
		stem = base
	}
	stem = strings.Trim(unsafeRun.ReplaceAllString(stem, "-"), ".-_")
	stem = strings.ReplaceAll(stem, ".", "-")
	if len(stem) > maxAttachStem {
		stem = strings.TrimRight(stem[:maxAttachStem], "-_")
	}
	if stem == "" {
		stem = "file"
	}
	return stem + ext
}

// ValidAttachmentFile reports whether name is a stored file name:
// yyyymmdd-hhmmss-<safe name> with an allowed extension.
func ValidAttachmentFile(name string) bool {
	return len(name) <= 120 && attachNameRe.MatchString(name) && !strings.Contains(name, "..") && AttachmentMime(name) != ""
}

// SplitAttachmentPath checks a vault path of an attachment and returns the
// document id and the file name. It refuses anything else: another folder,
// "..", a backslash, an absolute path, or a deeper path.
func SplitAttachmentPath(p string, validDoc func(string) bool) (doc, file string, ok bool) {
	rest, found := strings.CutPrefix(p, attachmentRoot)
	if !found || strings.Contains(p, `\`) {
		return "", "", false
	}
	doc, file, found = strings.Cut(rest, "/")
	if !found || strings.Contains(file, "/") || !validDoc(doc) || !ValidAttachmentFile(file) {
		return "", "", false
	}
	return doc, file, true
}

// AttachmentPath is the vault path of a stored file.
func AttachmentPath(doc, file string) string { return attachmentRoot + doc + "/" + file }
