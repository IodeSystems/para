package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/mail"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// A meeting is a conversation with people at a time, usually to settle one or
// more requests. Para stores the meeting, its questions, and its notes. It does
// not write a calendar, and it does not record or transcribe: notes are what a
// person or an agent gives it.
//
// Asking for a meeting is progress on the request it serves: each linked RFX
// gets a target per person with medium meeting, in state requested, then
// scheduled when a time is set.

const (
	questionsHeading = "## Questions"
	noQuestions      = "None yet."
)

// MeetingView is a meeting without its body. Start and End are as stored: RFC
// 3339 with the zone the meeting was booked in. A requested meeting has none.
type MeetingView struct {
	ID     string              `json:"id"`
	Folder model.Folder        `json:"folder"`
	Title  string              `json:"title"`
	Status model.MeetingStatus `json:"status"`
	Start  string              `json:"start,omitempty"`
	End    string              `json:"end,omitempty"`
	With   []string            `json:"with"`
	// Attendees are invitees not in the directory: names or emails as text.
	Attendees []string `json:"attendees"`
	RFX       []string `json:"rfx"`
	// CalendarUIDs are the meeting's calendar events (invite, room, prep).
	CalendarUIDs []model.CalendarEvent `json:"calendar_uids"`
	Rev          string                `json:"rev"`
	// Optional is true when the owner may skip it; Attend is yes, no, maybe,
	// or empty (undecided); AttendDefault is what the next occurrence of a
	// recurring meeting starts with.
	Optional      bool         `json:"optional,omitempty"`
	Attend        model.Attend `json:"attend,omitempty"`
	AttendDefault model.Attend `json:"attend_default,omitempty"`
	// Names are for people, one per id.
	WithNames []string `json:"with_names,omitempty" nameof:"With"`
	RFXNames  []string `json:"rfx_names,omitempty" nameof:"RFX"`
}

func meetingView(d model.Document) (MeetingView, error) {
	m, ok := readMeeting(d)
	if !ok {
		return MeetingView{}, notMeeting(d)
	}
	v := MeetingView{ID: d.ID, Folder: d.Folder, Title: d.Title, Status: m.Status, Start: m.Start, End: m.End,
		With: targets(m.With), Attendees: []string{}, RFX: targets(m.RFX), CalendarUIDs: []model.CalendarEvent{}, Rev: d.Rev}
	if m.occ != nil {
		v.CalendarUIDs = append(v.CalendarUIDs, m.occ.UIDs()...)
		v.Attendees = append(v.Attendees, m.occ.Attendees...)
		v.Optional, v.Attend, v.AttendDefault = m.occ.Optional, m.occ.Attend, m.occ.AttendDefault
	}
	return v, nil
}

// getMeeting reads a meeting in either shape: a task of kind meeting, or a
// `type: meeting` document.
func getMeeting(ctx context.Context, s store.Store, id string) (model.Document, *mtg, error) {
	id = normID(id)
	if id == "" {
		return model.Document{}, nil, errors.New("id is empty")
	}
	d, err := s.Get(ctx, id)
	if err != nil {
		return d, nil, err
	}
	m, ok := readMeeting(d)
	if !ok {
		return d, nil, notMeeting(d)
	}
	m.Day = s.Now().Format(dateLayout)
	return d, m, nil
}

// meetingOpen reports whether a meeting is still ahead: asked for or booked.
func meetingOpen(s model.MeetingStatus) bool {
	return s == model.MeetingRequested || s == model.MeetingScheduled
}

// loadMeetings returns every meeting. Open ones come first: booked by start
// time, then requested; then held and cancelled, latest first.
func loadMeetings(ctx context.Context, s store.Store) ([]MeetingView, error) {
	out := []MeetingView{}
	for _, typ := range []model.Type{model.TypeMeeting, model.TypeTask, model.TypeObligation} {
		refs, err := s.List(ctx, store.Filter{Type: typ})
		if err != nil {
			return nil, err
		}
		for _, r := range refs {
			doc, err := s.Get(ctx, r.ID)
			if err != nil {
				return nil, err
			}
			if _, ok := readMeeting(doc); !ok {
				continue
			}
			v, err := meetingView(doc)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
	}
	rank := func(v MeetingView) int {
		switch v.Status {
		case model.MeetingScheduled:
			return 0
		case model.MeetingRequested:
			return 1
		}
		return 2
	}
	at := func(v MeetingView) time.Time {
		t, _ := time.Parse(time.RFC3339, v.Start)
		return t
	}
	slices.SortStableFunc(out, func(a, b MeetingView) int {
		ra, rb := rank(a), rank(b)
		switch {
		case ra != rb:
			return cmp.Compare(ra, rb)
		case ra < 2:
			return cmp.Or(at(a).Compare(at(b)), cmp.Compare(a.ID, b.ID))
		}
		return cmp.Or(at(b).Compare(at(a)), cmp.Compare(a.ID, b.ID))
	})
	return out, nil
}

// checkSlot validates a start and end: both RFC 3339 with a zone, the end after
// the start. The zone is kept as written. They are compared as instants, so a
// meeting that spans midnight, or a clock change, is fine.
func checkSlot(start, end string) (string, string, error) {
	st, err := parseTime("start", start)
	if err != nil {
		return "", "", err
	}
	en, err := parseTime("end", end)
	if err != nil {
		return "", "", err
	}
	if !en.After(st) {
		return "", "", fmt.Errorf("end %s is not after start %s", end, start)
	}
	return st.Format(time.RFC3339), en.Format(time.RFC3339), nil
}

// meetingLinks resolves ids to links of the given types, dropping repeats.
func meetingLinks(ctx context.Context, d Deps, field string, ids []string, types ...model.Type) ([]model.Link, error) {
	var out []model.Link
	for _, raw := range ids {
		l, err := linkTo(ctx, d, field, raw, types...)
		if err != nil {
			return nil, err
		}
		if l == "" {
			return nil, fmt.Errorf("%s: empty id", field)
		}
		if !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	return out, nil
}

// syncTargets moves the meeting target of each person on each linked RFX. next
// maps the state a target has now to the state it gets; ok false leaves it. A
// person with no meeting target on an RFX gets one when add is set. It returns
// the ids of the RFX it changed. A closed RFX is skipped and named in warns.
func syncTargets(ctx context.Context, d Deps, rfx, with []model.Link, add bool, next func(model.TargetState) (model.TargetState, bool)) (changed, warns []string, err error) {
	for _, rl := range rfx {
		doc, m, err := getRFX(ctx, d, rl.Target())
		if err != nil {
			return changed, warns, err
		}
		if m.Status == model.RFXClosed {
			warns = append(warns, doc.ID+" is closed: its targets were not changed")
			continue
		}
		dirty := false
		// A new meeting target goes to the people this request already goes to
		// that are in the meeting; if none is, to everyone in it.
		asked := slices.DeleteFunc(slices.Clone(with), func(w model.Link) bool {
			return !slices.ContainsFunc(m.Targets, func(t model.Target) bool { return t.To == w })
		})
		if len(asked) == 0 {
			asked = with
		}
		for _, w := range with {
			i := slices.IndexFunc(m.Targets, func(t model.Target) bool { return t.To == w && t.Medium == model.MediumMeeting })
			if i < 0 {
				if !add || !slices.Contains(asked, w) {
					continue
				}
				st, ok := next("")
				if !ok {
					continue
				}
				m.Targets = append(m.Targets, model.Target{To: w, Medium: model.MediumMeeting, State: st})
				dirty = true
				continue
			}
			if st, ok := next(m.Targets[i].State); ok && st != m.Targets[i].State {
				m.Targets[i].State = st
				dirty = true
			}
		}
		if !dirty {
			continue
		}
		if _, err := d.Store.Update(ctx, doc); err != nil {
			return changed, warns, fmt.Errorf("request %s: %w", doc.ID, err)
		}
		changed = append(changed, doc.ID)
	}
	return changed, warns, nil
}

// askState is the state a target takes when the meeting is asked for or booked.
// A target that is already sent or replied by another route is left alone.
func askState(to model.TargetState) func(model.TargetState) (model.TargetState, bool) {
	return func(old model.TargetState) (model.TargetState, bool) {
		switch old {
		case "", model.TargetDrafted, model.TargetRequested, model.TargetScheduled, model.TargetNoReply:
			return to, true
		}
		return old, false
	}
}

// gatherQuestions lists what the meeting should settle: each linked request,
// then the open questions in scope that no linked request already asks. A
// question is in scope when it names a project or area in scope, or a task of
// one. Scope is the given ids plus the project of each linked request.
func gatherQuestions(ctx context.Context, d Deps, rfx []model.Link, scope []string) ([]string, error) {
	var lines []string
	asked := map[string]bool{}
	scopeSet := map[string]bool{}
	for _, s := range scope {
		scopeSet[normID(s)] = true
	}
	for _, rl := range rfx {
		doc, m, err := getRFX(ctx, d, rl.Target())
		if err != nil {
			return nil, err
		}
		line := fmt.Sprintf("- [[%s]] %s", doc.ID, doc.Title)
		if q := m.Question.Target(); q != "" {
			asked[q] = true
			line += fmt.Sprintf(" (question [[%s]])", q)
		}
		if p := m.Goal.Target(); p != "" {
			scopeSet[p] = true
		}
		lines = append(lines, line)
	}
	qs, err := loadQuestions(ctx, d.Store)
	if err != nil {
		return nil, err
	}
	for _, x := range qs {
		if !questionOpen(x.v) || asked[x.v.ID] {
			continue
		}
		if scopeSet[x.v.Scope] || (x.project != "" && scopeSet[x.project]) || (x.area != "" && scopeSet[x.area]) {
			lines = append(lines, fmt.Sprintf("- [[%s]] %s", x.v.ID, x.v.Title))
		}
	}
	return lines, nil
}

// meetingBody lays out a new meeting: any text the caller gave, the questions,
// and an empty Notes section.
func meetingBody(intro string, questions []string) string {
	var b strings.Builder
	if intro = strings.TrimSpace(intro); intro != "" {
		b.WriteString(intro + "\n\n")
	}
	b.WriteString(questionsHeading + "\n\n")
	if len(questions) == 0 {
		b.WriteString(noQuestions)
	} else {
		b.WriteString(strings.Join(questions, "\n"))
	}
	b.WriteString("\n\n" + notesHeading + "\n")
	return strings.TrimRight(b.String(), "\n")
}

// addQuestionLines adds the lines whose document is not yet linked in the body.
func addQuestionLines(body string, lines []string) string {
	for _, l := range lines {
		_, rest, _ := strings.Cut(l, "[[")
		id, _, _ := strings.Cut(rest, "]]")
		if strings.Contains(body, "[["+id+"]]") {
			continue
		}
		// An empty section holds the placeholder; the first real line replaces it.
		body = strings.Replace(body, questionsHeading+"\n\n"+noQuestions, questionsHeading+"\n\n"+l, 1)
		if strings.Contains(body, "[["+id+"]]") {
			continue
		}
		body = appendToSection(body, questionsHeading, l)
	}
	return body
}

// MeetingAddInput is the input of meeting_add.
type MeetingAddInput struct {
	Title string   `json:"title"`
	With  []string `json:"with,omitempty"` // person or group ids, or a name or email found in the directory; default: who the linked requests go to
	// Attendees are invitees by name, email, or "Name <email>", kept as text.
	// An entry that matches a person in the directory becomes a with link.
	Attendees []string `json:"attendees,omitempty"`
	RFX       []string `json:"rfx,omitempty"`   // requests the meeting should settle
	Scope     []string `json:"scope,omitempty"` // project, area, or task ids whose open questions go on the agenda
	Start     string   `json:"start,omitempty"` // RFC 3339 with a zone, such as 2026-11-04T10:00:00-05:00; with end, the meeting is scheduled
	End       string   `json:"end,omitempty"`
	Body      string   `json:"body,omitempty"` // text before the Questions section
	// CalendarUID is the UID of the meeting's event in the owner's calendar,
	// so calendar_compare matches it exactly; it is stored as role primary.
	// CalendarUIDs are more events of it (role: room, internal prep, ...).
	CalendarUID  string                `json:"calendar_uid,omitempty"`
	CalendarUIDs []model.CalendarEvent `json:"calendar_uids,omitempty"`
	// Optional is true when the calendar lists the owner as an optional
	// attendee: the meeting asks nothing of the owner until they say they go.
	Optional bool `json:"optional,omitempty"`
	// ImportedFrom is the file the new document comes from (import_verify).
	ImportedFrom *model.ImportedFrom `json:"imported_from,omitempty"`
}

// MeetingAddOutput is the output of meeting_add.
type MeetingAddOutput struct {
	ID       string              `json:"id"`
	Folder   model.Folder        `json:"folder"`
	Status   model.MeetingStatus `json:"status"`
	RFX      []string            `json:"rfx"` // requests whose meeting target changed
	Warnings []string            `json:"warnings"`
	Rev      string              `json:"rev"`
}

// MeetingAdd asks for a meeting, or books one when it has a time. The body
// gathers the questions to settle.
var MeetingAdd = defineSafe("meeting_add", "Ask for or book a meeting with people: with takes person or group ids, names, or emails (a match in the directory links; no match is kept as an attendee with a warning); attendees takes invitee names or emails as text; calendar_uid allows a hold with no one else. The body gathers the questions from linked requests and open questions in scope; linked requests move to requested or scheduled", func(ctx context.Context, d Deps, in MeetingAddInput) (MeetingAddOutput, error) {
	out, _, err := addMeetingDoc(ctx, d, in, nil, nil)
	return out, err
})

// addMeetingDoc makes a meeting: pre edits it as a task before it is stored,
// post edits it as an obligation when the vault stores one.
func addMeetingDoc(ctx context.Context, d Deps, in MeetingAddInput, pre func(*model.Task), post func(*model.Obligation)) (MeetingAddOutput, model.Document, error) {
	title, err := oneLine("title", in.Title)
	if err != nil {
		return MeetingAddOutput{}, model.Document{}, err
	}
	rfx, err := meetingLinks(ctx, d, "rfx", in.RFX, model.TypeRFX)
	if err != nil {
		return MeetingAddOutput{}, model.Document{}, err
	}
	with, attendees, warns, err := resolveWith(ctx, d, in.With, in.Attendees)
	if err != nil {
		return MeetingAddOutput{}, model.Document{}, err
	}
	if len(with) == 0 {
		for _, rl := range rfx {
			_, m, err := getRFX(ctx, d, rl.Target())
			if err != nil {
				return MeetingAddOutput{}, model.Document{}, err
			}
			for _, t := range m.Targets {
				if !slices.Contains(with, t.To) {
					with = append(with, t.To)
				}
			}
		}
	}
	uids, err := calendarEvents(in.CalendarUID, in.CalendarUIDs)
	if err != nil {
		return MeetingAddOutput{}, model.Document{}, err
	}
	if err := needsSomeone(with, attendees, uids, &warns); err != nil {
		return MeetingAddOutput{}, model.Document{}, err
	}
	for _, rl := range rfx {
		if _, m, err := getRFX(ctx, d, rl.Target()); err == nil && m.Status == model.RFXClosed {
			return MeetingAddOutput{}, model.Document{}, fmt.Errorf("request %s is closed", rl.Target())
		}
	}
	for _, s := range in.Scope {
		if _, err := linkTo(ctx, d, "scope", s, model.TypeGoal, model.TypeArea, model.TypeTask); err != nil {
			return MeetingAddOutput{}, model.Document{}, err
		}
	}
	meta := &model.Task{Common: model.Common{Type: model.TypeTask, ImportedFrom: in.ImportedFrom}, Status: model.TaskTodo, Kind: model.KindMeeting, Occasion: model.Occasion{With: with, Attendees: attendees, RFX: rfx, CalendarUIDs: uids, Optional: in.Optional}}
	status := model.MeetingRequested
	switch {
	case in.Start == "" && in.End == "":
	case in.Start == "" || in.End == "":
		return MeetingAddOutput{}, model.Document{}, errors.New("start and end: give both or neither")
	default:
		if meta.Scheduled, meta.End, err = checkSlot(in.Start, in.End); err != nil {
			return MeetingAddOutput{}, model.Document{}, err
		}
		status = model.MeetingScheduled
	}
	qs, err := gatherQuestions(ctx, d, rfx, in.Scope)
	if err != nil {
		return MeetingAddOutput{}, model.Document{}, err
	}
	if pre != nil {
		pre(meta)
	}
	doc, err := storedShape(d, model.Document{Folder: model.FolderGoals, Title: title, Body: meetingBody(in.Body, qs), Meta: meta})
	if err != nil {
		return MeetingAddOutput{}, model.Document{}, err
	}
	if o, ok := doc.Meta.(*model.Obligation); ok && post != nil {
		post(o)
	}
	if doc, err = d.Store.Create(ctx, doc); err != nil {
		return MeetingAddOutput{}, model.Document{}, err
	}
	to := model.TargetRequested
	if status == model.MeetingScheduled {
		to = model.TargetScheduled
	}
	changed, swarns, err := syncTargets(ctx, d, rfx, with, true, askState(to))
	out := MeetingAddOutput{ID: doc.ID, Folder: doc.Folder, Status: status, RFX: nonNil(changed), Warnings: nonNil(append(warns, swarns...)), Rev: doc.Rev}
	if err != nil {
		return out, doc, fmt.Errorf("meeting %s was made but its requests were not all updated: %w", doc.ID, err)
	}
	return out, doc, nil
}

// needsSomeone refuses a meeting with no with link, no attendee, and no
// linked-request target, unless it has a calendar event of the owner's (a
// hold, a room block, a solo block): that one is allowed, with a warning.
func needsSomeone(with []model.Link, attendees []string, uids []model.CalendarEvent, warns *[]string) error {
	if len(with) > 0 || len(attendees) > 0 {
		return nil
	}
	if len(uids) == 0 {
		return errors.New("a meeting needs someone: pass with (a person or group id, a name, or an email), attendees (names or emails from the invite), or calendar_uid for a hold with no one else; or link a request that has targets")
	}
	*warns = append(*warns, "no one else on this meeting")
	return nil
}

// resolveWith turns the with entries and attendees of a meeting into with
// links and attendee text. An id resolves as meetingLinks does. Any other
// value is looked up in the directory by name or email: one match becomes a
// link; none, or several, stays an attendee with a warning. It never fails on
// an unknown name.
func resolveWith(ctx context.Context, d Deps, with, attendees []string) (links []model.Link, kept, warns []string, err error) {
	add := func(l model.Link) {
		if !slices.Contains(links, l) {
			links = append(links, l)
		}
	}
	keep := func(raw string) {
		if raw = strings.Join(strings.Fields(raw), " "); raw != "" && !slices.Contains(kept, raw) {
			kept = append(kept, raw)
		}
	}
	rest := slices.Clone(attendees)
	for _, raw := range with {
		id := normID(raw)
		if id == "" {
			return nil, nil, nil, errors.New("meeting: with: empty id")
		}
		if _, gerr := d.Store.Get(ctx, id); gerr == nil {
			l, lerr := linkTo(ctx, d, "with", raw, model.TypePerson, model.TypeGroup)
			if lerr != nil {
				return nil, nil, nil, lerr
			}
			add(l)
			continue
		} else if !errors.Is(gerr, model.ErrNotFound) {
			return nil, nil, nil, fmt.Errorf("meeting: with %s: %w", id, gerr)
		}
		rest = append(rest, raw)
	}
	for _, raw := range rest {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		l, warn, lerr := lookupAttendee(ctx, d, raw)
		if lerr != nil {
			return nil, nil, nil, lerr
		}
		if l != "" {
			add(l)
			continue
		}
		keep(raw)
		warns = append(warns, warn)
	}
	return links, kept, warns, nil
}

// lookupAttendee finds the person a free-text attendee names: by email first
// when it has one, then by name. No single match gives an empty link and a
// warning.
func lookupAttendee(ctx context.Context, d Deps, raw string) (model.Link, string, error) {
	var queries []string
	if a, err := mail.ParseAddress(strings.TrimSpace(raw)); err == nil {
		queries = append(queries, a.Address)
		if a.Name != "" {
			queries = append(queries, a.Name)
		}
	} else {
		queries = append(queries, strings.Join(strings.Fields(raw), " "))
	}
	ambiguous := false
	for _, q := range queries {
		w, err := who(ctx, d, WhoInput{Query: q})
		if err != nil {
			return "", "", fmt.Errorf("meeting: lookupAttendee: %w", err)
		}
		if w.Person != nil {
			return model.LinkTo(w.Person.ID), "", nil
		}
		ambiguous = ambiguous || len(w.Candidates) > 1
	}
	why := "is not in the directory"
	if ambiguous {
		why = "matches more than one person"
	}
	return "", fmt.Sprintf("attendee %q %s: kept as text; org_person_add makes them a person", flatLine(raw), why), nil
}

// calendarEvents checks the calendar events of a meeting or call: one UID
// given alone (role primary) and a list. A UID listed twice is refused.
func calendarEvents(single string, list []model.CalendarEvent) ([]model.CalendarEvent, error) {
	var out []model.CalendarEvent
	add := func(uid, role string) error {
		uid, err := checkCalendarUID(uid)
		if err != nil {
			return err
		}
		role = flatLine(role)
		if uid == "" {
			return nil
		}
		if slices.ContainsFunc(out, func(e model.CalendarEvent) bool { return e.UID == uid }) {
			return fmt.Errorf("calendar uid %q is given twice", uid)
		}
		out = append(out, model.CalendarEvent{UID: uid, Role: role})
		return nil
	}
	if err := add(single, model.RolePrimary); err != nil {
		return nil, err
	}
	for _, e := range list {
		if strings.TrimSpace(e.UID) == "" {
			return nil, errors.New("calendar_uids: each entry needs uid")
		}
		if err := add(e.UID, e.Role); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// checkCalendarUID trims a calendar event UID; it must be one line.
func checkCalendarUID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if strings.ContainsAny(s, "\r\n") {
		return "", errors.New("calendar_uid must be one line")
	}
	if len(s) > 512 {
		return "", errors.New("calendar_uid is over 512 characters")
	}
	return s, nil
}

// MeetingListInput is the input of meeting_list.
type MeetingListInput struct {
	Status model.MeetingStatus `json:"status,omitempty" query:"status"`
	All    bool                `json:"all,omitempty" query:"all"` // include held and cancelled
}

// MeetingListOutput is the output of meeting_list.
type MeetingListOutput struct {
	Meetings []MeetingView `json:"meetings"`
}

// MeetingList lists requested and scheduled meetings, booked ones first by
// start time. All, or a Status, includes held and cancelled ones.
var MeetingList = defineRead("meeting_list", "List meetings: requested and scheduled by default, booked ones first by start time", func(ctx context.Context, d Deps, in MeetingListInput) (MeetingListOutput, error) {
	if in.Status != "" && !slices.Contains(model.MeetingStatuses, in.Status) {
		return MeetingListOutput{}, fmt.Errorf("status %q must be one of %v", in.Status, model.MeetingStatuses)
	}
	all, err := loadMeetings(ctx, d.Store)
	if err != nil {
		return MeetingListOutput{}, err
	}
	out := MeetingListOutput{Meetings: []MeetingView{}}
	for _, v := range all {
		switch {
		case in.Status != "" && v.Status != in.Status,
			in.Status == "" && !in.All && !meetingOpen(v.Status):
			continue
		}
		out.Meetings = append(out.Meetings, v)
	}
	return out, nil
})

// MeetingShowInput is the input of meeting_show.
type MeetingShowInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// MeetingShowOutput is the output of meeting_show.
type MeetingShowOutput struct {
	Meeting MeetingView `json:"meeting"`
	Body    string      `json:"body"`
}

// MeetingShow returns one meeting with its questions and notes.
var MeetingShow = defineRead("meeting_show", "Show a meeting with its questions and notes", func(ctx context.Context, d Deps, in MeetingShowInput) (MeetingShowOutput, error) {
	doc, _, err := getMeeting(ctx, d.Store, in.ID)
	if err != nil {
		return MeetingShowOutput{}, err
	}
	v, err := meetingView(doc)
	return MeetingShowOutput{Meeting: v, Body: doc.Body}, err
})

// MeetingChangeOutput is the output of meeting_schedule and meeting_cancel.
type MeetingChangeOutput struct {
	ID       string              `json:"id"`
	Status   model.MeetingStatus `json:"status"`
	Start    string              `json:"start,omitempty"`
	End      string              `json:"end,omitempty"`
	RFX      []string            `json:"rfx"` // requests whose meeting target changed
	Warnings []string            `json:"warnings"`
	Rev      string              `json:"rev"`
	Next     string              `json:"next,omitempty"`    // a recurring meeting: its next occurrence
	Request  string              `json:"request,omitempty"` // agent request queued for the next occurrence
}

// MeetingScheduleInput is the input of meeting_schedule.
type MeetingScheduleInput struct {
	ID    string `json:"id"`
	Start string `json:"start"` // RFC 3339 with a zone
	End   string `json:"end"`
	// CalendarUID adds the UID of the meeting's calendar event (role primary),
	// and CalendarUIDs more events; a UID already stored takes the new role.
	// Empty keeps the stored ones. An older type: meeting document has none.
	CalendarUID  string                `json:"calendar_uid,omitempty"`
	CalendarUIDs []model.CalendarEvent `json:"calendar_uids,omitempty"`
}

// MeetingSchedule books a requested meeting, or moves a booked one. The linked
// requests move to scheduled, and the questions list picks up new open ones.
var MeetingSchedule = defineSafe("meeting_schedule", "Set or change the time of a requested or scheduled meeting; linked requests move to scheduled", meetingSchedule)

func meetingSchedule(ctx context.Context, d Deps, in MeetingScheduleInput) (MeetingChangeOutput, error) {
	doc, m, err := getMeeting(ctx, d.Store, in.ID)
	if err != nil {
		return MeetingChangeOutput{}, err
	}
	if !meetingOpen(m.Status) {
		return MeetingChangeOutput{}, fmt.Errorf("%s is %s: add a new meeting", doc.ID, m.Status)
	}
	start, end, err := checkSlot(in.Start, in.End)
	if err != nil {
		return MeetingChangeOutput{}, err
	}
	uids, err := calendarEvents(in.CalendarUID, in.CalendarUIDs)
	if err != nil {
		return MeetingChangeOutput{}, err
	}
	if len(uids) > 0 {
		if m.occ == nil {
			return MeetingChangeOutput{}, fmt.Errorf("%s is an older meeting document with no calendar_uids: the owner runs `para vault upgrade` to make it a task, then set calendar_uids", doc.ID)
		}
		for _, e := range uids {
			m.occ.AddUID(e)
		}
	}
	if m.Status == model.MeetingScheduled && m.Start != "" && (m.Start != start || m.End != end) {
		// A move keeps the old time in the notes.
		doc.Body = appendToSection(doc.Body, notesHeading, fmt.Sprintf("- %s: Rescheduled from %s to %s.", d.Store.Now().Format(dateLayout), shortWhen(m.Start), start))
	}
	m.Status, m.Start, m.End = model.MeetingScheduled, start, end
	if qs, err := gatherQuestions(ctx, d, m.RFX, nil); err == nil {
		doc.Body = addQuestionLines(doc.Body, qs)
	}
	m.sync()
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return MeetingChangeOutput{}, err
	}
	if _, err := followObligation(ctx, d, doc.ID); err != nil {
		return MeetingChangeOutput{}, err
	}
	changed, warns, err := syncTargets(ctx, d, m.RFX, m.With, true, askState(model.TargetScheduled))
	out := MeetingChangeOutput{ID: doc.ID, Status: m.Status, Start: m.Start, End: m.End, RFX: nonNil(changed), Warnings: nonNil(warns), Rev: saved.Rev}
	if err != nil {
		return out, fmt.Errorf("meeting %s was scheduled but its requests were not all updated: %w", doc.ID, err)
	}
	return out, nil
}

// MeetingCancelInput is the input of meeting_cancel.
type MeetingCancelInput struct {
	ID     string `json:"id"`
	Reason string `json:"reason,omitempty"` // one line, added to the notes
}

// MeetingCancel cancels a requested or scheduled meeting. Its requests step
// back: a scheduled target is requested again, a requested one is drafted.
var MeetingCancel = define("meeting_cancel", "Cancel a requested or scheduled meeting; its requests step back to requested or drafted", func(ctx context.Context, d Deps, in MeetingCancelInput) (MeetingChangeOutput, error) {
	doc, m, err := getMeeting(ctx, d.Store, in.ID)
	if err != nil {
		return MeetingChangeOutput{}, err
	}
	if !meetingOpen(m.Status) {
		return MeetingChangeOutput{}, fmt.Errorf("%s is already %s", doc.ID, m.Status)
	}
	m.Status = model.MeetingCancelled
	line := "- " + d.Store.Now().Format(dateLayout) + ": Cancelled."
	if r := strings.Join(strings.Fields(in.Reason), " "); r != "" {
		line += " " + r
	}
	m.sync()
	next, req, nwarns, err := nextMeeting(ctx, d, doc, m)
	if err != nil {
		return MeetingChangeOutput{}, err
	}
	doc.Body = appendToSection(doc.Body, notesHeading, line)
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return MeetingChangeOutput{}, err
	}
	if _, err := followObligation(ctx, d, doc.ID); err != nil {
		return MeetingChangeOutput{}, err
	}
	changed, warns, err := syncTargets(ctx, d, m.RFX, m.With, false, func(old model.TargetState) (model.TargetState, bool) {
		switch old {
		case model.TargetScheduled:
			return model.TargetRequested, true
		case model.TargetRequested:
			return model.TargetDrafted, true
		}
		return old, false
	})
	out := MeetingChangeOutput{ID: doc.ID, Status: m.Status, Start: m.Start, End: m.End, RFX: nonNil(changed), Warnings: nonNil(append(nwarns, warns...)), Rev: saved.Rev, Next: next, Request: req}
	if err != nil {
		return out, fmt.Errorf("meeting %s was cancelled but its requests were not all updated: %w", doc.ID, err)
	}
	return out, nil
})
