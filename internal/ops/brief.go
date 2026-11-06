package ops

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// A brief is the packet the owner reads before a meeting: who is in it and
// what is open with each of them, what the meeting is linked to, and what came
// of the last occurrence. It is built from the vault alone, the same way on
// every call: no model writes it, and para fetches nothing for it.
//
// Per person it lists the asks the owner made of them (rfx out with them as a
// target), what they asked of the owner (rfx in from them, commitments owed to
// them), open tasks assigned to them or waiting on them, and open questions
// asked of them. An attendee kept as text is looked up again; one that is still
// not in the directory shows as unresolved.

// BriefInput names the item to brief.
type BriefInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// BriefLine is one item in a brief: its id, title, type word, status, and why
// it is listed.
type BriefLine struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Type   string `json:"type"`
	Status string `json:"status,omitempty"`
	Reason string `json:"reason"`
}

// BriefPerson is one person (or group) of the item and what is open with
// them. ID is empty for an attendee not in the directory: Text is how the
// invite names them, and Unresolved says why no person matched.
type BriefPerson struct {
	ID         string      `json:"id,omitempty"`
	Name       string      `json:"name"`
	Text       string      `json:"text,omitempty"`
	Resolved   bool        `json:"resolved"`
	Unresolved string      `json:"unresolved,omitempty"`
	YouAsked   []BriefLine `json:"you_asked"`
	TheyAsked  []BriefLine `json:"they_asked"`
	Tasks      []BriefLine `json:"tasks"`
	Questions  []BriefLine `json:"questions"`
}

// BriefPrevious is the last held occurrence of a recurring meeting: its notes
// and outcome. Skipped are the occurrences after it that were not held.
type BriefPrevious struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Start   string   `json:"start,omitempty"`
	Status  string   `json:"status"`
	Notes   string   `json:"notes"`
	Outcome string   `json:"outcome"`
	Skipped []string `json:"skipped"`
}

// BriefOutput is the brief of one item, as data and as markdown.
type BriefOutput struct {
	ID       string         `json:"id"`
	Title    string         `json:"title"`
	Type     string         `json:"type"`
	Status   string         `json:"status,omitempty"`
	Start    string         `json:"start,omitempty"`
	End      string         `json:"end,omitempty"`
	DoneWhen string         `json:"done_when,omitempty"`
	Agenda   string         `json:"agenda"`
	People   []BriefPerson  `json:"people"`
	Linked   []BriefLine    `json:"linked"`
	Previous *BriefPrevious `json:"previous" required:"false"`
	Markdown string         `json:"markdown"`
}

// BriefDescription is brief's tool text.
const BriefDescription = "Brief a meeting (or any task or ask with people) before it: per attendee, the asks you made of them, what they asked of you (asks in, commitments owed to them), " +
	"open tasks assigned to them or waiting on them, and open questions asked of them; items linked to the meeting; the notes and outcome of the last held occurrence of a recurring meeting; and the meeting's done_when and agenda text. " +
	"An attendee not in the directory is listed as unresolved. Built from the vault only, the same on every call. Each line has an id and a reason; markdown is the same brief as text"

// Brief reads the brief of one item.
var Brief = defineRead("brief", BriefDescription, brief)

func brief(ctx context.Context, d Deps, in BriefInput) (BriefOutput, error) {
	id := normID(in.ID)
	if id == "" {
		return BriefOutput{}, errors.New("id is empty")
	}
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return BriefOutput{}, linkErr("id", id, err)
	}
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		return BriefOutput{}, err
	}
	b := &briefer{sn: sn}
	out := BriefOutput{ID: doc.ID, Title: doc.Title, Type: typeWord(doc), Status: statusOf(doc.Meta),
		People: []BriefPerson{}, Linked: []BriefLine{}}
	var with []model.Link
	var text []string
	switch m := doc.Meta.(type) {
	case *model.Task:
		with, text = m.With, m.Attendees
		if IsOther(m.Assignee) && !IsAgent(m.Assignee) && !slices.Contains(with, model.LinkTo(m.Assignee)) {
			with = append(slices.Clone(with), model.LinkTo(m.Assignee))
		}
		out.DoneWhen = m.DoneWhen
		out.Start, out.End = m.Scheduled, m.End
		if mt, ok := readMeeting(doc); ok {
			out.Status = string(mt.Status)
		}
	case *model.Meeting:
		with, out.Start, out.End = m.With, m.Start, m.End
	case *model.RFX:
		for _, t := range m.Targets {
			if !slices.Contains(with, t.To) {
				with = append(with, t.To)
			}
		}
	default:
		return BriefOutput{}, fmt.Errorf("%s is a %s: brief takes a meeting, a task, or an ask that has people", doc.ID, typeWord(doc))
	}
	if len(with) == 0 && len(text) == 0 {
		return BriefOutput{}, fmt.Errorf("%s names no people (with, attendees, a person assignee, or ask targets): nothing to brief", doc.ID)
	}
	out.Agenda = briefAgenda(doc.Body)
	for _, l := range with {
		out.People = append(out.People, b.person(l.Target(), "", doc.ID))
	}
	for _, raw := range text {
		l, warn, err := lookupAttendee(ctx, d, raw)
		if err != nil {
			return BriefOutput{}, err
		}
		if l != "" {
			if slices.ContainsFunc(out.People, func(p BriefPerson) bool { return p.ID == l.Target() }) {
				continue
			}
			out.People = append(out.People, b.person(l.Target(), raw, doc.ID))
			continue
		}
		why := "not in the directory: org_person_add makes them a person"
		if strings.Contains(warn, "more than one") {
			why = "matches more than one person in the directory"
		}
		out.People = append(out.People, BriefPerson{Name: flatLine(raw), Text: raw, Unresolved: why,
			YouAsked: []BriefLine{}, TheyAsked: []BriefLine{}, Tasks: []BriefLine{}, Questions: []BriefLine{}})
	}
	out.Linked = b.linked(doc)
	if t, ok := doc.Meta.(*model.Task); ok && t.Previous != "" {
		out.Previous = b.previous(t.Previous.Target())
		if out.Previous != nil {
			for _, ln := range b.actionItems(out.Previous.ID, "action item from "+out.Previous.ID+", still open") {
				out.Linked = b.add(out.Linked, ln)
			}
		}
	}
	out.Markdown = briefMarkdown(out)
	return out, nil
}

// briefer reads one snapshot for a brief.
type briefer struct {
	sn *Snapshot
}

// open is the snapshot's documents that are open or later, in list order.
func (b *briefer) open() []model.Document {
	var out []model.Document
	for _, doc := range b.sn.Docs() {
		if lifeOf(doc.Meta) == LifeOpen {
			out = append(out, doc)
		}
	}
	return out
}

// line is the brief line of a document, with " (later)" on one put off.
func (b *briefer) line(doc model.Document, reason string) BriefLine {
	if b.sn.Need(doc.ID).Life == LifeLater {
		reason += " (later)"
	}
	return BriefLine{ID: doc.ID, Title: doc.Title, Type: typeWord(doc), Status: statusOf(doc.Meta), Reason: reason}
}

// add appends ln unless a line with its id is there.
func (b *briefer) add(list []BriefLine, ln BriefLine) []BriefLine {
	if slices.ContainsFunc(list, func(x BriefLine) bool { return x.ID == ln.ID }) {
		return list
	}
	return append(list, ln)
}

// asksOf are the open asks out with id as a target, by ask id.
func (b *briefer) asksOf(id string) map[string]bool {
	out := map[string]bool{}
	for _, doc := range b.open() {
		if r, ok := doc.Meta.(*model.RFX); ok && r.Direction == model.RFXOut && targetsHave(r.Targets, id) {
			out[doc.ID] = true
		}
	}
	return out
}

func targetsHave(ts []model.Target, id string) bool {
	return slices.ContainsFunc(ts, func(t model.Target) bool { return t.To.Target() == id })
}

// person is what is open with one person or group. self is the briefed item:
// it is not listed under its own people.
func (b *briefer) person(id, text, self string) BriefPerson {
	p := BriefPerson{ID: id, Name: id, Text: text, Resolved: true,
		YouAsked: []BriefLine{}, TheyAsked: []BriefLine{}, Tasks: []BriefLine{}, Questions: []BriefLine{}}
	pd, err := b.sn.Get(id)
	if err != nil {
		p.Resolved, p.Unresolved = false, id+" is not in the vault"
		return p
	}
	p.Name = pd.Title
	asks := b.asksOf(id)
	named := func(s string) bool {
		s = strings.ToLower(s)
		return strings.Contains(s, strings.ToLower(id)) || pd.Title != "" && strings.Contains(s, strings.ToLower(pd.Title))
	}
	for _, doc := range b.open() {
		if doc.ID == self {
			continue
		}
		switch m := doc.Meta.(type) {
		case *model.RFX:
			if !targetsHave(m.Targets, id) {
				continue
			}
			if m.Direction == model.RFXIn {
				p.TheyAsked = b.add(p.TheyAsked, b.line(doc, "they asked you"))
				continue
			}
			p.YouAsked = b.add(p.YouAsked, b.line(doc, askReason(m, id)))
			if q := m.Question.Target(); q != "" {
				if qd, err := b.sn.Get(q); err == nil && lifeOf(qd.Meta) == LifeOpen {
					p.Questions = b.add(p.Questions, b.line(qd, "asked of them in "+doc.ID))
				}
			}
		case *model.Task:
			kind := m.EffectiveKind()
			if kind == model.KindMeeting {
				continue
			}
			withThem := slices.Contains(targets(m.With), id)
			switch {
			case kind == model.KindCommitment && withThem:
				reason := "you owe them"
				if m.Due != "" {
					reason += ", due " + string(m.Due)
				}
				p.TheyAsked = b.add(p.TheyAsked, b.line(doc, reason))
			case m.Assignee == id:
				p.Tasks = b.add(p.Tasks, b.line(doc, "assigned to them"))
			case slices.ContainsFunc(m.BlockedBy, named):
				p.Tasks = b.add(p.Tasks, b.line(doc, "blocked by them"))
			case slices.ContainsFunc(targets(m.DependsOn), func(dep string) bool { return asks[dep] }):
				dep := targets(m.DependsOn)[slices.IndexFunc(targets(m.DependsOn), func(dep string) bool { return asks[dep] })]
				p.Tasks = b.add(p.Tasks, b.line(doc, "waits on their reply to "+dep))
			case withThem:
				p.Tasks = b.add(p.Tasks, b.line(doc, "with them"))
			}
		case *model.Question:
			switch {
			case asks[m.RFX.Target()]:
				p.Questions = b.add(p.Questions, b.line(doc, "asked of them in "+m.RFX.Target()))
			case slices.ContainsFunc(targets(m.DependsOn), func(dep string) bool { return asks[dep] }):
				p.Questions = b.add(p.Questions, b.line(doc, "waits on an ask of them"))
			}
		}
	}
	return p
}

// askReason says where an ask of the owner's stands with one target.
func askReason(r *model.RFX, id string) string {
	var parts []string
	for _, t := range r.Targets {
		if t.To.Target() != id {
			continue
		}
		s := "you asked them by " + string(t.Medium) + ": " + string(t.State)
		if t.Sent != "" {
			s += ", sent " + string(t.Sent)
		}
		parts = append(parts, s)
	}
	if r.NextNudge != "" {
		parts = append(parts, "follow up "+string(r.NextNudge))
	}
	return strings.Join(parts, "; ")
}

// linked are the items tied to the briefed one: the asks a meeting should
// settle, what its Questions section names, open questions scoped to it, its
// open action items, and open items that wait on it.
func (b *briefer) linked(doc model.Document) []BriefLine {
	out := []BriefLine{}
	addID := func(id, reason string) {
		if id == "" || id == doc.ID {
			return
		}
		if ld, err := b.sn.Get(id); err == nil {
			out = b.add(out, b.line(ld, reason))
		}
	}
	if mt, ok := readMeeting(doc); ok {
		for _, r := range targets(mt.RFX) {
			addID(r, "ask it should settle")
		}
		for _, id := range wikiIDs(headingText(doc.Body, questionsHeading)) {
			addID(id, "on its agenda")
		}
	}
	if t, ok := doc.Meta.(*model.Task); ok {
		for _, dep := range targets(t.DependsOn) {
			addID(dep, "it waits on this")
		}
	}
	for _, ln := range b.actionItems(doc.ID, "action item of this meeting") {
		out = b.add(out, ln)
	}
	for _, od := range b.open() {
		if od.ID == doc.ID {
			continue
		}
		switch m := od.Meta.(type) {
		case *model.Question:
			if scopeID(m) == doc.ID {
				out = b.add(out, b.line(od, "question scoped to it"))
			}
			if slices.Contains(targets(m.DependsOn), doc.ID) {
				out = b.add(out, b.line(od, "waits on it"))
			}
		case *model.Task:
			if slices.Contains(targets(m.DependsOn), doc.ID) {
				out = b.add(out, b.line(od, "waits on it"))
			}
		case *model.RFX:
			if slices.Contains(targets(m.DependsOn), doc.ID) {
				out = b.add(out, b.line(od, "waits on it"))
			}
		}
	}
	return out
}

// actionItems are the open tasks whose parent is id.
func (b *briefer) actionItems(id, reason string) []BriefLine {
	var out []BriefLine
	for _, od := range b.open() {
		if t, ok := od.Meta.(*model.Task); ok && t.Parent.Target() == id {
			out = append(out, b.line(od, reason))
		}
	}
	return out
}

// briefPreviousMax bounds the walk back through occurrences.
const briefPreviousMax = 20

// previous walks back from id to the last held occurrence. The ones on the way
// that were not held are listed as skipped. Nil when none was held.
func (b *briefer) previous(id string) *BriefPrevious {
	var skipped []string
	for range briefPreviousMax {
		doc, err := b.sn.Get(id)
		if err != nil {
			return nil
		}
		t, ok := doc.Meta.(*model.Task)
		if !ok {
			return nil
		}
		if t.Status == model.TaskDone && !meetingSkipped(t) {
			return &BriefPrevious{ID: doc.ID, Title: doc.Title, Start: t.Scheduled, Status: string(model.MeetingHeld),
				Notes: headingText(doc.Body, notesHeading), Outcome: headingText(doc.Body, outcomeHeading), Skipped: nonNil(skipped)}
		}
		skipped = append(skipped, doc.ID)
		if t.Previous == "" {
			return nil
		}
		id = t.Previous.Target()
	}
	return nil
}

// briefAgenda is the text of a body before its first section, and its
// Questions section.
func briefAgenda(body string) string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	end := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "## ") })
	if end < 0 {
		end = len(lines)
	}
	var kept []string
	for _, l := range lines[:end] {
		if !strings.HasPrefix(l, "# ") {
			kept = append(kept, l)
		}
	}
	parts := []string{}
	if intro := strings.TrimSpace(strings.Join(kept, "\n")); intro != "" {
		parts = append(parts, intro)
	}
	if q := headingText(body, questionsHeading); q != "" && q != noQuestions {
		parts = append(parts, q)
	}
	return strings.Join(parts, "\n\n")
}

var wikiRe = regexp.MustCompile(`\[\[([^\]|#]+)`)

// wikiIDs are the ids the wikilinks of text name, in order, once each.
func wikiIDs(text string) []string {
	var out []string
	for _, m := range wikiRe.FindAllStringSubmatch(text, -1) {
		if id := strings.TrimSpace(m[1]); id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// briefMarkdown renders a brief for an agent or a terminal.
func briefMarkdown(o BriefOutput) string {
	var w strings.Builder
	fmt.Fprintf(&w, "# Brief: %s ([[%s]])\n\n", firstOf(o.Title, o.ID), o.ID)
	head := []string{o.Type}
	if o.Status != "" {
		head = append(head, o.Status)
	}
	if o.Start != "" {
		head = append(head, o.Start)
	}
	w.WriteString(strings.Join(head, " · ") + "\n")
	if o.DoneWhen != "" || o.Agenda != "" {
		w.WriteString("\n## Agenda\n\n")
		if o.DoneWhen != "" {
			w.WriteString("Done when: " + o.DoneWhen + "\n")
		}
		if o.Agenda != "" {
			if o.DoneWhen != "" {
				w.WriteString("\n")
			}
			w.WriteString(flattenHeadings(o.Agenda) + "\n")
		}
	}
	w.WriteString("\n## People\n")
	for _, p := range o.People {
		if !p.Resolved {
			fmt.Fprintf(&w, "\n### %s (unresolved)\n\n%s\n", p.Name, p.Unresolved)
			continue
		}
		fmt.Fprintf(&w, "\n### %s ([[%s]])\n", p.Name, p.ID)
		n := 0
		for _, sec := range []struct {
			head  string
			lines []BriefLine
		}{{"You asked them", p.YouAsked}, {"They asked you", p.TheyAsked}, {"Tasks", p.Tasks}, {"Questions", p.Questions}} {
			if len(sec.lines) == 0 {
				continue
			}
			n++
			fmt.Fprintf(&w, "\n%s:\n", sec.head)
			writeBriefLines(&w, sec.lines)
		}
		if n == 0 {
			w.WriteString("\nNothing open with them.\n")
		}
	}
	if len(o.Linked) > 0 {
		w.WriteString("\n## Linked\n\n")
		writeBriefLines(&w, o.Linked)
	}
	if p := o.Previous; p != nil {
		fmt.Fprintf(&w, "\n## Last time: %s ([[%s]])\n\n", firstOf(p.Title, p.ID), p.ID)
		if p.Start != "" {
			w.WriteString("Held " + p.Start + ".")
		} else {
			w.WriteString("Held.")
		}
		if len(p.Skipped) > 0 {
			w.WriteString(" Not held since: " + strings.Join(p.Skipped, ", ") + ".")
		}
		w.WriteString("\n")
		if p.Notes != "" {
			w.WriteString("\nNotes:\n\n" + flattenHeadings(p.Notes) + "\n")
		}
		if p.Outcome != "" {
			w.WriteString("\nOutcome:\n\n" + flattenHeadings(p.Outcome) + "\n")
		}
	}
	return strings.TrimRight(w.String(), "\n") + "\n"
}

func writeBriefLines(w *strings.Builder, lines []BriefLine) {
	for _, l := range lines {
		status := ""
		if l.Status != "" {
			status = ", " + l.Status
		}
		fmt.Fprintf(w, "- [[%s]] %s (%s%s): %s\n", l.ID, firstOf(l.Title, l.ID), l.Type, status, l.Reason)
	}
}
