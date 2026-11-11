package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	evlog "github.com/iodesystems/para/internal/events"
	"github.com/iodesystems/para/internal/model"
)

// activity is the change log of the vault: what changed, newest first, from
// the event files of every install merged by time, joined with the receipts
// of agent writes. Events hold ids and metadata only; the title of an item
// and the reason of a write are read here, at read time, and stored nowhere.

// Kinds of activity rows. Who made the change is a separate field.
const (
	ActCreated  = "created"
	ActEdited   = "edited"
	ActState    = "state"
	ActClosed   = "closed"
	ActReopened = "reopened"
	ActMoved    = "moved"
	ActRenamed  = "renamed"
	ActSent     = "sent"
	ActApproved = "approved"
	ActReviewed = "reviewed"
	ActTurn     = "turn"
	ActReply    = "reply"
	ActExternal = "external"
	ActMerged   = "merged"
	ActUndone   = "undone"
)

// ActivityKinds lists every kind of an activity row.
var ActivityKinds = []string{ActCreated, ActEdited, ActState, ActClosed, ActReopened, ActMoved, ActRenamed,
	ActSent, ActApproved, ActReviewed, ActTurn, ActReply, ActExternal, ActMerged, ActUndone}

// Values of activity.actor besides an agent's name.
const (
	ActorOwner    = "owner"    // the owner on a person surface
	ActorAgents   = "agents"   // any agent, and the tick
	ActorExternal = "external" // hand edits, outside para
	ActorRemote   = "remote"   // events from another install
)

const (
	activityFeedWindow = 20000
	activityFeedLimit  = 50
	// activityBurst is how close two changes of one actor to one item are to
	// become one row.
	activityBurst = time.Minute
)

// ActivityInput is the input of activity. Empty fields match all. Actor is
// owner, agents, external, remote, or an agent's name. Types are kinds.
// Since and Until are RFC 3339 or YYYY-MM-DD (until is the end of that day).
type ActivityInput struct {
	Since  string   `json:"since,omitempty" query:"since"`
	Until  string   `json:"until,omitempty" query:"until"`
	Actor  string   `json:"actor,omitempty" query:"actor"`
	Types  []string `json:"types,omitempty" query:"types"`
	Doc    string   `json:"doc,omitempty" query:"doc"`
	Limit  int      `json:"limit,omitempty" query:"limit"`   // 0 means 50
	Cursor string   `json:"cursor,omitempty" query:"cursor"` // next_cursor of the last page
}

// ChangeRow is one change, or a burst of them: the same actor, item, and
// kind within a minute of each other. At is the latest of them, First the
// earliest. Count is how many events it holds. Who is owner, agent:<name>,
// tick, or hand edit; Remote is true when another install made it. Receipt
// is the latest receipt of the row, Receipts all of them; Why and Sources are
// the latest receipt's. Lines is the diff of a one-write row, filled only
// when the call names a doc.
type ChangeRow struct {
	At       string   `json:"at"`
	First    string   `json:"first"`
	Count    int      `json:"count"`
	Who      string   `json:"who"`
	Actor    string   `json:"actor,omitempty"`
	Install  string   `json:"install,omitempty"`
	Remote   bool     `json:"remote,omitempty"`
	Kind     string   `json:"kind"`
	Summary  string   `json:"summary"`
	From     string   `json:"from,omitempty"`
	To       string   `json:"to,omitempty"`
	Doc      string   `json:"doc"`
	DocType  string   `json:"doc_type,omitempty"`
	DocName  string   `json:"doc_name,omitempty"`
	Request  string   `json:"request,omitempty"`
	Receipt  string   `json:"receipt,omitempty"`
	Receipts []string `json:"receipts,omitempty"`
	Why      string   `json:"why,omitempty"`
	Sources  []string `json:"sources,omitempty"`
	Lines    []string `json:"lines,omitempty"`

	t time.Time
}

// ActivityOutput is one page of activity, newest first. NextCursor is the
// cursor of the next page when More.
type ActivityOutput struct {
	Items      []ChangeRow `json:"items"`
	More       bool          `json:"more"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

// Activity lists what changed in the vault.
var Activity = defineRead("activity", "List what changed in the vault, newest first: when, who (owner, agent, tick, hand edit, another install), what (created, edited, state, closed, reopened, moved, renamed, sent, approved, reviewed, turn, reply, external, merged, undone), the item, and why when an agent write has a receipt. Changes of one actor to one item within a minute are one row with a count. Filters: since, until, actor (owner, agents, external, remote, or an agent name), types, doc. Page with cursor (next_cursor)", func(ctx context.Context, d Deps, in ActivityInput) (ActivityOutput, error) {
	since, err := activityTime(in.Since, false)
	if err != nil {
		return ActivityOutput{}, fmt.Errorf("since: %w", err)
	}
	until, err := activityTime(in.Until, true)
	if err != nil {
		return ActivityOutput{}, fmt.Errorf("until: %w", err)
	}
	var cursor time.Time
	if in.Cursor != "" {
		if cursor, err = time.Parse(time.RFC3339Nano, in.Cursor); err != nil {
			return ActivityOutput{}, fmt.Errorf("cursor %q: want the next_cursor of a page", in.Cursor)
		}
	}
	for _, k := range in.Types {
		if !slices.Contains(ActivityKinds, k) {
			return ActivityOutput{}, fmt.Errorf("types %q: want one of %s", k, strings.Join(ActivityKinds, ", "))
		}
	}
	limit := in.Limit
	if limit <= 0 {
		limit = activityFeedLimit
	}
	rows, err := activityFeed(ctx, d, in, since, until)
	if err != nil {
		return ActivityOutput{}, err
	}
	out := ActivityOutput{Items: []ChangeRow{}}
	for _, r := range rows {
		if !cursor.IsZero() && !r.t.Before(cursor) {
			continue
		}
		// A page never splits rows of one instant, so a cursor loses none.
		if len(out.Items) >= limit && !r.t.Equal(out.Items[len(out.Items)-1].t) {
			out.More = true
			break
		}
		out.Items = append(out.Items, r)
	}
	if out.More {
		out.NextCursor = out.Items[len(out.Items)-1].At
	}
	for i := range out.Items {
		activityFill(ctx, d, &out.Items[i], in.Doc != "")
	}
	return out, nil
})

// activityTime reads an RFC 3339 time or a local day; endOfDay makes a day
// end at its close. Empty is the zero time.
func activityTime(s string, endOfDay bool) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	t, err := time.ParseInLocation(dateLayout, s, time.Local)
	if err != nil {
		return t, errors.New("want an RFC 3339 time or YYYY-MM-DD")
	}
	if endOfDay {
		t = t.AddDate(0, 0, 1)
	}
	return t, nil
}

// activityFeed builds every row that passes the filters, newest first.
func activityFeed(ctx context.Context, d Deps, in ActivityInput, since, until time.Time) ([]ChangeRow, error) {
	return activityFeedSkip(ctx, d, in, since, until, nil)
}

// activityFeedSkip is activityFeed without the receipts of the ops in skipOps. The Pinned read
// hides the pin itself this way.
func activityFeedSkip(ctx context.Context, d Deps, in ActivityInput, since, until time.Time, skipOps []string) ([]ChangeRow, error) {
	evs, err := d.Store.Events(ctx, activityFeedWindow)
	if err != nil {
		return nil, err
	}
	self := d.Store.Info().Install
	doc := normID(in.Doc)
	// The events a receipt already tells: the write it records.
	used := make([]bool, len(evs))
	for i, e := range evs {
		if e.Op != "receipt" {
			continue
		}
		for j := i - 1; j >= 0 && e.TS.Sub(evs[j].TS) <= receiptSlack; j-- {
			if p := evs[j]; p.Doc == e.Doc && p.Actor == e.Actor && p.Install == e.Install && slices.Contains(itemOps, p.Op) {
				used[j] = true
			}
		}
	}
	receipts := map[string]*model.Receipt{}
	var rows []ChangeRow
	for i, e := range evs {
		if used[i] || e.TS.Before(since) || (!until.IsZero() && !e.TS.Before(until)) {
			continue
		}
		row, ok := activityOf(e)
		if !ok || strings.HasPrefix(row.Doc, "receipt-") {
			continue
		}
		if doc != "" && row.Doc != doc {
			continue
		}
		row.Install, row.Remote = e.Install, e.Install != "" && self != "" && e.Install != self
		if !activityActor(in.Actor, row) {
			continue
		}
		if e.Op == "receipt" {
			row.Receipt, row.Receipts = e.To, []string{e.To}
			row.Request = e.From
			r, ok := receipts[e.To]
			if !ok {
				if _, r, err = getReceipt(ctx, d.Store, e.To); err != nil {
					r = nil
				}
				receipts[e.To] = r
			}
			if r != nil && slices.Contains(skipOps, r.Op) {
				continue
			}
			if r != nil {
				row.Kind, row.Summary = receiptKind(r.Op)
				row.Why, row.Sources = r.Why, r.Sources
			}
		}
		if len(in.Types) > 0 && !slices.Contains(in.Types, row.Kind) {
			continue
		}
		rows = append(rows, row)
	}
	return burst(rows), nil
}

// activityActor reports whether a row matches an actor filter.
func activityActor(want string, r ChangeRow) bool {
	switch want {
	case "":
		return true
	case ActorOwner:
		return r.Who == WhoOwner
	case ActorAgents:
		return strings.HasPrefix(r.Who, AgentPrefix) || r.Who == WhoTick
	case ActorExternal:
		return r.Who == WhoHandEdit
	case ActorRemote:
		return r.Remote
	}
	return r.Actor == strings.TrimPrefix(want, AgentPrefix)
}

// burst merges rows of one actor, item, and kind that follow each other
// within activityBurst, and returns them newest first. rows are oldest first.
func burst(rows []ChangeRow) []ChangeRow {
	type key struct{ who, actor, install, doc, kind string }
	open := map[key]int{}
	var out []ChangeRow
	for _, r := range rows {
		k := key{r.Who, r.Actor, r.Install, r.Doc, r.Kind}
		if i, ok := open[k]; ok && r.t.Sub(out[i].t) <= activityBurst {
			m := &out[i]
			m.Count += r.Count
			m.t, m.At, m.To, m.Summary = r.t, r.At, r.To, r.Summary
			if r.Receipt != "" {
				m.Receipt, m.Why, m.Sources = r.Receipt, r.Why, r.Sources
				m.Receipts = append(m.Receipts, r.Receipts...)
			}
			continue
		}
		open[k] = len(out)
		out = append(out, r)
	}
	slices.SortStableFunc(out, func(a, b ChangeRow) int { return b.t.Compare(a.t) })
	return out
}

// activityOf makes the row of one event, or reports that the op is not a
// change to an item.
func activityOf(e model.Event) (ChangeRow, bool) {
	r := ChangeRow{
		At: e.TS.UTC().Format(time.RFC3339Nano), First: e.TS.UTC().Format(time.RFC3339Nano), t: e.TS, Count: 1,
		Who: whoOf(e.Actor, e.Surface, e.Op), Actor: e.Actor, Doc: e.Doc, From: e.From, To: e.To,
	}
	prefix, _, _ := strings.Cut(e.Doc, "-")
	words := func(kind, s string) (ChangeRow, bool) { r.Kind, r.Summary = kind, s; return r, true }
	switch e.Op {
	case evlog.OpExternalEdit:
		return words(ActExternal, "Edited by hand, outside para")
	case evlog.OpCreate:
		switch prefix {
		case "request":
			return words(ActTurn, "Asked an agent")
		case "io":
			return words(ActReply, "Message added")
		}
		return words(ActCreated, "Created in "+e.To)
	case evlog.OpMove:
		return words(ActMoved, "Moved from "+e.From+" to "+e.To)
	case evlog.OpRename:
		return words(ActRenamed, "Renamed from "+e.From+" to "+e.To)
	case "reopen":
		return words(ActReopened, "Reopened: "+arrow(e.From, e.To))
	case "receipt":
		return words(ActEdited, "Agent write")
	case opReceiptUndo:
		r.Doc, r.Receipt, r.From, r.To = e.To, e.Doc, "", ""
		return words(ActUndone, "Undid a write")
	case opTaskClose:
		return words(ActClosed, "Closed: "+firstOf(e.To, "done"))
	case "doc_merge":
		r.Doc = e.To
		return words(ActMerged, "Merged "+e.Doc+" into "+e.To)
	case "doc_version_add":
		return words(ActEdited, "New version "+e.To)
	case evlog.OpUpdate:
		if e.From == e.To {
			return words(ActEdited, "Edited")
		}
		switch {
		case prefix == "request":
			return words(ActTurn, "Agent work: "+arrow(e.From, e.To))
		case prefix == "io" && e.To == string(model.IOSent):
			return words(ActSent, "Sent")
		case prefix == "io" && e.To == string(model.IOReady):
			return words(ActApproved, "Approved to send")
		case slices.Contains(closedStates, e.To):
			return words(ActClosed, "Closed: "+arrow(e.From, e.To))
		}
		return words(ActState, arrow(e.From, e.To))
	}
	return r, false
}

// closedStates are the states that end an item, in every type.
var closedStates = []string{"done", "dropped", "closed", "answered", "resolved", "accepted", "declined", "retired", "archived", "held", "cancelled", "superseded"}

func arrow(from, to string) string { return firstOf(from, "none") + " → " + firstOf(to, "none") }

// receiptKind names what an agent write was from its op.
func receiptKind(op string) (string, string) {
	kind := ActEdited
	switch {
	case strings.Contains(op, "review"):
		kind = ActReviewed
	case strings.Contains(op, "approve"):
		kind = ActApproved
	case op == "io_done" || strings.Contains(op, "send"):
		kind = ActSent
	case strings.Contains(op, "respond") || strings.HasPrefix(op, "request_") || strings.HasPrefix(op, "thread_"):
		kind = ActTurn
	case strings.Contains(op, "reply"):
		kind = ActReply
	case strings.Contains(op, "reopen"):
		kind = ActReopened
	case strings.Contains(op, "resolve") || strings.HasSuffix(op, "_done") || strings.HasSuffix(op, "_close"):
		kind = ActClosed
	case strings.HasSuffix(op, "_add") || strings.HasSuffix(op, "_create"):
		kind = ActCreated
	case strings.Contains(op, "move"):
		kind = ActMoved
	}
	return kind, opWords(op)
}

// activityFill adds what is read at read time: the title and type of the
// item, and, with diff, the lines a one-write row changed.
func activityFill(ctx context.Context, d Deps, r *ChangeRow, diff bool) {
	if doc, err := d.Store.Get(ctx, r.Doc); err == nil {
		r.DocName, r.DocType = doc.Title, string(doc.Meta.Header().Type)
	}
	if !diff || r.Receipt == "" || len(r.Receipts) != 1 {
		return
	}
	rd, rc, err := getReceipt(ctx, d.Store, r.Receipt)
	if err != nil {
		return
	}
	c, err := model.ParseReceiptBody(rd.Body)
	if err != nil {
		return
	}
	for _, p := range receiptParts(rc.Doc, c) {
		if p.Doc == r.Doc {
			r.Lines = trimContext(diffLines(p.Before, p.After), 2)
			return
		}
	}
}
