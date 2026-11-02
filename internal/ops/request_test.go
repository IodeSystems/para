package ops

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iodesystems/paradize/docs"
	"github.com/iodesystems/paradize/internal/model"
	"github.com/iodesystems/paradize/internal/store/local"
	"github.com/iodesystems/paradize/internal/vault"
)

// reqVault is a synthetic vault with a clock the test moves.
type reqVault struct {
	root string
	t    *testing.T
	now  atomic.Int64 // unix nanoseconds
}

func newReqVault(t *testing.T) *reqVault {
	t.Helper()
	rv := &reqVault{root: filepath.Join(t.TempDir(), "v"), t: t}
	rv.now.Store(time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC).UnixNano())
	tpl, err := fs.Sub(docs.Templates, "templates")
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(rv.root, "acme", tpl); err != nil {
		t.Fatal(err)
	}
	return rv
}

func (rv *reqVault) advance(d time.Duration) { rv.now.Add(int64(d)) }

// as opens the vault as actor on surface from install.
func (rv *reqVault) as(actor, surface, install string) Deps {
	rv.t.Helper()
	s, err := local.Open(rv.root, vault.Options{
		ID: "acme", Actor: actor, Surface: surface, Install: install,
		Now: func() time.Time { return time.Unix(0, rv.now.Load()).UTC() },
	})
	if err != nil {
		rv.t.Fatal(err)
	}
	return Deps{Store: s}
}

func addReq(t *testing.T, d Deps, ask string) RequestAddOutput {
	t.Helper()
	out, err := RequestAdd.Run(context.Background(), d, RequestAddInput{Ask: ask})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func nextReq(t *testing.T, d Deps) RequestNextOutput {
	t.Helper()
	out, err := RequestNext.Run(context.Background(), d, RequestNextInput{})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRequestLifecycle(t *testing.T) {
	ctx := context.Background()
	rv := newReqVault(t)
	person := rv.as("user", "cli", "inst-a")
	agent := rv.as("agent-1", "mcp", "inst-a")

	first := addReq(t, person, "Summarize the sample project\n\nKeep it short.")
	rv.advance(time.Second)
	addReq(t, person, "Second ask")
	if first.Status != model.RequestQueued || first.Runner != "inst-a" {
		t.Fatalf("add = %+v", first)
	}
	if got, _ := person.Store.Lint(ctx); len(got) != 0 {
		t.Fatalf("lint: %v", got)
	}

	// The oldest request comes first, with its body.
	n := nextReq(t, agent)
	if n.Request == nil || n.Request.ID != first.ID || n.Request.Status != model.RequestClaimed ||
		n.Request.ClaimedBy != "agent-1" || n.Request.Body != "Keep it short." || n.Request.LeaseUntil == "" {
		t.Fatalf("next = %+v", n.Request)
	}

	// Only the holder can add notes, finish, or fail.
	other := rv.as("agent-2", "mcp", "inst-a")
	if _, err := RequestNote.Run(ctx, other, RequestNoteInput{ID: first.ID, Text: "mine"}); err == nil {
		t.Fatal("another actor added a note")
	}
	if _, err := RequestDone.Run(ctx, other, RequestDoneInput{ID: first.ID, Result: "x"}); err == nil {
		t.Fatal("another actor finished it")
	}
	rv.advance(10 * time.Minute)
	note, err := RequestNote.Run(ctx, agent, RequestNoteInput{ID: first.ID, Text: "read the\nproject"})
	if err != nil {
		t.Fatal(err)
	}
	if note.LeaseUntil <= n.Request.LeaseUntil {
		t.Errorf("note did not renew the lease: %s then %s", n.Request.LeaseUntil, note.LeaseUntil)
	}
	if _, err := RequestNote.Run(ctx, agent, RequestNoteInput{ID: first.ID, Text: "second note"}); err != nil {
		t.Fatal(err)
	}
	if _, err := RequestDone.Run(ctx, agent, RequestDoneInput{ID: first.ID, Result: "Wrote the summary.", Links: []string{first.ID}}); err != nil {
		t.Fatal(err)
	}
	show, err := RequestShow.Run(ctx, person, RequestShowInput{ID: first.ID})
	if err != nil {
		t.Fatal(err)
	}
	r := show.Doc.Meta.(*model.Request)
	if r.Status != model.RequestDone || r.Lease != "" || r.Claimed != "agent-1" || len(r.Results) != 1 {
		t.Fatalf("done = %+v", r)
	}
	for _, want := range []string{"Keep it short.", "## Notes", "agent-1: read the project", "second note", "## Result\n\nWrote the summary."} {
		if !strings.Contains(show.Doc.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, show.Doc.Body)
		}
	}
	if _, err := RequestDone.Run(ctx, agent, RequestDoneInput{ID: first.ID, Result: "again"}); err == nil {
		t.Error("finished a done request twice")
	}
	if got, _ := agent.Store.Lint(ctx); len(got) != 0 {
		t.Fatalf("lint: %v", got)
	}

	// Fail, and cancel of a claimed request.
	n = nextReq(t, agent)
	if _, err := RequestFail.Run(ctx, agent, RequestFailInput{ID: n.Request.ID, Reason: "no access"}); err != nil {
		t.Fatal(err)
	}
	third := addReq(t, person, "Third ask")
	nextReq(t, agent)
	if _, err := RequestCancel.Run(ctx, person, RequestCancelInput{ID: third.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := RequestNote.Run(ctx, agent, RequestNoteInput{ID: third.ID, Text: "late"}); err == nil {
		t.Error("note after cancel")
	}
	if _, err := RequestCancel.Run(ctx, person, RequestCancelInput{ID: third.ID}); err == nil {
		t.Error("cancelled a cancelled request")
	}
	if out := nextReq(t, agent); out.Request != nil {
		t.Errorf("empty queue returned %+v", out.Request)
	}
	list, err := RequestList.Run(ctx, person, RequestListInput{Status: model.RequestFailed})
	if err != nil || len(list.Items) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
}

func TestRequestLeaseExpires(t *testing.T) {
	rv := newReqVault(t)
	person := rv.as("user", "cli", "inst-a")
	a1 := rv.as("agent-1", "mcp", "inst-a")
	a2 := rv.as("agent-2", "mcp", "inst-a")
	req := addReq(t, person, "Ask")

	if out := nextReq(t, a1); out.Request == nil {
		t.Fatal("no claim")
	}
	rv.advance(DefaultLease - time.Second)
	if out := nextReq(t, a2); out.Request != nil || len(out.Requeued) != 0 {
		t.Fatalf("claimed before the lease ended: %+v", out)
	}
	rv.advance(2 * time.Second)
	out := nextReq(t, a2)
	if out.Request == nil || out.Request.ID != req.ID || out.Request.ClaimedBy != "agent-2" || !slices.Equal(out.Requeued, []string{req.ID}) {
		t.Fatalf("after expiry: %+v", out)
	}
	// The first agent lost it.
	if _, err := RequestDone.Run(context.Background(), a1, RequestDoneInput{ID: req.ID, Result: "late"}); err == nil {
		t.Error("expired holder finished the request")
	}
	// An expired claim stays visible to a watch as claimable.
	if _, err := RequestNote.Run(context.Background(), a2, RequestNoteInput{ID: req.ID, Text: "ok", LeaseSeconds: 60}); err != nil {
		t.Fatal(err)
	}
	w := NewRequestWatch(person)
	if ok, err := w.Claimable(context.Background()); err != nil || ok {
		t.Fatalf("held claim is claimable: %v %v", ok, err)
	}
	rv.advance(2 * time.Minute)
	if ok, err := w.Claimable(context.Background()); err != nil || !ok {
		t.Fatalf("expired claim is not claimable: %v %v", ok, err)
	}
}

func TestRequestRunner(t *testing.T) {
	ctx := context.Background()
	rv := newReqVault(t)
	laptop := rv.as("user", "cli", "inst-laptop")
	desk := rv.as("agent", "mcp", "inst-desk")

	// Default runner is the adding install.
	own := addReq(t, laptop, "For the laptop")
	if own.Runner != "inst-laptop" {
		t.Fatalf("runner = %s", own.Runner)
	}
	// vault.properties names the default runner.
	cfg := filepath.Join(rv.root, ".para", "vault.properties")
	raw, _ := os.ReadFile(cfg)
	if err := os.WriteFile(cfg, append(raw, []byte("request.runner=inst-desk\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	named := addReq(t, laptop, "For the desktop")
	if named.Runner != "inst-desk" {
		t.Fatalf("runner = %s", named.Runner)
	}
	explicit, err := RequestAdd.Run(ctx, laptop, RequestAddInput{Ask: "Explicit", Runner: "inst-x"})
	if err != nil || explicit.Runner != "inst-x" {
		t.Fatalf("explicit = %+v, %v", explicit, err)
	}

	// Each install claims only its own.
	if out := nextReq(t, desk); out.Request == nil || out.Request.ID != named.ID {
		t.Fatalf("desk claimed %+v", out.Request)
	}
	if out := nextReq(t, desk); out.Request != nil {
		t.Fatalf("desk claimed another install's request: %+v", out.Request)
	}
	if ok, _ := NewRequestWatch(desk).Claimable(ctx); ok {
		t.Error("watch sees another install's request")
	}
	if out := nextReq(t, laptop); out.Request == nil || out.Request.ID != own.ID {
		t.Fatalf("laptop claimed %+v", out.Request)
	}
	// Another install's expired lease is not requeued by this one.
	rv.advance(2 * DefaultLease)
	if out := nextReq(t, desk); len(out.Requeued) != 1 || out.Requeued[0] != named.ID {
		t.Fatalf("requeued = %v", out.Requeued)
	}
}

func TestRequestSurfaceGate(t *testing.T) {
	ctx := context.Background()
	rv := newReqVault(t)
	for _, s := range []string{"cli", "tui", "pwa"} {
		if out := addReq(t, rv.as("user", s, "inst-a"), "From "+s); out.Status != model.RequestQueued {
			t.Errorf("%s: status %s", s, out.Status)
		}
	}
	agent := rv.as("agent", "mcp", "inst-a")
	prop := addReq(t, agent, "Injected ask")
	if prop.Status != model.RequestProposed {
		t.Fatalf("mcp status = %s", prop.Status)
	}
	if out := addReq(t, rv.as("x", "", "inst-a"), "No surface"); out.Status != model.RequestProposed {
		t.Errorf("empty surface status = %s", out.Status)
	}

	// Drain the queued ones: the proposal is never claimed.
	for range 10 {
		out := nextReq(t, agent)
		if out.Request == nil {
			break
		}
		if out.Request.ID == prop.ID {
			t.Fatal("claimed a proposed request")
		}
	}
	// The agent cannot accept it, and cannot cancel-then-claim around it.
	if _, err := RequestAccept(ctx, agent, RequestAcceptInput{ID: prop.ID}); err == nil {
		t.Fatal("mcp surface accepted a proposal")
	}
	if out := nextReq(t, agent); out.Request != nil {
		t.Fatalf("claimed after a refused accept: %+v", out.Request)
	}
	person := rv.as("user", "cli", "inst-a")
	if _, err := RequestAccept(ctx, person, RequestAcceptInput{ID: prop.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := RequestAccept(ctx, person, RequestAcceptInput{ID: prop.ID}); err == nil {
		t.Error("accepted a queued request")
	}
	if out := nextReq(t, agent); out.Request == nil || out.Request.ID != prop.ID {
		t.Fatalf("accepted request not claimed: %+v", out.Request)
	}

	// request.surfaces replaces the default list.
	cfg := filepath.Join(rv.root, ".para", "vault.properties")
	raw, _ := os.ReadFile(cfg)
	if err := os.WriteFile(cfg, append(raw, []byte("request.surfaces=cli\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := addReq(t, rv.as("user", "tui", "inst-a"), "Now barred"); out.Status != model.RequestProposed {
		t.Errorf("tui after override: %s", out.Status)
	}
}

func TestRequestLinks(t *testing.T) {
	ctx := context.Background()
	rv := newReqVault(t)
	p := rv.as("user", "cli", "inst-a")
	if _, err := p.Store.Create(ctx, model.Document{ID: "project-a", Folder: model.FolderProjects, Title: "A",
		Meta: &model.Project{Common: model.Common{Type: model.TypeProject}, Status: model.ProjectActive, Owner: "Jordan Example"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := RequestAdd.Run(ctx, p, RequestAddInput{Ask: "x", Links: []string{"project-a"}}); err != nil {
		t.Fatal(err)
	}
	for _, in := range []RequestAddInput{{Ask: "x", Links: []string{"nope"}}, {Ask: " "}, {Ask: "x", Runner: "a b"}} {
		if _, err := RequestAdd.Run(ctx, p, in); err == nil {
			t.Errorf("accepted %+v", in)
		}
	}
	if _, err := RequestAdd.Run(ctx, Deps{Store: rv.as("user", "cli", "").Store}, RequestAddInput{Ask: "x"}); err == nil {
		t.Error("no install id accepted")
	}
}

// TestRequestEventsHoldNoText: events name ids and states only.
func TestRequestEventsHoldNoText(t *testing.T) {
	ctx := context.Background()
	rv := newReqVault(t)
	p := rv.as("user", "cli", "inst-a")
	a := rv.as("agent", "mcp", "inst-a")
	req := addReq(t, p, "zebrafish ask text\n\nzebrafish body")
	nextReq(t, a)
	RequestNote.Run(ctx, a, RequestNoteInput{ID: req.ID, Text: "zebrafish note"})
	if _, err := RequestDone.Run(ctx, a, RequestDoneInput{ID: req.ID, Result: "zebrafish result"}); err != nil {
		t.Fatal(err)
	}
	evs, err := p.Store.Events(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	var trail []string
	for _, e := range evs {
		trail = append(trail, e.Op+":"+e.From+">"+e.To)
		if strings.Contains(strings.ToLower(e.Doc+e.From+e.To+e.Actor), "zebrafish") {
			t.Errorf("event holds request text: %+v", e)
		}
	}
	want := []string{"create:>requests", "update:queued>claimed", "update:>", "update:claimed>done"}
	if !slices.Equal(trail, want) {
		t.Errorf("events = %v, want %v", trail, want)
	}
	raw, _ := os.ReadDir(filepath.Join(rv.root, "events"))
	for _, f := range raw {
		b, _ := os.ReadFile(filepath.Join(rv.root, "events", f.Name()))
		if strings.Contains(string(b), "zebrafish") {
			t.Errorf("%s holds request text", f.Name())
		}
	}
}

// TestRequestClaimConcurrent: many agents, each with its own handle on one
// vault, claim distinct requests. Run with -race.
func TestRequestClaimConcurrent(t *testing.T) {
	const requests, agents = 12, 6
	rv := newReqVault(t)
	person := rv.as("user", "cli", "inst-a")
	want := map[string]bool{}
	for i := range requests {
		rv.advance(time.Millisecond)
		want[addReq(t, person, "Ask "+string(rune('A'+i))).ID] = true
	}
	var (
		mu     sync.Mutex
		got    = map[string][]string{} // id -> actors that claimed it
		wg     sync.WaitGroup
		failed atomic.Value
	)
	for i := range agents {
		wg.Add(1)
		go func() {
			defer wg.Done()
			actor := "agent-" + string(rune('a'+i))
			dep := rv.as(actor, "mcp", "inst-a")
			for {
				out, err := RequestNext.Run(context.Background(), dep, RequestNextInput{})
				if err != nil {
					failed.Store(err)
					return
				}
				if out.Request == nil {
					return
				}
				mu.Lock()
				got[out.Request.ID] = append(got[out.Request.ID], actor)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if err, _ := failed.Load().(error); err != nil {
		t.Fatal(err)
	}
	if len(got) != requests {
		t.Errorf("claimed %d of %d", len(got), requests)
	}
	for id, actors := range got {
		if !want[id] || len(actors) != 1 {
			t.Errorf("%s claimed by %v", id, actors)
		}
	}
	list, err := RequestList.Run(context.Background(), person, RequestListInput{Status: model.RequestClaimed})
	if err != nil || len(list.Items) != requests {
		t.Fatalf("claimed list = %d, %v", len(list.Items), err)
	}
	for _, it := range list.Items {
		if it.ClaimedBy != got[it.ID][0] {
			t.Errorf("%s: file says %s, claimant %v", it.ID, it.ClaimedBy, got[it.ID])
		}
	}
	if lint, _ := person.Store.Lint(context.Background()); len(lint) != 0 {
		t.Errorf("lint: %v", lint)
	}
}
