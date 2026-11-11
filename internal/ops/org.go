package ops

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/paramd"
	"github.com/iodesystems/para/internal/store"
)

// The org ops record facts about who owns what: owner, role, what was said,
// and where it was said. They take no opinion field, and a "said" without a
// source is refused.

// OrgAddInput is the input of org_<kind>_add. Which fields apply depends on
// the kind: Owner for domain; Role and Team for person.
type OrgAddInput struct {
	Name    string   `json:"name"`
	Org     string   `json:"org,omitempty"`     // organization the entry belongs to (required for team, role; optional for person)
	ID      string   `json:"id,omitempty"`      // optional; default <kind>-<slug of name>
	Owner   string   `json:"owner,omitempty"`   // domain: id of the owning role, team, or person; self is the vault owner
	Role    string   `json:"role,omitempty"`    // person: id of the role they hold, or free text when no role document matches
	Team    string   `json:"team,omitempty"`    // person: id of their team
	Aliases []string `json:"aliases,omitempty"` // person: other names
	Email   string   `json:"email,omitempty"`   // person: email address, kept as the preferred email contact
	Said    []string `json:"said,omitempty"`    // what was said, as stated
	Source  string   `json:"source,omitempty"`  // where it was said: URL, or "who, where, date"
	// ImportedFrom is the file the new document comes from (import_verify).
	ImportedFrom *model.ImportedFrom `json:"imported_from,omitempty"`
}

// OrgAddOutput is the output of org_<kind>_add.
type OrgAddOutput struct {
	ID     string       `json:"id"`
	Folder model.Folder `json:"folder"`
	Rev    string       `json:"rev"`
	// Notes say what the op did with an input that was not an id.
	Notes []string `json:"notes,omitempty"`
}

// OrgListInput is the input of org_<kind>_list.
type OrgListInput struct{}

// OrgListOutput is the output of org_<kind>_list.
type OrgListOutput struct {
	Items []model.Ref `json:"items"`
}

// OrgShowInput is the input of org_<kind>_show.
type OrgShowInput struct {
	ID string `json:"id" query:"id" required:"true"`
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

	OrgPersonList = defineRead("org_person_list", "List people", orgList(model.TypePerson))
	OrgTeamList   = defineRead("org_team_list", "List teams", orgList(model.TypeTeam))
	OrgRoleList   = defineRead("org_role_list", "List roles", orgList(model.TypeRole))
	OrgDomainList = defineRead("org_domain_list", "List domains", orgList(model.TypeDomain))

	OrgPersonShow = defineRead("org_person_show", "Show a person", orgShow(model.TypePerson))
	OrgTeamShow   = defineRead("org_team_show", "Show a team", orgShow(model.TypeTeam))
	OrgRoleShow   = defineRead("org_role_show", "Show a role", orgShow(model.TypeRole))
	OrgDomainShow = defineRead("org_domain_show", "Show a domain", orgShow(model.TypeDomain))
)

// OrgPersonEditInput is the input of org_person_edit. An empty field is left
// as it is; a clear flag removes it.
type OrgPersonEditInput struct {
	ID        string `json:"id"`
	Role      string `json:"role,omitempty"` // id of the role they hold, or free text when no role document matches
	Team      string `json:"team,omitempty"` // id of their team
	Org       string `json:"org,omitempty"`  // organization they belong to
	ClearRole bool   `json:"clear_role,omitempty"`
	ClearTeam bool   `json:"clear_team,omitempty"`
	ClearOrg  bool   `json:"clear_org,omitempty"`
	// Aliases and Contacts replace the whole list when given; [] empties it.
	Aliases  *[]string        `json:"aliases,omitempty"`
	Contacts *[]model.Contact `json:"contacts,omitempty"`
}

// OrgPersonEditOutput is the output of org_person_edit.
type OrgPersonEditOutput struct {
	ID       string          `json:"id"`
	Role     string          `json:"role"`
	RoleText string          `json:"role_text"`
	Team     string          `json:"team"`
	Org      string          `json:"org"`
	Aliases  []string        `json:"aliases"`
	Contacts []model.Contact `json:"contacts"`
	Rev      string          `json:"rev"`
	// Notes say what the op did with an input that was not an id.
	Notes []string `json:"notes,omitempty"`
}

// OrgPersonEdit sets the role, team, or org of a recorded person.
var OrgPersonEdit = define("org_person_edit", "Set the role, team, org, aliases, or contacts of a recorded person (facts only); a role that matches no role document is kept as role_text. aliases and contacts replace the whole list. Groups: link rel member_of", func(ctx context.Context, d Deps, in OrgPersonEditInput) (OrgPersonEditOutput, error) {
	role, team, org := strings.TrimSpace(in.Role), strings.TrimSpace(in.Team), strings.Join(strings.Fields(in.Org), " ")
	for _, c := range []struct {
		name  string
		set   bool
		clear bool
	}{{"role", role != "", in.ClearRole}, {"team", team != "", in.ClearTeam}, {"org", org != "", in.ClearOrg}} {
		if c.set && c.clear {
			return OrgPersonEditOutput{}, fmt.Errorf("give %s or clear_%s, not both", c.name, c.name)
		}
	}
	if role == "" && team == "" && org == "" && !in.ClearRole && !in.ClearTeam && !in.ClearOrg && in.Aliases == nil && in.Contacts == nil {
		return OrgPersonEditOutput{}, errors.New("nothing to edit: give role, team, org, aliases, or contacts (or a clear flag)")
	}
	var aliases []string
	if in.Aliases != nil {
		var err error
		if aliases, err = cleanAliases(*in.Aliases); err != nil {
			return OrgPersonEditOutput{}, err
		}
	}
	var contacts []model.Contact
	if in.Contacts != nil {
		var err error
		if contacts, err = cleanContacts(*in.Contacts); err != nil {
			return OrgPersonEditOutput{}, err
		}
	}
	var notes []string
	doc, p, err := changePerson(ctx, d, in.ID, func(_ *model.Document, p *model.Person) error {
		switch {
		case in.ClearRole:
			p.Role, p.RoleText = "", ""
		case role != "":
			l, text, n, err := personRole(ctx, d, role)
			if err != nil {
				return err
			}
			p.Role, p.RoleText, notes = l, text, n
		}
		switch {
		case in.ClearTeam:
			p.Team = ""
		case team != "":
			l, err := linkTo(ctx, d, "team", team, model.TypeTeam)
			if err != nil {
				return err
			}
			p.Team = l
		}
		switch {
		case in.ClearOrg:
			p.Org = ""
		case org != "":
			p.Org = org
		}
		if in.Aliases != nil {
			p.Aliases = aliases
		}
		if in.Contacts != nil {
			p.Contacts = contacts
		}
		return nil
	})
	if err != nil {
		return OrgPersonEditOutput{}, err
	}
	as, cs := p.Aliases, p.Contacts
	if as == nil {
		as = []string{}
	}
	if cs == nil {
		cs = []model.Contact{}
	}
	return OrgPersonEditOutput{ID: doc.ID, Role: p.Role.Target(), RoleText: p.RoleText, Team: p.Team.Target(), Org: p.Org, Aliases: as, Contacts: cs, Rev: doc.Rev, Notes: notes}, nil
})

// personRole resolves a person's role: the link to a role document, or free
// text with a note when none matches. An empty raw gives no role.
func personRole(ctx context.Context, d Deps, raw string) (model.Link, string, []string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", nil, nil
	}
	role, err := linkTo(ctx, d, "role", raw, model.TypeRole)
	if errors.Is(err, model.ErrNotFound) {
		text := strings.Join(strings.Fields(raw), " ")
		return "", text, []string{fmt.Sprintf("role %q matches no role document: stored as role_text", text)}, nil
	}
	if err != nil {
		return "", "", nil, err
	}
	return role, "", nil, nil
}

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
		meta, notes, err := orgMeta(ctx, d, kind, in)
		if err != nil {
			return OrgAddOutput{}, err
		}
		// At the small types format a team, role, or domain is a group of that kind.
		doc, err := smallShape(d, model.Document{
			ID: id, Folder: model.FolderOrg, Title: name, Body: factsBody(in.Said, in.Source), Meta: meta,
		})
		if err != nil {
			return OrgAddOutput{}, err
		}
		doc, err = d.Store.Create(ctx, doc)
		if err != nil {
			return OrgAddOutput{}, err
		}
		return OrgAddOutput{ID: doc.ID, Folder: doc.Folder, Rev: doc.Rev, Notes: notes}, nil
	}
}

func orgMeta(ctx context.Context, d Deps, kind model.Type, in OrgAddInput) (model.Meta, []string, error) {
	common := model.Common{Type: kind}
	org := strings.TrimSpace(in.Org)
	if kind != model.TypeDomain && kind != model.TypePerson && org == "" {
		return nil, nil, errors.New("org is empty")
	}
	if kind != model.TypePerson && (in.Role != "" || in.Team != "") {
		return nil, nil, fmt.Errorf("role and team apply to a person, not a %s", kind)
	}
	if kind != model.TypePerson && len(in.Aliases) > 0 {
		return nil, nil, fmt.Errorf("aliases apply to a person, not a %s", kind)
	}
	if kind != model.TypePerson && strings.TrimSpace(in.Email) != "" {
		return nil, nil, fmt.Errorf("email applies to a person, not a %s", kind)
	}
	if kind != model.TypeDomain && in.Owner != "" {
		return nil, nil, fmt.Errorf("owner applies to a domain, not a %s", kind)
	}
	switch kind {
	case model.TypeDomain:
		var owner model.Link
		if o := strings.TrimSpace(in.Owner); strings.EqualFold(o, "self") {
			owner = "self"
		} else if cfg, _ := d.Store.Config("owner"); o != "" && strings.TrimSpace(cfg) != "" && strings.EqualFold(o, strings.TrimSpace(cfg)) {
			owner = "self"
		} else {
			var err error
			if owner, err = linkTo(ctx, d, "owner", in.Owner, model.TypeRole, model.TypeTeam, model.TypePerson); err != nil {
				return nil, nil, err
			}
		}
		if owner == "" {
			return nil, nil, errors.New("owner is empty: a domain names who owns it (a role, team, or person id, or self)")
		}
		return &model.Domain{Common: common, Owner: owner}, nil, nil
	case model.TypePerson:
		role, roleText, notes, err := personRole(ctx, d, in.Role)
		if err != nil {
			return nil, nil, err
		}
		team, err := linkTo(ctx, d, "team", in.Team, model.TypeTeam)
		if err != nil {
			return nil, nil, err
		}
		var contacts []model.Contact
		if strings.TrimSpace(in.Email) != "" {
			value, err := cleanValue(in.Email)
			if err != nil {
				return nil, nil, fmt.Errorf("email: %w", err)
			}
			contacts = []model.Contact{{Method: model.MediumEmail, Value: value, Preferred: true}}
		}
		return &model.Person{Common: common, Org: org, Role: role, RoleText: roleText, Team: team, Aliases: cleanList(in.Aliases), Contacts: contacts}, notes, nil
	case model.TypeTeam:
		return &model.Team{Common: common, Org: org}, nil, nil
	case model.TypeRole:
		return &model.Role{Common: common, Org: org}, nil, nil
	}
	return nil, nil, fmt.Errorf("not an org type: %s", kind)
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
		// A team, role, or domain may be the group step small-types made of it.
		if got == t || asOlderType(doc, t) {
			return model.LinkTo(id), nil
		}
	}
	return "", fmt.Errorf("%s %s is a %s, want one of %v", field, id, got, types)
}

// aboutLink links what an Ask target or path is about: a policy (or an area
// that follows a source), an exception (a file, or the file an exception
// annotation was folded from), or a domain. An annotation made after the
// fold has no file to link: name its area.
func aboutLink(ctx context.Context, d Deps, raw string) (model.Link, error) {
	id := normID(raw)
	if id == "" {
		return "", nil
	}
	x, err := newRuleIndex(ctx, d.Store)
	if err != nil {
		return "", err
	}
	doc, err := x.get(id)
	if err != nil {
		return "", fmt.Errorf("about %s: %w", id, err)
	}
	types := []model.Type{model.TypePolicy, model.TypePolicyException, model.TypeDomain}
	switch got := model.OrgKind(doc); {
	case !slices.Contains(types, got):
		return "", fmt.Errorf("about %s is a %s, want one of %v", id, got, types)
	case paramd.ValidAnnotationID(doc.ID):
		return "", fmt.Errorf("about %s is an annotation with no file of its own: give its area", id)
	}
	return model.LinkTo(doc.ID), nil
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
		refs, err := refsOf(ctx, d.Store, store.Filter{Folder: model.FolderOrg, Type: kind}) // and the groups of that kind
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
		if got := doc.Meta.Header().Type; got != kind && !asOlderType(doc, kind) {
			return OrgShowOutput{}, fmt.Errorf("%s is %s, not %s", doc.ID, got, kind)
		}
		return OrgShowOutput{Doc: doc}, nil
	}
}
