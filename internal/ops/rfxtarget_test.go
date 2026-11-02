package ops

import (
	"reflect"
	"strings"
	"testing"

	"github.com/iodesystems/paradize/internal/model"
)

func targetDeps(t *testing.T) Deps {
	t.Helper()
	d := directoryDeps(t)
	must(t, OrgPersonContactAdd, d, PersonContactInput{ID: "person-ada-example", Method: "slack", Value: "@ada"})
	must(t, OrgPersonContactAdd, d, PersonContactInput{ID: "person-ada-example", Method: "slack", Value: "@ada-oncall", Preferred: true})
	must(t, OrgPersonContactAdd, d, PersonContactInput{ID: "person-ada-example", Method: "email", Value: "ada@acme.example"})
	must(t, OrgPersonContactAdd, d, PersonContactInput{ID: "person-ada-example", Method: "phone", Value: "+1 555 0100"})
	must(t, OrgPersonContactAdd, d, PersonContactInput{ID: "person-ada-example", Method: "phone", Value: "+1 555 0101"})
	return d
}

func TestPreferredValue(t *testing.T) {
	p := &model.Person{Contacts: []model.Contact{
		{Method: "slack", Value: "@a"}, {Method: "slack", Value: "@b", Preferred: true},
		{Method: "email", Value: "only@acme.example"},
		{Method: "phone", Value: "1"}, {Method: "phone", Value: "2"},
	}}
	cases := []struct {
		m    model.Medium
		want string
	}{{"slack", "@b"}, {"email", "only@acme.example"}, {"phone", ""}, {"jira", ""}}
	for _, c := range cases {
		if got := preferredValue(p, c.m); got != c.want {
			t.Errorf("%s: %q, want %q", c.m, got, c.want)
		}
	}
}

func TestRFXAddTargets(t *testing.T) {
	d := targetDeps(t)
	cases := []struct {
		name    string
		targets []TargetInput
		want    []model.Target
		wantErr string
	}{
		{"value from preferred contact", []TargetInput{{To: "person-ada-example", Medium: "slack"}},
			[]model.Target{{To: "[[person-ada-example]]", Medium: "slack", Value: "@ada-oncall", State: "drafted"}}, ""},
		{"several mediums for one person", []TargetInput{{To: "person-ada-example", Medium: "slack"}, {To: "[[person-ada-example]]", Medium: "email"}},
			[]model.Target{
				{To: "[[person-ada-example]]", Medium: "slack", Value: "@ada-oncall", State: "drafted"},
				{To: "[[person-ada-example]]", Medium: "email", Value: "ada@acme.example", State: "drafted"}}, ""},
		{"person and group", []TargetInput{{To: "person-bo-example", Medium: "email"}, {To: "group-ops-chat", Medium: "slack", Value: "#ops"}},
			[]model.Target{
				{To: "[[person-bo-example]]", Medium: "email", State: "drafted"},
				{To: "[[group-ops-chat]]", Medium: "slack", Value: "#ops", State: "drafted"}}, ""},
		{"explicit value wins", []TargetInput{{To: "person-ada-example", Medium: "slack", Value: "@other"}},
			[]model.Target{{To: "[[person-ada-example]]", Medium: "slack", Value: "@other", State: "drafted"}}, ""},
		{"ambiguous medium gives no default", []TargetInput{{To: "person-ada-example", Medium: "phone"}},
			[]model.Target{{To: "[[person-ada-example]]", Medium: "phone", State: "drafted"}}, ""},
		{"no targets", nil, nil, ""},
		{"missing medium", []TargetInput{{To: "person-ada-example"}}, nil, "medium"},
		{"bad medium", []TargetInput{{To: "person-ada-example", Medium: "fax"}}, nil, "medium"},
		{"missing to", []TargetInput{{Medium: "email"}}, nil, "to is empty"},
		{"unknown to", []TargetInput{{To: "person-gone", Medium: "email"}}, nil, "not found"},
		{"to is a role", []TargetInput{{To: "role-x", Medium: "email"}}, nil, "not found"},
		{"duplicate", []TargetInput{{To: "person-ada-example", Medium: "email"}, {To: "person-ada-example", Medium: "email"}}, nil, "already"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := RFXAdd.Run(ctx, d, RFXAddInput{Direction: "out", Ask: "synthetic", Route: "domain-x", SelfServe: "read", Targets: c.targets})
			if (err == nil) != (c.wantErr == "") || (err != nil && !strings.Contains(err.Error(), c.wantErr)) {
				t.Fatalf("err = %v", err)
			}
			if err != nil {
				return
			}
			_, m, err := getRFX(ctx, d, out.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(c.want) == 0 && len(m.Targets) == 0 {
				return
			}
			if !reflect.DeepEqual(m.Targets, c.want) {
				t.Errorf("targets = %+v\nwant %+v", m.Targets, c.want)
			}
		})
	}
	mustLintClean(t, d)
}

func TestRFXTargetStates(t *testing.T) {
	d := targetDeps(t)
	id := addRFX(t, d, RFXAddInput{Targets: []TargetInput{
		{To: "person-ada-example", Medium: "slack"}, {To: "person-ada-example", Medium: "email"}, {To: "person-bo-example", Medium: "email"}}})
	waiting := func() bool {
		out, err := RFXWaiting.Run(ctx, d, RFXWaitingInput{Today: "2026-11-01"})
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range out.Items {
			if it.ID == id {
				return true
			}
		}
		return false
	}
	if waiting() {
		t.Fatal("all targets drafted: not waiting")
	}
	steps := []struct {
		name        string
		in          RFXTargetStateInput
		wantWaiting bool
		wantErr     string
		check       func(t *testing.T, ts []model.Target)
	}{
		{"person with two mediums needs one", RFXTargetStateInput{ID: id, To: "person-ada-example", State: "sent"}, false, "pass medium", nil},
		{"send slack", RFXTargetStateInput{ID: id, To: "person-ada-example", Medium: "slack", State: "sent", Date: "2026-11-01", Ref: "https://acme.example/t/1"}, true, "",
			func(t *testing.T, ts []model.Target) {
				if ts[0].Sent != "2026-11-01" || ts[0].Ref != "https://acme.example/t/1" {
					t.Errorf("target = %+v", ts[0])
				}
			}},
		{"send again keeps the first date", RFXTargetStateInput{ID: id, To: "person-ada-example", Medium: "slack", State: "sent", Date: "2026-11-09"}, true, "",
			func(t *testing.T, ts []model.Target) {
				if ts[0].Sent != "2026-11-01" {
					t.Errorf("sent = %s", ts[0].Sent)
				}
			}},
		{"unique target needs no medium", RFXTargetStateInput{ID: id, To: "person-bo-example", State: "sent", Date: "2026-11-02"}, true, "", nil},
		{"reply to slack", RFXTargetStateInput{ID: id, To: "person-ada-example", Medium: "slack", State: "replied"}, true, "", nil},
		{"bo has not replied", RFXTargetStateInput{ID: id, To: "person-bo-example", State: "no_reply"}, false, "", nil},
		{"bad state", RFXTargetStateInput{ID: id, To: "person-bo-example", State: "ghosted"}, false, "state", nil},
		{"not a target", RFXTargetStateInput{ID: id, To: "group-atlas", State: "sent"}, false, "no target", nil},
		{"bad date", RFXTargetStateInput{ID: id, To: "person-bo-example", State: "sent", Date: "tomorrow"}, false, "date", nil},
	}
	for _, c := range steps {
		out, err := RFXTargetState.Run(ctx, d, c.in)
		if (err == nil) != (c.wantErr == "") || (err != nil && !strings.Contains(err.Error(), c.wantErr)) {
			t.Fatalf("%s: err = %v", c.name, err)
		}
		if err != nil {
			continue
		}
		if c.check != nil {
			c.check(t, out.Targets)
		}
		if out.Waiting != c.wantWaiting || waiting() != c.wantWaiting {
			t.Fatalf("%s: waiting = %v / %v, want %v", c.name, out.Waiting, waiting(), c.wantWaiting)
		}
	}
	// Add a target after the fact, then close: a closed request takes no change.
	if _, err := RFXTargetAdd.Run(ctx, d, RFXTargetAddInput{ID: id, To: "group-ops-chat", Medium: "slack", Value: "#ops"}); err != nil {
		t.Fatal(err)
	}
	if _, err := RFXTargetAdd.Run(ctx, d, RFXTargetAddInput{ID: id, To: "group-ops-chat", Medium: "slack", Value: "#ops"}); err == nil {
		t.Error("duplicate target accepted")
	}
	must(t, RFXClose, d, RFXCloseInput{ID: id})
	if _, err := RFXTargetAdd.Run(ctx, d, RFXTargetAddInput{ID: id, To: "group-atlas", Medium: "email"}); err == nil {
		t.Error("closed request took a target")
	}
	mustLintClean(t, d)
}

func TestRFXWaitingRules(t *testing.T) {
	d := targetDeps(t)
	none := addRFX(t, d, RFXAddInput{Ask: "no targets"})
	drafted := addRFX(t, d, RFXAddInput{Ask: "drafted", Targets: []TargetInput{{To: "person-bo-example", Medium: "email"}}})
	sent := addRFX(t, d, RFXAddInput{Ask: "sent", Targets: []TargetInput{{To: "person-bo-example", Medium: "email"}}})
	replied := addRFX(t, d, RFXAddInput{Ask: "replied", Targets: []TargetInput{{To: "person-bo-example", Medium: "email"}}})
	must(t, RFXTargetState, d, RFXTargetStateInput{ID: sent, To: "person-bo-example", State: "sent"})
	must(t, RFXTargetState, d, RFXTargetStateInput{ID: replied, To: "person-bo-example", State: "replied"})
	out, err := RFXWaiting.Run(ctx, d, RFXWaitingInput{Today: "2026-11-01"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range out.Items {
		got = append(got, it.ID)
	}
	if want := sorted(none, sent); !reflect.DeepEqual(sorted(got...), want) {
		t.Errorf("waiting = %v, want %v (drafted %s, replied %s are not waiting)", got, want, drafted, replied)
	}
}

func TestRFXWaitingStalled(t *testing.T) {
	d := targetDeps(t)
	stalled := addRFX(t, d, RFXAddInput{Ask: "stalled", Targets: []TargetInput{{To: "person-bo-example", Medium: "email"}, {To: "person-ada-example", Medium: "slack"}}})
	partial := addRFX(t, d, RFXAddInput{Ask: "partial", Targets: []TargetInput{{To: "person-bo-example", Medium: "email"}, {To: "person-ada-example", Medium: "slack"}}})
	for _, to := range []string{"person-bo-example", "person-ada-example"} {
		must(t, RFXTargetState, d, RFXTargetStateInput{ID: stalled, To: to, State: "no_reply"})
	}
	must(t, RFXTargetState, d, RFXTargetStateInput{ID: partial, To: "person-bo-example", State: "no_reply"})
	must(t, RFXTargetState, d, RFXTargetStateInput{ID: partial, To: "person-ada-example", State: "replied"})
	out, err := RFXWaiting.Run(ctx, d, RFXWaitingInput{Today: "2026-11-01"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 || out.Items[0].ID != stalled || !out.Items[0].Stalled {
		t.Fatalf("items = %+v, want only %s, stalled", out.Items, stalled)
	}
	// Closing it removes it.
	must(t, RFXClose, d, RFXCloseInput{ID: stalled})
	out, err = RFXWaiting.Run(ctx, d, RFXWaitingInput{Today: "2026-11-01"})
	if err != nil || len(out.Items) != 0 {
		t.Fatalf("after close: %+v, %v", out.Items, err)
	}
}
