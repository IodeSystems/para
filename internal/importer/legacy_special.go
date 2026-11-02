package importer

import (
	"fmt"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
)

// buildSpecial builds an incident, a meeting, or a policy exception from a
// legacy file whose frontmatter had every required field (finalizeSpecial).
func buildSpecial(b *bctx, c *cand, e Entry) (model.Document, []string, error) {
	p, err := prep(c, e)
	if err != nil {
		return model.Document{}, nil, err
	}
	md, taken, common := c.md, p.taken, p.common
	var meta model.Meta
	switch c.typ {
	case model.TypeIncident:
		st := map[string]model.IncidentStatus{"open": model.IncidentOpen, "mitigated": model.IncidentMitigated, "resolved": model.IncidentResolved,
			"closed": model.IncidentResolved, "done": model.IncidentResolved}[strings.ToLower(md.scalar("status"))]
		in := &model.Incident{Common: common, Severity: model.IncidentSeverity(md.scalar("severity")), Status: st, Started: md.scalar("started"), Ended: md.scalar("ended")}
		if r := b.refID(e.Area); r != "" {
			in.Area = model.LinkTo(r)
		}
		for _, k := range []string{"severity", "status", "started", "ended"} {
			taken[k] = true
		}
		meta = in
	case model.TypeMeeting:
		st := model.MeetingHeld
		for _, s := range []model.MeetingStatus{model.MeetingRequested, model.MeetingScheduled, model.MeetingHeld, model.MeetingCancelled} {
			if md.scalar("status") == string(s) {
				st = s
			}
		}
		m := &model.Meeting{Common: common, Status: st, Start: md.scalar("start"), End: md.scalar("end")}
		for _, w := range c.withCands {
			id := b.idOf(w)
			if id == "" {
				return model.Document{}, nil, fmt.Errorf("attendee %q is not imported", safe(w.title))
			}
			m.With = append(m.With, model.LinkTo(id))
		}
		for _, k := range []string{"status", "start", "end", "with"} {
			taken[k] = true
		}
		meta = m
	case model.TypePolicyException:
		id := b.idOf(c.policy)
		if id == "" {
			return model.Document{}, nil, fmt.Errorf("policy %q is not imported", safe(c.policy.title))
		}
		st := model.ExceptionOpen
		for _, s := range []model.ExceptionStatus{model.ExceptionOpen, model.ExceptionResolved, model.ExceptionAccepted} {
			if strings.ToLower(md.scalar("status")) == string(s) {
				st = s
			}
		}
		by := "unknown"
		for _, k := range []string{"by", "owner", "author"} {
			if v := md.scalar(k); v != "" {
				by = v
				taken[k] = true
				break
			}
		}
		meta = &model.PolicyException{Common: common, Policy: model.LinkTo(id), Kind: model.ExceptionKind(c.hint("kind")), Status: st, By: by}
		for _, k := range []string{"kind", "policy", "status"} {
			taken[k] = true
		}
		p.notes = append(p.notes, "kind: legacy "+md.scalar("kind")+" read as "+c.hint("kind"))
	default:
		return model.Document{}, nil, fmt.Errorf("type %s is not special", c.typ)
	}
	return finish(b, c, e, p, meta)
}
