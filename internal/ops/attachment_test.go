package ops

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/docs"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store/local"
	"github.com/iodesystems/para/internal/vault"
)

// attachDeps is a vault with its clock at 2026-11-05 14:03:09 local, and a
// task to attach to.
func attachDeps(t *testing.T) (Deps, string) {
	t.Helper()
	d, _ := attachVault(t)
	return d, attachTask(t, d)
}

func attachVault(t *testing.T) (Deps, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "v")
	tpl, err := fs.Sub(docs.Templates, "templates")
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(root, "acme", tpl, vault.InitOptions{}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 5, 14, 3, 9, 0, time.Local)
	s, err := local.Open(root, vault.Options{ID: "acme", Actor: "user", Surface: "cli", Install: "m1",
		StateDir: filepath.Join(filepath.Dir(root), "state"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return Deps{Store: s}, root
}

func attachTask(t *testing.T, d Deps) string {
	t.Helper()
	area := must(t, AreaAdd, d, AreaAddInput{Title: "Office"})
	task := must(t, TaskAdd, d, TaskAddInput{Title: "Photograph the board", Area: area.ID})
	return task.ID
}

// png is the start of a PNG file: enough for a type check, not a real image.
var png = []byte("\x89PNG\r\n\x1a\n synthetic")

func TestAttachmentAddStoresAndLogsNoContent(t *testing.T) {
	d, id := attachDeps(t)
	out := must(t, AttachmentAdd, d, AttachmentAddInput{ID: id, Name: `C:\Users\x\..\Board Photo (1).PNG`, Data: png})
	want := "attachments/" + id + "/20261105-140309-Board-Photo-1.png"
	if out.Path != want || out.Mime != "image/png" || out.Size != len(png) || !strings.HasPrefix(out.SHA256, "sha256:") {
		t.Fatalf("out = %+v, want path %s", out, want)
	}
	if out.Embed != "![Image attachment Board-Photo-1]("+want+")" {
		t.Fatalf("embed = %q", out.Embed)
	}
	evs, err := d.Store.Events(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	last := evs[len(evs)-1]
	if last.Op != "attachment_add" || last.Doc != want || last.From != strconv.Itoa(len(png)) || last.To != "image:png" || last.Hash != out.SHA256 {
		t.Fatalf("event = %+v", last)
	}
	if strings.Contains(last.Doc+last.From+last.To, "synthetic") {
		t.Fatal("event holds content")
	}
	// The same name in the same second gets a number.
	again := must(t, AttachmentAdd, d, AttachmentAddInput{ID: id, Name: "Board Photo (1).png", Data: png})
	if !strings.HasSuffix(again.Path, "-Board-Photo-1-2.png") {
		t.Fatalf("second path = %s", again.Path)
	}
	// A typed name without an extension takes the declared type's.
	rec := must(t, AttachmentAdd, d, AttachmentAddInput{ID: id, Name: "blob", Mime: "audio/webm;codecs=opus", Data: []byte("synthetic audio")})
	if !strings.HasSuffix(rec.Path, "-blob.weba") || rec.Mime != "audio/webm" {
		t.Fatalf("recording = %+v", rec)
	}
	mustLintClean(t, d)
}

func TestAttachmentAddRefuses(t *testing.T) {
	d, id := attachDeps(t)
	if err := d.Store.SetConfig(ctx, "attachments.max_mb", "1"); err != nil {
		t.Fatal(err)
	}
	for name, in := range map[string]AttachmentAddInput{
		"too big":       {ID: id, Name: "a.txt", Data: make([]byte, 1<<20+1)},
		"type":          {ID: id, Name: "run.exe", Mime: "application/x-msdownload", Data: []byte("MZ")},
		"svg":           {ID: id, Name: "a.svg", Mime: "image/svg+xml", Data: []byte("<svg/>")},
		"html as text":  {ID: id, Name: "notes.txt", Data: []byte("  <!DOCTYPE html><script>x</script>")},
		"svg as png":    {ID: id, Name: "a.png", Data: []byte("<svg onload=x>")},
		"empty":         {ID: id, Name: "a.txt"},
		"unknown item":  {ID: "task-none", Name: "a.txt", Data: []byte("x")},
		"traversing id": {ID: "../.para", Name: "a.txt", Data: []byte("x")},
	} {
		if _, err := AttachmentAdd.Run(ctx, d, in); err == nil {
			t.Errorf("%s: added", name)
		}
	}
}

func TestAttachmentRead(t *testing.T) {
	d, id := attachDeps(t)
	txt := must(t, AttachmentAdd, d, AttachmentAddInput{ID: id, Name: "notes.md", Data: []byte("# Synthetic\n")})
	img := must(t, AttachmentAdd, d, AttachmentAddInput{ID: id, Name: "board.png", Data: png})
	pdf := must(t, AttachmentAdd, d, AttachmentAddInput{ID: id, Name: "plan.pdf", Data: []byte("%PDF-1.4 synthetic")})

	got := must(t, AttachmentRead, d, AttachmentReadInput{File: txt.Path})
	if got.Kind != "text" || got.Text != "# Synthetic\n" || got.Data != nil || got.SHA256 != txt.SHA256 {
		t.Fatalf("text = %+v", got)
	}
	// An embed is taken as the path too.
	got = must(t, AttachmentRead, d, AttachmentReadInput{File: "![[" + img.Path + "]]"})
	if got.Kind != "image" || string(got.Data) != string(png) {
		t.Fatalf("image = %+v", got)
	}
	if mime, data, rest, ok := got.MCPImage(); !ok || mime != "image/png" || len(data) == 0 || rest.(AttachmentReadOutput).Data != nil {
		t.Fatalf("MCPImage = %v %v %v", mime, ok, rest)
	}
	got = must(t, AttachmentRead, d, AttachmentReadInput{File: pdf.Path})
	if got.Kind != "other" || got.Data != nil || got.Text != "" || !strings.Contains(got.Note, "own tools") {
		t.Fatalf("pdf = %+v", got)
	}
	for _, bad := range []string{"attachments/" + id + "/../../.para/vault.properties", "projects/" + id + ".md",
		"/etc/passwd", "attachments/" + id + "/none.txt", `attachments\` + id + `\x.txt`} {
		if _, err := AttachmentRead.Run(ctx, d, AttachmentReadInput{File: bad}); err == nil {
			t.Errorf("%s: read", bad)
		}
	}

	// An agent's read logs an egress event with the path, under deny too.
	agent := Deps{Store: d.Store.ActAs("agent", "mcp")}
	must(t, AttachmentRead, agent, AttachmentReadInput{File: txt.Path})
	evs, _ := d.Store.Events(ctx, 1)
	if e := evs[0]; e.Op != "egress" || e.Doc != txt.Path || e.From != "deny" || e.To != "agent:mcp" {
		t.Fatalf("event = %+v", e)
	}
}

func TestAttachmentReadRefusesALinkedFolder(t *testing.T) {
	d, root := attachVault(t)
	id := attachTask(t, d)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "20261105-140309-x.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "attachments"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "attachments", id)); err != nil {
		t.Fatal(err)
	}
	if _, err := AttachmentRead.Run(ctx, d, AttachmentReadInput{File: "attachments/" + id + "/20261105-140309-x.txt"}); err == nil {
		t.Fatal("read through a link")
	}
	if _, err := AttachmentAdd.Run(ctx, d, AttachmentAddInput{ID: id, Name: "y.txt", Data: []byte("y")}); err == nil {
		t.Fatal("wrote through a link")
	}
}

func TestAgentSendBody(t *testing.T) {
	d, id := attachDeps(t)
	att := must(t, AttachmentAdd, d, AttachmentAddInput{ID: id, Name: "board.png", Data: png})
	out := must(t, AgentSend, d, AgentSendInput{ID: id, Text: "Met Sam.\n## Decided\nShip in January.", Attachments: []string{att.Path}, Done: true})
	if out.Status != model.RequestQueued || out.Title != "From the owner: Photograph the board" {
		t.Fatalf("out = %+v", out)
	}
	doc, r, err := getRequest(ctx, d.Store, out.Request)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Links) != 1 || r.Links[0].Target() != id {
		t.Fatalf("links = %v", r.Links)
	}
	for _, want := range []string{"[[" + id + "]]", "prompt ingest_source, part An update on an item", "Met Sam.", "**Decided**", "Ship in January.",
		"Mark done: yes", "close for a task", att.Embed, "attachment_read"} {
		if !strings.Contains(doc.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, doc.Body)
		}
	}
	mustLintClean(t, d)

	// Without done, the agent closes the item only when the words say it is done.
	out = must(t, AgentSend, d, AgentSendInput{ID: id, Text: "Waiting on Sam."})
	doc, _, _ = getRequest(ctx, d.Store, out.Request)
	if !strings.Contains(doc.Body, "Mark done: no") || !strings.Contains(doc.Body, "clearly say the item is done") {
		t.Fatalf("body = %s", doc.Body)
	}
	// An area is a link type now: the work is linked to it.
	area := must(t, AreaAdd, d, AreaAddInput{Title: "Health"})
	out = must(t, AgentSend, d, AgentSendInput{ID: area.ID, Text: "Book the checkup."})
	_, r, _ = getRequest(ctx, d.Store, out.Request)
	if len(r.Context) != 0 || len(r.Links) != 1 || r.Links[0].Target() != area.ID {
		t.Fatalf("area request = %+v", r)
	}

	for name, in := range map[string]AgentSendInput{
		"empty":           {ID: id},
		"unknown":         {ID: "task-none", Text: "x"},
		"bad attachment":  {ID: id, Attachments: []string{"projects/x.md"}},
		"gone attachment": {ID: id, Attachments: []string{"attachments/" + id + "/20261105-140309-none.png"}},
	} {
		if _, err := AgentSend.Run(ctx, d, in); err == nil {
			t.Errorf("%s: sent", name)
		}
	}
}

func TestItemClose(t *testing.T) {
	d, id := attachDeps(t)
	if got := must(t, ItemClose, d, ItemCloseInput{ID: id}); got.By != "task_done" {
		t.Fatalf("task: %+v", got)
	}
	if _, task, _ := getTask(ctx, d.Store, id); task.Status != model.TaskDone {
		t.Fatalf("task status = %s", task.Status)
	}
	sam := must(t, OrgPersonAdd, d, OrgAddInput{Name: "Sam Example", Org: "acme.example"})
	m := must(t, MeetingAdd, d, MeetingAddInput{Title: "Board review", With: []string{sam.ID}, Start: "2026-11-04T10:00:00-05:00", End: "2026-11-04T10:30:00-05:00"})
	if got := must(t, ItemClose, d, ItemCloseInput{ID: m.ID}); got.By != "meeting_hold" {
		t.Fatalf("meeting: %+v", got)
	}
	if _, mt, _ := getMeeting(ctx, d.Store, m.ID); mt.Status != model.MeetingHeld {
		t.Fatalf("meeting status = %s", mt.Status)
	}
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Which board?", Root: true})
	if _, err := ItemClose.Run(ctx, d, ItemCloseInput{ID: q.ID}); err == nil || !strings.Contains(err.Error(), "send it to an agent") {
		t.Fatalf("question closed: %v", err)
	}
}
