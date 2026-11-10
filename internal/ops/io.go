package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/paramd"
	"github.com/iodesystems/para/internal/store"
)

// An io item is one message not dealt with yet, in either direction. Incoming:
// unread, then read, then filed into a task, question, Ask, note, or
// interaction (became links it), or dropped. Outgoing: a draft, then ready,
// then sent, when it becomes an interaction (expects none) or an Ask (expects
// response), or dropped. Para sends nothing: an agent sends with its own tools
// and reports where the message is. The item stays in io/ with its state: the
// inbox and outbox views show only open items.

const ioTitleMax = 80

// IOAddInput is the input of io_add.
type IOAddInput struct {
	Direction model.IODirection `json:"direction"` // in: it came to the owner; out: a draft to send
	Text      string            `json:"text"`      // the message; with no subject its first line is the title
	Medium    model.Medium      `json:"medium,omitempty"`
	Subject   string            `json:"subject,omitempty"`
	From      string            `json:"from,omitempty"` // person or group id, or free text such as an address
	To        []string          `json:"to,omitempty"`   // person or group ids, or free text; out needs one person or group
	At        string            `json:"at,omitempty"`   // RFC 3339; in: default now; out: set on send
	Ref       string            `json:"ref,omitempty"`  // public link a person can open
	Via       *model.IOVia      `json:"via,omitempty"`  // private connector reference: {source, ref, thread}; thread is the tool's conversation id
	// Verify tells the owner how to find a message with via and no ref.
	Verify string `json:"verify,omitempty"`
	// Tags are topics, such as socials.
	Tags []string `json:"tags,omitempty"`
	// Expects: out: response (sending makes an Ask) or none. In: response when
	// the sender waits for the owner's reply.
	Expects model.IOExpects `json:"expects,omitempty"`
	Route   string          `json:"route,omitempty"`  // out with expects response: the domain the Ask goes to
	Thread  string          `json:"thread,omitempty"` // id of the io item, interaction, Ask, or question this one answers or acts on
	// Effort is about how long it takes the owner: xs, s, m, or l.
	Effort model.Effort `json:"effort,omitempty"`
	// ImportedFrom is the file the new document comes from (import_verify).
	ImportedFrom *model.ImportedFrom `json:"imported_from,omitempty"`
	// Voice of an outgoing draft: as_me (default: the owner approves it and
	// it goes out as theirs) or for_me (an agent sends it for the owner,
	// stamped, when a delegation rule allows it). Kind is a free label the
	// rules match (scheduling, acknowledgement, status). Agent: for_me from
	// a person, the agent that sends it; on an agent surface, you.
	Voice model.IOVoice `json:"voice,omitempty"`
	Kind  string        `json:"kind,omitempty"`
	Agent string        `json:"agent,omitempty"`
}

// IOAddOutput is the output of io_add.
type IOAddOutput struct {
	ID       string        `json:"id"`
	Folder   model.Folder  `json:"folder"`
	State    model.IOState `json:"state"`
	Rev      string        `json:"rev"`
	Warnings []string      `json:"warnings"`
	// Triage is what the owner's triage rules did to an incoming message.
	Triage []TriageAct `json:"triage,omitempty"`
	// Reply is the reply obligation made for an incoming message that
	// expects a reply (ioreply.go).
	Reply string `json:"reply,omitempty"`
}

// IOAdd captures an incoming message or drafts an outgoing one.
var IOAdd = defineSafe("io_add", "Capture an incoming message (direction in) or draft one to send (direction out). In takes expects response when the sender waits for the owner's reply: set it honestly, since an incoming message needs the owner only when the sender waits for a reply, the sender (or a group they are in) or the source is priority high, or a triage rule says so; the rest goes to the day's inbox digest. The owner's triage rules apply at once (output triage). Out names a person or group in to, and expects: response (sending makes an Ask; give route) or none (sending logs an interaction); an outward action (a PR, a ticket, a deploy, a document edit) is a draft too: to names where it lands (a repo, a URL), expects none, the text says what and how. Effort is xs, s, m, or l. An incoming message with expects response gets a reply obligation (output reply), due io.reply_days after at; the reply closes it. Give via.thread, your tool's id of the conversation, so the pre-send check (io_send_check) sees replies in it. Voice: as_me (default) goes out as the owner's after their approval; for_me is one you send for the owner without their review, only when a rule of the owner's .para/delegation.yaml allows it: para appends the stamp that says so (output warnings say whether a rule allows it), and kind is a label the rules match. Never present a for_me message as the owner's own words. Para sends nothing", ioAdd)

func ioAdd(ctx context.Context, d Deps, in IOAddInput) (IOAddOutput, error) {
	if !slices.Contains(model.IODirections, in.Direction) {
		return IOAddOutput{}, fmt.Errorf("direction %q: want in or out", in.Direction)
	}
	text := cleanText(in.Text)
	if text == "" {
		return IOAddOutput{}, errors.New("text is empty")
	}
	m := &model.IO{Common: model.Common{Type: model.TypeIO, Tags: normTags(in.Tags)}, Direction: in.Direction, Medium: in.Medium,
		Subject: ioLine(in.Subject), Expects: in.Expects, Route: ioLine(in.Route), Via: in.Via, Verify: ioLine(in.Verify), Effort: in.Effort}
	if err := checkEffort(in.Effort); err != nil {
		return IOAddOutput{}, err
	}
	if err := checkVoice(in.Voice); err != nil {
		return IOAddOutput{}, err
	}
	m.Kind = normTag(in.Kind)
	if m.Medium == "" {
		m.Medium = model.MediumOther
	}
	if err := checkMedium(m.Medium); err != nil {
		return IOAddOutput{}, err
	}
	var err error
	if m.From, err = ioParty(ctx, d, "from", in.From); err != nil {
		return IOAddOutput{}, err
	}
	if m.To, err = ioParties(ctx, d, in.To); err != nil {
		return IOAddOutput{}, err
	}
	if m.At, err = ioTime(in.At); err != nil {
		return IOAddOutput{}, err
	}
	if m.Ref, err = ioRef(in.Ref); err != nil {
		return IOAddOutput{}, err
	}
	if v := m.Via; v != nil {
		v.Source, v.Ref = ioLine(v.Source), strings.TrimSpace(v.Ref)
		if v.Source == "" || v.Ref == "" {
			return IOAddOutput{}, errors.New("via: give source and ref, or leave via out")
		}
	}
	if m.Thread, err = linkTo(ctx, d, "thread", replyThread(ctx, d, in.Thread), model.TypeIO, model.TypeInteraction, model.TypeRFX, model.TypeQuestion); err != nil {
		return IOAddOutput{}, err
	}
	if err := checkVerifiable(m); err != nil {
		return IOAddOutput{}, err
	}
	switch in.Direction {
	case model.IOIn:
		m.State = model.IOUnread
		if m.Route != "" {
			return IOAddOutput{}, errors.New("route is for an outgoing message")
		}
		if m.Expects != "" && !slices.Contains(model.IOExpectsAll, m.Expects) {
			return IOAddOutput{}, fmt.Errorf("expects %q: want response (the sender waits for a reply) or none", m.Expects)
		}
		if m.At == "" {
			m.At = d.Store.Now().UTC().Format(time.RFC3339)
		}
	case model.IOOut:
		m.State = model.IODraft
		if err := checkOut(m); err != nil {
			return IOAddOutput{}, err
		}
	}
	nd := model.Document{Folder: model.FolderIO, Title: ioTitle(m.Subject, text), Body: text, Meta: m}
	var notes []string
	if in.Voice == model.VoiceForMe {
		agent, err := draftAgent(d, in.Agent)
		if err != nil {
			return IOAddOutput{}, err
		}
		m.Created = model.Date(d.Store.Now().Format(dateLayout)) // the stamp's {date}
		if notes, err = forMe(ctx, d, &nd, m, agent); err != nil {
			return IOAddOutput{}, err
		}
	} else if in.Agent != "" {
		return IOAddOutput{}, errors.New("agent is for voice for_me")
	}
	doc, err := d.Store.Create(ctx, nd)
	if err != nil {
		return IOAddOutput{}, err
	}
	out := IOAddOutput{ID: doc.ID, Folder: doc.Folder, State: m.State, Rev: doc.Rev, Warnings: append([]string{}, notes...)}
	if in.Direction == model.IOIn {
		var warn []string
		if out.Triage, warn = triageNew(ctx, d, doc.ID); len(out.Triage) > 0 {
			if cur, err := d.Store.Get(ctx, doc.ID); err == nil {
				out.Rev, out.State = cur.Rev, cur.Meta.(*model.IO).State
			}
		}
		out.Warnings = append(out.Warnings, warn...)
		if replyWanted(m) {
			out.Warnings = append(out.Warnings, syncReplyWarn(ctx, d, doc.ID)...)
			if cur, err := d.Store.Get(ctx, doc.ID); err == nil {
				out.Rev, out.Reply = cur.Rev, cur.Meta.(*model.IO).Reply.Target()
			}
		}
	}
	if m.Expects == model.ExpectsResponse && in.Direction == model.IOOut {
		if domains, err := refsOf(ctx, d.Store, store.Filter{Type: model.TypeDomain}); err == nil {
			if picked, _ := pickDomain(m.Route, domains); picked == nil {
				out.Warnings = append(out.Warnings, fmt.Sprintf("route %q matches no domain: kept as text", m.Route))
			}
		}
	}
	return out, nil
}

// checkOut checks what an outgoing message needs to be sent and recorded.
func checkOut(m *model.IO) error {
	if !slices.Contains(model.IOExpectsAll, m.Expects) {
		return fmt.Errorf("expects %q: want response (sending makes an Ask) or none (sending logs an interaction)", m.Expects)
	}
	if m.Expects == model.ExpectsResponse && m.Route == "" {
		return errors.New("expects response needs route: the domain the Ask goes to")
	}
	if m.Expects == model.ExpectsNone && m.Route != "" {
		return errors.New("route is for expects response")
	}
	if len(ioLinks(m.To)) > 0 {
		return nil
	}
	// An action (a PR, a ticket, a deploy, a document edit) goes to no
	// person: to names where it lands, and a send logs no interaction.
	if m.Expects == model.ExpectsNone && len(m.To) > 0 {
		return nil
	}
	return errors.New("to needs a person or group id: the interaction or Ask names them (org_person_add first); an action with expects none may name where it lands instead (a repo, a ticket, a URL)")
}

func cleanText(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
}

func ioLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// ioTitle is the subject, or else the first line of the text, cut to fit.
func ioTitle(subject, text string) string {
	t := ioLine(subject)
	if t == "" {
		first, _, _ := strings.Cut(text, "\n")
		t = ioLine(first)
	}
	if utf8.RuneCountInString(t) > ioTitleMax {
		t = string([]rune(t)[:ioTitleMax]) + "..."
	}
	return t
}

// ioText is the message as a reader needs it: the subject, when the body does
// not start with it, then the body.
func ioText(doc model.Document, m *model.IO) string {
	if m.Subject != "" && !strings.HasPrefix(doc.Body, m.Subject) {
		return joinBody(m.Subject, doc.Body)
	}
	return firstOf(doc.Body, doc.Title)
}

// ioParty turns an id of a person or group into a link and keeps other text as
// it is. A "[[x]]" link must name a person or group.
func ioParty(ctx context.Context, d Deps, field, raw string) (string, error) {
	raw = ioLine(raw)
	if raw == "" {
		return "", nil
	}
	id := normID(raw)
	linked := strings.HasPrefix(raw, "[[")
	if !linked && !paramd.ValidStem(id) {
		return raw, nil
	}
	doc, err := d.Store.Get(ctx, id)
	switch {
	case errors.Is(err, model.ErrNotFound) && !linked:
		return raw, nil
	case err != nil:
		return "", fmt.Errorf("%s %s: %w", field, id, err)
	}
	if t := doc.Meta.Header().Type; t != model.TypePerson && t != model.TypeGroup {
		return "", fmt.Errorf("%s %s is a %s: want a person, a group, or free text", field, id, t)
	}
	return string(model.LinkTo(doc.ID)), nil
}

func ioParties(ctx context.Context, d Deps, raw []string) ([]string, error) {
	var out []string
	for i, r := range raw {
		p, err := ioParty(ctx, d, fmt.Sprintf("to[%d]", i), r)
		if err != nil {
			return nil, err
		}
		if p != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out, nil
}

// ioLinks are the person and group ids among parties.
func ioLinks(parties []string) []string {
	var out []string
	for _, p := range parties {
		if strings.HasPrefix(p, "[[") {
			out = append(out, model.Link(p).Target())
		}
	}
	return out
}

func ioTime(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if _, err := time.Parse(time.RFC3339Nano, s); err != nil {
		return "", fmt.Errorf("at %q: want an RFC 3339 time such as 2026-11-04T09:00:00Z", s)
	}
	return s, nil
}

func ioRef(s string) (string, error) {
	if strings.TrimSpace(s) == "" {
		return "", nil
	}
	u, err := checkURL(s)
	if err != nil {
		return "", fmt.Errorf("ref: %w", err)
	}
	return u, nil
}

// IOItem is one io item in a list.
type IOItem struct {
	// NeedsYou is ops.NeedsYou for the row: open, not put off, not an
	// agent's turn, and the owner's turn. NeedsYouReason says why, in words.
	NeedsYou       bool              `json:"needs_you"`
	NeedsYouReason string            `json:"needs_you_reason,omitempty"`
	ID             string            `json:"id"`
	Title          string            `json:"title"`
	Direction      model.IODirection `json:"direction"`
	Medium         model.Medium      `json:"medium"`
	State          model.IOState     `json:"state"`
	From           string            `json:"from,omitempty"`
	To             []string          `json:"to"`
	At             string            `json:"at,omitempty"`
	Subject        string            `json:"subject,omitempty"`
	Ref            string            `json:"ref,omitempty"`
	Verify         string            `json:"verify,omitempty"` // how the owner finds a message with via and no ref
	// Unverifiable: an incoming message of a medium with permalinks that has
	// no ref and no via with verify; the owner cannot check it.
	Unverifiable bool `json:"unverifiable,omitempty"`
	// DeferUntil: the item is put off until this date, or someday.
	DeferUntil string          `json:"defer_until,omitempty"`
	Expects    model.IOExpects `json:"expects,omitempty"`
	Route      string          `json:"route,omitempty"`
	Became     string          `json:"became,omitempty"` // id
	Thread     string          `json:"thread,omitempty"` // id
	Reason     string          `json:"reason,omitempty"`
	Created    model.Date      `json:"created"`
	// Legacy marks a type inbox capture that upgrade step io-folder has not
	// moved yet. It reads as unread incoming; file it after the upgrade.
	Legacy bool         `json:"legacy,omitempty"`
	Effort model.Effort `json:"effort,omitempty"`
	// Digest: an open incoming message that is not the owner's turn (no
	// reply expected, sender and source not high, or a triage rule): it is
	// listed in the day's inbox digest. Set by io_list.
	Digest bool `json:"digest,omitempty"`
	// Triage and TriageBy are what a triage rule decided and which rule;
	// Links are the items a rule linked the message to (ids).
	Triage   model.IOTriage `json:"triage,omitempty"`
	TriageBy string         `json:"triage_by,omitempty"`
	Links    []string       `json:"links"`
	// Voice of an outgoing item: as_me or for_me. A for_me item names its
	// Agent and Stamp; AllowedBy is the delegation rule that lets the agent
	// send it, stamped, without the owner's review (or that did). Kind is
	// its label for the rules.
	Voice     model.IOVoice `json:"voice,omitempty"`
	Agent     string        `json:"agent,omitempty"`
	Stamp     string        `json:"stamp,omitempty"`
	Kind      string        `json:"kind,omitempty"`
	AllowedBy string        `json:"allowed_by,omitempty"`
	// Proposal fields of an outgoing draft.
	// NotStated lists the known names (confidence, recommend, risk) the
	// draft gives no assessment for: views show them as "not stated".
	Why         string               `json:"why,omitempty"`
	Impact      string               `json:"impact,omitempty"`
	Assessments []model.IOAssessment `json:"assessments,omitempty"`
	NotStated   []string             `json:"not_stated,omitempty"`
	Group       string               `json:"group,omitempty"`
	VariantOf   string               `json:"variant_of,omitempty"`
	Revision    int                  `json:"revision,omitempty"`
	Revisions   []model.IORevision   `json:"revisions,omitempty"`
	Attempts    []model.IOAttempt    `json:"attempts,omitempty"`
	Reviewed    string               `json:"reviewed,omitempty"`
	// Made is the number of documents agent writes changed while ingesting
	// this item (io_made lists them). Set by io_list.
	Made int `json:"made,omitempty"`
}

func ioItem(doc model.Document, m *model.IO) IOItem {
	it := ioItemBase(doc, m)
	it.Why, it.Impact, it.Group, it.VariantOf, it.Revision, it.Reviewed = m.Why, m.Impact, m.Group, m.VariantOf, m.Revision, m.Reviewed
	it.Assessments = append([]model.IOAssessment{}, m.Assessments...)
	it.Revisions, it.Attempts = m.Revisions, m.Attempts
	if m.Direction == model.IOOut {
		for _, n := range model.AssessmentNames {
			if !slices.ContainsFunc(m.Assessments, func(a model.IOAssessment) bool { return a.Name == n }) {
				it.NotStated = append(it.NotStated, n)
			}
		}
	}
	return it
}

func ioItemBase(doc model.Document, m *model.IO) IOItem {
	return IOItem{ID: doc.ID, Title: doc.Title, Direction: m.Direction, Medium: m.Medium, State: m.State, From: m.From, Effort: m.Effort,
		Triage: m.Triage, TriageBy: m.TriageBy, Links: targets(m.Links),
		To: append([]string{}, m.To...), At: m.At, Subject: m.Subject, Ref: m.Ref, Expects: m.Expects, Route: m.Route,
		Became: m.Became.Target(), Thread: m.Thread.Target(), Reason: m.Reason, Created: m.Created,
		Verify: m.Verify, DeferUntil: m.DeferUntil, Unverifiable: m.Direction == model.IOIn && slices.Contains(model.PermalinkMediums, m.Medium) && !m.Verifiable(),
		Voice: ioVoice(m), Agent: m.Agent, Stamp: m.Stamp, Kind: m.Kind, AllowedBy: m.AllowedBy}
}

// ioVoice is the voice of an outgoing item, as_me when not set; "" for
// incoming.
func ioVoice(m *model.IO) model.IOVoice {
	switch {
	case m.Direction != model.IOOut:
		return ""
	case m.Voice == "":
		return model.VoiceAsMe
	}
	return m.Voice
}

// IOCounts counts open items: the inbox and the outbox.
type IOCounts struct {
	Unread int `json:"unread"`
	Read   int `json:"read"`
	Draft  int `json:"draft"`
	Ready  int `json:"ready"`
}

// IOListInput is the input of io_list. Empty fields match all; with no state
// and no all, only open items (unread, read, draft, ready) are listed.
type IOListInput struct {
	Direction model.IODirection `json:"direction,omitempty" query:"direction"`
	State     model.IOState     `json:"state,omitempty" query:"state"`
	Medium    model.Medium      `json:"medium,omitempty" query:"medium"`
	Expects   model.IOExpects   `json:"expects,omitempty" query:"expects"`
	All       bool              `json:"all,omitempty" query:"all"` // include filed, sent, and dropped items
	// Digest keeps only open incoming messages that are not the owner's
	// turn: what goes to the day's inbox digest.
	Digest bool `json:"digest,omitempty" query:"digest"`
	// Voice keeps outgoing items of that voice: for_me with all lists what
	// agents sent for the owner, stamped, without review.
	Voice model.IOVoice `json:"voice,omitempty" query:"voice"`
}

// IOListOutput is the output of io_list. Counts covers every open item,
// whatever the filters.
type IOListOutput struct {
	Items  []IOItem `json:"items"`
	Counts IOCounts `json:"counts"`
}

// IOList lists io items, oldest first.
var IOList = defineRead("io_list", "List io items, oldest first: the inbox (direction in: unread, read) and the outbox (direction out: draft, ready). all adds filed, sent, and dropped items. digest keeps open incoming messages that do not need the owner (each row has needs_you and digest). voice for_me with all lists what agents sent for the owner, stamped (allowed_by names the rule)", func(ctx context.Context, d Deps, in IOListInput) (IOListOutput, error) {
	if in.Direction != "" && !slices.Contains(model.IODirections, in.Direction) {
		return IOListOutput{}, fmt.Errorf("direction %q: want in or out", in.Direction)
	}
	if in.State != "" && !slices.Contains(model.IOStates, in.State) {
		return IOListOutput{}, fmt.Errorf("state %q: want one of %v", in.State, model.IOStates)
	}
	if in.Expects != "" && !slices.Contains(model.IOExpectsAll, in.Expects) {
		return IOListOutput{}, fmt.Errorf("expects %q: want response or none", in.Expects)
	}
	if err := checkVoice(in.Voice); err != nil {
		return IOListOutput{}, err
	}
	all, err := loadIO(ctx, d)
	if err != nil {
		return IOListOutput{}, err
	}
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		return IOListOutput{}, err
	}
	byInput, err := receiptsByInput(ctx, d.Store)
	if err != nil {
		return IOListOutput{}, err
	}
	out := IOListOutput{Items: []IOItem{}}
	for _, it := range all {
		it.Made = madeCount(byInput[it.ID])
		need := sn.Need(it.ID)
		it.NeedsYou, it.NeedsYouReason = need.Yes, need.Reason
		it.Digest = it.Direction == model.IOIn && need.Life == LifeOpen && need.Turn == TurnNone
		switch it.State {
		case model.IOUnread:
			out.Counts.Unread++
		case model.IORead:
			out.Counts.Read++
		case model.IODraft:
			out.Counts.Draft++
		case model.IOReady:
			out.Counts.Ready++
		}
		switch {
		case in.Direction != "" && it.Direction != in.Direction,
			in.State != "" && it.State != in.State,
			in.State == "" && !in.All && !it.State.Open(),
			in.Medium != "" && it.Medium != in.Medium,
			in.Expects != "" && it.Expects != in.Expects,
			in.Voice != "" && it.Voice != in.Voice,
			in.Digest && !it.Digest:
			continue
		}
		out.Items = append(out.Items, it)
	}
	return out, nil
})

// loadIO reads every io item and every capture still in inbox/, oldest first.
func loadIO(ctx context.Context, d Deps) ([]IOItem, error) {
	var out []IOItem
	for _, t := range []model.Type{model.TypeIO, model.TypeInbox} {
		refs, err := d.Store.List(ctx, store.Filter{Type: t})
		if err != nil {
			return nil, err
		}
		for _, r := range refs {
			if t == model.TypeInbox && r.Folder != model.FolderInbox {
				continue // triaged before io: history
			}
			doc, err := d.Store.Get(ctx, r.ID)
			if err != nil {
				return nil, err
			}
			switch m := doc.Meta.(type) {
			case *model.IO:
				out = append(out, ioItem(doc, m))
			case *model.Inbox:
				out = append(out, IOItem{ID: doc.ID, Title: doc.Title, Direction: model.IOIn, Medium: model.MediumOther,
					State: model.IOUnread, To: []string{}, Links: []string{}, Created: m.Created, Legacy: true})
			}
		}
	}
	key := func(it IOItem) string { return firstOf(it.At, string(it.Created)) }
	slices.SortStableFunc(out, func(a, b IOItem) int {
		if c := strings.Compare(key(a), key(b)); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

// IOShowInput is the input of io_show.
type IOShowInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// IOShowOutput is the output of io_show.
type IOShowOutput struct {
	Doc  model.Document `json:"doc"`
	Item IOItem         `json:"item"`
}

// IOShow shows one io item.
var IOShow = defineRead("io_show", "Show one io item: the message and its state", func(ctx context.Context, d Deps, in IOShowInput) (IOShowOutput, error) {
	id := normID(in.ID)
	if id == "" {
		return IOShowOutput{}, errors.New("id is empty")
	}
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return IOShowOutput{}, err
	}
	switch m := doc.Meta.(type) {
	case *model.IO:
		return IOShowOutput{Doc: doc, Item: ioItem(doc, m)}, nil
	case *model.Inbox:
		return IOShowOutput{Doc: doc, Item: IOItem{ID: doc.ID, Title: doc.Title, Direction: model.IOIn, Medium: model.MediumOther,
			State: model.IOUnread, To: []string{}, Links: []string{}, Created: m.Created, Legacy: doc.Folder == model.FolderInbox}}, nil
	}
	return IOShowOutput{}, fmt.Errorf("%s is %s, not io", id, doc.Meta.Header().Type)
})

// getIO reads an io item. A capture still in inbox/ is refused with the fix.
func getIO(ctx context.Context, d Deps, id string) (model.Document, *model.IO, error) {
	id = normID(id)
	if id == "" {
		return model.Document{}, nil, errors.New("id is empty")
	}
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return doc, nil, err
	}
	switch m := doc.Meta.(type) {
	case *model.IO:
		return doc, m, nil
	case *model.Inbox:
		return doc, nil, fmt.Errorf("%s is an inbox capture from before io: run `para vault upgrade` (step io-folder), then file it", id)
	}
	return doc, nil, fmt.Errorf("%s is %s, not io", id, doc.Meta.Header().Type)
}

// getOpenIO reads an io item that is not dealt with yet.
func getOpenIO(ctx context.Context, d Deps, id string) (model.Document, *model.IO, error) {
	doc, m, err := getIO(ctx, d, id)
	if err != nil {
		return doc, m, err
	}
	if !m.State.Open() {
		return doc, m, fmt.Errorf("%s is already %s", doc.ID, m.State)
	}
	return doc, m, nil
}

// IOEditInput is the input of io_edit. Empty fields stay as they are. An
// incoming message takes only state (read or unread). An outgoing draft takes
// the rest; changing its text, subject, or recipients makes a ready message a
// draft again, unless state ready is given in the same call.
type IOEditInput struct {
	ID      string          `json:"id"`
	State   model.IOState   `json:"state,omitempty"` // in: read or unread; out: draft or ready
	Text    string          `json:"text,omitempty"`
	Subject string          `json:"subject,omitempty"`
	To      []string        `json:"to,omitempty"`
	Medium  model.Medium    `json:"medium,omitempty"`
	Expects model.IOExpects `json:"expects,omitempty"` // in: response when the sender waits for a reply
	Route   string          `json:"route,omitempty"`
	Effort  model.Effort    `json:"effort,omitempty"` // xs, s, m, or l; either direction
	// Ref, Via, and Verify say where the owner finds the message: an incoming
	// one takes them too.
	Ref    string       `json:"ref,omitempty"`
	Via    *model.IOVia `json:"via,omitempty"`
	Verify string       `json:"verify,omitempty"`
	// Voice as_me takes the stamp off (the owner then approves it); for_me
	// stamps it again for the delegation rule that matches now. Kind is the
	// label rules match; Agent as on io_add.
	Voice model.IOVoice `json:"voice,omitempty"`
	Kind  string        `json:"kind,omitempty"`
	Agent string        `json:"agent,omitempty"`
}

// IOEditOutput is the output of io_edit.
type IOEditOutput struct {
	ID       string        `json:"id"`
	State    model.IOState `json:"state"`
	Rev      string        `json:"rev"`
	Warnings []string      `json:"warnings,omitempty"`
}

// IOEdit changes an outgoing draft, or marks an incoming message read or unread.
var IOEdit = defineSafe("io_edit", "Change an outgoing draft (text, subject, to, medium, expects, route, state draft), or mark an incoming message read or unread or expects response (the sender waits for a reply). Either direction takes ref (the message permalink), or via {source, ref} with verify (how the owner finds it), so the owner can check it. Effort (xs, s, m, l) is set in either direction. State ready is the owner's approval: io_approve. An edit of the text, subject, to, or medium of a ready message makes it a draft again. Voice as_me or for_me changes whose words a draft goes out as (io_add); the text of a for_me draft must keep its stamp at the end, and para stamps it again when the matching rule changes", func(ctx context.Context, d Deps, in IOEditInput) (IOEditOutput, error) {
	doc, m, err := getOpenIO(ctx, d, in.ID)
	if err != nil {
		return IOEditOutput{}, err
	}
	var prior []byte // the draft as it is now: kept as a revision if the edit changes it
	if m.Direction == model.IOOut {
		if prior, err = model.Render(doc); err != nil {
			return IOEditOutput{}, err
		}
	}
	if in.State != "" && (!slices.Contains(model.IOStatesOf(m.Direction), in.State) || !in.State.Open()) {
		return IOEditOutput{}, fmt.Errorf("state %q: an %s message takes %s here; file, send, or drop it with its own tool", in.State, m.Direction, openStates(m.Direction))
	}
	if in.State == model.IOReady && agentSurface(d) {
		return IOEditOutput{}, errors.New("state ready is the owner's approval: call io_approve with the owner's words (owner_said), or leave it a draft for the owner")
	}
	content := in.Text != "" || in.Subject != "" || in.To != nil || in.Medium != "" || in.Route != "" ||
		in.Voice != "" || in.Kind != "" || in.Agent != ""
	if m.Direction == model.IOIn && content {
		return IOEditOutput{}, errors.New("an incoming message keeps its text and fields: give state, expects, or effort, or file or drop it")
	}
	if err := checkEffort(in.Effort); err != nil {
		return IOEditOutput{}, err
	}
	if in.Effort != "" {
		m.Effort = in.Effort
	}
	if m.Direction == model.IOIn && in.Expects != "" && !slices.Contains(model.IOExpectsAll, in.Expects) {
		return IOEditOutput{}, fmt.Errorf("expects %q: want response or none", in.Expects)
	}
	if err := checkVoice(in.Voice); err != nil {
		return IOEditOutput{}, err
	}
	changed := false
	if t := cleanText(in.Text); t != "" && t != doc.Body {
		if m.ForMe() && in.Voice == "" && !stamped(t, m.Stamp) {
			return IOEditOutput{}, fmt.Errorf("the text of a for_me draft ends with its stamp %q: keep it, or give voice as_me so the owner approves it as their own", m.Stamp)
		}
		doc.Body, changed = t, true
	}
	if k := normTag(in.Kind); in.Kind != "" && k != m.Kind {
		m.Kind, changed = k, true
	}
	if s := ioLine(in.Subject); s != "" && s != m.Subject {
		m.Subject, changed = s, true
	}
	if in.To != nil {
		to, err := ioParties(ctx, d, in.To)
		if err != nil {
			return IOEditOutput{}, err
		}
		if !slices.Equal(to, m.To) {
			m.To, changed = to, true
		}
	}
	if in.Medium != "" {
		if err := checkMedium(in.Medium); err != nil {
			return IOEditOutput{}, err
		}
		changed = changed || in.Medium != m.Medium
		m.Medium = in.Medium
	}
	if in.Expects != "" {
		m.Expects = in.Expects
		if m.Expects == model.ExpectsNone {
			m.Route = ""
		}
	}
	if r := ioLine(in.Route); r != "" {
		m.Route = r
	}
	where := in.Ref != "" || in.Via != nil || in.Verify != ""
	if in.Ref != "" {
		if m.Ref, err = ioRef(in.Ref); err != nil {
			return IOEditOutput{}, err
		}
	}
	if v := in.Via; v != nil {
		v.Source, v.Ref = ioLine(v.Source), strings.TrimSpace(v.Ref)
		if v.Source == "" || v.Ref == "" {
			return IOEditOutput{}, errors.New("via: give source and ref")
		}
		m.Via = v
	}
	if v := ioLine(in.Verify); v != "" {
		m.Verify = v
	}
	if where || in.Medium != "" {
		if err := checkVerifiable(m); err != nil {
			return IOEditOutput{}, err
		}
	}
	if m.Direction == model.IOOut {
		if err := checkOut(m); err != nil {
			return IOEditOutput{}, err
		}
	}
	var notes []string
	switch {
	case in.Voice == model.VoiceAsMe:
		if m.ForMe() {
			asMe(&doc, m)
			changed = true
		}
	case in.Voice == model.VoiceForMe || m.ForMe():
		agent := m.Agent
		if in.Voice == model.VoiceForMe {
			if agent, err = draftAgent(d, firstOf(in.Agent, m.Agent)); err != nil {
				return IOEditOutput{}, err
			}
		}
		before, stamp := doc.Body, m.Stamp
		if notes, err = forMe(ctx, d, &doc, m, agent); err != nil {
			return IOEditOutput{}, err
		}
		changed = changed || doc.Body != before || m.Stamp != stamp
	case in.Agent != "":
		return IOEditOutput{}, errors.New("agent is for voice for_me")
	}
	doc.Title = ioTitle(m.Subject, doc.Body)
	if changed && m.State == model.IOReady {
		m.State, m.ApprovedHash = model.IODraft, ""
	}
	if changed {
		m.CheckedAt = "" // a check covers the text it saw
	}
	switch in.State {
	case model.IOReady:
		// The owner marks it ready: that is their approval of this text.
		if err := markApproved(ctx, d, &doc, m); err != nil {
			return IOEditOutput{}, err
		}
	case model.IODraft:
		m.State, m.ApprovedHash = model.IODraft, ""
	case "":
	default:
		m.State = in.State
	}
	if changed && m.Direction == model.IOOut {
		if err := saveRevision(ctx, d, doc.ID, m, prior); err != nil {
			return IOEditOutput{}, err
		}
	}
	doc, err = d.Store.Update(ctx, doc)
	if err != nil {
		return IOEditOutput{}, err
	}
	out := IOEditOutput{ID: doc.ID, State: m.State, Rev: doc.Rev, Warnings: notes}
	if m.Direction == model.IOIn && in.Expects != "" {
		out.Warnings = syncReplyWarn(ctx, d, doc.ID)
		if cur, err := d.Store.Get(ctx, doc.ID); err == nil {
			out.Rev = cur.Rev
		}
	}
	return out, nil
})

// IOApproveInput is the input of io_approve.
type IOApproveInput struct {
	ID string `json:"id"`
	// OwnerSaid is the owner's words from the chat that approve this draft,
	// verbatim. Required on an agent surface; the receipt is self-reported.
	OwnerSaid string `json:"owner_said,omitempty"`
}

// IOApproveOutput is the output of io_approve.
type IOApproveOutput struct {
	ID           string        `json:"id"`
	State        model.IOState `json:"state"`
	ApprovedHash string        `json:"approved_hash"`
	SelfReported bool          `json:"self_reported"` // an agent relayed the owner's words
	Rev          string        `json:"rev"`
}

// IOApprove is the owner's approval of an outgoing draft: it binds the
// draft's hash and makes it ready to send. An agent may record it only with
// the owner's words from the chat (D3); no other op takes chat approval.
var IOApprove = defineSafe("io_approve", "Approve an outgoing draft for sending: its exact medium, to, subject, and text are approved, and it becomes ready. From an agent, only with owner_said: the owner's words in this chat that approve this draft, verbatim; the receipt is marked self-reported. Never approve without them. An edit of the draft after approval makes it a draft again. A for_me draft becomes as_me: its stamp comes off, and the owner approves the text without it. The approval records the newest incoming message of the draft's conversation: right before you send, read the channel again, io_add what is new, and call io_send_check; send with your own tool only when it passes, then io_send", func(ctx context.Context, d Deps, in IOApproveInput) (IOApproveOutput, error) {
	said := strings.TrimSpace(in.OwnerSaid)
	agent := agentSurface(d)
	if agent && said == "" {
		return IOApproveOutput{}, errors.New("owner_said is empty: give the owner's words from the chat that approve this draft, verbatim; without them the draft waits for the owner")
	}
	if said != "" {
		if err := lintText("owner_said", said); err != nil {
			return IOApproveOutput{}, err
		}
	}
	doc, m, err := getOpenIO(ctx, d, in.ID)
	if err != nil {
		return IOApproveOutput{}, err
	}
	if m.Direction != model.IOOut {
		return IOApproveOutput{}, fmt.Errorf("%s is incoming: only an outgoing draft is approved", doc.ID)
	}
	if err := checkOut(m); err != nil {
		return IOApproveOutput{}, err
	}
	if err := markApproved(ctx, d, &doc, m); err != nil {
		return IOApproveOutput{}, err
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return IOApproveOutput{}, err
	}
	return IOApproveOutput{ID: saved.ID, State: m.State, ApprovedHash: m.ApprovedHash, SelfReported: agent, Rev: saved.Rev}, nil
})

func openStates(dir model.IODirection) string {
	if dir == model.IOOut {
		return "draft or ready"
	}
	return "unread or read"
}

// IODropInput is the input of io_drop.
type IODropInput struct {
	ID     string `json:"id"`
	Reason string `json:"reason"` // why it needs nothing, or will not be sent
}

// IODropOutput is the output of io_drop.
type IODropOutput struct {
	ID       string        `json:"id"`
	State    model.IOState `json:"state"`
	Rev      string        `json:"rev"`
	Warnings []string      `json:"warnings,omitempty"`
}

// IODrop sets an open item aside with a reason. Nothing is deleted.
var IODrop = define("io_drop", "Drop an open io item with a reason: an incoming message that needs nothing, or a draft that will not be sent. Nothing is deleted", func(ctx context.Context, d Deps, in IODropInput) (IODropOutput, error) {
	reason := ioLine(in.Reason)
	if reason == "" {
		return IODropOutput{}, errors.New("reason is empty: say why it needs nothing")
	}
	doc, m, err := getOpenIO(ctx, d, in.ID)
	if err != nil {
		return IODropOutput{}, err
	}
	m.State, m.Reason = model.IODropped, reason
	doc, err = d.Store.Update(ctx, doc)
	if err != nil {
		return IODropOutput{}, err
	}
	return IODropOutput{ID: doc.ID, State: m.State, Rev: doc.Rev, Warnings: syncReplyWarn(ctx, d, doc.ID)}, nil
})

// IOSendInput is the input of io_send.
type IOSendInput struct {
	ID      string `json:"id"`
	URL     string `json:"url,omitempty"`     // where the sent message is
	Locator string `json:"locator,omitempty"` // where it is, when it has no URL
	At      string `json:"at,omitempty"`      // RFC 3339; default now
	// Observed: you record an act already done without this draft's
	// approval: the owner sent it themself and scan_out saw it, or an act the
	// owner allowed without review (a local commit). The pre-send check does
	// not apply; the receipt shows it.
	Observed bool `json:"observed,omitempty"`
}

// IOSendOutput is the output of io_send.
type IOSendOutput struct {
	ID         string        `json:"id"`
	State      model.IOState `json:"state"`
	Became     string        `json:"became"`                // id of the interaction or Ask
	BecameType model.Type    `json:"became_type,omitempty"` // interaction or rfx; empty for an action
	Rev        string        `json:"rev"`
	Warnings   []string      `json:"warnings"`
}

// IOSend records that an outgoing message was sent. Expects none logs an
// interaction; expects response opens an Ask with each person or group as a
// sent target, so follow-ups use the Ask tools.
var IOSend = defineSafe("io_send", "Record that you sent an outgoing draft, with its url or locator. Expects none logs an interaction; expects response opens an Ask (rfx out) with the recipients as sent targets; an action (to no person: a PR, a ticket, a deploy, a document edit) logs nothing else and needs url. From an agent it is refused unless the owner approved the draft (io_approve), or it is for_me and a rule of the owner's delegation.yaml allows it now and its text ends with the stamp, and io_send_check passed after that, within 30 minutes (the item and the receipt then name the rule in allowed_by); observed records a send the owner made themself that scan_out saw. A send threaded to an incoming message closes its reply obligation", func(ctx context.Context, d Deps, in IOSendInput) (IOSendOutput, error) {
	return sendIO(ctx, d, in)
})

// sendIO is io_send. A message already sent returns what it became, so a
// report that comes twice changes nothing.
func sendIO(ctx context.Context, d Deps, in IOSendInput) (IOSendOutput, error) {
	doc, m, err := getIO(ctx, d, in.ID)
	if err != nil {
		return IOSendOutput{}, err
	}
	if m.Direction != model.IOOut {
		return IOSendOutput{}, fmt.Errorf("%s is incoming: file it or drop it", doc.ID)
	}
	out := IOSendOutput{ID: doc.ID, State: m.State, Became: m.Became.Target(), Rev: doc.Rev, Warnings: []string{}}
	if m.State == model.IOSent {
		if b, err := d.Store.Get(ctx, out.Became); err == nil {
			out.BecameType = b.Meta.Header().Type
		}
		return out, nil
	}
	if !m.State.Open() {
		return out, fmt.Errorf("%s is %s", doc.ID, m.State)
	}
	link, locator := strings.TrimSpace(in.URL), ioLine(in.Locator)
	if link == "" && locator == "" {
		return out, errors.New("give url or locator: where the owner can see the sent message")
	}
	if link == "" && m.Expects == model.ExpectsNone && len(ioLinks(m.To)) == 0 {
		return out, errors.New("an action (to no person) needs url: where the owner can see what you did")
	}
	if link != "" {
		if link, err = checkURL(link); err != nil {
			return out, err
		}
	}
	at, err := ioTime(in.At)
	if err != nil {
		return out, err
	}
	if at == "" {
		at = d.Store.Now().UTC().Format(time.RFC3339)
	}
	if err := checkOut(m); err != nil {
		return out, err
	}
	allowedBy := ""
	switch {
	case !agentSurface(d) || in.Observed:
	case m.ForMe() && m.State == model.IODraft:
		if allowedBy, err = delegationGate(ctx, d, doc, m); err != nil {
			return out, err
		}
		addReceiptNote(ctx, fmt.Sprintf("Sent for the owner without their review, stamped, allowed by delegation %s.", allowedBy))
	default:
		if err := sendGate(d, doc, m); err != nil {
			return out, err
		}
	}
	back := "Sent from [[" + doc.ID + "]]."
	if locator != "" {
		back += " Where: " + locator + "."
	}
	date := at[:10]
	switch {
	case m.Expects == model.ExpectsNone && len(ioLinks(m.To)) == 0:
		// An action: nobody to log an interaction with.
	case m.Expects == model.ExpectsNone:
		r, err := InteractionAdd.Run(ctx, d, InteractionAddInput{Summary: joinBody(doc.Title, back), With: ioLinks(m.To),
			Medium: m.Medium, Direction: model.InteractionOut, URL: link, Date: date})
		if err != nil {
			return out, err
		}
		out.Became, out.BecameType = r.ID, model.TypeInteraction
		out.Warnings = append(out.Warnings, r.Warnings...)
	case m.Expects == model.ExpectsResponse:
		var tg []TargetInput
		for _, id := range ioLinks(m.To) {
			tg = append(tg, TargetInput{To: id, Medium: m.Medium})
		}
		r, err := RFXAdd.Run(ctx, d, RFXAddInput{Direction: model.RFXOut, Ask: joinBody(ioText(doc, m), back), Route: m.Route,
			Targets: tg, SelfServe: "sent from " + doc.ID})
		if err != nil {
			return out, err
		}
		out.Became, out.BecameType = r.ID, model.TypeRFX
		out.Warnings = append(out.Warnings, r.Warnings...)
		if _, err := changeTargets(ctx, d, r.ID, func(x *model.RFX) error {
			for i := range x.Targets {
				x.Targets[i].State, x.Targets[i].Sent, x.Targets[i].Ref = model.TargetSent, model.Date(date), firstOf(link, locator)
			}
			return nil
		}); err != nil {
			return out, fmt.Errorf("%s was made but its targets were not marked sent: %w", r.ID, err)
		}
	}
	doc, m, err = getIO(ctx, d, doc.ID)
	if err != nil {
		return out, fmt.Errorf("%s was made but %s was not marked sent: %w", out.Became, in.ID, err)
	}
	m.State, m.At = model.IOSent, at
	if allowedBy != "" {
		m.AllowedBy = allowedBy
	}
	if out.Became != "" {
		m.Became = model.LinkTo(out.Became)
	}
	if m.Ref == "" {
		m.Ref = link
	}
	if doc, err = d.Store.Update(ctx, doc); err != nil {
		return out, fmt.Errorf("%s was made but %s was not marked sent: %w", out.Became, doc.ID, err)
	}
	out.State, out.Rev = m.State, doc.Rev
	// A reply threaded to an incoming message answers it: the message is
	// filed, and became the reply.
	if th := m.Thread.Target(); th != "" {
		if tdoc, tm, err := getIO(ctx, d, th); err == nil && tm.Direction == model.IOIn && (tm.State == model.IOUnread || tm.State == model.IORead) {
			tm.State, tm.Became = model.IOFiled, model.LinkTo(doc.ID)
			if _, err := d.Store.Update(ctx, tdoc); err != nil {
				out.Warnings = append(out.Warnings, fmt.Sprintf("%s was sent, but %s it answers was not filed: %v", doc.ID, th, err))
			} else {
				out.Warnings = append(out.Warnings, syncReplyWarn(ctx, d, th)...)
			}
		} else if err == nil && tm.Direction == model.IOIn && tm.State == model.IOFiled {
			// Filed before the reply: it stays where it was filed, and the
			// reply it owed is done.
			if err := closeReplyDone(ctx, d, th, doc.ID); err != nil {
				out.Warnings = append(out.Warnings, fmt.Sprintf("%s was sent, but the reply owed on %s was not closed: %v", doc.ID, th, err))
			}
		}
	}
	return out, nil
}

// checkVerifiable refuses an incoming message of a medium with permalinks
// that the owner could not find themself: it needs ref, or via with verify.
func checkVerifiable(m *model.IO) error {
	if m.Direction != model.IOIn || !slices.Contains(model.PermalinkMediums, m.Medium) || m.Verifiable() {
		return nil
	}
	if m.Via != nil && m.Verify == "" {
		return fmt.Errorf("a message on %s with via needs verify: a sentence that tells the owner how to find it themself; or give ref, the message permalink", m.Medium)
	}
	return fmt.Errorf("a message on %s needs ref: use the message permalink; with no link, give via {source, ref} and verify, a sentence that tells the owner how to find it", m.Medium)
}
