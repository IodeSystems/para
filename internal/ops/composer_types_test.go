package ops

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// The composer works on people, groups, asks, tracked documents, and areas: the work is linked to
// the item and tells the agent what managing it means.
func TestAgentSendLinksAndManagesMoreTypes(t *testing.T) {
	f := newAgentFix(t)
	person := mustRun(t, OrgPersonAdd, f.owner, OrgAddInput{Name: "Ada Example", Org: "acme.example"}).ID
	group := mustRun(t, OrgGroupAdd, f.owner, GroupAddInput{Name: "Atlas", Org: "acme.example", Kind: model.GroupTeam}).ID
	rfx := mustRun(t, RFXAdd, f.owner, RFXAddInput{Direction: model.RFXOut, Ask: "Chase the audit", Route: "domain-logging", SelfServe: "x",
		Targets: []TargetInput{{To: person, Medium: model.MediumEmail}}}).ID
	doc := mustRun(t, DocTrack, f.owner, DocTrackInput{URL: "https://wiki.acme.example/runbooks/portal", Text: runbookV1, System: model.SystemConfluence,
		Goal: f.proj, Owner: person, Via: &model.IOVia{Source: "confluence", Ref: "12345"}}).ID
	area := mustRun(t, AreaAdd, f.owner, AreaAddInput{Title: "Platform"}).ID
	for id, want := range map[string]string{
		person: "update its record",
		group:  "update its record",
		rfx:    "the reply",
		doc:    "versions and comments",
		area:   "its items",
	} {
		out := mustRun(t, AgentSend, f.owner, AgentSendInput{ID: id, Text: "Sam confirmed."})
		r := mustRun(t, RequestShow, f.owner, RequestShowInput{ID: out.Request}).Doc
		rm := r.Meta.(*model.Request)
		if len(rm.Links) != 1 || rm.Links[0].Target() != id || len(rm.Context) != 0 {
			t.Errorf("%s: links = %v context = %v", id, rm.Links, rm.Context)
		}
		if !strings.Contains(r.Body, want) || !strings.Contains(r.Body, "Sam confirmed.") {
			t.Errorf("%s: body = %s", id, r.Body)
		}
	}
	// request_add links a person and a group.
	r := mustRun(t, RequestAdd, f.owner, RequestAddInput{Title: "Tidy", Ask: "Tidy their record.", Links: []string{person, group}})
	rm := mustRun(t, RequestShow, f.owner, RequestShowInput{ID: r.ID}).Doc.Meta.(*model.Request)
	if len(rm.Links) != 2 {
		t.Errorf("links = %v", rm.Links)
	}
}
