package ops

import "testing"

func TestPersonAliasOps(t *testing.T) {
	d := directoryDeps(t)
	out := must(t, OrgPersonAliasAdd, d, PersonAliasInput{ID: "person-bo-example", Alias: "  Bobby  Ex "})
	if len(out.Aliases) != 1 || out.Aliases[0] != "Bobby Ex" {
		t.Fatalf("aliases = %v", out.Aliases)
	}
	if _, err := OrgPersonAliasAdd.Run(ctx, d, PersonAliasInput{ID: "person-bo-example", Alias: "bobby ex"}); err == nil {
		t.Error("duplicate alias accepted")
	}
	// who matches the alias.
	if w := must(t, Who, d, WhoInput{Query: "Bobby Ex"}); w.Person == nil || w.Person.ID != "person-bo-example" {
		t.Fatalf("who = %+v", w)
	}
	out = must(t, OrgPersonAliasRemove, d, PersonAliasInput{ID: "person-bo-example", Alias: "BOBBY EX"})
	if out.Aliases == nil || len(out.Aliases) != 0 {
		t.Fatalf("aliases = %#v", out.Aliases)
	}
	for _, c := range []struct {
		name string
		op   func() error
	}{
		{"remove missing", func() error {
			_, err := OrgPersonAliasRemove.Run(ctx, d, PersonAliasInput{ID: "person-bo-example", Alias: "Nope"})
			return err
		}},
		{"empty", func() error {
			_, err := OrgPersonAliasAdd.Run(ctx, d, PersonAliasInput{ID: "person-bo-example", Alias: " "})
			return err
		}},
		{"not a person", func() error {
			_, err := OrgPersonAliasAdd.Run(ctx, d, PersonAliasInput{ID: "group-atlas", Alias: "x"})
			return err
		}},
	} {
		if c.op() == nil {
			t.Errorf("%s accepted", c.name)
		}
	}
	mustLintClean(t, d)
}
