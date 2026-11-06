package upgrade

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/iodesystems/para/internal/model"
)

// DossierFormat is the format at which dossiers hold no open item: the
// review stack is the needs-you list, and a dossier is a read-only record.
const DossierFormat = 8

// Step dossiers moves what an open dossier holds to vault items, and closes
// the dossier as superseded once nothing is left in it:
//
//	question            a vault question (context and options from the
//	                    dossier, D9); with for, the vault question it names
//	message with io     its io draft, which exists
//	message, action,    a new outgoing io draft: medium from the subtype,
//	doc edit            the action's text as the body
//	change, upstream    staged: the judgement part, an agent re-files it as
//	                    a direct write or drops it (dossier_refile)
//
// An item put off keeps its date on the new item (later until); an item
// that runs after another, or was held by one, waits on where that one went
// (later after). An approval in the dossier is not carried: the owner
// approves the draft again (io_approve). Each moved item stays in its block
// with state moved and result, and the dossier lists where each went.

// itemOpen reports whether a dossier item still waits on someone: the owner,
// an agent, or its turn.
func itemOpen(it model.ProposalItem) bool {
	switch it.State {
	case model.ItemPending, model.ItemHeld, model.ItemSkipped, model.ItemSentBack, model.ItemRunning, model.ItemClaimed, model.ItemHalted, "":
		return true
	case model.ItemApproved:
		return it.Kind.Outward() || it.Waits
	}
	return false
}

// Where an open item goes.
const (
	MoveQuestion = "question"       // a new vault question
	MoveLinkQ    = "question (for)" // the vault question its for names
	MoveDraft    = "io draft"       // a new outgoing io draft
	MoveLinkIO   = "io draft (io)"  // the io draft its io names
	MoveStaged   = "staged"         // the judgement part
)

// moveOf is where an open item goes.
func moveOf(it model.ProposalItem) string {
	switch it.Kind {
	case model.ItemQuestion:
		if linkID(it.For) != "" {
			return MoveLinkQ
		}
		return MoveQuestion
	case model.ItemMessage:
		if linkID(it.IO) != "" {
			return MoveLinkIO
		}
		return MoveDraft
	case model.ItemAction, model.ItemExternal, model.ItemDocEdit:
		return MoveDraft
	}
	return MoveStaged
}

func linkID(s string) string { return model.Link(strings.TrimSpace(s)).Target() }

// openDossier reads a proposal that is not superseded or closed, as a
// dossier (an older one-block body is read as one). Anything else is nil.
func openDossier(d model.Document) (*model.Proposal, *model.Dossier) {
	m, ok := d.Meta.(*model.Proposal)
	if !ok || m.Status == model.ProposalSuperseded || m.Status == model.ProposalClosed {
		return nil, nil
	}
	if model.IsDossier(d.Body) {
		dz, err := model.ParseDossier(d.Body)
		if err != nil {
			return nil, nil
		}
		return m, dz
	}
	c, err := model.ParseProposalBody(d.Body)
	if err != nil {
		return nil, nil
	}
	return m, model.NewDossier(model.DossierHeader{TLDR: cmpOr(c.TLDR, d.Title), Context: c.Context}, "Items", c.Items)
}

func cmpOr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// countOpen counts a dossier's open items by where they go.
func countOpen(d model.Document) map[string]int {
	_, dz := openDossier(d)
	out := map[string]int{}
	if dz == nil {
		return out
	}
	for _, it := range dz.Items {
		if itemOpen(it) {
			out[moveOf(it)]++
		}
	}
	return out
}

// DossierMovable selects an open dossier with an open item the automated
// part moves, or with no open item at all (it only closes).
func DossierMovable(d model.Document) bool {
	if _, dz := openDossier(d); dz == nil {
		return false
	}
	c := countOpen(d)
	return c[MoveQuestion]+c[MoveLinkQ]+c[MoveDraft]+c[MoveLinkIO] > 0 || c[MoveStaged] == 0
}

// DossierStaged selects an open dossier with an open staged change: the
// judgement part.
func DossierStaged(d model.Document) bool { return countOpen(d)[MoveStaged] > 0 }

// DossierCounts counts the open items of a dossier by where they go: the
// count test of step dossiers.
func DossierCounts(d model.Document) map[string]int { return countOpen(d) }

// movedID is the id of the item made from a dossier item: the type's word,
// the dossier id without its prefix, and the item id. It is the same on
// every run, so a dry run names what a run makes.
func movedID(prefix string, dossier, item string) string {
	return prefix + "-" + strings.TrimPrefix(dossier, "proposal-") + "-" + item
}

// explainDossier is the plan of step dossiers for one dossier.
func explainDossier(d model.Document) []string {
	_, dz := openDossier(d)
	if dz == nil {
		return nil
	}
	var out []string
	for _, it := range dz.Items {
		if !itemOpen(it) {
			continue
		}
		switch moveOf(it) {
		case MoveQuestion:
			out = append(out, fmt.Sprintf("%s question: new question %s", it.ID, movedID("question", d.ID, it.ID)))
		case MoveLinkQ:
			out = append(out, fmt.Sprintf("%s question: the vault question %s (for)", it.ID, linkID(it.For)))
		case MoveDraft:
			out = append(out, fmt.Sprintf("%s %s: new outgoing io draft %s (approve it again)", it.ID, it.Kind, movedID("io", d.ID, it.ID)))
		case MoveLinkIO:
			out = append(out, fmt.Sprintf("%s message: its io draft %s (approve it again if it was approved here)", it.ID, linkID(it.IO)))
		case MoveStaged:
			out = append(out, fmt.Sprintf("%s %s %s: staged, for an agent to re-file as a direct write or drop (judgement)", it.ID, model.BlockKind(it.Kind), it.Op))
		}
	}
	if countOpen(d)[MoveStaged] == 0 {
		out = append(out, "the dossier closes as superseded")
	}
	return out
}

// expandDossier is the automated part of step dossiers for one dossier.
func expandDossier(d model.Document) (model.Document, []model.Document, error) {
	m, dz := openDossier(d)
	if dz == nil {
		return d, nil, errors.New("not an open dossier that reads")
	}
	// Where each open item goes, first, so after can name it.
	dest := map[string]string{}
	for _, it := range dz.Items {
		if !itemOpen(it) {
			continue
		}
		switch moveOf(it) {
		case MoveQuestion:
			dest[it.ID] = movedID("question", d.ID, it.ID)
		case MoveLinkQ:
			dest[it.ID] = linkID(it.For)
		case MoveDraft:
			dest[it.ID] = movedID("io", d.ID, it.ID)
		case MoveLinkIO:
			dest[it.ID] = linkID(it.IO)
		case MoveStaged:
			dest[it.ID] = d.ID // waits while the dossier is open
		}
	}
	var made []model.Document
	for i := range dz.Items {
		it := &dz.Items[i]
		if !itemOpen(*it) {
			continue
		}
		mv := moveOf(*it)
		if mv == MoveStaged {
			continue
		}
		later := laterOf(*it, dest)
		tags := model.ItemTags(m.Tags, *it)
		switch mv {
		case MoveQuestion:
			made = append(made, questionOf(d, dz, *it, later, tags))
		case MoveDraft:
			made = append(made, draftOf(d, *it, later, tags))
		}
		it.State, it.Result = model.ItemMoved, dest[it.ID]
		it.Reason = "moved to " + dest[it.ID] + " by vault upgrade (dossiers)"
		it.ClaimedBy, it.LeaseUntil = "", ""
	}
	nd, err := renderDossier(d, dz)
	return nd, made, err
}

// laterOf is the deferral the new item keeps: the item's date, and what it
// waits on (after, or held_by in an older file).
func laterOf(it model.ProposalItem, dest map[string]string) model.Deferral {
	l := model.Deferral{DeferUntil: it.DeferUntil, DeferredReason: it.DeferredReason}
	if id := dest[it.After]; it.After != "" && id != "" {
		l.DeferredAfter = append(l.DeferredAfter, id)
	}
	if id := it.HeldBy.Target(); id != "" && !slices.Contains(l.DeferredAfter, id) {
		l.DeferredAfter = append(l.DeferredAfter, id)
	}
	if len(l.DeferredAfter) > 0 && l.DeferredReason == "" {
		l.DeferredReason = "waits on " + strings.Join(l.DeferredAfter, ", ") + " (from its dossier)"
	}
	return l
}

func questionOf(d model.Document, dz *model.Dossier, it model.ProposalItem, later model.Deferral, tags []string) model.Document {
	q := &model.Question{Common: model.Common{Type: model.TypeQuestion, Tags: tags}, Status: model.QuestionOpen, Scope: model.ScopeRoot,
		Options: it.Options, Recommended: it.Recommended, OptionDetails: it.OptionDetails, Deferral: later}
	if len(it.Options) > 0 {
		q.Kind = model.KindDecision
	}
	q.Context = joinParas(dz.Header.TLDR, dz.Header.Context, dz.Narrative(it.ID))
	var body []string
	body = append(body, strings.TrimSpace(it.Body))
	body = append(body, evidenceLine(it.Evidence))
	if a := it.Answer; !a.Empty() {
		var parts []string
		if len(a.Choices) > 0 {
			parts = append(parts, "choices: "+strings.Join(a.Choices, ", "))
		}
		if len(a.AddedOptions) > 0 {
			parts = append(parts, "added options: "+strings.Join(a.AddedOptions, ", "))
		}
		if w := strings.TrimSpace(a.OwnWords); w != "" {
			parts = append(parts, "own words: "+w)
		}
		if c := strings.TrimSpace(a.Comment); c != "" {
			parts = append(parts, "comment: "+c)
		}
		if len(parts) > 0 {
			body = append(body, "The owner's draft answer in the dossier, not submitted: "+strings.Join(parts, "; ")+".")
		}
	}
	body = append(body, fmt.Sprintf("Moved from dossier [[%s]], item %s, by vault upgrade.", d.ID, it.ID))
	return model.Document{ID: movedID("question", d.ID, it.ID), Folder: model.FolderQuestions, Title: oneLine(it.Title, 200),
		Body: joinParas(body...), Meta: q}
}

func draftOf(d model.Document, it model.ProposalItem, later model.Deferral, tags []string) model.Document {
	m := &model.IO{Common: model.Common{Type: model.TypeIO, Tags: tags}, Direction: model.IOOut, Medium: mediumOf(it), State: model.IODraft,
		Expects: model.ExpectsNone, Links: []model.Link{model.LinkTo(d.ID)}, Deferral: later}
	var text string
	switch it.Kind {
	case model.ItemMessage:
		m.To = append([]string(nil), it.People...)
		if c := strings.TrimSpace(it.Channel); c != "" {
			m.To = append(m.To, c)
		}
		m.Subject, text = oneLine(it.Subject, 0), strings.TrimSpace(it.Body)
	case model.ItemDocEdit:
		m.To = nonEmpty(it.Target)
		m.Subject = oneLine("Edit "+it.Target, 0)
		text = joinParas(labelled("Range", it.Range), labelled("Old", it.Old), labelled("New", it.New))
	default: // action
		m.To = nonEmpty(cmpOr(it.Target, it.Previous))
		m.Subject = oneLine(cmpOr(it.Title, it.What), 0)
		diff := ""
		if strings.TrimSpace(it.Diff) != "" {
			diff = "```diff\n" + strings.TrimRight(it.Diff, "\n") + "\n```"
		}
		text = joinParas(labelled("What", it.What), labelled("How", it.How), labelled("Previous", it.Previous), strings.TrimSpace(it.Body), diff)
	}
	if text == "" {
		text = cmpOr(m.Subject, it.ID)
	}
	return model.Document{ID: movedID("io", d.ID, it.ID), Folder: model.FolderIO, Title: ioTitle(m.Subject, text), Body: text, Meta: m}
}

// mediumOf reads the item's free-text subtype as an io medium.
func mediumOf(it model.ProposalItem) model.Medium {
	s := strings.ToLower(strings.TrimSpace(it.Subtype))
	for _, md := range model.Mediums {
		if s == string(md) {
			return md
		}
	}
	switch {
	case s == "pr" || s == "issue" || s == "github issue" || strings.Contains(s, "pull"):
		return model.MediumGithub
	case s == "ticket":
		return model.MediumJira
	case s == "mail":
		return model.MediumEmail
	case it.Kind == model.ItemDocEdit && strings.Contains(strings.ToLower(it.Target), "confluence"):
		return model.MediumConfluence
	}
	return model.MediumOther
}

func nonEmpty(s string) []string {
	if s = strings.TrimSpace(s); s == "" {
		return nil
	}
	return []string{s}
}

func labelled(label, text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	if strings.Contains(strings.TrimSpace(text), "\n") {
		return label + ":\n\n" + strings.TrimRight(text, "\n")
	}
	return label + ": " + strings.TrimSpace(text)
}

func evidenceLine(e model.Evidence) string {
	q, l := strings.TrimSpace(e.Quote), strings.TrimSpace(e.Locator)
	switch {
	case q == "" && l == "":
		return ""
	case q == "":
		return "Evidence: " + l
	}
	return fmt.Sprintf("Evidence: %q (%s)", q, cmpOr(l, "no locator"))
}

func joinParas(ps ...string) string {
	var out []string
	for _, p := range ps {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "\n\n")
}

// oneLine joins the white space of s; max > 0 cuts it to max runes.
func oneLine(s string, max int) string {
	t := strings.Join(strings.Fields(s), " ")
	if max > 0 && utf8.RuneCountInString(t) > max {
		t = string([]rune(t)[:max]) + "..."
	}
	return t
}

// ioTitle is ops' title of an io item: the subject, or the first line.
func ioTitle(subject, text string) string {
	if t := oneLine(subject, 80); t != "" {
		return t
	}
	first, _, _ := strings.Cut(text, "\n")
	return oneLine(first, 80)
}

// whereHeading opens the section that lists where a dossier's items went.
const whereHeading = "## Where the items went"

// renderDossier writes the dossier back with the list of where its items
// went, and closes it as superseded when no item is open.
func renderDossier(d model.Document, dz *model.Dossier) (model.Document, error) {
	body, err := dz.Render()
	if err != nil {
		return d, err
	}
	if i := strings.Index(body, "\n"+whereHeading+"\n"); i >= 0 {
		body = strings.TrimRight(body[:i], "\n")
	}
	var lines []string
	open := false
	for _, it := range dz.Items {
		open = open || itemOpen(it)
		switch {
		case it.State == model.ItemMoved && it.Result != "":
			lines = append(lines, fmt.Sprintf("- %s: [[%s]]", it.ID, it.Result))
		case it.State == model.ItemDone && it.Reason == refiledPrefix:
			lines = append(lines, fmt.Sprintf("- %s: re-filed as [[%s]]", it.ID, it.Result))
		case it.State == model.ItemWithdrawn && strings.HasPrefix(it.Reason, droppedPrefix):
			lines = append(lines, fmt.Sprintf("- %s: dropped: %s", it.ID, strings.TrimPrefix(it.Reason, droppedPrefix)))
		}
	}
	if len(lines) > 0 {
		body += "\n\n" + whereHeading + "\n\nWritten by vault upgrade (dossiers): each item that was open, and where it is now.\n\n" + strings.Join(lines, "\n")
	}
	d.Body = body
	if !open {
		d.Meta.(*model.Proposal).Status = model.ProposalSuperseded
	}
	return d, nil
}

// Reasons dossier_refile writes on a staged item.
const (
	refiledPrefix = "re-filed as a direct write"
	droppedPrefix = "dropped by vault upgrade (dossiers): "
)

// RefileItem is the judgement part of step dossiers for one staged item:
// result names the item or receipt an agent made with a direct write, or
// dropped says why the change is not needed. The item closes (done, or
// withdrawn with the reason), and the dossier closes as superseded once no
// item is open.
func RefileItem(d model.Document, item, result, dropped string) (model.Document, error) {
	_, dz := openDossier(d)
	if dz == nil {
		return d, fmt.Errorf("%s is not an open dossier", d.ID)
	}
	if !model.IsDossier(d.Body) {
		return d, fmt.Errorf("%s is in the older one-block form: run para vault upgrade first", d.ID)
	}
	result, dropped = linkID(result), oneLine(dropped, 0)
	if (result == "") == (dropped == "") {
		return d, errors.New("give result (the id of what you wrote) or dropped (why the change is not needed), not both")
	}
	i := slices.IndexFunc(dz.Items, func(it model.ProposalItem) bool { return it.ID == item })
	if i < 0 {
		return d, fmt.Errorf("%s has no item %q", d.ID, item)
	}
	it := &dz.Items[i]
	if !itemOpen(*it) || moveOf(*it) != MoveStaged {
		return d, fmt.Errorf("%s %s is not an open staged change (state %s, kind %s)", d.ID, item, it.State, it.Kind)
	}
	it.ClaimedBy, it.LeaseUntil, it.Waits = "", "", false
	if result != "" {
		it.State, it.Result, it.Reason = model.ItemDone, result, refiledPrefix
	} else {
		it.State, it.Reason = model.ItemWithdrawn, droppedPrefix+dropped
	}
	return renderDossier(d, dz)
}
