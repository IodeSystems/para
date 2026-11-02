package ops

import (
	"reflect"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
)

// directoryDeps seeds a vault with synthetic people and groups.
func directoryDeps(t *testing.T) Deps {
	t.Helper()
	d := newDeps(t)
	must(t, OrgGroupAdd, d, GroupAddInput{Name: "Atlas", Org: "acme.example", Kind: model.GroupTeam})
	must(t, OrgGroupAdd, d, GroupAddInput{Name: "Ops Chat", Org: "acme.example", Kind: model.GroupChannel})
	must(t, OrgPersonAdd, d, OrgAddInput{Name: "Ada Example", Org: "acme.example", Aliases: []string{"Ada E", " ada e ", ""}})
	must(t, OrgPersonAdd, d, OrgAddInput{Name: "Bo Example", Org: "acme.example"})
	return d
}

func TestGroupAdd(t *testing.T) {
	d := newDeps(t)
	cases := []struct {
		name    string
		in      GroupAddInput
		wantErr string
	}{
		{"team", GroupAddInput{Name: "Atlas", Org: "acme.example", Kind: model.GroupTeam}, ""},
		{"free org text", GroupAddInput{Name: "Book Club", Org: "just friends", Kind: model.GroupOther}, ""},
		{"empty name", GroupAddInput{Org: "o", Kind: model.GroupTeam}, "name"},
		{"empty org", GroupAddInput{Name: "X", Kind: model.GroupTeam}, "org"},
		{"bad kind", GroupAddInput{Name: "X", Org: "o", Kind: "club"}, "kind"},
		{"missing kind", GroupAddInput{Name: "X", Org: "o"}, "kind"},
		{"duplicate", GroupAddInput{Name: "Atlas", Org: "o", Kind: model.GroupList}, "in use"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := OrgGroupAdd.Run(ctx, d, c.in)
			if (err == nil) != (c.wantErr == "") || (err != nil && !strings.Contains(err.Error(), c.wantErr)) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	mustLintClean(t, d)
}

func TestPersonAliases(t *testing.T) {
	d := directoryDeps(t)
	show, err := OrgPersonShow.Run(ctx, d, OrgShowInput{ID: "person-ada-example"})
	if err != nil {
		t.Fatal(err)
	}
	if got := show.Doc.Meta.(*model.Person).Aliases; !reflect.DeepEqual(got, []string{"Ada E"}) {
		t.Errorf("aliases = %v", got)
	}
	if _, err := OrgTeamAdd.Run(ctx, d, OrgAddInput{Name: "T", Org: "o", Aliases: []string{"x"}}); err == nil {
		t.Error("aliases on a team accepted")
	}
}

func TestContacts(t *testing.T) {
	d := directoryDeps(t)
	add := func(in PersonContactInput) error {
		_, err := OrgPersonContactAdd.Run(ctx, d, in)
		return err
	}
	cases := []struct {
		name    string
		in      PersonContactInput
		wantErr string
	}{
		{"first slack", PersonContactInput{ID: "person-ada-example", Method: "slack", Value: "@ada", Label: "day to day"}, ""},
		{"second slack, preferred", PersonContactInput{ID: "person-ada-example", Method: "slack", Value: "@ada-oncall", Preferred: true}, ""},
		{"email", PersonContactInput{ID: "person-ada-example", Method: "email", Value: "ada@acme.example"}, ""},
		{"duplicate ignoring case", PersonContactInput{ID: "person-ada-example", Method: "email", Value: "ADA@acme.example"}, "already"},
		{"same value other method", PersonContactInput{ID: "person-ada-example", Method: "other", Value: "ada@acme.example"}, ""},
		{"bad method", PersonContactInput{ID: "person-ada-example", Method: "fax", Value: "1"}, "medium"},
		{"empty value", PersonContactInput{ID: "person-ada-example", Method: "email", Value: "  "}, "value"},
		{"not a person", PersonContactInput{ID: "group-atlas", Method: "email", Value: "a@b.example"}, "not person"},
		{"unknown person", PersonContactInput{ID: "person-nobody", Method: "email", Value: "a@b.example"}, "not found"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := add(c.in)
			if (err == nil) != (c.wantErr == "") || (err != nil && !strings.Contains(err.Error(), c.wantErr)) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	// Preferring a contact replaces the preferred one of the same method only.
	show, _ := OrgPersonShow.Run(ctx, d, OrgShowInput{ID: "person-ada-example"})
	var pref []string
	for _, c := range show.Doc.Meta.(*model.Person).Contacts {
		if c.Preferred {
			pref = append(pref, string(c.Method)+":"+c.Value)
		}
	}
	if !reflect.DeepEqual(pref, []string{"slack:@ada-oncall"}) {
		t.Errorf("preferred = %v", pref)
	}
	rm := []struct {
		name    string
		in      PersonContactInput
		wantLen int
		wantErr string
	}{
		{"remove ignoring case", PersonContactInput{ID: "person-ada-example", Method: "email", Value: "ADA@acme.example"}, 3, ""},
		{"remove again", PersonContactInput{ID: "person-ada-example", Method: "email", Value: "ada@acme.example"}, 0, "no contact"},
		{"wrong method", PersonContactInput{ID: "person-ada-example", Method: "github", Value: "@ada"}, 0, "no contact"},
	}
	for _, c := range rm {
		t.Run(c.name, func(t *testing.T) {
			out, err := OrgPersonContactRemove.Run(ctx, d, c.in)
			if (err == nil) != (c.wantErr == "") || (err != nil && !strings.Contains(err.Error(), c.wantErr)) {
				t.Fatalf("err = %v", err)
			}
			if err == nil && len(out.Contacts) != c.wantLen {
				t.Errorf("contacts = %v", out.Contacts)
			}
		})
	}
	mustLintClean(t, d)
}

func TestGroupMembership(t *testing.T) {
	d := directoryDeps(t)
	steps := []struct {
		name    string
		op      func() (PersonGroupOutput, error)
		want    []string
		wantErr string
	}{
		{"add", func() (PersonGroupOutput, error) {
			return OrgPersonGroupAdd.Run(ctx, d, PersonGroupInput{ID: "person-ada-example", Group: "group-atlas"})
		}, []string{"group-atlas"}, ""},
		{"add again does nothing", func() (PersonGroupOutput, error) {
			return OrgPersonGroupAdd.Run(ctx, d, PersonGroupInput{ID: "person-ada-example", Group: "[[group-atlas]]"})
		}, []string{"group-atlas"}, ""},
		{"second group", func() (PersonGroupOutput, error) {
			return OrgPersonGroupAdd.Run(ctx, d, PersonGroupInput{ID: "person-ada-example", Group: "group-ops-chat"})
		}, []string{"group-atlas", "group-ops-chat"}, ""},
		{"a person is not a group", func() (PersonGroupOutput, error) {
			return OrgPersonGroupAdd.Run(ctx, d, PersonGroupInput{ID: "person-ada-example", Group: "person-bo-example"})
		}, nil, "want one of"},
		{"unknown group", func() (PersonGroupOutput, error) {
			return OrgPersonGroupAdd.Run(ctx, d, PersonGroupInput{ID: "person-ada-example", Group: "group-gone"})
		}, nil, "not found"},
		{"empty group", func() (PersonGroupOutput, error) {
			return OrgPersonGroupAdd.Run(ctx, d, PersonGroupInput{ID: "person-ada-example"})
		}, nil, "group is empty"},
		{"remove", func() (PersonGroupOutput, error) {
			return OrgPersonGroupRemove.Run(ctx, d, PersonGroupInput{ID: "person-ada-example", Group: "group-atlas"})
		}, []string{"group-ops-chat"}, ""},
		{"remove a non-member", func() (PersonGroupOutput, error) {
			return OrgPersonGroupRemove.Run(ctx, d, PersonGroupInput{ID: "person-ada-example", Group: "group-atlas"})
		}, nil, "not in group"},
	}
	for _, c := range steps {
		out, err := c.op()
		if (err == nil) != (c.wantErr == "") || (err != nil && !strings.Contains(err.Error(), c.wantErr)) {
			t.Fatalf("%s: err = %v", c.name, err)
		}
		if err == nil && !reflect.DeepEqual(out.Groups, c.want) {
			t.Fatalf("%s: groups = %v, want %v", c.name, out.Groups, c.want)
		}
	}
	must(t, OrgPersonGroupAdd, d, PersonGroupInput{ID: "person-bo-example", Group: "group-ops-chat"})
	show, err := OrgGroupShow.Run(ctx, d, OrgShowInput{ID: "group-ops-chat"})
	if err != nil {
		t.Fatal(err)
	}
	if got := refIDs(show.Members); !reflect.DeepEqual(got, []string{"person-ada-example", "person-bo-example"}) {
		t.Errorf("members = %v", got)
	}
	if _, err := OrgGroupShow.Run(ctx, d, OrgShowInput{ID: "person-ada-example"}); err == nil {
		t.Error("group show accepted a person")
	}
	list, err := OrgGroupList.Run(ctx, d, OrgListInput{})
	if err != nil || len(list.Items) != 2 {
		t.Errorf("list = %+v, %v", list, err)
	}
	mustLintClean(t, d)
}

func TestAppendToSection(t *testing.T) {
	const line = "- 2026-11-01: new"
	cases := []struct{ name, body, want string }{
		{"empty body", "", "## Notes\n\n" + line},
		{"no section", "## Facts\n\n- a", "## Facts\n\n- a\n\n## Notes\n\n" + line},
		{"section at end", "## Notes\n\n- old", "## Notes\n\n- old\n" + line},
		{"section with trailing blanks", "## Notes\n\n- old\n\n\n", "## Notes\n\n- old\n" + line},
		{"section before another", "## Notes\n\n- old\n\n## Facts\n\n- a", "## Notes\n\n- old\n" + line + "\n\n## Facts\n\n- a"},
		{"empty section before another", "## Notes\n\n## Facts\n\n- a", "## Notes\n\n" + line + "\n\n## Facts\n\n- a"},
		{"heading with trailing space", "## Notes  \n\n- old", "## Notes  \n\n- old\n" + line},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := appendToSection(c.body, notesHeading, line); got != c.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, c.want)
			}
		})
	}
}

func TestPersonNote(t *testing.T) {
	d := directoryDeps(t)
	cases := []struct {
		name    string
		in      PersonNoteInput
		want    string
		wantErr string
	}{
		{"first note", PersonNoteInput{ID: "person-ada-example", Text: "Prefers  Slack\nfor quick questions.", Date: "2026-11-01"}, "- 2026-11-01: Prefers Slack for quick questions.", ""},
		{"second note", PersonNoteInput{ID: "person-ada-example", Text: "UTC-5.", Date: "2026-11-02"}, "- 2026-11-02: UTC-5.", ""},
		{"default date", PersonNoteInput{ID: "person-ada-example", Text: "Owns the deploy pipeline."}, "", ""},
		{"empty text", PersonNoteInput{ID: "person-ada-example", Text: " "}, "", "text"},
		{"bad date", PersonNoteInput{ID: "person-ada-example", Text: "x", Date: "11/01"}, "", "date"},
		{"not a person", PersonNoteInput{ID: "group-atlas", Text: "x"}, "", "not person"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := OrgPersonNote.Run(ctx, d, c.in)
			if (err == nil) != (c.wantErr == "") || (err != nil && !strings.Contains(err.Error(), c.wantErr)) {
				t.Fatalf("err = %v", err)
			}
			if c.want != "" && out.Line != c.want {
				t.Errorf("line = %q", out.Line)
			}
		})
	}
	show, _ := OrgPersonShow.Run(ctx, d, OrgShowInput{ID: "person-ada-example"})
	if n := strings.Count(show.Doc.Body, "## Notes"); n != 1 {
		t.Errorf("notes headings = %d in %q", n, show.Doc.Body)
	}
	if got := sectionLines(show.Doc.Body, notesHeading); len(got) != 3 || got[0] != "- 2026-11-01: Prefers Slack for quick questions." {
		t.Errorf("notes = %v", got)
	}
	mustLintClean(t, d)
}
