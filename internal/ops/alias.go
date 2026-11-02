package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
)

// PersonAliasInput is the input of org_person_alias_add and _remove.
type PersonAliasInput struct {
	ID    string `json:"id"`
	Alias string `json:"alias"` // another name for the person; compared without case
}

// PersonAliasOutput is the output of the alias ops.
type PersonAliasOutput struct {
	ID      string   `json:"id"`
	Aliases []string `json:"aliases"`
	Rev     string   `json:"rev"`
}

// The alias ops. Aliases are what `who` and search match besides the name.
var (
	OrgPersonAliasAdd = define("org_person_alias_add", "Add another name for a person", func(ctx context.Context, d Deps, in PersonAliasInput) (PersonAliasOutput, error) {
		alias, err := cleanAlias(in.Alias)
		if err != nil {
			return PersonAliasOutput{}, err
		}
		doc, p, err := changePerson(ctx, d, in.ID, func(_ *model.Document, p *model.Person) error {
			if indexAlias(p.Aliases, alias) >= 0 {
				return fmt.Errorf("alias %q already recorded", alias)
			}
			p.Aliases = append(p.Aliases, alias)
			return nil
		})
		return aliasOut(doc, p, err)
	})

	OrgPersonAliasRemove = define("org_person_alias_remove", "Remove another name of a person", func(ctx context.Context, d Deps, in PersonAliasInput) (PersonAliasOutput, error) {
		alias, err := cleanAlias(in.Alias)
		if err != nil {
			return PersonAliasOutput{}, err
		}
		doc, p, err := changePerson(ctx, d, in.ID, func(_ *model.Document, p *model.Person) error {
			i := indexAlias(p.Aliases, alias)
			if i < 0 {
				return fmt.Errorf("no alias %q", alias)
			}
			p.Aliases = slices.Delete(p.Aliases, i, i+1)
			return nil
		})
		return aliasOut(doc, p, err)
	})
)

func cleanAlias(s string) (string, error) {
	v := strings.Join(strings.Fields(s), " ")
	if v == "" {
		return "", errors.New("alias is empty")
	}
	return v, nil
}

func indexAlias(as []string, alias string) int {
	return slices.IndexFunc(as, func(a string) bool { return strings.EqualFold(a, alias) })
}

func aliasOut(doc model.Document, p *model.Person, err error) (PersonAliasOutput, error) {
	if err != nil {
		return PersonAliasOutput{}, err
	}
	as := p.Aliases
	if as == nil {
		as = []string{}
	}
	return PersonAliasOutput{ID: doc.ID, Aliases: as, Rev: doc.Rev}, nil
}
