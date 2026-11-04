package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/recur"
	"github.com/iodesystems/para/internal/store"
)

// A sweep is an agent reading its sources and writing what it found with the
// usual ops: io_add for new input, doc_version_add and doc_comment_add for
// tracked documents, io_send or rfx_reply when sent output matches a draft or
// an Ask, and one dossier for what needs the owner. sweep_plan says what to
// read; sweep_record stores the cursor and the time of the scan. Para reads
// nothing outside the vault.

// sweepListMax caps each list of related items in a plan.
const sweepListMax = 50

// sweepDefaultDays is how far back a source never swept is read.
const sweepDefaultDays = 7

// mediumOfSource is the io and Ask medium of a source system.
func mediumOfSource(s model.SourceSystem) model.Medium {
	switch s {
	case model.SourceGmail:
		return model.MediumEmail
	case model.SourceSlack:
		return model.MediumSlack
	case model.SourceJira:
		return model.MediumJira
	case model.SourceConfluence:
		return model.MediumConfluence
	case model.SourceGitHub:
		return model.MediumGithub
	case model.SourceGCal:
		return model.MediumMeeting
	}
	return ""
}

// docSystemOfSource is the tracked_doc system of a source system.
func docSystemOfSource(s model.SourceSystem) model.DocSystem {
	switch s {
	case model.SourceGDocs:
		return model.SystemGDoc
	case model.SourceJira:
		return model.SystemJira
	case model.SourceConfluence:
		return model.SystemConfluence
	case model.SourceGitHub:
		return model.SystemGitHub
	}
	return ""
}

// SweepStep is a source step with its templates filled for this sweep:
// {cursor}, {since}, {since_date}, and {query}. {ref} and {url} stay for
// each item.
type SweepStep struct {
	model.SourceStep
	Rendered string `json:"rendered,omitempty"` // args or command, filled
	Query    string `json:"query,omitempty"`    // query, filled
}

// SweepItem is one vault item a sweep matches against.
type SweepItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	State string `json:"state,omitempty"`
	Ref   string `json:"ref,omitempty"` // public link
	Via   string `json:"via,omitempty"` // the connector's own id
}

// SweepSource is one source in a plan.
type SweepSource struct {
	ID       string             `json:"id"`
	Title    string             `json:"title"`
	System   model.SourceSystem `json:"system"`
	Via      model.SourceVia    `json:"via"`
	Server   string             `json:"server,omitempty"`
	Tools    []string           `json:"tools"`
	Due      string             `json:"due,omitempty"`
	LastScan string             `json:"last_scan,omitempty"`
	Cursor   string             `json:"cursor,omitempty"`
	// Since is where this sweep starts: the last scan, else sweepDefaultDays
	// ago.
	Since   string     `json:"since"`
	Fetch   *SweepStep `json:"fetch,omitempty"`
	ScanIn  *SweepStep `json:"scan_in,omitempty"`
	ScanOut *SweepStep `json:"scan_out,omitempty"`
	State   *SweepStep `json:"state,omitempty"`
	// Request is the open sweep request of the source, if any: claim it with
	// request_next, or name it in sweep_record.
	Request string `json:"request,omitempty"`
	// Known lists io items already in the vault from this source: do not add
	// them again.
	Known []SweepItem `json:"known"`
	// Drafts are outgoing io items not sent: a sent message that matches one
	// is io_send.
	Drafts []SweepItem `json:"drafts"`
	// Asks are open Asks out by this source's medium: a reply is rfx_reply.
	Asks []SweepItem `json:"asks"`
	// Tracked are tracked documents read through this source: pass each new
	// text with doc_version_add.
	Tracked []SweepItem `json:"tracked"`
}

// SweepPlanInput is the input of sweep_plan.
type SweepPlanInput struct {
	// Sources names sources to sweep now, due or not. Empty: every source you
	// declared that is due.
	Sources []string `json:"sources,omitempty" query:"sources"`
}

// SweepPlanOutput is the output of sweep_plan.
type SweepPlanOutput struct {
	Sources []SweepSource `json:"sources"`
	// Skipped names sources left out and why: not declared, paused, unknown.
	Skipped []string `json:"skipped"`
	// Declared is false when you have not declared your sources in
	// sources.reach_days: call agent_sources first.
	Declared bool   `json:"declared"`
	Next     string `json:"next"`
}

// SweepPlan says what the calling agent should sweep now.
var SweepPlan = defineRead("sweep_plan", "Plan a sweep: for you, the sources you declared (agent_sources) that are due, or the ones you name, each with how to fetch and scan it, its cursor and since, and the vault's io items, drafts, Asks, and tracked documents to match against. Read with your own tools, write findings with io_add, doc_version_add, doc_comment_add, io_send, rfx_reply, and one dossier, then sweep_record each source", func(ctx context.Context, d Deps, in SweepPlanInput) (SweepPlanOutput, error) {
	info := d.Store.Info()
	out := SweepPlanOutput{Sources: []SweepSource{}, Skipped: []string{}}
	days, err := reachDays(d.Store)
	if err != nil {
		return out, err
	}
	decl, err := readDeclarations(info.StateDir)
	if err != nil {
		return out, err
	}
	now := d.Store.Now()
	cut := reachCut(now, days)
	mine, ok := declaredBy(decl, info.Actor)
	out.Declared = ok && mine.Fresh(cut)
	sources, err := loadSources(ctx, d.Store)
	if err != nil {
		return out, err
	}
	open, err := openSweeps(ctx, d.Store)
	if err != nil {
		return out, err
	}
	today := now.Local().Format(recur.Layout)
	named := map[string]bool{}
	for _, raw := range in.Sources {
		named[normID(raw)] = true
	}
	var picked []model.Document
	for _, doc := range sources {
		m := doc.Meta.(*model.SourceDoc)
		isNamed := named[doc.ID]
		delete(named, doc.ID)
		if len(in.Sources) > 0 && !isNamed {
			continue
		}
		switch {
		case !out.Declared || !slices.Contains(mine.Sources, doc.ID):
			if isNamed {
				out.Skipped = append(out.Skipped, doc.ID+": not declared: call agent_sources with it if you reach it")
			}
		case m.Status != model.SourceActive:
			if isNamed {
				out.Skipped = append(out.Skipped, doc.ID+": paused")
			}
		case isNamed:
			picked = append(picked, doc)
		default:
			if due := sourceDue(m); due != "" && due <= today {
				picked = append(picked, doc)
			}
		}
	}
	for id := range named {
		out.Skipped = append(out.Skipped, id+": no such source")
	}
	slices.Sort(out.Skipped)
	if len(picked) > 0 {
		rel, err := sweepRelated(ctx, d, sources)
		if err != nil {
			return out, err
		}
		for _, doc := range picked {
			out.Sources = append(out.Sources, rel.plan(doc, now, open[doc.ID]))
		}
	}
	switch {
	case !out.Declared:
		out.Next = "call agent_sources with the source ids you reach, then sweep_plan again"
	case len(out.Sources) == 0:
		out.Next = "nothing due; name sources to sweep them anyway"
	default:
		out.Next = "for each source: scan_in and scan_out with your own tools from since; write findings; then sweep_record {source, cursor} (failed: true when the scan did not finish)"
	}
	return out, nil
})

// sweepRel holds the vault items a plan matches against, by source.
type sweepRel struct {
	res     sourceResolver
	ios     []model.Document
	tracked []model.Document
	asks    []RFXSummary
}

func sweepRelated(ctx context.Context, d Deps, sources []model.Document) (sweepRel, error) {
	r := sweepRel{res: newSourceResolver(sources)}
	for _, t := range []model.Type{model.TypeIO, model.TypeTrackedDoc} {
		refs, err := d.Store.List(ctx, store.Filter{Type: t})
		if err != nil {
			return r, err
		}
		for _, ref := range refs {
			doc, err := d.Store.Get(ctx, ref.ID)
			if err != nil {
				return r, err
			}
			switch doc.Meta.(type) {
			case *model.IO:
				r.ios = append(r.ios, doc)
			case *model.TrackedDoc:
				r.tracked = append(r.tracked, doc)
			}
		}
	}
	asks, err := loadRFX(ctx, d)
	if err != nil {
		return r, err
	}
	r.asks = asks
	return r, nil
}

// cites reports whether via names src, or, with no via, the medium matches.
func (r sweepRel) cites(via *model.IOVia, src string) bool {
	return via != nil && r.res.resolve(via.Source) == src
}

func (r sweepRel) plan(doc model.Document, now time.Time, request string) SweepSource {
	m := doc.Meta.(*model.SourceDoc)
	since := now.Add(-sweepDefaultDays * 24 * time.Hour).UTC().Format(time.RFC3339)
	if m.LastScan != "" {
		since = m.LastScan
	}
	s := SweepSource{ID: doc.ID, Title: doc.Title, System: m.System, Via: m.Via, Server: m.Server, Tools: append([]string{}, m.Tools...),
		Due: sourceDue(m), LastScan: m.LastScan, Cursor: m.Cursor, Since: since, Request: request,
		Known: []SweepItem{}, Drafts: []SweepItem{}, Asks: []SweepItem{}, Tracked: []SweepItem{}}
	fill := func(st *model.SourceStep) *SweepStep {
		if st == nil {
			return nil
		}
		vals := map[string]string{"cursor": m.Cursor, "since": since, "since_date": dayOf(since)}
		q := renderTemplate(st.Query, vals)
		vals["query"] = q
		return &SweepStep{SourceStep: *st, Query: q, Rendered: renderTemplate(st.Args+st.Command, vals)}
	}
	s.Fetch, s.ScanIn, s.ScanOut, s.State = fill(m.Fetch), fill(m.ScanIn), fill(m.ScanOut), fill(m.State)
	medium := mediumOfSource(m.System)
	add := func(list *[]SweepItem, it SweepItem) {
		if len(*list) < sweepListMax {
			*list = append(*list, it)
		}
	}
	for _, io := range r.ios {
		v := io.Meta.(*model.IO)
		match := r.cites(v.Via, doc.ID) || (v.Via == nil && medium != "" && v.Medium == medium)
		if !match {
			continue
		}
		it := SweepItem{ID: io.ID, Title: io.Title, State: string(v.State), Ref: v.Ref}
		if v.Via != nil {
			it.Via = v.Via.Ref
		}
		switch {
		case v.Direction == model.IOIn && r.cites(v.Via, doc.ID):
			add(&s.Known, it)
		case v.Direction == model.IOOut && (v.State == model.IODraft || v.State == model.IOReady):
			add(&s.Drafts, it)
		}
	}
	if medium != "" {
		for _, a := range r.asks {
			if a.Status != model.RFXOpen || a.Direction != model.RFXOut {
				continue
			}
			for _, t := range a.Targets {
				if t.Medium == medium {
					add(&s.Asks, SweepItem{ID: a.ID, Title: a.Title, State: string(t.State), Ref: t.Ref})
					break
				}
			}
		}
	}
	sys := docSystemOfSource(m.System)
	for _, td := range r.tracked {
		v := td.Meta.(*model.TrackedDoc)
		if v.Status != model.DocWatching {
			continue
		}
		if r.cites(v.Via, doc.ID) || (v.Via == nil && sys != "" && v.System == sys) {
			it := SweepItem{ID: td.ID, Title: td.Title, State: fmt.Sprintf("v%d", v.Latest), Ref: v.URL}
			if v.Via != nil {
				it.Via = v.Via.Ref
			}
			add(&s.Tracked, it)
		}
	}
	return s
}

// renderTemplate fills {name} from vals; other placeholders stay.
func renderTemplate(t string, vals map[string]string) string {
	if t == "" {
		return ""
	}
	for k, v := range vals {
		t = strings.ReplaceAll(t, "{"+k+"}", v)
	}
	return t
}

// SweepRecordInput is the input of sweep_record.
type SweepRecordInput struct {
	Source string `json:"source"`
	// Cursor is the opaque text your tool returned to continue from; nil
	// keeps the stored one, empty clears it.
	Cursor *string `json:"cursor,omitempty"`
	// ScannedAt is when the scan ran, RFC 3339; default now.
	ScannedAt string `json:"scanned_at,omitempty"`
	// Counts are what the sweep wrote, such as {"in": 3, "out": 1, "docs": 0}.
	Counts map[string]int `json:"counts,omitempty"`
	// Errors are what went wrong, in words. No secret goes here.
	Errors []string `json:"errors,omitempty"`
	// Failed: the scan did not finish; last_scan and cursor stay.
	Failed bool `json:"failed,omitempty"`
	// Request is the sweep request you claimed for the source; it is ended
	// done (or failed) with the counts.
	Request string `json:"request,omitempty"`
	// Outward are acts scan_out saw (sent, posted, pushed) that match no
	// draft, Ask, or interaction. Para keeps them; health names each one with
	// no record (unreviewed_outward).
	Outward []SweepOutward `json:"outward,omitempty"`
}

// SweepRecordOutput is the output of sweep_record.
type SweepRecordOutput struct {
	Source   string `json:"source"`
	LastScan string `json:"last_scan,omitempty"`
	Cursor   string `json:"cursor,omitempty"`
	Due      string `json:"due,omitempty"` // the next sweep
	Request  string `json:"request,omitempty"`
	// Unreviewed are the URLs of outward acts seen at this source with no
	// record in the vault: no approved item done, no action_record.
	Unreviewed []string `json:"unreviewed"`
	Rev        string   `json:"rev"`
}

const lastSweepHeading = "## Last sweep"

// SweepRecord stores what one sweep of a source did.
var SweepRecord = defineSafe("sweep_record", "Record a sweep of one source: the new cursor your tool returned, when it ran, counts of what you wrote, and errors; failed keeps last_scan. Name the sweep request you claimed to end it. outward lists acts scan_out saw (sent, posted, pushed) that match no draft, Ask, or interaction; the output's unreviewed are those with no record (health unreviewed_outward)", func(ctx context.Context, d Deps, in SweepRecordInput) (SweepRecordOutput, error) {
	info := d.Store.Info()
	doc, m, err := getSource(ctx, d.Store, in.Source)
	if err != nil {
		return SweepRecordOutput{}, err
	}
	at := d.Store.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(in.ScannedAt) != "" {
		t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(in.ScannedAt))
		if err != nil {
			return SweepRecordOutput{}, fmt.Errorf("scanned_at %q: want an RFC 3339 time", in.ScannedAt)
		}
		if t.After(d.Store.Now().Add(5 * time.Minute)) {
			return SweepRecordOutput{}, fmt.Errorf("scanned_at %q is in the future", in.ScannedAt)
		}
		at = t.UTC().Format(time.RFC3339)
	}
	if in.Cursor != nil && len(*in.Cursor) > 4096 {
		return SweepRecordOutput{}, errors.New("cursor: at most 4096 bytes")
	}
	for _, e := range in.Errors {
		if strings.ContainsAny(e, "\n\r") {
			return SweepRecordOutput{}, errors.New("errors: one line each")
		}
	}
	if !in.Failed {
		m.LastScan = at
		if in.Cursor != nil {
			m.Cursor = *in.Cursor
		}
	}
	add, err := checkOutward(in.Outward, at)
	if err != nil {
		return SweepRecordOutput{}, err
	}
	line := sweepLine(at, info.Actor, in)
	head, seen, _ := splitSourceBody(doc.Body)
	doc.Body = joinSourceBody(head, mergeOutward(seen, add), line)
	doc, err = d.Store.Update(ctx, doc)
	if err != nil {
		return SweepRecordOutput{}, err
	}
	out := SweepRecordOutput{Source: doc.ID, LastScan: m.LastScan, Cursor: m.Cursor, Due: sourceDue(m), Unreviewed: []string{}, Rev: doc.Rev}
	if len(add) > 0 {
		all, err := d.Store.List(ctx, store.Filter{})
		if err != nil {
			return out, err
		}
		docs := map[string]model.Document{}
		for _, r := range all {
			if r.Type != model.TypeReceipt && r.Type != model.TypeIO && r.Type != model.TypeInteraction {
				continue
			}
			if x, err := d.Store.Get(ctx, r.ID); err == nil {
				docs[r.ID] = x
			}
		}
		for _, o := range unreviewedOutward(doc.Body, recordedURLs(docs)) {
			out.Unreviewed = append(out.Unreviewed, o.url)
		}
	}
	if in.Request != "" {
		rq, r, err := ownClaim(ctx, d, in.Request)
		if err != nil {
			return out, fmt.Errorf("request: %w", err)
		}
		if r.RequiresSource.Target() != doc.ID {
			return out, fmt.Errorf("request %s is not a sweep of %s", rq.ID, doc.ID)
		}
		if in.Failed {
			rq.Body = appendSection(rq.Body, "Failure", line)
			r.Status, r.Lease, r.Resolution = model.RequestFailed, "", model.ResolutionFailed
		} else {
			rq.Body = appendSection(rq.Body, "Result", line)
			r.Status, r.Lease, r.Resolution = model.RequestDone, "", model.ResolutionDone
			r.Results = []model.Link{model.LinkTo(doc.ID)}
		}
		if _, err := d.Store.Update(ctx, rq); err != nil {
			return out, err
		}
		out.Request = rq.ID
	}
	return out, nil
})

// sweepLine is the one line a sweep leaves in the source body.
func sweepLine(at, actor string, in SweepRecordInput) string {
	keys := make([]string, 0, len(in.Counts))
	for k := range in.Counts {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, in.Counts[k]))
	}
	line := fmt.Sprintf("%s by %s", at, firstOf(actor, "unknown"))
	if in.Failed {
		line += ", failed"
	}
	if len(parts) > 0 {
		line += ": " + strings.Join(parts, ", ")
	}
	if len(in.Errors) > 0 {
		line += ". Errors: " + strings.Join(in.Errors, "; ")
	}
	return line
}
