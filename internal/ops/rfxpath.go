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

// The path log of an Ask is its targets: each one is a try, with the medium,
// when it was added and sent, and how it went. When an Ask stalls, rfx_paths
// lists the next moves; rfx_path_add takes one as a new target with a reason.
// Para never sends. An agent may add a move with rfx_path_add.

// contactStat is how often and how lately a person was asked, across all Asks.
type contactStat struct {
	Count int
	Last  string // YYYY-MM-DD of the latest send
}

// contactStats counts the targets that were sent, per person, over every Ask.
func contactStats(ctx context.Context, s store.Store) (map[string]contactStat, error) {
	refs, err := s.List(ctx, store.Filter{Type: model.TypeRFX})
	if err != nil {
		return nil, err
	}
	out := map[string]contactStat{}
	for _, r := range refs {
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		m, ok := doc.Meta.(*model.RFX)
		if !ok || m.Direction != model.RFXOut {
			continue
		}
		for _, t := range m.Targets {
			if t.Sent == "" {
				continue
			}
			c := out[t.To.Target()]
			c.Count++
			if string(t.Sent) > c.Last {
				c.Last = string(t.Sent)
			}
			out[t.To.Target()] = c
		}
	}
	return out, nil
}

// holdBack says why a repeat Ask to a person should wait, or "". It uses only
// the structured fields away_until and quiet_days.
func holdBack(p *model.Person, st contactStat, today time.Time) string {
	day := today.Format("2006-01-02")
	if p.AwayUntil != "" && string(p.AwayUntil) >= day {
		return "away until " + string(p.AwayUntil)
	}
	if p.QuietDays > 0 && st.Last != "" {
		last, err := time.Parse("2006-01-02", st.Last)
		if err == nil {
			until := last.AddDate(0, 0, p.QuietDays)
			if until.Format("2006-01-02") > day {
				return fmt.Sprintf("quiet time: asked %s, %d days apart; free %s", st.Last, p.QuietDays, until.Format("2006-01-02"))
			}
		}
	}
	return ""
}

// PathEntry is one target of the path log.
type PathEntry struct {
	To       string               `json:"to"`
	ToName   string               `json:"to_name,omitempty" nameof:"To"`
	Medium   model.Medium         `json:"medium"`
	Value    string               `json:"value,omitempty"`
	State    model.TargetState    `json:"state"`
	Added    string               `json:"added,omitempty"`
	Sent     string               `json:"sent,omitempty"`
	Reason   model.FrictionReason `json:"reason,omitempty"`
	Why      string               `json:"why,omitempty"`
	About    string               `json:"about,omitempty"`
	Contacts int                  `json:"contacts"`       // times the person was asked, over all Asks
	Last     string               `json:"last,omitempty"` // latest send to the person, YYYY-MM-DD
}

// Next move kinds.
const (
	MoveOtherContact = "other_contact"
	MoveSameDomain   = "same_domain"
	MoveSameGroup    = "same_group"
	MoveIntroduction = "introduction"
	MoveMeeting      = "meeting"
	MoveCall         = "call"
	MoveSelfServe    = "self_serve"
	MoveBlocker      = "blocker"
)

// PathMove is a next move for a stalled Ask. A move with To and Medium can be
// taken with rfx_path_add; Hold says why it should wait.
type PathMove struct {
	Kind     string       `json:"kind"`
	Label    string       `json:"label"`
	To       string       `json:"to,omitempty"`
	ToName   string       `json:"to_name,omitempty" nameof:"To"`
	Medium   model.Medium `json:"medium,omitempty"`
	Value    string       `json:"value,omitempty"`
	Why      string       `json:"why"`
	Contacts int          `json:"contacts,omitempty"`
	Last     string       `json:"last,omitempty"`
	Hold     string       `json:"hold,omitempty"`
	Command  string       `json:"command,omitempty"`
}

// RFXPathsInput is the input of rfx_paths.
type RFXPathsInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// RFXPathsOutput is the output of rfx_paths.
type RFXPathsOutput struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Status      string      `json:"status"`
	Stalled     bool        `json:"stalled"`
	StalledWhy  string      `json:"stalled_why,omitempty"`
	StalledDays int         `json:"stalled_days,omitempty"`
	Log         []PathEntry `json:"log"`
	Moves       []PathMove  `json:"moves"`
	Rev         string      `json:"rev"`
}

// stalled says whether an open Ask has stopped moving: every target went
// unanswered, or the follow-up date passed while a target waits, or a send is a
// week old. It returns the reason and the days since the latest send.
func stalled(m *model.RFX, today time.Time) (bool, string, int) {
	if m.Status != model.RFXOpen || m.Direction != model.RFXOut {
		return false, "", 0
	}
	day := today.Format("2006-01-02")
	var last string
	for _, t := range m.Targets {
		if string(t.Sent) > last {
			last = string(t.Sent)
		}
	}
	days := 0
	if last != "" {
		if l, err := time.Parse("2006-01-02", last); err == nil {
			days = int(today.Sub(l).Hours() / 24)
		}
	}
	switch {
	case allNoReply(m.Targets):
		return true, "every target went unanswered", days
	case isWaiting(m) && m.NextNudge != "" && string(m.NextNudge) <= day:
		return true, "the follow-up date " + string(m.NextNudge) + " passed with no reply", days
	case isWaiting(m) && m.NextNudge == "" && days >= 7:
		return true, fmt.Sprintf("sent %d days ago with no reply", days), days
	}
	return false, "", days
}

type peopleIndex struct {
	byID map[string]*model.Person
	body map[string]string
	name map[string]string
	ids  []string
}

func loadPeople(ctx context.Context, s store.Store) (peopleIndex, error) {
	refs, err := s.List(ctx, store.Filter{Type: model.TypePerson})
	if err != nil {
		return peopleIndex{}, err
	}
	ix := peopleIndex{byID: map[string]*model.Person{}, body: map[string]string{}, name: map[string]string{}}
	for _, r := range refs {
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return ix, err
		}
		if p, ok := doc.Meta.(*model.Person); ok {
			ix.byID[doc.ID] = p
			ix.body[doc.ID] = strings.ToLower(doc.Body)
			ix.name[doc.ID] = doc.Title
			ix.ids = append(ix.ids, doc.ID)
		}
	}
	slices.Sort(ix.ids)
	return ix, nil
}

// contactRank orders the contacts of a person not yet tried: the preferred one
// first, then a method the person's notes mention, then the rest.
func contactRank(p *model.Person, body string, c model.Contact) int {
	switch {
	case c.Preferred:
		return 0
	case strings.Contains(body, strings.ToLower(string(c.Method))):
		return 1
	}
	return 2
}

const maxMovesPerKind = 4

var RFXPaths = defineRead("rfx_paths", "List the path log of an Ask, a request, and the next moves when it is stalled: other contact methods, others for the same domain or group, an introduction, a meeting or call, self-serve again, a blocker. Shows how often each person was asked, and holds back a repeat inside a stated quiet time", func(ctx context.Context, d Deps, in RFXPathsInput) (RFXPathsOutput, error) {
	doc, m, err := getRFX(ctx, d, normID(in.ID))
	if err != nil {
		return RFXPathsOutput{}, err
	}
	today := d.Store.Now()
	stats, err := contactStats(ctx, d.Store)
	if err != nil {
		return RFXPathsOutput{}, err
	}
	people, err := loadPeople(ctx, d.Store)
	if err != nil {
		return RFXPathsOutput{}, err
	}
	stall, why, days := stalled(m, today)
	out := RFXPathsOutput{ID: doc.ID, Title: doc.Title, Status: string(m.Status), Stalled: stall, StalledWhy: why, StalledDays: days,
		Log: []PathEntry{}, Moves: []PathMove{}, Rev: doc.Rev}
	tried := map[string]bool{}    // person + medium
	targeted := map[string]bool{} // person
	for _, t := range m.Targets {
		st := stats[t.To.Target()]
		out.Log = append(out.Log, PathEntry{To: t.To.Target(), Medium: t.Medium, Value: t.Value, State: t.State,
			Added: string(t.Added), Sent: string(t.Sent), Reason: t.Reason, Why: t.Why, About: t.About.Target(),
			Contacts: st.Count, Last: st.Last})
		tried[t.To.Target()+"|"+string(t.Medium)] = true
		targeted[t.To.Target()] = true
	}
	who := func(id string) string {
		if n := people.name[id]; n != "" {
			return n
		}
		return id
	}
	hold := func(id string) (string, contactStat) {
		st := stats[id]
		if p := people.byID[id]; p != nil {
			return holdBack(p, st, today), st
		}
		return "", st
	}
	add := func(mv PathMove) {
		if mv.To != "" {
			h, st := hold(mv.To)
			mv.Hold, mv.Contacts, mv.Last = h, st.Count, st.Last
			if mv.Medium != "" {
				mv.Command = fmt.Sprintf("para rfx path add %s %s --medium %s --reason <code> --why <text>", doc.ID, mv.To, mv.Medium)
			}
		}
		out.Moves = append(out.Moves, mv)
	}
	if m.Direction == model.RFXOut {
		// Other contact methods for the people already asked.
		for _, t := range m.Targets {
			p := people.byID[t.To.Target()]
			if p == nil {
				continue
			}
			cs := slices.Clone(p.Contacts)
			slices.SortStableFunc(cs, func(a, b model.Contact) int {
				return cmp.Compare(contactRank(p, people.body[t.To.Target()], a), contactRank(p, people.body[t.To.Target()], b))
			})
			n := 0
			for _, c := range cs {
				id := t.To.Target()
				if tried[id+"|"+string(c.Method)] || c.Method == model.MediumMeeting || n >= maxMovesPerKind {
					continue
				}
				tried[id+"|"+string(c.Method)] = true
				n++
				why := "another way to reach them"
				if c.Preferred {
					why = "their preferred contact"
				} else if contactRank(p, people.body[id], c) == 1 {
					why = "their notes mention " + string(c.Method)
				}
				add(PathMove{Kind: MoveOtherContact, Label: fmt.Sprintf("Try %s by %s", who(id), c.Method), To: id, Medium: c.Method, Value: c.Value, Why: why})
			}
		}
		// Someone else for the same domain, team, or group.
		seen := map[string]bool{}
		other := func(kind, label, why string, ok func(id string, p *model.Person) bool) {
			n := 0
			for _, id := range people.ids {
				p := people.byID[id]
				if targeted[id] || seen[id] || n >= maxMovesPerKind || !ok(id, p) {
					continue
				}
				seen[id] = true
				n++
				med, val := model.MediumEmail, ""
				if len(p.Contacts) > 0 {
					best := slices.MinFunc(p.Contacts, func(a, b model.Contact) int {
						return cmp.Compare(contactRank(p, people.body[id], a), contactRank(p, people.body[id], b))
					})
					med, val = best.Method, best.Value
				}
				add(PathMove{Kind: kind, Label: label + " " + who(id), To: id, Medium: med, Value: val, Why: why})
			}
		}
		if dom, err := d.Store.Get(ctx, normID(m.Route)); err == nil {
			if owner, ok := model.DomainOwner(dom); ok && owner != "" {
				other(MoveSameDomain, "Ask", "holds the role that owns the domain "+dom.Title, func(_ string, p *model.Person) bool { return p.Role == owner })
			}
		}
		var teams, groups []model.Link
		for _, t := range m.Targets {
			if p := people.byID[t.To.Target()]; p != nil {
				if p.Team != "" {
					teams = append(teams, p.Team)
				}
				groups = append(groups, p.Groups...)
			} else {
				groups = append(groups, t.To) // a group target: its members
			}
		}
		other(MoveSameGroup, "Ask", "same team or group as someone already asked", func(_ string, p *model.Person) bool {
			return slices.Contains(teams, p.Team) && p.Team != "" || slices.ContainsFunc(p.Groups, func(g model.Link) bool { return slices.Contains(groups, g) })
		})
		out.Moves = append(out.Moves, PathMove{Kind: MoveIntroduction, Why: "someone who knows the owner can introduce you, or the person who sets the priority can say who answers",
			Label: "Ask for an introduction, or for whoever sets the priority"})
		for _, t := range m.Targets {
			p := people.byID[t.To.Target()]
			if p == nil {
				continue
			}
			id := t.To.Target()
			if !tried[id+"|meeting"] {
				add(PathMove{Kind: MoveMeeting, Label: "Ask " + who(id) + " for a meeting", To: id, Medium: model.MediumMeeting, Why: "a conversation settles what messages did not"})
				tried[id+"|meeting"] = true
			}
			if i := slices.IndexFunc(p.Contacts, func(c model.Contact) bool { return c.Method == model.MediumPhone }); i >= 0 && !tried[id+"|phone"] {
				tried[id+"|phone"] = true
				add(PathMove{Kind: MoveCall, Label: "Call " + who(id), To: id, Medium: model.MediumPhone, Value: p.Contacts[i].Value, Why: "a call is quicker than another message"})
			}
		}
		out.Moves = append(out.Moves, PathMove{Kind: MoveSelfServe, Label: "Self-serve again", Why: "policy, earlier Asks, and search may answer it now",
			Command: fmt.Sprintf("para search %q", doc.Title)})
		blocker := "para attempt add " + doc.ID + " --what <tried> --result <result>"
		if len(m.Blocks) > 0 {
			blocker = fmt.Sprintf("para task block %s --reason <who said it, and where it applies>", m.Blocks[0].Target())
		}
		out.Moves = append(out.Moves, PathMove{Kind: MoveBlocker, Label: "Record a blocker", Why: "the work waits on this Ask; say who and why", Command: blocker})
	}
	return out, nil
})

// RFXPathAddInput is the input of rfx_path_add.
type RFXPathAddInput struct {
	ID       string               `json:"id"`
	To       string               `json:"to"`
	Medium   model.Medium         `json:"medium"`
	Value    string               `json:"value,omitempty"`
	Reason   model.FrictionReason `json:"reason"`             // why the Ask moves: wrong_owner, no_response, ...
	Why      string               `json:"why"`                // the reason in words, one line
	About    string               `json:"about,omitempty"`    // policy, exception, or domain that applies
	Nudge    string               `json:"nudge,omitempty"`    // YYYY-MM-DD, the new follow-up date
	Override bool                 `json:"override,omitempty"` // a person may repeat inside a quiet time
}

// RFXPathAddOutput is the output of rfx_path_add.
type RFXPathAddOutput struct {
	ID      string         `json:"id"`
	Targets []model.Target `json:"targets"`
	Held    string         `json:"held,omitempty"`
	Rev     string         `json:"rev"`
}

// RFXPathAdd takes a next move: a new target on the same Ask with a reason, the
// attempt recorded as attempt_add records it, and optionally a new follow-up
// date. It sends nothing. A repeat inside the person's away_until or quiet_days
// is refused unless a person passes override.
var RFXPathAdd = defineSafe("rfx_path_add", "Take a next move on a stalled Ask, a request: add a target with a reason code and a reason, record the attempt, optionally set the follow-up date. Para sends nothing. A person's structured away_until and quiet_days hold a repeat back; only a person can override", func(ctx context.Context, d Deps, in RFXPathAddInput) (RFXPathAddOutput, error) {
	if !slices.Contains(model.FrictionReasons, in.Reason) {
		return RFXPathAddOutput{}, fmt.Errorf("reason %q: want one of %v", in.Reason, model.FrictionReasons)
	}
	why, err := oneLine("why", strings.Join(strings.Fields(in.Why), " "))
	if err != nil {
		return RFXPathAddOutput{}, err
	}
	if why == "" {
		return RFXPathAddOutput{}, errors.New("why is empty: say in a line why the Ask moves")
	}
	if in.Override && !slices.Contains(PersonSurfaces, d.Store.Info().Surface) {
		return RFXPathAddOutput{}, fmt.Errorf("override: %w", ErrPersonOnly)
	}
	if in.Nudge != "" {
		if err := checkDate("nudge", in.Nudge); err != nil {
			return RFXPathAddOutput{}, err
		}
	}
	about, err := aboutLink(ctx, d, in.About)
	if err != nil {
		return RFXPathAddOutput{}, err
	}
	doc, m, err := getRFX(ctx, d, normID(in.ID))
	if err != nil {
		return RFXPathAddOutput{}, err
	}
	if m.Status == model.RFXClosed {
		return RFXPathAddOutput{}, errors.New("request is closed")
	}
	held := ""
	to := normID(in.To)
	if pd, err := d.Store.Get(ctx, to); err == nil {
		if p, ok := pd.Meta.(*model.Person); ok {
			stats, err := contactStats(ctx, d.Store)
			if err != nil {
				return RFXPathAddOutput{}, err
			}
			held = holdBack(p, stats[to], d.Store.Now())
			if held != "" && !in.Override {
				return RFXPathAddOutput{}, fmt.Errorf("%s is held back (%s): choose someone else, wait, or have the owner pass override", to, held)
			}
		}
	}
	before := len(m.Targets)
	tg, err := buildTargets(ctx, d, m.Targets, []TargetInput{{To: in.To, Medium: in.Medium, Value: in.Value}})
	if err != nil {
		return RFXPathAddOutput{}, err
	}
	n := &tg[before]
	n.Added = model.Date(d.Store.Now().Format("2006-01-02"))
	n.Reason, n.Why, n.About = in.Reason, why, about
	m.Targets = tg
	if in.Nudge != "" {
		m.NextNudge = model.Date(in.Nudge)
	}
	addAttemptLine(&doc, attemptLine(d, fmt.Sprintf("Next move: %s by %s", to, in.Medium), string(in.Reason)+": "+why, nil))
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return RFXPathAddOutput{}, fmt.Errorf("rfx_path_add: %w", err)
	}
	return RFXPathAddOutput{ID: saved.ID, Targets: slices.Clone(m.Targets), Held: held, Rev: saved.Rev}, nil
})
