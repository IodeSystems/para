package importer

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
)

// lgCtx is what the builders of the events-derived units share.
type lgCtx struct {
	ix       *lgIndex
	people   map[string]*cand
	unsorted *cand
	rfxOf    map[*item][]*cand
}

func (lg *lgCtx) person(name string) *cand {
	s := slug(name)
	if s == "" {
		s = "x-" + strings.TrimPrefix(sha([]byte(name)), "sha256:")[:8]
	}
	if c := lg.ix.people[s]; c != nil {
		return c
	}
	if c := lg.people[s]; c != nil {
		return c
	}
	key := "events.jsonl#person/" + s
	c := &cand{
		key: key, srcRel: "events.jsonl", frag: "person/" + s, hash: sha([]byte("person:" + name)),
		title: name, typ: model.TypePerson, folder: model.FolderOrg, fixedType: true, idBase: "person-" + s,
	}
	c.build = func(b *bctx, c *cand, e Entry) (model.Document, []string, error) {
		pe := &model.Person{Common: model.Common{Type: model.TypePerson, Created: model.Date(b.now[:10]), Tags: e.Tags}, Org: "unknown"}
		doc := model.Document{ID: b.idOf(c), Folder: model.Folder(e.Folder), Title: c.title, Body: "Named in the legacy task events. No other details were in the source.", Meta: pe}
		return doc, nil, doc.SetExtra("source", b.provenance(c))
	}
	lg.people[s] = c
	return c
}

func (lg *lgCtx) unsortedArea() *cand {
	if lg.unsorted != nil {
		return lg.unsorted
	}
	key := "events.jsonl#area/unsorted"
	c := &cand{
		key: key, srcRel: "events.jsonl", frag: "area/unsorted", hash: sha([]byte(key)),
		title: "Imported: unsorted tasks", typ: model.TypeArea, folder: model.FolderAreas, fixedType: true, idBase: "area-imported-unsorted",
	}
	c.build = func(b *bctx, c *cand, e Entry) (model.Document, []string, error) {
		a := &model.Area{Common: model.Common{Type: model.TypeArea, Created: model.Date(b.now[:10]), Tags: e.Tags}, Status: model.AreaActive}
		doc := model.Document{ID: b.idOf(c), Folder: model.Folder(e.Folder), Title: c.title,
			Body: "Tasks from the legacy events that name no project, or a project with no project file. Sort them into projects or areas, then delete this area.", Meta: a}
		return doc, nil, doc.SetExtra("source", b.provenance(c))
	}
	lg.unsorted = c
	return c
}

// eventCands makes the units for the folded events.
func eventCands(fr *foldResult, ix *lgIndex, ef walkFile) []*cand {
	lg := &lgCtx{ix: ix, people: map[string]*cand{}, rfxOf: map[*item][]*cand{}}
	var out []*cand
	itemOf := map[*cand]*item{}
	// Pass 1: a unit per item.
	for _, id := range fr.order {
		it := fr.items[id]
		if it.Type == "" {
			continue
		}
		s := slug(id)
		if s == "" {
			s = "x-" + strings.TrimPrefix(sha([]byte(id)), "sha256:")[:8]
		}
		title, _ := titleBody(it.Text)
		c := &cand{
			key: "events.jsonl#" + id, srcRel: "events.jsonl", frag: id, title: title, fixedType: true, idBase: s,
			hash: sha(bytes.Join(it.raw, []byte("\n"))), names: []string{strings.ToLower(id)},
		}
		if title == "" {
			c.title = id
			c.notes = append(c.notes, "title: the item has no text; its id is the title")
		}
		c.project = ix.projects[slug(it.Project)]
		if it.Project != "" && c.project == nil {
			c.notes = append(c.notes, "project: "+safe(it.Project)+" has no project file; no project link")
		}
		switch it.Type {
		case "T", "D":
			c.typ, c.folder = model.TypeTask, model.FolderProjects
			if c.project == nil {
				c.area = lg.unsortedArea()
			}
		case "Q":
			c.typ, c.folder = model.TypeQuestion, model.FolderQuestions
		case "N", "X":
			c.typ, c.folder = model.TypeNote, model.FolderResources
			if it.Type == "X" {
				c.tags = []string{"decision"}
			}
		case "I":
			c.typ, c.folder = model.TypeInteraction, model.FolderInteractions
			if it.Who == "" {
				c.typ, c.folder, c.tags = model.TypeNote, model.FolderResources, []string{"interaction"}
				c.notes = append(c.notes, "type: interaction names no one (who); imported as a note tagged interaction")
			}
		}
		ix.items[id] = c
		ix.byImportedID[strings.ToLower(id)] = c
		itemOf[c] = it
		out = append(out, c)
	}
	// Pass 2: contacts and builders.
	for _, c := range append([]*cand(nil), out...) {
		it := itemOf[c]
		if it.Type == "I" && c.typ == model.TypeInteraction {
			out = append(out, lg.person(it.Who))
		}
		for i, ct := range it.Contacts {
			pc := lg.person(ct.Who)
			out = append(out, pc)
			r := lg.rfxCand(it, c, ct, pc, i)
			lg.rfxOf[it] = append(lg.rfxOf[it], r)
			out = append(out, r)
		}
		c.build = lg.builder(it, c)
	}
	if lg.unsorted != nil {
		out = append(out, lg.unsorted)
	}
	// Persons are added from several items: keep one unit per key.
	seen := map[*cand]bool{}
	uniq := out[:0]
	for _, c := range out {
		if !seen[c] {
			seen[c] = true
			uniq = append(uniq, c)
		}
	}
	return uniq
}

func titleBody(text string) (title, rest string) {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r", ""))
	t, r, _ := strings.Cut(text, "\n")
	return strings.TrimSpace(t), strings.TrimSpace(r)
}

func dateStr(t time.Time, fallback string) model.Date {
	if t.IsZero() {
		return model.Date(fallback[:10])
	}
	return model.Date(t.UTC().Format("2006-01-02"))
}

var mediums = map[string]model.Medium{
	"email": model.MediumEmail, "mail": model.MediumEmail, "slack": model.MediumSlack, "jira": model.MediumJira,
	"confluence": model.MediumConfluence, "github": model.MediumGithub, "teams": model.MediumTeams,
	"phone": model.MediumPhone, "call": model.MediumPhone, "meeting": model.MediumMeeting, "in-person": model.MediumMeeting,
}

func mediumOf(via string) model.Medium {
	if m, ok := mediums[strings.ToLower(strings.TrimSpace(via))]; ok {
		return m
	}
	return model.MediumOther
}

// fence wraps text in a code fence longer than any backtick run inside it.
func fence(text string) string {
	n, run := 3, 0
	for _, r := range text {
		if r == '`' {
			run++
			if run >= n {
				n = run + 1
			}
		} else {
			run = 0
		}
	}
	f := strings.Repeat("`", n)
	return f + "\n" + text + "\n" + f
}

func bullet(l noteLine, fallback string) string {
	text := strings.ReplaceAll(strings.TrimSpace(l.Text), "\n", "\n  ")
	return "- " + string(dateStr(l.TS, fallback)) + ": " + text
}

type builderFn = func(b *bctx, c *cand, e Entry) (model.Document, []string, error)

// itemBody assembles the document body of an item from its text and its
// recorded notes, risks, and check. A check is text: it is written down, and
// never run.
func itemBody(b *bctx, it *item, withText bool, extra ...string) string {
	var parts []string
	if withText {
		if _, rest := titleBody(it.Text); rest != "" {
			parts = append(parts, rest)
		}
	}
	parts = append(parts, extra...)
	if it.Source != "" {
		parts = append(parts, "Origin: "+strings.ReplaceAll(it.Source, "\n", " "))
	}
	if len(it.Notes) > 0 {
		var l []string
		for _, n := range it.Notes {
			l = append(l, bullet(n, b.now))
		}
		parts = append(parts, "## Notes\n\n"+strings.Join(l, "\n"))
	}
	if len(it.Risks) > 0 {
		var l []string
		for _, n := range it.Risks {
			l = append(l, bullet(n, b.now))
		}
		parts = append(parts, "## Risks\n\n"+strings.Join(l, "\n"))
	}
	if it.Check != "" {
		parts = append(parts, "## Check (recorded from the source, not run)\n\n"+fence(it.Check))
	}
	parts = append(parts, "Imported id: "+it.ID)
	return strings.Join(parts, "\n\n")
}

func (lg *lgCtx) builder(it *item, c *cand) builderFn {
	switch c.typ {
	case model.TypeTask:
		return lg.buildTask(it)
	case model.TypeQuestion:
		return lg.buildQuestion(it)
	case model.TypeInteraction:
		return lg.buildInteraction(it)
	}
	return lg.buildNote(it)
}

func (lg *lgCtx) link(b *bctx, id string) model.Link {
	if r := b.refID(id); r != "" {
		return model.LinkTo(r)
	}
	return ""
}

var taskStatusOf = map[string]model.TaskStatus{
	"todo": model.TaskTodo, "inflight": model.TaskDoing, "waiting": model.TaskBlocked, "blocked": model.TaskBlocked,
	"done": model.TaskDone, "dropped": model.TaskDropped, "open": model.TaskTodo, "answered": model.TaskDone,
}

func (lg *lgCtx) buildTask(it *item) builderFn {
	return func(b *bctx, c *cand, e Entry) (model.Document, []string, error) {
		var notes []string
		eff := it.effective()
		t := &model.Task{Common: model.Common{Type: model.TypeTask, Created: dateStr(it.Created, b.now), Tags: e.Tags},
			Status:  taskStatusOf[eff],
			Project: lg.link(b, e.Project), Area: lg.link(b, e.Area)}
		due := it.Due
		if due == "" && it.Type == "D" {
			due = it.On
		}
		var extra []string
		if due != "" {
			if d, ok := dateOf(due); ok {
				t.Due = d
			} else {
				notes = append(notes, "due: "+safe(due)+" is not a date; kept in the body")
				extra = append(extra, "Due (as written in the source): "+strings.ReplaceAll(due, "\n", " "))
			}
		}
		if it.Ref != "" {
			extra = append(extra, "Ref: "+strings.ReplaceAll(it.Ref, "\n", " "))
		}
		if eff == "waiting" || eff == "blocked" {
			if it.StatusNote != "" && (it.Status == "waiting" || it.Status == "blocked") {
				t.BlockedBy = []string{strings.ReplaceAll(it.StatusNote, "\n", " ")}
			}
			for _, ct := range it.Contacts {
				if ct.State == "sent" {
					t.BlockedBy = append(t.BlockedBy, "waiting on "+ct.Who)
				}
			}
		}
		for _, d := range it.Depends {
			dc := lg.ix.items[d]
			switch {
			case dc == nil:
				notes = append(notes, "depends: "+safe(d)+" is not in the source events")
				extra = append(extra, "Depends on (not imported): "+d)
			case dc.typ == model.TypeTask || dc.typ == model.TypeQuestion:
				if id := b.idOf(dc); id != "" {
					t.DependsOn = append(t.DependsOn, model.LinkTo(id))
				} else {
					extra = append(extra, "Depends on (not imported): "+d)
				}
			default:
				notes = append(notes, "depends: "+safe(d)+" is a note, not a task or question; kept in the body")
				extra = append(extra, "Depends on: "+d)
			}
		}
		for i, ct := range it.Contacts {
			if ct.State == "sent" {
				if id := b.idOf(lg.rfxOf[it][i]); id != "" {
					t.DependsOn = append(t.DependsOn, model.LinkTo(id))
				}
			}
		}
		title, _ := titleBody(it.Text)
		if title == "" {
			title = it.ID
		}
		doc := model.Document{ID: b.idOf(c), Folder: model.Folder(e.Folder), Title: title, Body: itemBody(b, it, true, extra...), Meta: t}
		return lg.finish(b, c, it, doc, notes)
	}
}

func (lg *lgCtx) finish(b *bctx, c *cand, it *item, doc model.Document, notes []string) (model.Document, []string, error) {
	if err := doc.SetExtra("imported_id", it.ID); err != nil {
		return doc, notes, err
	}
	return doc, notes, doc.SetExtra("source", b.provenance(c))
}

func (lg *lgCtx) buildQuestion(it *item) builderFn {
	return func(b *bctx, c *cand, e Entry) (model.Document, []string, error) {
		eff := it.effective()
		st := model.QuestionOpen
		switch eff {
		case "answered", "done":
			st = model.QuestionAnswered
		case "dropped":
			st = model.QuestionDropped
		}
		q := &model.Question{Common: model.Common{Type: model.TypeQuestion, Created: dateStr(it.Created, b.now), Tags: e.Tags}, Status: st, Scope: model.ScopeRoot}
		if l := lg.link(b, e.Project); l != "" {
			q.Scope = l
		}
		var extra []string
		if it.Ref != "" {
			extra = append(extra, "Ref: "+strings.ReplaceAll(it.Ref, "\n", " "))
		}
		if rs := lg.rfxOf[it]; len(rs) > 0 {
			pick := rs[0]
			for i, ct := range it.Contacts {
				if ct.State == "sent" {
					pick = rs[i]
					break
				}
			}
			if id := b.idOf(pick); id != "" {
				q.RFX = model.LinkTo(id)
			}
		}
		if st == model.QuestionAnswered && it.Answer != "" {
			q.Answered, q.AnsweredBy = dateStr(it.AnswerTS, b.now), it.AnswerActor
			extra = append(extra, "## Answer\n\n"+it.Answer)
		}
		title, _ := titleBody(it.Text)
		if title == "" {
			title = it.ID
		}
		doc := model.Document{ID: b.idOf(c), Folder: model.Folder(e.Folder), Title: title, Body: itemBody(b, it, true, extra...), Meta: q}
		return lg.finish(b, c, it, doc, nil)
	}
}

func (lg *lgCtx) buildNote(it *item) builderFn {
	return func(b *bctx, c *cand, e Entry) (model.Document, []string, error) {
		n := &model.Note{Common: model.Common{Type: model.TypeNote, Created: dateStr(it.Created, b.now), Tags: e.Tags}, Project: lg.link(b, e.Project)}
		var extra []string
		if it.Ref != "" {
			extra = append(extra, "Ref: "+strings.ReplaceAll(it.Ref, "\n", " "))
		}
		if it.Type == "I" {
			extra = append(extra, "Who: "+it.Who)
		}
		title, _ := titleBody(it.Text)
		if title == "" {
			title = it.ID
		}
		doc := model.Document{ID: b.idOf(c), Folder: model.Folder(e.Folder), Title: title, Body: itemBody(b, it, true, extra...), Meta: n}
		return lg.finish(b, c, it, doc, nil)
	}
}

func (lg *lgCtx) buildInteraction(it *item) builderFn {
	return func(b *bctx, c *cand, e Entry) (model.Document, []string, error) {
		pc := lg.person(it.Who)
		pid := b.idOf(pc)
		if pid == "" {
			return model.Document{}, nil, fmt.Errorf("person %q is not imported", safe(it.Who))
		}
		date := dateStr(it.Created, b.now)
		if d, ok := dateOf(it.On); ok {
			date = d
		}
		dir := model.InteractionObserved
		switch strings.ToLower(it.Direction) {
		case "in":
			dir = model.InteractionIn
		case "out":
			dir = model.InteractionOut
		}
		ia := &model.Interaction{Common: model.Common{Type: model.TypeInteraction, Created: dateStr(it.Created, b.now), Tags: e.Tags},
			Date: date, With: []model.Link{model.LinkTo(pid)}, Medium: mediumOf(it.Via), Direction: dir, Visibility: model.VisibilityPrivate}
		var extra []string
		if it.Ref != "" {
			extra = append(extra, "Ref: "+strings.ReplaceAll(it.Ref, "\n", " "))
		}
		// An opinion is kept only when the source recorded one.
		if it.Opinion != "" {
			extra = append(extra, "Opinion (recorded in the source): "+strings.ReplaceAll(it.Opinion, "\n", " "))
		}
		title, _ := titleBody(it.Text)
		if title == "" {
			title = it.ID
		}
		doc := model.Document{ID: b.idOf(c), Folder: model.Folder(e.Folder), Title: title, Body: itemBody(b, it, true, extra...), Meta: ia}
		return lg.finish(b, c, it, doc, nil)
	}
}

// rfxCand is the request made from one contact of an item.
func (lg *lgCtx) rfxCand(it *item, ic *cand, ct *contact, pc *cand, n int) *cand {
	s := slug(ct.Who)
	if s == "" {
		s = fmt.Sprint(n + 1)
	}
	title, _ := titleBody(it.Text)
	if title == "" {
		title = it.ID
	}
	frag := fmt.Sprintf("%s/contact/%s", it.ID, s)
	r := &cand{
		key: "events.jsonl#" + frag, srcRel: "events.jsonl", frag: frag, fixedType: true,
		hash: sha(append(bytes.Join(it.raw, []byte("\n")), []byte("\ncontact:"+ct.Who)...)),
		typ:  model.TypeRFX, folder: model.FolderRFX, idBase: "rfx-" + slug(it.ID) + "-" + s, project: ic.project,
		names: nil,
	}
	if ct.Direction == "in" {
		r.title = ct.Who + " asks: " + title
	} else {
		r.title = "Ask " + ct.Who + ": " + title
	}
	r.build = func(b *bctx, c *cand, e Entry) (model.Document, []string, error) {
		pid := b.idOf(pc)
		if pid == "" {
			return model.Document{}, nil, fmt.Errorf("person %q is not imported", safe(ct.Who))
		}
		state := map[string]model.TargetState{"drafted": model.TargetDrafted, "sent": model.TargetSent, "replied": model.TargetReplied, "no-reply": model.TargetNoReply}[ct.State]
		if state == "" {
			state = model.TargetDrafted
		}
		tg := model.Target{To: model.LinkTo(pid), Medium: mediumOf(ct.Via), State: state, Ref: ct.Ref}
		if !ct.SentTS.IsZero() && state != model.TargetDrafted {
			tg.Sent = dateStr(ct.SentTS, b.now)
		}
		dir := model.RFXOut
		if ct.Direction == "in" {
			dir = model.RFXIn
		}
		status := model.RFXOpen
		switch state {
		case model.TargetReplied:
			status = model.RFXAnswered
		case model.TargetNoReply:
			status = model.RFXClosed
		}
		x := &model.RFX{Common: model.Common{Type: model.TypeRFX, Created: dateStr(ct.FirstTS, b.now), Tags: e.Tags},
			Direction: dir, Status: status, Route: "imported", SelfServeCheck: "not recorded in the source",
			Project: lg.link(b, e.Project), Targets: []model.Target{tg}}
		if d, ok := dateOf(ct.ReplyBy); ok {
			x.NextNudge = d
		}
		switch ic.typ {
		case model.TypeTask:
			if id := b.idOf(ic); id != "" {
				x.Blocks = []model.Link{model.LinkTo(id)}
			}
		case model.TypeQuestion:
			if id := b.idOf(ic); id != "" {
				x.Question = model.LinkTo(id)
			}
		}
		body := "Imported id: " + it.ID + "/" + ct.Who
		if ic.typ != model.TypeTask && ic.typ != model.TypeQuestion {
			if id := b.idOf(ic); id != "" {
				body = "About: [[" + id + "]]\n\n" + body
			}
		}
		doc := model.Document{ID: b.idOf(c), Folder: model.Folder(e.Folder), Title: c.title, Body: body, Meta: x}
		if err := doc.SetExtra("imported_id", it.ID+"/"+ct.Who); err != nil {
			return doc, nil, err
		}
		return doc, nil, doc.SetExtra("source", b.provenance(c))
	}
	return r
}
