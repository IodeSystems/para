package ops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// A lint refusal of an agent's write is not lost. The op layer files one
// request of kind retry-write with the op, the lint findings, and the input
// the agent sent. The next loop works it in one of two tiers:
//
//   - form only (escape raw HTML, flatten a heading, fix a link target, trim a
//     field: the meaning is the same): the agent fixes the input, calls the op
//     again, and ends the request done with the receipt.
//   - meaning changes, or it cannot fix it: retry_propose records the proposed
//     input on the request and ends the turn needs_clarification. The request
//     then needs the owner, who decides with retry_decide: accept, edit,
//     drop, or reject with notes (back to the agent).
//
// The kind is its own, not fix-write: fix-write is the owner marking a
// receipt wrong, and this is the agent's write that never happened.

// RetryWriteKind is the kind of agent work that retries a refused write.
const RetryWriteKind = "retry-write"

// Vault setting of retry work.
const (
	RetryCapKey = "request.retry_daily_cap"
	// DefaultRetryCap is how many retry-write requests a day when unset.
	DefaultRetryCap = 20
)

const (
	opRetryWrite = "retry_write" // event: a refused write was filed as a request
	// retryInputMax is the most bytes of input a request holds. A larger one
	// is withheld.
	retryInputMax = 8000
)

// forceQueueKey on a context queues a request at once, whatever the surface:
// the op layer files retry work for an agent that the owner already approved.
type forceQueueKey struct{}

// retryNoteError is the error of a refused write plus the note that it was
// filed. It unwraps to the lint error.
type retryNoteError struct {
	err  error
	note string
}

func (e *retryNoteError) Error() string { return e.err.Error() + "\n  " + e.note }
func (e *retryNoteError) Unwrap() error { return e.err }

// fileRetry files the request for a refused agent write and returns err with
// a note. Any other outcome returns err as it was: a filing problem never
// hides the lint error.
func fileRetry[I any](ctx context.Context, d Deps, opName string, in I, err error) error {
	var lint *model.LintError
	if !errors.As(err, &lint) {
		return err
	}
	id, filed, ferr := fileRetryRequest(ctx, d, opName, inputMap(in), lint)
	switch {
	case ferr != nil || id == "":
		return err
	case filed:
		return &retryNoteError{err, fmt.Sprintf("filed as agent work %s: the next loop retries it", id)}
	}
	return &retryNoteError{err, fmt.Sprintf("already filed as agent work %s: nothing added", id)}
}

var (
	retryOpRe    = regexp.MustCompile(`(?m)^Op: (\S+)$`)
	retryActorRe = regexp.MustCompile(`(?m)^Actor: (.+)$`)
	retryHashRe  = regexp.MustCompile(`(?m)^Input hash: (\S+)$`)
	backtickRun  = regexp.MustCompile("`+")
)

// retryActive: a retry-write request that still waits for an agent or the owner.
func retryActive(r *model.Request) bool {
	switch r.Status {
	case model.RequestProposed, model.RequestQueued, model.RequestClaimed:
		return true
	case model.RequestDone:
		return r.Resolution.NeedsOwner()
	}
	return false
}

func fileRetryRequest(ctx context.Context, d Deps, opName string, input map[string]any, lint *model.LintError) (id string, filed bool, err error) {
	actor := d.Store.Info().Actor
	raw, err := marshalNoEscape(input)
	if err != nil {
		return "", false, err
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:8])
	docs, err := loadRequests(ctx, d.Store)
	if err != nil {
		return "", false, err
	}
	today, now := 0, d.Store.Now()
	for _, doc := range docs {
		r := doc.Meta.(*model.Request)
		if r.Kind != RetryWriteKind {
			continue
		}
		if retryActive(r) && field(retryOpRe, doc.Body) == opName && field(retryActorRe, doc.Body) == actor && field(retryHashRe, doc.Body) == hash {
			return doc.ID, false, nil
		}
		if t, err := time.Parse(time.RFC3339Nano, r.Queued); err == nil && t.In(now.Location()).Format(dateLayout) == now.Format(dateLayout) {
			today++
		}
	}
	limit, err := configInt(d.Store, RetryCapKey, DefaultRetryCap)
	if err != nil {
		return "", false, err
	}
	if today >= limit {
		return "", false, nil
	}
	target, _ := input["id"].(string)
	var findings strings.Builder
	for _, f := range lint.Findings {
		findings.WriteString(f.String() + "\n")
	}
	ask := func(withInput bool) string {
		var b strings.Builder
		fmt.Fprintf(&b, "Op: %s\nActor: %s\nInput hash: %s\n\n", opName, actor, hash)
		fmt.Fprintf(&b, "Lint refused the write of %s, so nothing was written. Fix the input so lint accepts it (the findings say what), call %s again, then close this request with the receipt; if the write is no longer wanted, close it as dropped with why.\n\n", opName, opName)
		b.WriteString("Form only (escape raw HTML, flatten a heading, fix a link target, trim a field: same meaning): fix it, call the op again, and end this request done with the receipt and a line on what you changed. Anything that changes the meaning, or that you cannot fix: do not retry; call retry_propose with the input you propose, and the owner decides.\n\n")
		b.WriteString(appendSection("", "Findings", fence("text", findings.String())))
		if withInput {
			b.WriteString("\n\n" + appendSection("", "Refused input", fence("json", string(raw))))
		} else {
			b.WriteString("\n\n## Refused input\n\nWithheld: too large, or it holds text the vault refuses. Rebuild it from what you were doing.")
		}
		return b.String()
	}
	title := "Retry a refused write: " + opName
	fctx := context.WithValue(ctx, forceQueueKey{}, true)
	// Try with the link and the input; drop what the vault refuses.
	for _, withLink := range []bool{target != "", false} {
		for _, withInput := range []bool{len(raw) <= retryInputMax, false} {
			in := RequestAddInput{Title: title, Ask: ask(withInput), Kind: RetryWriteKind}
			if withLink {
				in.Links = []string{target}
			}
			out, aerr := addRequest(fctx, d, in, "")
			if aerr != nil {
				continue
			}
			if rerr := d.Store.Record(ctx, opRetryWrite, out.ID, opName, target, "sha256:"+hex.EncodeToString(sum[:])); rerr != nil {
				return out.ID, true, rerr
			}
			return out.ID, true, nil
		}
		if target == "" {
			break
		}
	}
	return "", false, nil
}

func field(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// marshalNoEscape is indented JSON that keeps <, >, and & as written.
func marshalNoEscape(v any) ([]byte, error) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	if err := e.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(b.Bytes()), nil
}

// fence wraps text in a code fence longer than any backtick run inside it.
func fence(lang, text string) string {
	n := 3
	for _, run := range backtickRun.FindAllString(text, -1) {
		n = max(n, len(run)+1)
	}
	f := strings.Repeat("`", n)
	return f + lang + "\n" + strings.TrimRight(text, "\n") + "\n" + f
}

// lastFence returns the text of the last closed fenced block of a section.
func lastFence(section string) string {
	lines := strings.Split(section, "\n")
	start, last := -1, ""
	var mark string
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if start < 0 {
			if strings.HasPrefix(t, "```") {
				mark = strings.TrimRight(t, "abcdefghijklmnopqrstuvwxyz")
				start = i
			}
			continue
		}
		if t == mark {
			last, start = strings.Join(lines[start+1:i], "\n"), -1
		}
	}
	return last
}

// RetryProposeInput is the input of retry_propose.
type RetryProposeInput struct {
	Request string         `json:"request"`         // the retry-write request you claimed
	Input   map[string]any `json:"input"`           // the input you propose for the op
	Why     string         `json:"why"`             // what you changed and why the meaning changes, or why you could not fix it
	Links   []string       `json:"links,omitempty"` // ids the proposal rests on
}

// RetryProposeOutput is the output of retry_propose.
type RetryProposeOutput struct {
	ID     string              `json:"id"`
	Status model.RequestStatus `json:"status"`
	Rev    string              `json:"rev"`
}

// RetryPropose ends a turn of a retry-write request with a proposed input for
// the owner to decide on. The agent does not retry the op.
var RetryPropose = defineSafe("retry_propose", "For a retry-write request you claimed whose fix changes the meaning, or that you cannot fix: record the input you propose, and why. The owner decides (accept, edit, drop, reject with notes). Do not call the op yourself then", func(ctx context.Context, d Deps, in RetryProposeInput) (RetryProposeOutput, error) {
	why := strings.TrimSpace(in.Why)
	if why == "" {
		return RetryProposeOutput{}, errors.New("why is empty: say what you changed and why it changes the meaning")
	}
	if len(in.Input) == 0 {
		return RetryProposeOutput{}, errors.New("input is empty: the input you propose for the op")
	}
	doc, r, err := ownClaim(ctx, d, in.Request)
	if err != nil {
		return RetryProposeOutput{}, err
	}
	if r.Kind != RetryWriteKind {
		return RetryProposeOutput{}, fmt.Errorf("%s is kind %q, not %s", doc.ID, r.Kind, RetryWriteKind)
	}
	raw, err := marshalNoEscape(in.Input)
	if err != nil {
		return RetryProposeOutput{}, err
	}
	links, err := requestLinks(ctx, d, "links", in.Links)
	if err != nil {
		return RetryProposeOutput{}, err
	}
	// The input sits in a fence, so raw HTML in it cannot fail this write; the
	// why is the agent's prose and lint checks it like any body.
	doc.Body = appendSection(doc.Body, "Proposed fix", flattenHeadings(why)+"\n\n"+fence("json", string(raw)))
	r.Status, r.Lease, r.Resolution, r.Results, r.ActiveAt = model.RequestDone, "", model.ResolutionNeedsClarification, links, nowString(d.Store)
	doc, err = d.Store.Update(ctx, doc)
	return RetryProposeOutput{ID: doc.ID, Status: r.Status, Rev: doc.Rev}, err
})

// RetryAction is what the owner does with a proposed fix.
type RetryAction string

const (
	RetryAccept RetryAction = "accept" // run the op with the proposed input
	RetryEdit   RetryAction = "edit"   // save the owner's input as the proposal; with approve, run it
	RetryDrop   RetryAction = "drop"   // close it; nothing is written
	RetryReject RetryAction = "reject" // back to the agent with the owner's notes
)

// RetryActions lists every RetryAction.
var RetryActions = []RetryAction{RetryAccept, RetryEdit, RetryDrop, RetryReject}

// Enum lists the allowed values, for schema generation.
func (RetryAction) Enum() []string { return enumStrings(RetryActions) }

// RetryDecideInput is the input of retry_decide.
type RetryDecideInput struct {
	Request string         `json:"request"`
	Action  RetryAction    `json:"action"`            // accept, edit, drop, or reject
	Input   map[string]any `json:"input,omitempty"`   // edit: the input to save as the proposal (and run, with approve)
	Notes   string         `json:"notes,omitempty"`   // reject: the owner's words to the agent; drop: why
	Approve bool           `json:"approve,omitempty"` // edit: also run it; off, the edit only replaces the proposal
}

// RetryDecideOutput is the output of retry_decide.
type RetryDecideOutput struct {
	ID      string              `json:"id"`
	Status  model.RequestStatus `json:"status"`
	Applied bool                `json:"applied"`       // the op ran
	Doc     string              `json:"doc,omitempty"` // the document the op made or changed, when it says
}

// RetryDecide is the owner's decision on a proposed fix.
var RetryDecide = definePerson("retry_decide", "Decide on an agent's proposed fix of a refused write: accept (run the op with the proposed input), edit (save your input as the proposal; with approve, run it too), drop (close it, nothing written), or reject with notes (back to the agent). People only", func(ctx context.Context, d Deps, in RetryDecideInput) (RetryDecideOutput, error) {
	if !slices.Contains(RetryActions, in.Action) {
		return RetryDecideOutput{}, fmt.Errorf("action %q: want accept, edit, drop, or reject", in.Action)
	}
	notes := strings.TrimSpace(in.Notes)
	doc, r, err := getRequest(ctx, d.Store, in.Request)
	if err != nil {
		return RetryDecideOutput{}, err
	}
	if r.Kind != RetryWriteKind {
		return RetryDecideOutput{}, fmt.Errorf("%s is kind %q, not %s", doc.ID, r.Kind, RetryWriteKind)
	}
	if !retryActive(r) {
		return RetryDecideOutput{}, fmt.Errorf("%s is %s: nothing to decide", doc.ID, r.Status)
	}
	out := RetryDecideOutput{ID: doc.ID}
	switch in.Action {
	case RetryReject:
		if notes == "" {
			return out, errors.New("notes is empty: say what the agent should change")
		}
		r.Turn++
		doc.Body = appendSection(doc.Body, "Owner's notes", flattenHeadings(notes)+"\n\nPropose again with retry_propose, or fix the form and retry the op.")
		r.Status, r.Claimed, r.Lease, r.Resolution, r.Queued = model.RequestQueued, "", "", "", nowString(d.Store)
		r.ClaimedAt, r.ActiveAt = "", r.Queued
	case RetryDrop:
		doc.Body = appendSection(doc.Body, "Dropped", firstOf(flattenHeadings(notes), "The owner dropped the write. Nothing was written."))
		r.Status, r.Claimed, r.Lease, r.Resolution, r.ActiveAt = model.RequestCancelled, "", "", model.ResolutionCancelled, nowString(d.Store)
	case RetryEdit:
		if !in.Approve {
			// Save only: the edit is the new proposal and the request stays
			// with the owner. The earlier proposal stays above it in the body.
			if r.Status != model.RequestDone {
				return out, fmt.Errorf("%s is %s: there is no proposal to edit yet", doc.ID, r.Status)
			}
			if len(in.Input) == 0 {
				return out, errors.New("input is empty: the input to save")
			}
			shown, err := marshalNoEscape(in.Input)
			if err != nil {
				return out, err
			}
			doc.Body = appendSection(doc.Body, "Proposed fix", "Edited by the owner, not run.\n\n"+fence("json", string(shown)))
			r.ActiveAt = nowString(d.Store)
			break
		}
		fallthrough
	default:
		opName := field(retryOpRe, doc.Body)
		e, ok := registry[opName]
		if !ok || e.PersonOnly() || e.Risk() == RiskRead || slices.Contains(FlowOps, opName) {
			return out, fmt.Errorf("op %q: only a write an agent may call is run from here", opName)
		}
		input := in.Input
		if in.Action == RetryAccept {
			var perr error
			if input, perr = proposedInput(doc.Body); perr != nil {
				return out, perr
			}
		} else if len(input) == 0 {
			return out, errors.New("input is empty: the input to run")
		}
		raw, err := json.Marshal(input)
		if err != nil {
			return out, err
		}
		res, err := e.Invoke(ctx, d, raw)
		if err != nil {
			return out, fmt.Errorf("retry_decide: %s: %w", opName, err)
		}
		out.Applied = true
		if id, ok := inputMap(res)["id"].(string); ok {
			out.Doc = id
		}
		what := "accepted the proposed fix"
		if in.Action == RetryEdit {
			what = "edited the fix"
		}
		shown, _ := marshalNoEscape(input)
		doc.Body = appendSection(doc.Body, "Applied", fmt.Sprintf("The owner %s and %s ran.\n\n%s", what, opName, fence("json", string(shown))))
		now := nowString(d.Store)
		r.Status, r.Claimed, r.Lease, r.Resolution, r.ActiveAt, r.Seen = model.RequestDone, "", "", model.ResolutionDone, now, now
		if out.Doc != "" {
			if l, lerr := requestLinks(ctx, d, "links", []string{out.Doc}); lerr == nil {
				r.Results = l
			}
		}
	}
	saved, err := d.Store.Update(ctx, doc)
	if err == nil {
		out.Status = saved.Meta.(*model.Request).Status
	}
	return out, err
})

// proposedInput is the input of the newest Proposed fix section.
func proposedInput(body string) (map[string]any, error) {
	text := lastFence(lastSection(body, "Proposed fix"))
	if text == "" {
		return nil, errors.New("no proposed fix on this request: edit it, or reject it with notes")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		return nil, fmt.Errorf("proposed fix: %w", err)
	}
	return m, nil
}
