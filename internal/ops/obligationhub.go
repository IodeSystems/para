package ops

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// An obligation is a hub. What it needs first (Before: the open and done prep,
// its children and what it depends on), the documents that link it, what it
// serves (its reasons, reason.go), and what came out of it (children made
// after its date, such as a meeting's action items and follow-up obligations)
// are read from links; none is stored. A task's, an ask's, and an
// obligation's parent may be an obligation.

// parentRef is a checked parent: the link, and the parent's project and
// area, which a child with neither takes.
type parentRef struct {
	link          model.Link
	project, area model.Link
}

// parentOf checks the id of a parent: an open task or obligation.
func parentOf(ctx context.Context, d Deps, id string) (parentRef, error) {
	doc, err := d.Store.Get(ctx, normID(id))
	if err != nil {
		return parentRef{}, linkErr("parent", normID(id), err)
	}
	switch m := doc.Meta.(type) {
	case *model.Task:
		if taskTerminal(m.Status) {
			return parentRef{}, fmt.Errorf("parent %s is already %s", doc.ID, m.Status)
		}
		return parentRef{link: model.LinkTo(doc.ID), project: m.Goal, area: m.Area}, nil
	case *model.Obligation:
		if !m.Open() {
			return parentRef{}, fmt.Errorf("parent %s is already closed (%s)", doc.ID, m.Outcome)
		}
		return parentRef{link: model.LinkTo(doc.ID), project: m.Goal, area: m.Area}, nil
	}
	return parentRef{}, fmt.Errorf("parent %s is a %s: want a task or an obligation", doc.ID, typeWord(doc))
}

// obligationDate is the day an obligation falls on: its due date, else the
// day of its start; "" when it has neither.
func obligationDate(o *model.Obligation) string {
	if o.Due != "" {
		return string(o.Due)
	}
	day, _ := meetingDay(o.Scheduled)
	return day
}

// party is one interested party of an obligation: a person or group link
// (ID set), or free text (an attendee not in the directory).
type party struct {
	ID   string
	Text string
}

// partiesOf are the interested parties of an obligation: owed_to, with,
// interested, and the attendees, each once.
func partiesOf(o *model.Obligation) []party {
	var out []party
	seen := map[string]bool{}
	for _, ls := range [][]model.Link{o.OwedTo, o.With, o.Interested} {
		for _, l := range ls {
			if t := l.Target(); t != "" && !seen[t] {
				seen[t] = true
				out = append(out, party{ID: t})
			}
		}
	}
	for _, a := range o.Attendees {
		if a = strings.TrimSpace(a); a != "" && !seen[a] {
			seen[a] = true
			out = append(out, party{Text: a})
		}
	}
	return out
}

// promised reports whether an obligation is a promise para keeps score of:
// the owner's, made to people (it has interested parties or replies to a
// message), and not a recurring artifact an agent produces.
func promised(o *model.Obligation) bool {
	return o.Produce == nil && (len(partiesOf(o)) > 0 || o.ReplyTo != "")
}

// noticeDay is the day a notice counts as given: the day it was approved,
// else the day it was sent; "" when it was neither (a draft, dropped, or
// gone).
func noticeDay(doc model.Document) string {
	m, ok := doc.Meta.(*model.IO)
	if !ok || doc.Folder == model.FolderArchive || m.Direction != model.IOOut {
		return ""
	}
	switch {
	case m.ApprovedAt != "" && (m.State == model.IOReady || m.State == model.IOSent):
		day, _ := meetingDay(m.ApprovedAt)
		return day
	case m.State == model.IOSent:
		day, _ := meetingDay(m.At)
		return cmp.Or(day, string(m.Updated))
	case m.State == model.IOReady:
		return string(m.Updated)
	}
	return ""
}

// noticeBy reports whether a notice of the obligation was approved or sent
// on or before day ("" is any day).
func noticeBy(get func(string) (model.Document, error), o *model.Obligation, day string) bool {
	for _, l := range o.Notices {
		doc, err := get(l.Target())
		if err != nil {
			continue
		}
		if nd := noticeDay(doc); nd != "" && (day == "" || nd <= day) {
			return true
		}
	}
	return false
}

// promiseNow is how the promise stands on today: broken stays broken; an open
// one past its date is broken; a notice approved or sent by its date makes
// it renegotiated; closed done by its date is kept, after it broken.
func promiseNow(get func(string) (model.Document, error), o *model.Obligation, today string) model.Promise {
	if !promised(o) || o.Promise == model.PromiseBroken {
		return o.Promise
	}
	date := obligationDate(o)
	if o.Open() {
		switch {
		case date != "" && date < today:
			return model.PromiseBroken
		case noticeBy(get, o, date):
			return model.PromiseRenegotiated
		}
		return o.Promise
	}
	closed := string(o.Closed)
	switch {
	case noticeBy(get, o, cmp.Or(date, closed)):
		return model.PromiseRenegotiated
	case date != "" && closed > date:
		return model.PromiseBroken
	case o.Outcome == model.OutcomeDone && o.Promise == "":
		return model.PromiseKept
	}
	return o.Promise
}

// storeGet reads a document of the store by id.
func storeGet(ctx context.Context, s store.Store) func(string) (model.Document, error) {
	return func(id string) (model.Document, error) { return s.Get(ctx, id) }
}

// effortHours are the hours of work an effort stands for, for the timeline's
// at-risk check.
var effortHours = map[model.Effort]float64{model.EffortXS: 0.25, model.EffortS: 1, model.EffortM: 4, model.EffortL: 8}

// effortLeadDays is how many days before an obligation its prep of an effort
// falls due (the backward date).
var effortLeadDays = map[model.Effort]int{model.EffortXS: 0, model.EffortS: 0, model.EffortM: 1, model.EffortL: 2}

// prepEffort is the effort of a prep item, the value order's default when
// unset.
func prepEffort(kind string, e model.Effort, owed bool) model.Effort {
	eff, _ := effortOf(ValueNode{Kind: kind, Effort: e, Owed: owed})
	return eff
}

// backDate is the date prep of effort e falls due before an obligation on
// date: the date minus the effort's lead.
func backDate(date string, e model.Effort) string {
	t, err := time.Parse(dateLayout, date)
	if err != nil {
		return ""
	}
	return t.AddDate(0, 0, -effortLeadDays[e]).Format(dateLayout)
}

// ObligationPart is one item of an obligation's page: prep, or what came out
// of it. Relation is child (its parent is the obligation) or depends_on (the
// obligation waits on it). Back is the backward date of open prep: the
// obligation's date minus the effort's lead.
type ObligationPart struct {
	ID       string       `json:"id"`
	Title    string       `json:"title"`
	Type     model.Type   `json:"type"`
	Kind     string       `json:"kind,omitempty"`
	Status   string       `json:"status"`
	Done     bool         `json:"done"`
	Effort   model.Effort `json:"effort"`
	Due      string       `json:"due,omitempty"`
	Back     string       `json:"back,omitempty"`
	Blocked  bool         `json:"blocked,omitempty"`
	Relation string       `json:"relation"`
	Created  string       `json:"created,omitempty"`
}

// Relations of an ObligationPart.
const (
	PartChild     = "child"
	PartDependsOn = "depends_on"
)

// ObligationPartsInput is the input of obligation_parts.
type ObligationPartsInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// ObligationParty is an interested party with the medium a notice to them
// goes by.
type ObligationParty struct {
	ID     string       `json:"id,omitempty"`
	Name   string       `json:"name"`
	Medium model.Medium `json:"medium"`
}

// NoticeRef is one notice an obligation_change wrote.
type NoticeRef struct {
	ID     string        `json:"id"`
	Title  string        `json:"title"`
	State  model.IOState `json:"state"`
	Medium model.Medium  `json:"medium"`
	To     []string      `json:"to"`
}

// ObligationPartsOutput is the hub of an obligation. Changes are the lines
// under ## Changes, as written.
type ObligationPartsOutput struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Kind      model.TaskKind    `json:"kind"`
	State     model.State       `json:"state"`
	Outcome   model.TaskOutcome `json:"outcome,omitempty"`
	Date      string            `json:"date,omitempty"`
	Start     string            `json:"start,omitempty"`
	End       string            `json:"end,omitempty"`
	Series    bool              `json:"series"`
	Promise   model.Promise     `json:"promise,omitempty"`
	Before    []ObligationPart  `json:"before"`
	Documents []PartRef         `json:"documents"`
	CameOut   []ObligationPart  `json:"came_out"`
	Parties   []ObligationParty `json:"parties"`
	Notices   []NoticeRef       `json:"notices"`
	Changes   []string          `json:"changes"`
	// Calendar is true for a meeting with calendar events: a push or cancel
	// also drafts the calendar change.
	Calendar bool `json:"calendar"`
}

// ObligationParts shows an obligation's hub.
var ObligationParts = defineRead("obligation_parts", "Show an obligation as a hub: before (its prep: open and done children, whose parent is the obligation, and what it depends_on, each with state, effort, and the backward date open prep falls due by: the obligation's date minus the effort's lead, xs and s 0 days, m 1, l 2), documents (notes, tracked documents, and sources it links or that link it), came_out (children made after its date: a meeting's action items, follow-up obligations), parties (who is told of a change, and by which medium), notices (the drafts obligation_change wrote), changes, and promise (kept, renegotiated, broken). Its reasons are the context op's", func(ctx context.Context, d Deps, in ObligationPartsInput) (ObligationPartsOutput, error) {
	doc, o, err := getObligation(ctx, d, in.ID)
	if err != nil {
		return ObligationPartsOutput{}, err
	}
	docs, err := allDocs(ctx, d.Store)
	if err != nil {
		return ObligationPartsOutput{}, fmt.Errorf("ops: obligation_parts: %w", err)
	}
	get := func(id string) (model.Document, error) {
		if x, ok := docs[id]; ok {
			return x, nil
		}
		return model.Document{}, model.ErrNotFound
	}
	today := d.Store.Now().Format(dateLayout)
	out := ObligationPartsOutput{ID: doc.ID, Title: doc.Title, Kind: o.Kind, State: o.State, Outcome: o.Outcome, Date: obligationDate(o),
		Start: o.Scheduled, End: o.End, Series: o.Schedule == model.ScheduleSeries, Promise: promiseNow(get, o, today),
		Before: []ObligationPart{}, Documents: []PartRef{}, CameOut: []ObligationPart{}, Parties: []ObligationParty{}, Notices: []NoticeRef{},
		Changes: changeLines(doc.Body), Calendar: o.Kind == model.KindMeeting && len(o.UIDs()) > 0}
	before, after := obligationPrep(docs, doc.ID, o)
	out.Before, out.CameOut = before, after
	out.Documents = linkedDocs(docs, doc)
	for _, p := range partiesOf(o) {
		out.Parties = append(out.Parties, partyView(docs, p))
	}
	for _, l := range o.Notices {
		nd, ok := docs[l.Target()]
		if !ok {
			continue
		}
		if m, ok := nd.Meta.(*model.IO); ok {
			out.Notices = append(out.Notices, NoticeRef{ID: nd.ID, Title: nd.Title, State: m.State, Medium: m.Medium, To: m.To})
		}
	}
	return out, nil
})

// obligationPrep sorts the items of an obligation: prep (children made by its
// date, and what it depends on, through any chain) and what came out of it
// (children made after its date, or after it closed when it has none).
func obligationPrep(docs map[string]model.Document, id string, o *model.Obligation) (before, after []ObligationPart) {
	before, after = []ObligationPart{}, []ObligationPart{}
	date := obligationDate(o)
	cut := cmp.Or(date, string(o.Closed))
	kids := map[string][]string{}
	for cid, cd := range docs {
		if cd.Folder == model.FolderArchive || cd.Meta.Header().DuplicateOf != "" {
			continue
		}
		if p := parentLink(cd.Meta); p != "" {
			kids[p] = append(kids[p], cid)
		}
	}
	for k := range kids {
		slices.Sort(kids[k])
	}
	seen := map[string]bool{id: true}
	var walk func(cur string, rel string, deps []model.Link, children bool)
	add := func(cid, rel string) (ObligationPart, bool) {
		cd, ok := docs[cid]
		if !ok || seen[cid] {
			return ObligationPart{}, false
		}
		seen[cid] = true
		return partOf(cd, rel, date), true
	}
	walk = func(cur, rel string, deps []model.Link, children bool) {
		for _, l := range deps {
			p, ok := add(l.Target(), PartDependsOn)
			if !ok {
				continue
			}
			before = append(before, p)
			walk(p.ID, PartDependsOn, depsOf(docs[p.ID].Meta), true)
		}
		if !children {
			return
		}
		for _, cid := range kids[cur] {
			cd := docs[cid]
			created := string(cd.Meta.Header().Created)
			// A child of the obligation itself came out of it when it was
			// made after its date (or its close, with no date), or is an
			// action item meeting_hold filed; prep of prep is prep.
			if cur == id && (cut != "" && created > cut || strings.Contains(cd.Body, fmt.Sprintf(fromMeeting, id))) {
				if p, ok := add(cid, PartChild); ok {
					after = append(after, p)
				}
				continue
			}
			p, ok := add(cid, PartChild)
			if !ok {
				continue
			}
			before = append(before, p)
			walk(p.ID, PartChild, depsOf(cd.Meta), true)
		}
	}
	walk(id, PartChild, model.HardDeps(o), true)
	return before, after
}

// parentLink is the parent a document names: a task's, an obligation's, or
// an ask's.
func parentLink(m model.Meta) string {
	switch x := m.(type) {
	case *model.Task:
		return x.Parent.Target()
	case *model.Obligation:
		return x.Parent.Target()
	case *model.RFX:
		return x.Parent.Target()
	}
	return ""
}

// depsOf is what a work item depends on.
func depsOf(m model.Meta) []model.Link {
	if deps := model.HardDeps(m); deps != nil {
		return deps
	}
	return nil
}

// partOf is a document as an item of an obligation on date.
func partOf(doc model.Document, rel, date string) ObligationPart {
	p := ObligationPart{ID: doc.ID, Title: doc.Title, Type: doc.Meta.Header().Type, Relation: rel, Created: string(doc.Meta.Header().Created)}
	owed := false
	kind := KindTask
	switch m := doc.Meta.(type) {
	case *model.Task:
		p.Kind, p.Status, p.Done, p.Effort, p.Due = string(m.EffectiveKind()), string(m.Status), taskTerminal(m.Status), m.Effort, string(m.Due)
		p.Blocked = m.Status == model.TaskBlocked || len(m.BlockedBy) > 0
	case *model.Obligation:
		p.Kind, p.Status, p.Done, p.Effort, p.Due = string(m.Kind), string(m.State), !m.Open(), m.Effort, obligationDate(m)
		if m.Outcome != "" {
			p.Status = string(m.Outcome)
		}
	case *model.RFX:
		p.Kind, p.Status, p.Done, p.Effort, p.Due = "ask", string(m.Status), m.Status != model.RFXOpen, m.Effort, string(m.NextNudge)
		kind, owed = KindAsk, m.Direction == model.RFXIn
	case *model.Question:
		p.Kind, p.Status, p.Done, p.Effort = "question", string(m.Status), m.Status != model.QuestionOpen, m.Effort
		kind = KindQuestion
	default:
		p.Status = string(lifeOf(doc.Meta))
		p.Done = lifeOf(doc.Meta) == LifeClosed
	}
	p.Effort = prepEffort(kind, p.Effort, owed)
	if !p.Done && date != "" {
		p.Back = backDate(date, p.Effort)
	}
	return p
}

// linkedDocs are the notes, tracked documents, and sources an obligation's
// body links, and those whose body links it.
func linkedDocs(docs map[string]model.Document, doc model.Document) []PartRef {
	out := []PartRef{}
	for oid, od := range docs {
		switch od.Meta.Header().Type {
		case model.TypeNote, model.TypeTrackedDoc, model.TypeSource, model.TypeTranscript:
		default:
			continue
		}
		if od.Folder == model.FolderArchive || !bodyLinks(doc.Body, oid) && !bodyLinks(od.Body, doc.ID) {
			continue
		}
		out = append(out, PartRef{ID: oid, Title: od.Title, Kind: string(od.Meta.Header().Type)})
	}
	slices.SortFunc(out, func(a, b PartRef) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

// partyView names a party and the medium a notice to them goes by.
func partyView(docs map[string]model.Document, p party) ObligationParty {
	if p.ID == "" {
		return ObligationParty{Name: p.Text, Medium: textMedium(p.Text)}
	}
	v := ObligationParty{ID: p.ID, Name: p.ID, Medium: model.MediumOther}
	if pd, ok := docs[p.ID]; ok {
		v.Name = cmp.Or(pd.Title, p.ID)
		v.Medium = partyMedium(pd.Meta)
	}
	return v
}

// partyMedium is how a notice reaches a person: their preferred contact
// method, else their first one that is not a meeting; other for a group or
// a person with none.
func partyMedium(m model.Meta) model.Medium {
	p, ok := m.(*model.Person)
	if !ok {
		return model.MediumOther
	}
	var first model.Medium
	for _, c := range p.Contacts {
		if c.Method == model.MediumMeeting {
			continue
		}
		if c.Preferred {
			return c.Method
		}
		if first == "" {
			first = c.Method
		}
	}
	return cmp.Or(first, model.MediumOther)
}

// textMedium is the medium of a party in free text: email when it holds an
// address.
func textMedium(s string) model.Medium {
	if strings.Contains(s, "@") {
		return model.MediumEmail
	}
	return model.MediumOther
}

// changesHeading is the section of an obligation's changes.
const changesHeading = "## Changes"

// changeLines are the lines under ## Changes, without the list marker.
func changeLines(body string) []string {
	out := []string{}
	for _, l := range sectionLines(body, changesHeading) {
		out = append(out, strings.TrimPrefix(l, "- "))
	}
	return out
}
