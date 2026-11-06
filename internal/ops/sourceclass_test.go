package ops

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// An older source names a product in system: it reads as class and vendor,
// and a write that does not edit system or vendor leaves the file's value.
func TestSourceOldSystemReadsAsAlias(t *testing.T) {
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.Local)
	d, root := sourceClockDeps(t, &now)
	cases := []struct {
		system model.SourceSystem
		class  model.SourceSystem
		vendor string
	}{
		{model.SourceGmail, model.SourceMail, "gmail"},
		{model.SourceGDocs, model.SourceWiki, "gdocs"},
		{model.SourceGCal, model.SourceCalendar, "gcal"},
		{model.SourceSlack, model.SourceChat, "slack"},
		{model.SourceGitHub, model.SourceTracker, "github"},
		{model.SourceJira, model.SourceTracker, "jira"},
		{model.SourceConfluence, model.SourceWiki, "confluence"},
		{model.SourceFolder, model.SourceOther, "folder"},
		{model.SourceChat, model.SourceChat, ""},
	}
	for _, c := range cases {
		id := "old-" + string(c.system)
		raw := "---\ntype: source\ncreated: 2026-11-01\nsystem: " + string(c.system) + "\nstatus: active\nvia: none\n---\n\n# Old " + string(c.system) + "\n"
		if err := os.WriteFile(filepath.Join(root, "sources", id+".md"), []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		doc, m, err := getSource(ctx, d.Store, id)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if k := sourceKindOf(m); k.class != c.class || k.vendor != c.vendor {
			t.Errorf("%s: kind %+v, want %s/%s", id, k, c.class, c.vendor)
		}
		if m.System != c.system {
			t.Errorf("%s: system decoded as %s", id, m.System)
		}
		row := sourceView(doc, nil, now, nil, "2026-11-06")
		if row.System != c.class || row.Vendor != c.vendor {
			t.Errorf("%s: row %s/%s", id, row.System, row.Vendor)
		}
	}
	// A status change is not an edit of the kind: the file keeps gmail.
	must(t, SourceStatus, d, SourceStatusInput{ID: "old-gmail", Status: model.SourcePaused})
	if b, _ := os.ReadFile(filepath.Join(root, "sources", "old-gmail.md")); !strings.Contains(string(b), "system: gmail") || strings.Contains(string(b), "vendor:") {
		t.Errorf("status change rewrote the kind:\n%s", b)
	}
	// via.source "gmail" still finds the old source; "mail" finds it by class.
	all, _ := loadSources(ctx, d.Store)
	res := newSourceResolver(all)
	if got := res.resolve("gmail"); got != "old-gmail" {
		t.Errorf("resolve gmail = %q", got)
	}
	if got := res.resolve("mail"); got != "old-gmail" {
		t.Errorf("resolve mail = %q", got)
	}
	if got := res.resolve("tracker"); got != "" {
		t.Errorf("resolve tracker (two sources) = %q", got)
	}
	// doc_edit of the vendor writes the new form.
	must(t, DocEdit, d, DocEditInput{ID: "old-slack", Fields: map[string]any{"vendor": "slack"}})
	if b, _ := os.ReadFile(filepath.Join(root, "sources", "old-slack.md")); !strings.Contains(string(b), "system: chat") || !strings.Contains(string(b), "vendor: slack") {
		t.Errorf("edited source not in the new form:\n%s", b)
	}
}

// source_add writes class and vendor, from an older system value too, and
// refuses a priority that is not one.
func TestSourceAddWritesClassAndVendor(t *testing.T) {
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.Local)
	d, root := sourceClockDeps(t, &now)
	must(t, SourceAdd, d, SourceAddInput{ID: "src-a", Title: "A", System: model.SourceJira})
	must(t, SourceAdd, d, SourceAddInput{ID: "src-b", Title: "B", System: model.SourceMail, Vendor: " Outlook ", Priority: model.PriorityHigh})
	must(t, SourceAdd, d, SourceAddInput{ID: "src-c", Title: "C", Example: model.SourceGmail})
	for id, want := range map[string][]string{
		"src-a": {"system: tracker", "vendor: jira"},
		"src-b": {"system: mail", "vendor: outlook", "priority: high"},
		"src-c": {"system: mail", "vendor: gmail"},
	} {
		b, _ := os.ReadFile(filepath.Join(root, "sources", id+".md"))
		for _, w := range want {
			if !strings.Contains(string(b), w) {
				t.Errorf("%s: no %q in\n%s", id, w, b)
			}
		}
	}
	if _, err := SourceAdd.Run(ctx, d, SourceAddInput{Title: "D", System: model.SourceChat, Priority: "urgent"}); err == nil {
		t.Error("priority urgent was taken")
	}
	if _, err := SourceAdd.Run(ctx, d, SourceAddInput{Title: "E", System: "pager"}); err == nil {
		t.Error("system pager was taken")
	}
}

// The medium of a source goes through the one table: by class, else by
// vendor, else none.
func TestMediumOfSource(t *testing.T) {
	for _, c := range []struct {
		m    model.SourceDoc
		want model.Medium
	}{
		{model.SourceDoc{System: model.SourceGmail}, model.MediumEmail},
		{model.SourceDoc{System: model.SourceMail, Vendor: "outlook"}, model.MediumEmail},
		{model.SourceDoc{System: model.SourceMail}, model.MediumEmail},
		{model.SourceDoc{System: model.SourceSlack}, model.MediumSlack},
		{model.SourceDoc{System: model.SourceChat, Vendor: "teams"}, model.MediumTeams},
		{model.SourceDoc{System: model.SourceChat, Vendor: "Teams"}, model.MediumTeams},
		{model.SourceDoc{System: model.SourceChat, Vendor: "matrix"}, ""},
		{model.SourceDoc{System: model.SourceJira}, model.MediumJira},
		{model.SourceDoc{System: model.SourceTracker, Vendor: "linear"}, ""},
		{model.SourceDoc{System: model.SourceConfluence}, model.MediumConfluence},
		{model.SourceDoc{System: model.SourceGitHub}, model.MediumGithub},
		{model.SourceDoc{System: model.SourceGCal}, model.MediumMeeting},
		{model.SourceDoc{System: model.SourceCalendar, Vendor: "outlook"}, model.MediumMeeting},
		{model.SourceDoc{System: model.SourceFolder}, ""},
		{model.SourceDoc{System: model.SourceOther}, ""},
	} {
		if got := mediumOfSource(&c.m); got != c.want {
			t.Errorf("%s/%s: medium %q, want %q", c.m.System, c.m.Vendor, got, c.want)
		}
	}
	if got := docSystemOfSource(&model.SourceDoc{System: model.SourceGDocs}); got != model.SystemGDoc {
		t.Errorf("gdocs doc system %q", got)
	}
	if got := docSystemOfSource(&model.SourceDoc{System: model.SourceWiki, Vendor: "notion"}); got != "" {
		t.Errorf("notion doc system %q", got)
	}
}

// vendorNames are product names that may appear in internal/ops only in the
// mapping table and the export renderers.
var vendorNames = regexp.MustCompile(`(?i)\b(gmail|outlook|slack|jira|linear|confluence|github|gdocs?|gcal|google|microsoft|teams message)\b`)

// vendorAllowed are the files that may name vendors: the one mapping table,
// and the export renderers (paste formats named after a product).
var vendorAllowed = map[string]bool{"sourceclass.go": true, "copy.go": true, "export.go": true}

// vendorIdent matches a model constant named after a product.
var vendorIdent = regexp.MustCompile(`^(Source|System|Medium)(Gmail|GDocs|GCal|Slack|GitHub|Github|Jira|Confluence|Teams)$`)

// No vendor name is in internal/ops code but the mapping table: not in a
// string, and not as a model constant named after a product. Comments may
// name them.
func TestNoVendorNamesOutsideTheTable(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || vendorAllowed[name] {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.ImportSpec:
				return false
			case *ast.BasicLit:
				if x.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(x.Value)
				if err != nil {
					s = x.Value
				}
				if m := vendorNames.FindString(s); m != "" {
					t.Errorf("%s: vendor name %q in a string; put it in the mapping table (sourceclass.go) or in vault data", fset.Position(x.Pos()), m)
				}
			case *ast.SelectorExpr:
				if vendorIdent.MatchString(x.Sel.Name) {
					t.Errorf("%s: %s names a vendor; go through the mapping table (sourceclass.go)", fset.Position(x.Pos()), x.Sel.Name)
				}
			}
			return true
		})
	}
}

// A person takes their own priority, else the highest priority of their
// groups; priority_set sets and clears it, from a person surface only.
func TestPriorityPropagates(t *testing.T) {
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.Local)
	d, _ := sourceClockDeps(t, &now)
	must(t, OrgGroupAdd, d, GroupAddInput{Name: "Leads", Org: "acme.example", Kind: model.GroupTeam})
	must(t, OrgGroupAdd, d, GroupAddInput{Name: "Lists", Org: "acme.example", Kind: model.GroupList})
	must(t, OrgPersonAdd, d, OrgAddInput{Name: "Ada Example"})
	must(t, OrgPersonGroupAdd, d, PersonGroupInput{ID: "person-ada-example", Group: "group-leads"})
	must(t, OrgPersonGroupAdd, d, PersonGroupInput{ID: "person-ada-example", Group: "group-lists"})
	get := func(id string) (model.Document, error) { return d.Store.Get(ctx, id) }
	prio := func() (model.Priority, string) {
		doc, err := get("person-ada-example")
		if err != nil {
			t.Fatal(err)
		}
		return partyPriority(get, doc)
	}
	if p, from := prio(); p != "" || from != "" {
		t.Fatalf("unset: %q from %q", p, from)
	}
	must(t, PrioritySet, d, PrioritySetInput{ID: "group-lists", Priority: model.PriorityLow})
	must(t, PrioritySet, d, PrioritySetInput{ID: "group-leads", Priority: model.PriorityHigh})
	if p, from := prio(); p != model.PriorityHigh || from != "group-leads" {
		t.Fatalf("from groups: %q from %q", p, from)
	}
	must(t, PrioritySet, d, PrioritySetInput{ID: "person-ada-example", Priority: model.PriorityNormal})
	if p, from := prio(); p != model.PriorityNormal || from != "person-ada-example" {
		t.Fatalf("own: %q from %q", p, from)
	}
	must(t, PrioritySet, d, PrioritySetInput{ID: "person-ada-example"})
	if p, _ := prio(); p != model.PriorityHigh {
		t.Fatalf("cleared: %q", p)
	}
	if _, err := PrioritySet.Run(ctx, d, PrioritySetInput{ID: "person-ada-example", Priority: "urgent"}); err == nil {
		t.Error("priority urgent was taken")
	}
	if _, err := PrioritySet.Run(ctx, agentDeps(d, "claude"), PrioritySetInput{ID: "group-leads", Priority: model.PriorityLow}); err == nil {
		t.Error("an agent set a priority with priority_set")
	}
}
