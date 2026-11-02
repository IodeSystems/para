package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// The directory ops record who people are and how to reach them: contacts,
// groups, and working notes. Membership lives on the person only.

// GroupAddInput is the input of org_group_add.
type GroupAddInput struct {
	Name string          `json:"name"`
	Org  string          `json:"org"`
	Kind model.GroupKind `json:"kind"`
	ID   string          `json:"id,omitempty"` // default group-<slug of name>
}

// GroupShowOutput is the output of org_group_show.
type GroupShowOutput struct {
	Doc     model.Document `json:"doc"`
	Members []model.Ref    `json:"members"` // people that list this group
}

// The group ops.
var (
	OrgGroupAdd = define("org_group_add", "Record a group: team, department, channel, or list", orgGroupAdd)

	OrgGroupList = defineRead("org_group_list", "List groups", orgList(model.TypeGroup))

	OrgGroupShow = defineRead("org_group_show", "Show a group and its members", func(ctx context.Context, d Deps, in OrgShowInput) (GroupShowOutput, error) {
		doc, err := d.Store.Get(ctx, normID(in.ID))
		if err != nil {
			return GroupShowOutput{}, err
		}
		if doc.Meta.Header().Type != model.TypeGroup {
			return GroupShowOutput{}, fmt.Errorf("%s is %s, not group", doc.ID, doc.Meta.Header().Type)
		}
		out := GroupShowOutput{Doc: doc, Members: []model.Ref{}}
		refs, err := d.Store.List(ctx, store.Filter{Type: model.TypePerson})
		if err != nil {
			return GroupShowOutput{}, err
		}
		for _, r := range refs {
			_, p, err := getPerson(ctx, d, r.ID)
			if err != nil {
				return GroupShowOutput{}, err
			}
			if slices.Contains(targets(p.Groups), doc.ID) {
				out.Members = append(out.Members, r)
			}
		}
		return out, nil
	})
)

func orgGroupAdd(ctx context.Context, d Deps, in GroupAddInput) (OrgAddOutput, error) {
	name := strings.Join(strings.Fields(in.Name), " ")
	if name == "" {
		return OrgAddOutput{}, errors.New("name is empty")
	}
	org := strings.TrimSpace(in.Org)
	if org == "" {
		return OrgAddOutput{}, errors.New("org is empty")
	}
	if !slices.Contains(model.GroupKinds, in.Kind) {
		return OrgAddOutput{}, fmt.Errorf("kind %q: want one of %v", in.Kind, model.GroupKinds)
	}
	id := normID(in.ID)
	if id == "" {
		s := slug(name)
		if s == "" {
			return OrgAddOutput{}, fmt.Errorf("name %q has no usable characters: pass id", name)
		}
		id = "group-" + s
	}
	doc, err := d.Store.Create(ctx, model.Document{
		ID: id, Folder: model.FolderOrg, Title: name,
		Meta: &model.Group{Common: model.Common{Type: model.TypeGroup}, Org: org, Kind: in.Kind},
	})
	if err != nil {
		return OrgAddOutput{}, err
	}
	return OrgAddOutput{ID: doc.ID, Folder: doc.Folder, Rev: doc.Rev}, nil
}

// getPerson reads a person.
func getPerson(ctx context.Context, d Deps, id string) (model.Document, *model.Person, error) {
	if id == "" {
		return model.Document{}, nil, errors.New("id is empty")
	}
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return doc, nil, err
	}
	p, ok := doc.Meta.(*model.Person)
	if !ok {
		return doc, nil, fmt.Errorf("%s is %s, not person", id, doc.Meta.Header().Type)
	}
	return doc, p, nil
}

// changePerson reads a person, applies change, and writes the result.
func changePerson(ctx context.Context, d Deps, id string, change func(doc *model.Document, p *model.Person) error) (model.Document, *model.Person, error) {
	doc, p, err := getPerson(ctx, d, normID(id))
	if err != nil {
		return doc, nil, err
	}
	if err := change(&doc, p); err != nil {
		return doc, nil, err
	}
	doc, err = d.Store.Update(ctx, doc)
	if err != nil {
		return doc, nil, err
	}
	return doc, doc.Meta.(*model.Person), nil
}

// PersonContactInput is the input of org_person_contact_add and _remove.
type PersonContactInput struct {
	ID        string       `json:"id"`
	Method    model.Medium `json:"method"`
	Value     string       `json:"value"`
	Label     string       `json:"label,omitempty"`     // add only
	Preferred bool         `json:"preferred,omitempty"` // add only; replaces the preferred contact of the method
}

// PersonContactOutput is the output of the contact ops.
type PersonContactOutput struct {
	ID       string          `json:"id"`
	Contacts []model.Contact `json:"contacts"`
	Rev      string          `json:"rev"`
}

// The contact ops.
var (
	OrgPersonContactAdd = define("org_person_contact_add", "Add a way to reach a person", func(ctx context.Context, d Deps, in PersonContactInput) (PersonContactOutput, error) {
		value, err := cleanValue(in.Value)
		if err != nil {
			return PersonContactOutput{}, err
		}
		if err := checkMedium(in.Method); err != nil {
			return PersonContactOutput{}, err
		}
		doc, p, err := changePerson(ctx, d, in.ID, func(_ *model.Document, p *model.Person) error {
			if indexContact(p.Contacts, in.Method, value) >= 0 {
				return fmt.Errorf("contact %s %s already recorded", in.Method, value)
			}
			if in.Preferred {
				for i := range p.Contacts {
					if p.Contacts[i].Method == in.Method {
						p.Contacts[i].Preferred = false
					}
				}
			}
			p.Contacts = append(p.Contacts, model.Contact{
				Method: in.Method, Value: value, Label: strings.Join(strings.Fields(in.Label), " "), Preferred: in.Preferred,
			})
			return nil
		})
		return contactOut(doc, p, err)
	})

	OrgPersonContactRemove = define("org_person_contact_remove", "Remove a way to reach a person", func(ctx context.Context, d Deps, in PersonContactInput) (PersonContactOutput, error) {
		value, err := cleanValue(in.Value)
		if err != nil {
			return PersonContactOutput{}, err
		}
		doc, p, err := changePerson(ctx, d, in.ID, func(_ *model.Document, p *model.Person) error {
			i := indexContact(p.Contacts, in.Method, value)
			if i < 0 {
				return fmt.Errorf("no contact %s %s", in.Method, value)
			}
			p.Contacts = slices.Delete(p.Contacts, i, i+1)
			return nil
		})
		return contactOut(doc, p, err)
	})
)

func contactOut(doc model.Document, p *model.Person, err error) (PersonContactOutput, error) {
	if err != nil {
		return PersonContactOutput{}, err
	}
	cs := p.Contacts
	if cs == nil {
		cs = []model.Contact{}
	}
	return PersonContactOutput{ID: doc.ID, Contacts: cs, Rev: doc.Rev}, nil
}

func indexContact(cs []model.Contact, m model.Medium, value string) int {
	return slices.IndexFunc(cs, func(c model.Contact) bool {
		return c.Method == m && strings.EqualFold(c.Value, value)
	})
}

func cleanValue(s string) (string, error) {
	v := strings.Join(strings.Fields(s), " ")
	if v == "" {
		return "", errors.New("value is empty")
	}
	return v, nil
}

func checkMedium(m model.Medium) error {
	if !slices.Contains(model.Mediums, m) {
		return fmt.Errorf("medium %q: want one of %v", m, model.Mediums)
	}
	return nil
}

// preferredValue is the address to use for a person by a medium: the
// preferred contact of that medium, else the only contact of that medium.
func preferredValue(p *model.Person, m model.Medium) string {
	var of []model.Contact
	for _, c := range p.Contacts {
		if c.Method == m {
			if c.Preferred {
				return c.Value
			}
			of = append(of, c)
		}
	}
	if len(of) == 1 {
		return of[0].Value
	}
	return ""
}

// PersonGroupInput is the input of org_person_group_add and _remove.
type PersonGroupInput struct {
	ID    string `json:"id"`
	Group string `json:"group"`
}

// PersonGroupOutput is the output of the group membership ops.
type PersonGroupOutput struct {
	ID     string   `json:"id"`
	Groups []string `json:"groups"`
	Rev    string   `json:"rev"`
}

// The membership ops. Adding a member twice does nothing.
var (
	OrgPersonGroupAdd = define("org_person_group_add", "Put a person in a group", func(ctx context.Context, d Deps, in PersonGroupInput) (PersonGroupOutput, error) {
		link, err := linkTo(ctx, d, "group", in.Group, model.TypeGroup)
		if err != nil {
			return PersonGroupOutput{}, err
		}
		if link == "" {
			return PersonGroupOutput{}, errors.New("group is empty")
		}
		return groupOut(changePerson(ctx, d, in.ID, func(_ *model.Document, p *model.Person) error {
			if !slices.Contains(p.Groups, link) {
				p.Groups = append(p.Groups, link)
			}
			return nil
		}))
	})

	OrgPersonGroupRemove = define("org_person_group_remove", "Take a person out of a group", func(ctx context.Context, d Deps, in PersonGroupInput) (PersonGroupOutput, error) {
		link := model.LinkTo(normID(in.Group))
		return groupOut(changePerson(ctx, d, in.ID, func(_ *model.Document, p *model.Person) error {
			i := slices.Index(p.Groups, link)
			if i < 0 {
				return fmt.Errorf("not in group %s", link.Target())
			}
			p.Groups = slices.Delete(p.Groups, i, i+1)
			return nil
		}))
	})
)

func groupOut(doc model.Document, p *model.Person, err error) (PersonGroupOutput, error) {
	if err != nil {
		return PersonGroupOutput{}, err
	}
	return PersonGroupOutput{ID: doc.ID, Groups: targets(p.Groups), Rev: doc.Rev}, nil
}

// notesHeading is the body section for working notes.
const notesHeading = "## Notes"

// PersonNoteInput is the input of org_person_note.
type PersonNoteInput struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Date string `json:"date,omitempty"` // YYYY-MM-DD; empty means the local date
}

// PersonNoteOutput is the output of org_person_note.
type PersonNoteOutput struct {
	ID   string `json:"id"`
	Line string `json:"line"` // the line appended
	Rev  string `json:"rev"`
}

// OrgPersonNote appends a dated line under "## Notes" in the person's body.
// Notes are for working well with someone. Para does not filter their content.
var OrgPersonNote = defineSafe("org_person_note", "Add a dated working note about a person", func(ctx context.Context, d Deps, in PersonNoteInput) (PersonNoteOutput, error) {
	text := strings.Join(strings.Fields(in.Text), " ")
	if text == "" {
		return PersonNoteOutput{}, errors.New("text is empty")
	}
	date, err := dateOrToday(in.Date)
	if err != nil {
		return PersonNoteOutput{}, err
	}
	line := "- " + date + ": " + text
	doc, _, err := changePerson(ctx, d, in.ID, func(doc *model.Document, _ *model.Person) error {
		doc.Body = appendToSection(doc.Body, notesHeading, line)
		return nil
	})
	if err != nil {
		return PersonNoteOutput{}, err
	}
	return PersonNoteOutput{ID: doc.ID, Line: line, Rev: doc.Rev}, nil
})

func dateOrToday(s string) (string, error) {
	if s == "" {
		return time.Now().Format(dateLayout), nil
	}
	if err := optionalDate(s); err != nil {
		return "", fmt.Errorf("date: %w", err)
	}
	return s, nil
}

// appendToSection adds line at the end of the section that starts with
// heading, or a new section at the end of body.
func appendToSection(body, heading, line string) string {
	body = strings.Trim(body, "\n")
	lines := strings.Split(body, "\n")
	at := slices.IndexFunc(lines, func(l string) bool { return strings.TrimRight(l, " \t") == heading })
	if body == "" {
		return heading + "\n\n" + line
	}
	if at < 0 {
		return body + "\n\n" + heading + "\n\n" + line
	}
	end := len(lines)
	for i := at + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "# ") || strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	sect := lines[at+1 : end]
	for len(sect) > 0 && strings.TrimSpace(sect[len(sect)-1]) == "" {
		sect = sect[:len(sect)-1]
	}
	if len(sect) == 0 {
		sect = []string{""}
	}
	out := append([]string(nil), lines[:at+1]...)
	out = append(out, sect...)
	out = append(out, line)
	if end < len(lines) {
		out = append(out, "")
		out = append(out, lines[end:]...)
	}
	return strings.Join(out, "\n")
}

// sectionLines returns the non-blank lines of the section under heading.
func sectionLines(body, heading string) []string {
	out := []string{}
	in := false
	for _, l := range strings.Split(body, "\n") {
		switch {
		case strings.TrimRight(l, " \t") == heading:
			in = true
		case in && (strings.HasPrefix(l, "# ") || strings.HasPrefix(l, "## ")):
			return out
		case in && strings.TrimSpace(l) != "":
			out = append(out, strings.TrimSpace(l))
		}
	}
	return out
}
