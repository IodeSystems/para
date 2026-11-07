package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// A change to an obligation tells the people it concerns. Push out moves its
// date or time (a series occurrence alone: the next occurrence is made first
// from the old slot), redirect hands it to someone else or reroutes it to
// another item or a URL, and cancel closes it. Each takes a reason: the facts
// on the ground in words, or the higher-priority item it gives way to
// (because). Unless notify is false, the change writes one outgoing io draft
// per medium to the interested parties (owed_to, with, interested, and the
// attendees), saying what changed and why; a meeting with calendar events
// also gets a draft of the calendar change. Drafts follow the normal gate:
// the owner approves, and io_send_check applies before a send. One receipt
// undoes the change and takes back the drafts.

// Actions of obligation_change.
const (
	ChangePush     = "push"
	ChangeRedirect = "redirect"
	ChangeCancel   = "cancel"
)

// ObligationChangeInput is the input of obligation_change.
type ObligationChangeInput struct {
	ID     string `json:"id"`
	Action string `json:"action"` // push, redirect, or cancel
	// Push: due (YYYY-MM-DD) for a commitment; start and end (RFC 3339 with
	// a zone) for a meeting, or a commitment's start.
	Due   string `json:"due,omitempty"`
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
	// To is a redirect's new owner (a person or group id, or agent:<name>),
	// or the item id or URL that holds it from now on (it closes rerouted).
	To string `json:"to,omitempty"`
	// Outcome is a cancel's: declined (the owner says no; the default) or
	// dropped (nobody needs it any more). A meeting is cancelled as
	// meeting_cancel does.
	Outcome string `json:"outcome,omitempty"`
	// Reason is the facts on the ground, in words; Because the id of the
	// higher-priority item it gives way to. One of the two is required.
	Reason  string `json:"reason,omitempty"`
	Because string `json:"because,omitempty"`
	// Notify false writes no notice. Default true.
	Notify *bool `json:"notify,omitempty"`
}

// ObligationChangeOutput is what a change did. Notices are the drafts it
// wrote, Told the names of who they go to, Calendar the calendar draft.
// Receipt undoes it all (on a person's surface).
type ObligationChangeOutput struct {
	ID       string        `json:"id"`
	Action   string        `json:"action"`
	State    model.State   `json:"state"`
	Due      string        `json:"due,omitempty"`
	Start    string        `json:"start,omitempty"`
	End      string        `json:"end,omitempty"`
	OwedBy   []string      `json:"owed_by"`
	Next     string        `json:"next,omitempty"`
	Notices  []string      `json:"notices"`
	Told     []string      `json:"told"`
	Calendar string        `json:"calendar,omitempty"`
	Promise  model.Promise `json:"promise,omitempty"`
	Receipt  string        `json:"receipt,omitempty"`
	Warnings []string      `json:"warnings"`
}

// ObligationChange pushes out, redirects, or cancels an obligation and tells
// the people it concerns.
var ObligationChange = define("obligation_change", "Change an obligation and tell the people it concerns: action push (a new due, or start and end for a meeting; a series occurrence moves alone), redirect (to: a person or group id or agent:<name> owes it now; or an item id or URL holds it, and it closes rerouted), or cancel (outcome declined, the default, or dropped; a meeting is cancelled as meeting_cancel does). reason (the facts on the ground) or because (the id of the higher-priority item) is required. Unless notify is false it writes one outgoing io draft per medium to the interested parties (owed_to, with, interested, attendees), saying what changed and why, threaded on the last notice; a meeting with calendar events also gets a calendar draft (medium meeting) for the agent to apply after the owner approves. The owner approves each draft; io_send_check applies. Outputs notices, told, and on a person's surface receipt, which undoes the change and takes back the drafts", func(ctx context.Context, d Deps, in ObligationChangeInput) (ObligationChangeOutput, error) {
	if ctx.Value(recordingKey{}) != nil || agentSurface(d) {
		return obligationChange(ctx, d, in)
	}
	// On a person's surface the change files one receipt, so the web app can
	// undo it.
	rec := newRecorder(d.Store)
	inner := d
	inner.Store = rec
	out, err := obligationChange(context.WithValue(ctx, recordingKey{}, "obligation_change"), inner, in)
	id, ferr := rec.fileOneID(ctx, d, "obligation_change", writeMeta(ctx))
	if ferr != nil {
		return out, errors.Join(err, fmt.Errorf("the change happened, but its receipt failed: %w", ferr))
	}
	out.Receipt = id
	return out, err
})

func obligationChange(ctx context.Context, d Deps, in ObligationChangeInput) (ObligationChangeOutput, error) {
	doc, o, err := getObligation(ctx, d, in.ID)
	if err != nil {
		return ObligationChangeOutput{}, err
	}
	if !o.Open() {
		return ObligationChangeOutput{}, fmt.Errorf("%s is closed (%s): reopen it first", doc.ID, o.Outcome)
	}
	action := strings.TrimSpace(in.Action)
	if !slices.Contains([]string{ChangePush, ChangeRedirect, ChangeCancel}, action) {
		return ObligationChangeOutput{}, fmt.Errorf("action %q: want push, redirect, or cancel", in.Action)
	}
	reason := flatLine(in.Reason)
	if err := lintText("reason", reason); err != nil {
		return ObligationChangeOutput{}, err
	}
	var because model.Document
	if id := normID(in.Because); id != "" {
		if because, err = d.Store.Get(ctx, id); err != nil {
			return ObligationChangeOutput{}, linkErr("because", id, err)
		}
	}
	if reason == "" && because.ID == "" {
		return ObligationChangeOutput{}, errors.New("give reason (the facts on the ground) or because (the id of the higher-priority item): the people it concerns are told why")
	}
	notify := in.Notify == nil || *in.Notify
	today := d.Store.Now().Format(dateLayout)
	out := ObligationChangeOutput{ID: doc.ID, Action: action, OwedBy: []string{}, Notices: []string{}, Told: []string{}, Warnings: []string{}}
	// A promise past its date with no notice is broken before it changes.
	o.Promise = promiseNow(storeGet(ctx, d.Store), o, today)
	oldWhen := whenWords(o)
	var what, rerouted string
	var newOwner []model.Link
	switch action {
	case ChangePush:
		if in.To != "" || in.Outcome != "" {
			return out, errors.New("push takes due, start, and end")
		}
		if what, out.Next, err = pushObligation(ctx, d, doc, o, in); err != nil {
			return out, err
		}
	case ChangeRedirect:
		if in.Due != "" || in.Start != "" || in.End != "" || in.Outcome != "" {
			return out, errors.New("redirect takes to")
		}
		to := strings.TrimSpace(in.To)
		if to == "" {
			return out, errors.New("redirect needs to: a person or group id or agent:<name> who owes it now, or the item id or URL that holds it")
		}
		if rerouted, newOwner, err = redirectTarget(ctx, d, to); err != nil {
			return out, err
		}
		if rerouted != "" {
			what = "rerouted to " + rerouted
		} else {
			o.OwedBy = newOwner
			what = "handed to " + strings.Join(targets(newOwner), ", ")
		}
	case ChangeCancel:
		if in.Due != "" || in.Start != "" || in.End != "" || in.To != "" {
			return out, errors.New("cancel takes outcome")
		}
		switch strings.TrimSpace(in.Outcome) {
		case "", string(model.OutcomeDeclined), string(model.OutcomeDropped):
		default:
			return out, fmt.Errorf("outcome %q: a cancel is declined (the default) or dropped", in.Outcome)
		}
		what = "cancelled"
	}
	why := reason
	if because.ID != "" {
		why = strings.TrimSpace(why + " (because [[" + because.ID + "]])")
	}
	if action == ChangeRedirect && rerouted == "" {
		if _, err := d.Store.Update(ctx, doc); err != nil {
			return out, err
		}
	}
	// Reread: a push saved the new date itself. The notices come first, so
	// the change line names them.
	if doc, o, err = getObligation(ctx, d, doc.ID); err != nil {
		return out, err
	}
	if notify {
		ns, told, cal, warns, err := writeNotices(ctx, d, doc, o, action, oldWhen, rerouted, newOwner, reason, because, today)
		if err != nil {
			return out, err
		}
		out.Notices, out.Told, out.Calendar = ns, told, cal
		out.Warnings = append(out.Warnings, warns...)
		for _, id := range ns {
			o.Notices = append(o.Notices, model.LinkTo(id))
		}
		if cal != "" {
			o.Notices = append(o.Notices, model.LinkTo(cal))
		}
	}
	line := fmt.Sprintf("- %s: %s: %s", today, what, why)
	var ls []string
	for _, id := range append(slices.Clone(out.Notices), out.Calendar) {
		if id != "" {
			ls = append(ls, string(model.LinkTo(id)))
		}
	}
	if len(ls) > 0 {
		line += "; told: " + strings.Join(ls, ", ")
	} else if !notify {
		line += "; no notice (the owner chose not to tell them)"
	}
	doc.Body = appendToSection(doc.Body, changesHeading, line)
	o.Promise = promiseNow(storeGet(ctx, d.Store), o, today)
	if _, err := d.Store.Update(ctx, doc); err != nil {
		return out, err
	}
	switch {
	case action == ChangeCancel && o.Kind == model.KindMeeting:
		if _, err := MeetingCancel.run(ctx, d, MeetingCancelInput{ID: doc.ID, Reason: firstOf(reason, "gives way to "+because.Title)}); err != nil {
			return out, err
		}
	case action == ChangeCancel:
		outcome := cmp.Or(strings.TrimSpace(in.Outcome), string(model.OutcomeDeclined))
		if _, err := obligationClose.run(ctx, d, obligationCloseInput{ID: doc.ID, Outcome: outcome, Reason: why}); err != nil {
			return out, err
		}
	case rerouted != "":
		if _, err := obligationClose.run(ctx, d, obligationCloseInput{ID: doc.ID, Outcome: string(model.OutcomeRerouted), Reason: "Rerouted to " + rerouted + ": " + why}); err != nil {
			return out, err
		}
	}
	if doc, o, err = getObligation(ctx, d, doc.ID); err != nil {
		return out, err
	}
	if !o.Open() {
		if o.Closed == "" {
			o.Closed = model.Date(today)
		}
		o.Promise = promiseNow(storeGet(ctx, d.Store), o, today)
		if _, err := d.Store.Update(ctx, doc); err != nil {
			return out, err
		}
	}
	out.State, out.Due, out.Start, out.End, out.Promise = o.State, string(o.Due), o.Scheduled, o.End, o.Promise
	out.OwedBy = append(out.OwedBy, targets(o.OwedBy)...)
	return out, nil
}

// pushObligation moves an obligation's date or time and says how in words.
// A series occurrence moves alone: its next occurrence is made first, from
// the old slot, so the series keeps its days.
func pushObligation(ctx context.Context, d Deps, doc model.Document, o *model.Obligation, in ObligationChangeInput) (what, next string, err error) {
	if o.Schedule == model.ScheduleOngoing {
		return "", "", fmt.Errorf("%s is ongoing: it has no date to push", doc.ID)
	}
	if in.Due == "" && in.Start == "" {
		if o.Kind == model.KindMeeting {
			return "", "", errors.New("push a meeting with start (and end): RFC 3339 with a zone")
		}
		return "", "", errors.New("push needs due (YYYY-MM-DD) or start")
	}
	if o.Kind == model.KindMeeting && in.Due != "" {
		return "", "", errors.New("a meeting moves by start and end, not due")
	}
	old := whenWords(o)
	if rule, ok := recurRule(o.Recur); ok && o.Schedule == model.ScheduleSeries {
		if next, err = nextObligation(ctx, d, doc, o, rule); err != nil {
			return "", "", err
		}
	}
	if o.Kind == model.KindMeeting {
		if _, err := meetingSchedule(ctx, d, MeetingScheduleInput{ID: doc.ID, Start: in.Start, End: in.End}); err != nil {
			return "", "", err
		}
	} else {
		if in.Due != "" {
			if o.Due, err = parseDue(in.Due); err != nil {
				return "", "", err
			}
		}
		if in.Start != "" || in.End != "" {
			if o.Scheduled, o.End, err = checkKindSlot(in.Start, in.End); err != nil {
				return "", "", err
			}
		}
		if _, err := d.Store.Update(ctx, doc); err != nil {
			return "", "", err
		}
	}
	_, now, err := getObligation(ctx, d, doc.ID)
	if err != nil {
		return "", "", err
	}
	return "pushed from " + old + " to " + whenWords(now), next, nil
}

// redirectTarget reads a redirect's to: a URL or an item that holds the
// obligation from now on (rerouted, as text for the reason), or who owes it.
func redirectTarget(ctx context.Context, d Deps, to string) (rerouted string, owedBy []model.Link, err error) {
	if isWebURL(to) {
		return to, nil, nil
	}
	if model.IsAgentOwer(model.Link(to)) {
		ls, err := owers(ctx, d, []string{to})
		return "", ls, err
	}
	doc, err := d.Store.Get(ctx, normID(to))
	if err != nil {
		return "", nil, linkErr("to", normID(to), err)
	}
	switch doc.Meta.Header().Type {
	case model.TypePerson, model.TypeGroup:
		return "", []model.Link{model.LinkTo(doc.ID)}, nil
	}
	return string(model.LinkTo(doc.ID)), nil, nil
}

// whenWords says when an obligation falls: "Nov 10 10:00" for a timed one,
// "Nov 12" for a date, "no date".
func whenWords(o *model.Obligation) string {
	if t, err := time.Parse(time.RFC3339, o.Scheduled); err == nil && o.Kind == model.KindMeeting {
		return t.Format("Jan 2 15:04")
	}
	if day := obligationDate(o); day != "" {
		return ShortDate(day)
	}
	return "no date"
}

// writeNotices writes one outgoing draft per medium to the interested parties
// of a change, and for a meeting with calendar events a calendar draft. It
// returns the drafts, the names told, the calendar draft, and warnings.
func writeNotices(ctx context.Context, d Deps, doc model.Document, o *model.Obligation, action, oldWhen, rerouted string, newOwner []model.Link,
	reason string, because model.Document, today string) (ids, told []string, cal string, warns []string, err error) {
	ids, told = []string{}, []string{}
	parties := partiesOf(o)
	for _, l := range newOwner {
		if t := l.Target(); !model.IsAgentOwer(l) && !slices.ContainsFunc(parties, func(p party) bool { return p.ID == t }) {
			parties = append(parties, party{ID: t})
		}
	}
	if o.ReplyTo != "" && len(o.OwedTo) == 0 {
		if rd, err := d.Store.Get(ctx, o.ReplyTo.Target()); err == nil {
			if m, ok := rd.Meta.(*model.IO); ok && strings.TrimSpace(m.From) != "" && !strings.HasPrefix(m.From, "[[") {
				parties = append(parties, party{Text: m.From})
			}
		}
	}
	byMedium := map[model.Medium][]string{}
	var mediums []model.Medium
	for _, p := range parties {
		v, err := partyOf(ctx, d.Store, p)
		if err != nil {
			return nil, nil, "", nil, err
		}
		if _, ok := byMedium[v.Medium]; !ok {
			mediums = append(mediums, v.Medium)
		}
		to := p.Text
		if p.ID != "" {
			to = p.ID
		}
		byMedium[v.Medium] = append(byMedium[v.Medium], to)
		told = append(told, v.Name)
	}
	slices.Sort(mediums)
	subject, text := noticeText(ctx, d.Store, doc, o, action, oldWhen, rerouted, newOwner, reason, because)
	thread := ""
	if n := len(o.Notices); n > 0 {
		thread = o.Notices[n-1].Target()
	}
	if len(parties) == 0 {
		warns = append(warns, "nobody to tell: "+doc.ID+" names no owed_to, with, attendees, or interested")
	}
	for _, m := range mediums {
		r, err := IOAdd.run(ctx, d, IOAddInput{Direction: model.IOOut, Medium: m, Subject: subject, Text: text, To: byMedium[m], Expects: model.ExpectsNone, Thread: thread})
		if err != nil {
			return nil, nil, "", nil, fmt.Errorf("notice by %s: %w", m, err)
		}
		ids = append(ids, r.ID)
		warns = append(warns, r.Warnings...)
	}
	if o.Kind == model.KindMeeting && len(o.UIDs()) > 0 && action != ChangeRedirect {
		where, err := calendarSource(ctx, d.Store)
		if err != nil {
			return nil, nil, "", nil, err
		}
		var uids []string
		for _, e := range o.UIDs() {
			uids = append(uids, e.UID)
		}
		ct := fmt.Sprintf("Cancel the calendar event of %s (%s): %s.", firstOf(doc.Title, doc.ID), oldWhen, strings.Join(uids, ", "))
		if action == ChangePush {
			ct = fmt.Sprintf("Move the calendar event of %s from %s to %s (start %s, end %s): %s.", firstOf(doc.Title, doc.ID), oldWhen, whenWords(o), o.Scheduled, firstOf(o.End, "as before"), strings.Join(uids, ", "))
		}
		ct += "\n\nApply it in the calendar after the owner approves, then mark this sent."
		r, err := IOAdd.run(ctx, d, IOAddInput{Direction: model.IOOut, Medium: model.MediumMeeting, Subject: "Calendar: " + subject, Text: ct, To: []string{where}, Expects: model.ExpectsNone})
		if err != nil {
			return nil, nil, "", nil, fmt.Errorf("calendar draft: %w", err)
		}
		cal = r.ID
	}
	return ids, told, cal, warns, nil
}

// partyOf reads a party's name and medium from the store.
func partyOf(ctx context.Context, s store.Store, p party) (ObligationParty, error) {
	docs := map[string]model.Document{}
	if p.ID != "" {
		pd, err := s.Get(ctx, p.ID)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return ObligationParty{}, err
		}
		if err == nil {
			docs[p.ID] = pd
		}
	}
	return partyView(docs, p), nil
}

// calendarSource names where a calendar draft lands: the first active source
// of class calendar, else the word calendar.
func calendarSource(ctx context.Context, s store.Store) (string, error) {
	refs, err := s.List(ctx, store.Filter{Type: model.TypeSource})
	if err != nil {
		return "", err
	}
	for _, r := range refs {
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return "", err
		}
		if src, ok := doc.Meta.(*model.SourceDoc); ok && src.System == model.SourceCalendar {
			return "calendar: " + firstOf(doc.Title, doc.ID) + " (" + doc.ID + ")", nil
		}
	}
	return "calendar", nil
}

// noticeText says a change in plain words, in the owner's voice.
func noticeText(ctx context.Context, s store.Store, doc model.Document, o *model.Obligation, action, oldWhen, rerouted string, newOwner []model.Link, reason string, because model.Document) (subject, text string) {
	title := firstOf(doc.Title, doc.ID)
	var lead string
	switch action {
	case ChangePush:
		subject = "New time: " + title
		lead = fmt.Sprintf("I need to move %s from %s to %s.", title, oldWhen, whenWords(o))
	case ChangeRedirect:
		subject = "Change of hands: " + title
		if rerouted != "" {
			where := rerouted
			if t := model.Link(rerouted).Target(); t != rerouted {
				if rd, err := s.Get(ctx, t); err == nil {
					where = firstOf(rd.Title, t)
				}
			}
			lead = fmt.Sprintf("%s is handled in %s from now on.", title, where)
		} else {
			var names []string
			for _, l := range newOwner {
				name := l.Target()
				if model.IsAgentOwer(l) {
					name = "an assistant (" + string(l) + ")"
				} else if pd, err := s.Get(ctx, name); err == nil {
					name = firstOf(pd.Title, name)
				}
				names = append(names, name)
			}
			lead = fmt.Sprintf("%s will take %s from here, not me.", strings.Join(names, " and "), title)
		}
	case ChangeCancel:
		subject = "Cancelled: " + title
		lead = fmt.Sprintf("I have to cancel %s (%s).", title, oldWhen)
	}
	var why []string
	if reason != "" {
		why = append(why, "Why: "+strings.TrimSuffix(reason, ".")+".")
	}
	if because.ID != "" {
		why = append(why, "Something with a higher priority needs the time: "+firstOf(because.Title, because.ID)+".")
	}
	return subject, strings.Join(append([]string{lead}, why...), "\n\n")
}
