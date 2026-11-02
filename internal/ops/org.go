package ops

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/iodesystems/paradize/internal/model"
	"github.com/iodesystems/paradize/internal/store"
)

// The org ops record facts about who owns what: owner, role, what was said,
// and where it was said. They take no opinion field, and a "said" without a
// source is refused.

// OrgAddInput is the input of org_<kind>_add. Which fields apply depends on
// the kind: Owner for domain; Role and Team for person.
type OrgAddInput struct {
	Name    string   `json:"name"`
	Org     string   `json:"org,omitempty"`     // organization the entry belongs to (person, team, role)
	ID      string   `json:"id,omitempty"`      // optional; default <kind>-<slug of name>
	Owner   string   `json:"owner,omitempty"`   // domain: id of the owning role, team, or person
	Role    string   `json:"role,omitempty"`    // person: id of the role they hold
	Team    string   `json:"team,omitempty"`    // person: id of their team
	Aliases []string `json:"aliases,omitempty"` // person: other names
	Said    []string `json:"said,omitempty"`    // what was said, as stated
	Source  string   `json:"source,omitempty"`  // where it was said: URL, or "who, where, date"
}

// OrgAddOutput is the output of org_<kind>_add.
type OrgAddOutput struct {
	ID     string       `json:"id"`
	Folder model.Folder `json:"folder"`
	Rev    string       `json:"rev"`
}

// OrgListInput is the input of org_<kind>_list.
type OrgListInput struct{}

// OrgListOutput is the output of org_<kind>_list.
type OrgListOutput struct {
	Items []model.Ref `json:"items"`
}

// OrgShowInput is the input of org_<kind>_show.
type OrgShowInput struct {
	ID string `json:"id"`
}

// OrgShowOutput is the output of org_<kind>_show.
type OrgShowOutput struct {
	Doc model.Document `json:"doc"`
}

// The twelve org ops. Each kind shares one implementation.
var (
	OrgPersonAdd = define("org_person_add", "Record a person (facts only)", orgAdd(model.TypePerson))
	OrgTeamAdd   = define("org_team_add", "Record a team (facts only)", orgAdd(model.TypeTeam))
	OrgRoleAdd   = define("org_role_add", "Record a role (facts only)", orgAdd(model.TypeRole))
	OrgDomainAdd = define("org_domain_add", "Record a domain and its owner (facts only)", orgAdd(model.TypeDomain))

	OrgPersonList = define("org_person_list", "List people", orgList(model.TypePerson))
	OrgTeamList   = define("org_team_list", "List teams", orgList(model.TypeTeam))
	OrgRoleList   = define("org_role_list", "List roles", orgList(model.TypeRole))
	OrgDomainList = define("org_domain_list", "List domains", orgList(model.TypeDomain))

	OrgPersonShow = define("org_person_show", "Show a person", orgShow(model.TypePerson))
	OrgTeamShow   = define("org_team_show", "Show a team", orgShow(model.TypeTeam))
	OrgRoleShow   = define("org_role_show", "Show a role", orgShow(model.TypeRole))
	OrgDomainShow = define("org_domain_show", "Show a domain", orgShow(model.TypeDomain))
)

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// slug makes a kebab-case id part from a name.
func slug(s string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

func orgAdd(kind model.Type) Func[OrgAddInput, OrgAddOutput] {
	return func(ctx context.Context, d Deps, in OrgAddInput) (OrgAddOutput, error) {
		name := strings.Join(strings.Fields(in.Name), " ")
		if name == "" {
			return OrgAddOutput{}, errors.New("name is empty")
		}
		if len(in.Said) > 0 && strings.TrimSpace(in.Source) == "" {
			return OrgAddOutput{}, errors.New("said needs a source: who said it, where, when")
		}
		id := normID(in.ID)
		if id == "" {
			s := slug(name)
			if s == "" {
				return OrgAddOutput{}, fmt.Errorf("name %q has no usable characters: pass id", name)
			}
			id = strings.ReplaceAll(string(kind), "_", "-") + "-" + s
		}
		meta, err := orgMeta(ctx, d, kind, in)
		if err != nil {
			return OrgAddOutput{}, err
		}
		doc, err := d.Store.Create(ctx, model.Document{
			ID: id, Folder: model.FolderOrg, Title: name, Body: factsBody(in.Said, in.Source), Meta: meta,
		})
		if err != nil {
			return OrgAddOutput{}, err
		}
		return OrgAddOutput{ID: doc.ID, Folder: doc.Folder, Rev: doc.Rev}, nil
	}
}

func orgMeta(ctx context.Context, d Deps, kind model.Type, in OrgAddInput) (model.Meta, error) {
	common := model.Common{Type: kind}
	org := strings.TrimSpace(in.Org)
	if kind != model.TypeDomain && org == "" {
		return nil, errors.New("org is empty")
	}
	if kind != model.TypePerson && (in.Role != "" || in.Team != "") {
		return nil, fmt.Errorf("role and team apply to a person, not a %s", kind)
	}
	if kind != model.TypePerson && len(in.Aliases) > 0 {
		return nil, fmt.Errorf("aliases apply to a person, not a %s", kind)
	}
	if kind != model.TypeDomain && in.Owner != "" {
		return nil, fmt.Errorf("owner applies to a domain, not a %s", kind)
	}
	switch kind {
	case model.TypeDomain:
		owner, err := linkTo(ctx, d, "owner", in.Owner, model.TypeRole, model.TypeTeam, model.TypePerson)
		if err != nil {
			return nil, err
		}
		if owner == "" {
			return nil, errors.New("owner is empty: a domain names who owns it")
		}
		return &model.Domain{Common: common, Owner: owner}, nil
	case model.TypePerson:
		role, err := linkTo(ctx, d, "role", in.Role, model.TypeRole)
		if err != nil {
			return nil, err
		}
		team, err := linkTo(ctx, d, "team", in.Team, model.TypeTeam)
		if err != nil {
			return nil, err
		}
		return &model.Person{Common: common, Org: org, Role: role, Team: team, Aliases: cleanList(in.Aliases)}, nil
	case model.TypeTeam:
		return &model.Team{Common: common, Org: org}, nil
	case model.TypeRole:
		return &model.Role{Common: common, Org: org}, nil
	}
	return nil, fmt.Errorf("not an org type: %s", kind)
}

// cleanList trims each item, drops empty and repeated ones (case-insensitive),
// and returns nil for an empty result.
func cleanList(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.Join(strings.Fields(s), " ")
		if k := strings.ToLower(s); s != "" && !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	return out
}

// linkTo returns the link to an existing document of one of the given types.
// An empty id gives an empty link.
func linkTo(ctx context.Context, d Deps, field, raw string, types ...model.Type) (model.Link, error) {
	id := normID(raw)
	if id == "" {
		return "", nil
	}
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", field, id, err)
	}
	got := doc.Meta.Header().Type
	for _, t := range types {
		if got == t {
			return model.LinkTo(id), nil
		}
	}
	return "", fmt.Errorf("%s %s is a %s, want one of %v", field, id, got, types)
}

// factsBody renders sourced statements. Plain list lines, no commentary.
func factsBody(said []string, source string) string {
	if len(said) == 0 {
		return ""
	}
	src := strings.Join(strings.Fields(source), " ")
	var b strings.Builder
	b.WriteString("## Facts\n\n")
	for _, s := range said {
		fmt.Fprintf(&b, "- %s (source: %s)\n", strings.Join(strings.Fields(s), " "), src)
	}
	return strings.TrimRight(b.String(), "\n")
}

func orgList(kind model.Type) Func[OrgListInput, OrgListOutput] {
	return func(ctx context.Context, d Deps, _ OrgListInput) (OrgListOutput, error) {
		refs, err := d.Store.List(ctx, store.Filter{Folder: model.FolderOrg, Type: kind})
		if err != nil {
			return OrgListOutput{}, err
		}
		if refs == nil {
			refs = []model.Ref{}
		}
		return OrgListOutput{Items: refs}, nil
	}
}

func orgShow(kind model.Type) Func[OrgShowInput, OrgShowOutput] {
	return func(ctx context.Context, d Deps, in OrgShowInput) (OrgShowOutput, error) {
		doc, err := d.Store.Get(ctx, normID(in.ID))
		if err != nil {
			return OrgShowOutput{}, err
		}
		if got := doc.Meta.Header().Type; got != kind {
			return OrgShowOutput{}, fmt.Errorf("%s is %s, not %s", doc.ID, got, kind)
		}
		return OrgShowOutput{Doc: doc}, nil
	}
}
