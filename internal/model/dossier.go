package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// A dossier is a proposal body written as plain markdown. Each item is one
// fenced block whose info string is `yaml para-<kind> id=<id>`, placed where it
// belongs in the narrative, and one `yaml para-dossier` block holds the header
// and the owner's response. Answers live in the item blocks: the owner writes
// them (web app, CLI, or by hand in a file), and para rewrites the blocks. A
// block para has not changed is written back byte for byte.

// Dossier lint codes. Each is documented in docs/format.md, "Dossier blocks".
const (
	CodeDossierBlock  = "dossier-block"
	CodeDossierID     = "dossier-id"
	CodeDossierRef    = "dossier-ref"
	CodeDossierAnswer = "dossier-answer"
	CodeDossierState  = "dossier-state"
)

// Answer decisions an owner writes in a block.
const (
	AnswerApprove = "approve"
	AnswerReject  = "reject"
	AnswerEdit    = "edit"
	// AnswerSkip leaves the item undecided in this submit: it stays in the
	// stack as skipped, with the note in comment, for a later submit.
	AnswerSkip = "skip"
)

// Where an answer was written.
const (
	ViaPWA  = "pwa"
	ViaCLI  = "cli"
	ViaFile = "file"
)

// Response states of a dossier.
const (
	ResponseDraft     = "draft"
	ResponseSubmitted = "submitted"
)

// ItemAnswer is the owner's answer to one item. Which keys apply depends on the
// kind: a question takes choices, added and edited options, and own words; a
// change takes decision and, to edit, input; a message or an action takes
// decision and, to edit, body and subject; a doc edit takes decision and, to
// edit, new. Every kind takes comment. Via says where it was written. Stale is
// set by para when the agent changed the item after the answer.
type ItemAnswer struct {
	Decision string         `yaml:"decision,omitempty" json:"decision,omitempty"`
	Input    map[string]any `yaml:"input,omitempty" json:"input,omitempty"`
	Subject  string         `yaml:"subject,omitempty" json:"subject,omitempty"`
	Body     string         `yaml:"body,omitempty" json:"body,omitempty"`
	// People is who a message goes to, as the owner edited it (message only).
	People        []string          `yaml:"people,omitempty" json:"people,omitempty"`
	New           string            `yaml:"new,omitempty" json:"new,omitempty"`
	Choices       []string          `yaml:"choices,omitempty" json:"choices,omitempty"`
	AddedOptions  []string          `yaml:"added_options,omitempty" json:"added_options,omitempty"`
	EditedOptions map[string]string `yaml:"edited_options,omitempty" json:"edited_options,omitempty"`
	OwnWords      string            `yaml:"own_words,omitempty" json:"own_words,omitempty"`
	// Comments says what each comment of an upstream item becomes.
	Comments []CommentAnswer `yaml:"comments,omitempty" json:"comments,omitempty"`
	Comment  string          `yaml:"comment,omitempty" json:"comment,omitempty"`
	Via      string          `yaml:"via,omitempty" json:"via,omitempty"`
	Stale    bool            `yaml:"stale,omitempty" json:"stale,omitempty"`
}

// CommentAnswer is what the owner makes of one comment of a tracked document
// in an upstream item: To is ask, question, commitment, resolved, or dropped.
// Due is the due date of a commitment (default a week on); Note is kept with
// a resolved or dropped comment.
type CommentAnswer struct {
	ID   string `yaml:"id" json:"id"`
	To   string `yaml:"to" json:"to"`
	Note string `yaml:"note,omitempty" json:"note,omitempty"`
	Due  string `yaml:"due,omitempty" json:"due,omitempty"`
}

// CommentTargets are the values of CommentAnswer.To.
var CommentTargets = []string{"ask", "question", "commitment", "resolved", "dropped"}

// Empty reports whether the answer says nothing.
func (a *ItemAnswer) Empty() bool {
	return a == nil || a.Decision == "" && a.Input == nil && a.Subject == "" && a.Body == "" && len(a.People) == 0 && a.New == "" && len(a.Choices) == 0 &&
		len(a.AddedOptions) == 0 && len(a.EditedOptions) == 0 && strings.TrimSpace(a.OwnWords) == "" && strings.TrimSpace(a.Comment) == "" &&
		len(a.Comments) == 0
}

// DossierResponse is the owner's response to the whole dossier: a comment per
// section (by heading id), an overall comment, and whether it is submitted.
type DossierResponse struct {
	State       string            `yaml:"state,omitempty" json:"state,omitempty"`
	Via         string            `yaml:"via,omitempty" json:"via,omitempty"`
	SubmittedAt string            `yaml:"submitted_at,omitempty" json:"submitted_at,omitempty"`
	Overall     string            `yaml:"overall,omitempty" json:"overall,omitempty"`
	Sections    map[string]string `yaml:"sections,omitempty" json:"sections,omitempty"`
}

// DossierHeader is the para-dossier block. Status, revision, and proposed_by
// are in the frontmatter, the one place para keeps them. Replies holds the
// agent's replies to section comments, by section id; "overall" answers the
// overall comment.
type DossierHeader struct {
	TLDR     string            `yaml:"tldr" json:"tldr"`
	Context  string            `yaml:"context,omitempty" json:"context,omitempty"`
	Response *DossierResponse  `yaml:"response,omitempty" json:"response,omitempty"`
	Replies  map[string][]Turn `yaml:"replies,omitempty" json:"replies,omitempty"`
}

// OverallReply is the Replies key of a reply to the overall comment.
const OverallReply = "overall"

// DossierSection is one ## heading of a dossier.
type DossierSection struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Dossier is a parsed dossier body.
type Dossier struct {
	Header   DossierHeader
	Items    []ProposalItem
	Sections []DossierSection
	parts    []dossierPart
}

// dossierPart is a run of markdown text or one block, in file order. canon is
// the block's value as para would write it when it was read; raw is the text
// it was read from. A block whose value still renders to canon is written back
// as raw.
type dossierPart struct {
	text   string
	header bool
	item   string // item id
	fence  string
	raw    string
	canon  string
}

// dossierItem is the YAML of an item block. Kind and id are in the info string.
type dossierItem struct {
	Title    string         `yaml:"title,omitempty" json:"title,omitempty"`
	Subtype  string         `yaml:"subtype,omitempty" json:"subtype,omitempty"`
	Group    string         `yaml:"group,omitempty" json:"group,omitempty"`
	Bundle   string         `yaml:"bundle,omitempty" json:"bundle,omitempty"`
	Choice   string         `yaml:"choice,omitempty" json:"choice,omitempty"`
	After    string         `yaml:"after,omitempty" json:"after,omitempty"`
	For      string         `yaml:"for,omitempty" json:"for,omitempty"`
	People   []string       `yaml:"people,omitempty" json:"people,omitempty"`
	Channel  string         `yaml:"channel,omitempty" json:"channel,omitempty"`
	Subject  string         `yaml:"subject,omitempty" json:"subject,omitempty"`
	IO       string         `yaml:"io,omitempty" json:"io,omitempty"`
	Target   string         `yaml:"target,omitempty" json:"target,omitempty"`
	What     string         `yaml:"what,omitempty" json:"what,omitempty"`
	Previous string         `yaml:"previous,omitempty" json:"previous,omitempty"`
	Op       string         `yaml:"op,omitempty" json:"op,omitempty"`
	Input    map[string]any `yaml:"input,omitempty" json:"input,omitempty"`
	How      string         `yaml:"how,omitempty" json:"how,omitempty"`
	Impact   string         `yaml:"impact,omitempty" json:"impact,omitempty"`
	Body     string         `yaml:"body,omitempty" json:"body,omitempty"`
	Diff     string         `yaml:"diff,omitempty" json:"diff,omitempty"`
	// question
	Multi         bool           `yaml:"multi,omitempty" json:"multi,omitempty"`
	Options       []string       `yaml:"options,omitempty" json:"options,omitempty"`
	Recommended   string         `yaml:"recommended,omitempty" json:"recommended,omitempty"`
	OptionDetails []OptionDetail `yaml:"option_details,omitempty" json:"option_details,omitempty"`
	// doc edit
	Range string `yaml:"range,omitempty" json:"range,omitempty"`
	Old   string `yaml:"old,omitempty" json:"old,omitempty"`
	New   string `yaml:"new,omitempty" json:"new,omitempty"`
	// upstream
	From     int      `yaml:"from,omitempty" json:"from,omitempty"`
	To       int      `yaml:"to,omitempty" json:"to,omitempty"`
	Comments []string `yaml:"comments,omitempty" json:"comments,omitempty"`

	Fields          map[string]any `yaml:"fields,omitempty" json:"fields,omitempty"`
	NotDuplicate    string         `yaml:"not_duplicate,omitempty" json:"not_duplicate,omitempty"`
	Evidence        *Evidence      `yaml:"evidence,omitempty" json:"evidence,omitempty"`
	Pros            []Claim        `yaml:"pros,omitempty" json:"pros,omitempty"`
	Cons            []Claim        `yaml:"cons,omitempty" json:"cons,omitempty"`
	Cost            *Claim         `yaml:"cost,omitempty" json:"cost,omitempty"`
	Benefit         *Claim         `yaml:"benefit,omitempty" json:"benefit,omitempty"`
	Confidence      int            `yaml:"confidence,omitempty" json:"confidence,omitempty"`
	ConfidenceBasis string         `yaml:"confidence_basis,omitempty" json:"confidence_basis,omitempty"`
	Recommendation  int            `yaml:"recommendation,omitempty" json:"recommendation,omitempty"`
	Reversible      Reversible     `yaml:"reversible,omitempty" json:"reversible,omitempty"`
	ReversibleHow   string         `yaml:"reversible_how,omitempty" json:"reversible_how,omitempty"`
	Risk            string         `yaml:"risk,omitempty" json:"risk,omitempty"`

	// para's
	Rev          int           `yaml:"rev,omitempty" json:"rev,omitempty"`
	State        ItemState     `yaml:"state,omitempty" json:"state,omitempty"`
	Reason       string        `yaml:"reason,omitempty" json:"reason,omitempty"`
	Edited       bool          `yaml:"edited,omitempty" json:"edited,omitempty"`
	ApprovedHash string        `yaml:"approved_hash,omitempty" json:"approved_hash,omitempty"`
	Result       string        `yaml:"result,omitempty" json:"result,omitempty"`
	Waits        bool          `yaml:"waits,omitempty" json:"waits,omitempty"`
	Exec         *Execution    `yaml:"exec,omitempty" json:"exec,omitempty"`
	ClaimedBy    string        `yaml:"claimed_by,omitempty" json:"claimed_by,omitempty"`
	LeaseUntil   string        `yaml:"lease_until,omitempty" json:"lease_until,omitempty"`
	Decided      *ItemDecision `yaml:"decided,omitempty" json:"decided,omitempty"`
	Thread       []Turn        `yaml:"thread,omitempty" json:"thread,omitempty"`

	// the owner's
	Answer *ItemAnswer `yaml:"answer,omitempty" json:"answer,omitempty"`
}

// DossierInfo opens the header block.
const dossierInfo = "yaml para-dossier"

var (
	infoRe    = regexp.MustCompile(`^yaml para-(dossier|question|change|message|action|doc-edit|upstream)(?: id=([a-z][a-z0-9-]{0,39}))?$`)
	fenceRe   = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})(.*)$")
	headingRe = regexp.MustCompile(`^##\s+(.+?)\s*#*\s*$`)
	slugRe    = regexp.MustCompile(`[^a-z0-9]+`)
)

// blockKinds maps an info-string kind to the item kind.
var blockKinds = map[string]ItemKind{
	"question": ItemQuestion, "change": ItemPara, "message": ItemMessage, "action": ItemAction,
	"doc-edit": ItemDocEdit, "upstream": ItemUpstream,
}

// BlockKind is the info-string word of an item kind.
func BlockKind(k ItemKind) string {
	switch k {
	case ItemPara, ItemOp:
		return "change"
	case ItemExternal:
		return "action"
	case ItemDocEdit:
		return "doc-edit"
	}
	return string(k)
}

// IsDossier reports whether a proposal body is a dossier.
func IsDossier(body string) bool {
	for _, ln := range strings.Split(body, "\n") {
		if m := fenceRe.FindStringSubmatch(ln); m != nil && strings.TrimSpace(m[2]) == dossierInfo {
			return true
		}
	}
	return false
}

// SectionID is the id of a ## heading: lower case, runs of other characters
// as one dash.
func SectionID(title string) string {
	return strings.Trim(slugRe.ReplaceAllString(strings.ToLower(title), "-"), "-")
}

// DossierFinding is one problem in a dossier body. Line is 1-based in the body.
type DossierFinding struct {
	Line int
	Code string
	Msg  string
}

func (f DossierFinding) String() string {
	return fmt.Sprintf("line %d: [%s] %s", f.Line, f.Code, f.Msg)
}

// ParseDossier reads a dossier body. Any finding is an error: a hand edit that
// breaks a block makes the dossier unreviewable until it is fixed.
func ParseDossier(body string) (*Dossier, error) {
	d, found := scanDossier(body)
	if len(found) > 0 {
		msgs := make([]string, 0, len(found))
		for _, f := range found {
			msgs = append(msgs, f.String())
		}
		return d, fmt.Errorf("model: dossier: %s", strings.Join(msgs, "; "))
	}
	return d, nil
}

// LintDossier lists every problem of a dossier body.
func LintDossier(body string) []DossierFinding {
	_, found := scanDossier(body)
	return found
}

type blockAt struct {
	line int
	item *ProposalItem
}

// scanDossier splits the body into text and blocks, and checks each.
func scanDossier(body string) (*Dossier, []DossierFinding) {
	d := &Dossier{}
	var found []DossierFinding
	add := func(line int, code, format string, a ...any) {
		found = append(found, DossierFinding{Line: line, Code: code, Msg: fmt.Sprintf(format, a...)})
	}
	lines := strings.SplitAfter(body, "\n")
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			d.parts = append(d.parts, dossierPart{text: text.String()})
			text.Reset()
		}
	}
	section := ""
	sections := map[string]int{}
	ids := map[string]int{}
	var at []blockAt
	headers := 0
	for i := 0; i < len(lines); i++ {
		ln := strings.TrimRight(lines[i], "\n")
		m := fenceRe.FindStringSubmatch(ln)
		if m == nil {
			if h := headingRe.FindStringSubmatch(ln); h != nil {
				id := SectionID(h[1])
				if prev, dup := sections[id]; dup {
					add(i+1, CodeDossierID, "section %q repeats the heading id of line %d", id, prev)
				} else if id != "" {
					sections[id] = i + 1
					d.Sections = append(d.Sections, DossierSection{ID: id, Title: h[1]})
				}
				section = id
			}
			text.WriteString(lines[i])
			continue
		}
		// A fence: find its close, the same character, at least as long.
		open, info := m[1], strings.TrimSpace(m[2])
		end := -1
		for j := i + 1; j < len(lines); j++ {
			c := strings.TrimRight(lines[j], "\n")
			if cm := fenceRe.FindStringSubmatch(c); cm != nil && strings.TrimSpace(cm[2]) == "" && cm[1][0] == open[0] && len(cm[1]) >= len(open) {
				end = j
				break
			}
		}
		para := strings.HasPrefix(info, "yaml para-") && info != proposalInfo
		if !para {
			last := len(lines) - 1
			if end >= 0 {
				last = end
			}
			for j := i; j <= last; j++ {
				text.WriteString(lines[j])
			}
			i = last
			continue
		}
		if end < 0 {
			add(i+1, CodeDossierBlock, "block %q is not closed", info)
			text.WriteString(strings.Join(lines[i:], ""))
			break
		}
		raw := strings.Join(lines[i+1:end], "")
		fence := open
		next := i + 1
		i = end
		im := infoRe.FindStringSubmatch(info)
		if im == nil {
			add(next, CodeDossierBlock, "info string %q: want yaml para-<dossier|question|change|message|action|doc-edit|upstream> id=<id>", info)
			continue
		}
		flush()
		if im[1] == "dossier" {
			headers++
			if im[2] != "" {
				add(next, CodeDossierBlock, "the para-dossier block takes no id")
			}
			if headers > 1 {
				add(next, CodeDossierBlock, "a dossier has one para-dossier block")
				continue
			}
			dec := yaml.NewDecoder(strings.NewReader(raw))
			dec.KnownFields(true)
			if err := dec.Decode(&d.Header); err != nil && !errors.Is(err, io.EOF) {
				add(next, CodeDossierBlock, "para-dossier: %s", yamlMsg(err))
			}
			if strings.TrimSpace(d.Header.TLDR) == "" {
				add(next, CodeDossierBlock, "para-dossier: tldr is required")
			}
			if r := d.Header.Response; r != nil && r.State != "" && r.State != ResponseDraft && r.State != ResponseSubmitted {
				add(next, CodeDossierState, "response state %q: want draft or submitted", r.State)
			}
			canon, _ := renderYAML(d.Header)
			d.parts = append(d.parts, dossierPart{header: true, fence: fence, raw: raw, canon: canon})
			continue
		}
		id := im[2]
		if id == "" {
			add(next, CodeDossierID, "block para-%s has no id: write yaml para-%s id=<id>", im[1], im[1])
			continue
		}
		if prev, dup := ids[id]; dup {
			add(next, CodeDossierID, "id %q repeats the block of line %d", id, prev)
			continue
		}
		ids[id] = next
		var b dossierItem
		dec := yaml.NewDecoder(strings.NewReader(raw))
		dec.KnownFields(true)
		if err := dec.Decode(&b); err != nil && !errors.Is(err, io.EOF) {
			add(next, CodeDossierBlock, "%s: %s", id, yamlMsg(err))
			continue
		}
		it := b.item(id, blockKinds[im[1]])
		it.Section = section
		for _, f := range checkBlock(it) {
			add(next, f[0], "%s: %s", id, f[1])
		}
		canon, _ := renderYAML(b)
		d.parts = append(d.parts, dossierPart{item: id, fence: fence, raw: raw, canon: canon})
		d.Items = append(d.Items, it)
		at = append(at, blockAt{line: next})
	}
	flush()
	if headers == 0 {
		add(1, CodeDossierBlock, "no para-dossier block")
	}
	for i := range at {
		at[i].item = &d.Items[i]
	}
	found = append(found, checkRefs(at)...)
	return d, found
}

func yamlMsg(err error) string {
	if err.Error() == "EOF" {
		return "empty"
	}
	return strings.TrimPrefix(err.Error(), "yaml: ")
}

func (b dossierItem) item(id string, kind ItemKind) ProposalItem {
	it := ProposalItem{ID: id, Kind: kind, Title: b.Title, Subtype: b.Subtype, Group: b.Group, Bundle: b.Bundle, Choice: b.Choice,
		After: b.After, For: b.For, People: b.People, Channel: b.Channel, Subject: b.Subject, IO: b.IO, Target: b.Target, What: b.What,
		Previous: b.Previous, Op: b.Op, Input: b.Input, How: b.How, Impact: b.Impact, Body: b.Body, Diff: b.Diff,
		Multi: b.Multi, Options: b.Options, Recommended: b.Recommended, OptionDetails: b.OptionDetails,
		Range: b.Range, Old: b.Old, New: b.New, From: b.From, To: b.To, Comments: b.Comments, Fields: b.Fields, NotDuplicate: b.NotDuplicate,
		Tradeoffs: Tradeoffs{Pros: b.Pros, Cons: b.Cons, Cost: b.Cost, Benefit: b.Benefit, Confidence: b.Confidence,
			ConfidenceBasis: b.ConfidenceBasis, Recommendation: b.Recommendation},
		Reversible: b.Reversible, ReversibleHow: b.ReversibleHow, Risk: b.Risk,
		Rev: b.Rev, State: b.State, Reason: b.Reason, Edited: b.Edited, ApprovedHash: b.ApprovedHash, Result: b.Result, Waits: b.Waits,
		Execution: b.Exec, ClaimedBy: b.ClaimedBy, LeaseUntil: b.LeaseUntil, Decided: b.Decided, Thread: b.Thread, Answer: b.Answer}
	if b.Evidence != nil {
		it.Evidence = *b.Evidence
	}
	return it
}

func blockOf(it ProposalItem) dossierItem {
	b := dossierItem{Title: it.Title, Subtype: it.Subtype, Group: it.Group, Bundle: it.Bundle, Choice: it.Choice,
		After: it.After, For: it.For, People: it.People, Channel: it.Channel, Subject: it.Subject, IO: it.IO, Target: it.Target, What: it.What,
		Previous: it.Previous, Op: it.Op, Input: it.Input, How: it.How, Impact: it.Impact, Body: it.Body, Diff: it.Diff,
		Multi: it.Multi, Options: it.Options, Recommended: it.Recommended, OptionDetails: it.OptionDetails,
		Range: it.Range, Old: it.Old, New: it.New, From: it.From, To: it.To, Comments: it.Comments, Fields: it.Fields, NotDuplicate: it.NotDuplicate,
		Pros: it.Pros, Cons: it.Cons, Cost: it.Cost, Benefit: it.Benefit, Confidence: it.Confidence,
		ConfidenceBasis: it.ConfidenceBasis, Recommendation: it.Recommendation,
		Reversible: it.Reversible, ReversibleHow: it.ReversibleHow, Risk: it.Risk,
		Rev: it.Rev, State: it.State, Reason: it.Reason, Edited: it.Edited, ApprovedHash: it.ApprovedHash, Result: it.Result, Waits: it.Waits,
		Exec: it.Execution, ClaimedBy: it.ClaimedBy, LeaseUntil: it.LeaseUntil, Decided: it.Decided, Thread: it.Thread, Answer: it.Answer}
	if it.Evidence != (Evidence{}) {
		e := it.Evidence
		b.Evidence = &e
	}
	if it.IO != "" { // the io draft holds the text: one copy
		b.People, b.Subject, b.Body = nil, "", ""
	}
	return b
}

// checkBlock lists what one item block lacks or gets wrong, as code and text.
func checkBlock(it ProposalItem) [][2]string {
	var out [][2]string
	bad := func(code, format string, a ...any) { out = append(out, [2]string{code, fmt.Sprintf(format, a...)}) }
	blank := func(s string) bool { return strings.TrimSpace(s) == "" }
	if blank(it.Evidence.Quote) || blank(it.Evidence.Locator) {
		bad(CodeDossierBlock, "evidence needs a quote and a locator")
	}
	switch it.Kind {
	case ItemQuestion:
		if blank(it.Title) {
			bad(CodeDossierBlock, "a question needs title")
		}
		if it.Recommended != "" && !slices.Contains(it.Options, it.Recommended) {
			bad(CodeDossierBlock, "recommended %q is not one of options", it.Recommended)
		}
	case ItemPara:
		if blank(it.Op) || it.Input == nil {
			bad(CodeDossierBlock, "a change needs op and input")
		}
	case ItemMessage:
		if it.IO != "" {
			if len(it.People) > 0 || it.Subject != "" || it.Body != "" {
				bad(CodeDossierBlock, "a message with io takes people, subject, and body from the io draft: leave them out")
			}
			break
		}
		if len(it.People) == 0 && blank(it.Channel) && !IsPathBody(it.Body) {
			bad(CodeDossierBlock, "a message names people or a channel")
		}
		if blank(it.Body) {
			bad(CodeDossierBlock, "a message needs body")
		}
	case ItemAction:
		// what and how; target and body is the older form of an action.
		if (blank(it.What) || blank(it.How)) && (blank(it.Target) || blank(it.Body)) {
			bad(CodeDossierBlock, "an action needs what and how")
		}
	case ItemDocEdit:
		if blank(it.Target) || blank(it.New) {
			bad(CodeDossierBlock, "a doc edit needs target and new")
		}
	case ItemUpstream:
		if blank(it.Target) {
			bad(CodeDossierBlock, "an upstream item needs target, the tracked document")
		}
		if it.From < 0 || it.To < 0 || (it.To > 0 && it.From >= it.To) {
			bad(CodeDossierBlock, "from %d and to %d: want versions with from before to", it.From, it.To)
		}
		seen := map[string]bool{}
		for _, c := range it.Comments {
			if seen[c] {
				bad(CodeDossierBlock, "comment %s is listed twice", c)
			}
			seen[c] = true
		}
	}
	if it.Kind != ItemUpstream && (it.From != 0 || it.To != 0 || len(it.Comments) > 0) {
		bad(CodeDossierBlock, "from, to, and comments are for an upstream item")
	}
	if it.IO != "" && it.Kind != ItemMessage {
		bad(CodeDossierBlock, "io is for a message")
	}
	if it.State != "" && !slices.Contains(ItemStates, it.State) {
		bad(CodeDossierState, "state %q: want one of %v", it.State, ItemStates)
	}
	if a := it.Answer; a != nil {
		for _, msg := range checkAnswer(it, *a) {
			bad(CodeDossierAnswer, "%s", msg)
		}
	}
	return out
}

// CheckAnswer lists what is wrong with an answer to an item.
func CheckAnswer(it ProposalItem, a ItemAnswer) []string { return checkAnswer(it, a) }

func checkAnswer(it ProposalItem, a ItemAnswer) []string {
	var out []string
	if a.Via != "" && a.Via != ViaPWA && a.Via != ViaCLI && a.Via != ViaFile {
		out = append(out, fmt.Sprintf("answer via %q: want pwa, cli, or file", a.Via))
	}
	if a.Decision == AnswerSkip {
		if a.Input != nil || a.Body != "" || a.Subject != "" || len(a.People) > 0 || a.New != "" || len(a.Choices) > 0 || a.OwnWords != "" || len(a.Comments) > 0 {
			out = append(out, "skip takes a comment only: the note the agent reads")
		}
		return out
	}
	if it.Kind == ItemQuestion {
		if a.Decision != "" || a.Input != nil || a.Body != "" || a.Subject != "" || len(a.People) > 0 || a.New != "" {
			out = append(out, "a question's answer takes choices, added_options, edited_options, own_words, and comment")
		}
		allowed := append(slices.Clone(it.Options), a.AddedOptions...)
		for _, c := range a.Choices {
			if !slices.Contains(allowed, c) {
				out = append(out, fmt.Sprintf("choice %q is not an option or an added option", c))
			}
		}
		for k := range a.EditedOptions {
			if !slices.Contains(it.Options, k) {
				out = append(out, fmt.Sprintf("edited option %q is not an option", k))
			}
		}
		if !it.Multi && len(a.Choices) > 1 {
			out = append(out, "more than one choice, and multi is false")
		}
		return out
	}
	if len(a.Choices) > 0 || len(a.AddedOptions) > 0 || len(a.EditedOptions) > 0 || a.OwnWords != "" {
		out = append(out, "choices, options, and own_words are for a question")
	}
	if it.Kind == ItemUpstream {
		return checkUpstreamAnswer(it, a, out)
	}
	if len(a.Comments) > 0 {
		out = append(out, "comments are for an upstream item")
	}
	switch a.Decision {
	case "", AnswerApprove, AnswerReject, AnswerEdit:
	default:
		out = append(out, fmt.Sprintf("decision %q: want approve, reject, edit, or skip", a.Decision))
	}
	edits := a.Input != nil || a.Body != "" || a.Subject != "" || len(a.People) > 0 || a.New != ""
	if len(a.People) > 0 && it.Kind != ItemMessage {
		out = append(out, "people is edited on a message only")
	}
	if edits && a.Decision != AnswerEdit {
		out = append(out, "edited text goes with decision edit")
	}
	switch it.Kind {
	case ItemPara:
		if a.Body != "" || a.Subject != "" || a.New != "" {
			out = append(out, "a change is edited by input")
		}
		if a.Decision == AnswerEdit && a.Input == nil {
			out = append(out, "decision edit on a change needs input")
		}
	case ItemDocEdit:
		if a.Input != nil || a.Body != "" || a.Subject != "" {
			out = append(out, "a doc edit is edited by new")
		}
		if a.Decision == AnswerEdit && a.New == "" {
			out = append(out, "decision edit on a doc edit needs new")
		}
	default:
		if a.Input != nil || a.New != "" {
			out = append(out, "a message or action is edited by body and subject")
		}
		if a.Decision == AnswerEdit && a.Body == "" && a.Subject == "" && len(a.People) == 0 {
			out = append(out, "decision edit needs body, subject, or people")
		}
	}
	return out
}

// checkUpstreamAnswer: approve marks the change seen and settles the comments
// as answered; reject leaves it unseen. An upstream item has no text to edit.
func checkUpstreamAnswer(it ProposalItem, a ItemAnswer, out []string) []string {
	switch a.Decision {
	case "", AnswerApprove, AnswerReject:
	default:
		out = append(out, fmt.Sprintf("decision %q: an upstream item takes approve (seen) or reject", a.Decision))
	}
	if a.Input != nil || a.Body != "" || a.Subject != "" || a.New != "" {
		out = append(out, "an upstream item has no text to edit: answer comments with comments")
	}
	if len(a.Comments) > 0 && a.Decision != AnswerApprove {
		out = append(out, "comments go with decision approve")
	}
	seen := map[string]bool{}
	for _, c := range a.Comments {
		switch {
		case seen[c.ID]:
			out = append(out, fmt.Sprintf("comment %s is answered twice", c.ID))
		case len(it.Comments) > 0 && !slices.Contains(it.Comments, c.ID):
			out = append(out, fmt.Sprintf("comment %s is not one of the item's comments", c.ID))
		case !slices.Contains(CommentTargets, c.To):
			out = append(out, fmt.Sprintf("comment %s: to %q: want one of %v", c.ID, c.To, CommentTargets))
		case c.Due != "" && c.To != "commitment":
			out = append(out, fmt.Sprintf("comment %s: due is for a commitment", c.ID))
		}
		seen[c.ID] = true
	}
	return out
}

// checkRefs checks after: it names an item and makes no cycle; and a choice
// group has alternatives.
func checkRefs(at []blockAt) []DossierFinding {
	var out []DossierFinding
	byID := map[string]blockAt{}
	choices := map[string]int{}
	for _, b := range at {
		byID[b.item.ID] = b
		if c := b.item.Choice; c != "" && b.item.State != ItemWithdrawn {
			choices[c]++
		}
	}
	for _, b := range at {
		if c := b.item.Choice; c != "" && choices[c] == 1 && b.item.State != ItemWithdrawn {
			out = append(out, DossierFinding{Line: b.line, Code: CodeDossierRef, Msg: fmt.Sprintf("%s: choice %q has one item: a choice needs alternatives", b.item.ID, c)})
		}
		seen := map[string]bool{b.item.ID: true}
		for cur := b.item; cur.After != ""; {
			next, ok := byID[cur.After]
			if !ok {
				out = append(out, DossierFinding{Line: b.line, Code: CodeDossierRef, Msg: fmt.Sprintf("%s: after %q is not an item", cur.ID, cur.After)})
				break
			}
			if seen[next.item.ID] {
				out = append(out, DossierFinding{Line: b.line, Code: CodeDossierRef, Msg: fmt.Sprintf("%s: after makes a cycle", b.item.ID)})
				break
			}
			seen[next.item.ID] = true
			cur = next.item
		}
	}
	return out
}

// renderYAML writes v as YAML by way of JSON, as proposalNode does.
func renderYAML(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("model: dossier: %w", err)
	}
	var node yaml.Node
	if err := yaml.Unmarshal(raw, &node); err != nil {
		return "", fmt.Errorf("model: dossier: %w", err)
	}
	blockStyle(&node)
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(&node); err != nil {
		return "", fmt.Errorf("model: dossier: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("model: dossier: %w", err)
	}
	if b.String() == "{}\n" {
		return "", nil
	}
	return b.String(), nil
}

// fenceFor is a backtick fence longer than any backtick run that starts a line
// of the text, so no text can close the block.
func fenceFor(text string) string {
	n := 3
	for _, ln := range strings.Split(text, "\n") {
		t := strings.TrimLeft(ln, " ")
		k := len(t) - len(strings.TrimLeft(t, "`"))
		if k >= n {
			n = k + 1
		}
	}
	return strings.Repeat("`", n)
}

// Append adds an item at the end of the dossier, as a new block.
func (d *Dossier) Append(it ProposalItem) {
	if len(d.parts) > 0 {
		if last := d.parts[len(d.parts)-1]; last.text != "" && !strings.HasSuffix(last.text, "\n\n") {
			d.parts = append(d.parts, dossierPart{text: "\n"})
		}
		if last := d.parts[len(d.parts)-1]; last.text == "" {
			d.parts = append(d.parts, dossierPart{text: "\n"})
		}
	}
	d.Items = append(d.Items, it)
	d.parts = append(d.parts, dossierPart{item: it.ID})
}

// Render writes the dossier body. Text runs and unchanged blocks come back as
// they were read. An item that has no block (Items grew) is written at the end.
func (d *Dossier) Render() (string, error) {
	byID := map[string]ProposalItem{}
	for _, it := range d.Items {
		byID[it.ID] = it
	}
	placed := map[string]bool{}
	var b strings.Builder
	block := func(info string, v any, p dossierPart) error {
		y, err := renderYAML(v)
		if err != nil {
			return err
		}
		if p.fence != "" && y == p.canon {
			y = p.raw
		}
		f := fenceFor(y)
		if p.fence != "" && len(p.fence) >= len(f) && y == p.raw {
			f = p.fence
		}
		b.WriteString(f + info + "\n" + y + f + "\n")
		return nil
	}
	parts := d.parts
	if len(parts) == 0 || !slices.ContainsFunc(parts, func(p dossierPart) bool { return p.header }) {
		parts = append([]dossierPart{{header: true}, {text: "\n"}}, parts...)
	}
	for _, p := range parts {
		switch {
		case p.header:
			if err := block(dossierInfo, d.Header, p); err != nil {
				return "", err
			}
		case p.item != "":
			it, ok := byID[p.item]
			if !ok || placed[p.item] {
				continue
			}
			placed[p.item] = true
			if err := block(itemInfo(it), blockOf(it), p); err != nil {
				return "", err
			}
		default:
			b.WriteString(p.text)
		}
	}
	for _, it := range d.Items {
		if placed[it.ID] {
			continue
		}
		s := b.String()
		if s != "" && !strings.HasSuffix(s, "\n\n") {
			b.WriteString("\n")
		}
		if err := block(itemInfo(it), blockOf(it), dossierPart{}); err != nil {
			return "", err
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func itemInfo(it ProposalItem) string { return "yaml para-" + BlockKind(it.Kind) + " id=" + it.ID }

// NewDossier builds a dossier from a header and items: a short narrative with
// every item under one heading. It is how para writes items that came in as
// fields, not markdown.
func NewDossier(h DossierHeader, heading string, items []ProposalItem) *Dossier {
	d := &Dossier{Header: h, parts: []dossierPart{{header: true}}}
	if heading != "" {
		d.parts = append(d.parts, dossierPart{text: "\n## " + heading + "\n"})
		d.Sections = append(d.Sections, DossierSection{ID: SectionID(heading), Title: heading})
	}
	for _, it := range items {
		it.Section = SectionID(heading)
		d.Append(it)
	}
	return d
}

// Content is the dossier as the fields proposal ops work on.
func (d *Dossier) Content() ProposalContent {
	items := d.Items
	if items == nil {
		items = []ProposalItem{}
	}
	return ProposalContent{TLDR: d.Header.TLDR, Context: d.Header.Context, Items: items}
}

// IsPathBody reports whether an item body is a path alone: one line, with no
// white space or link marks, that ends in .md or starts with / or ~/. Para
// reads such a body as a pointer to a vault file (a message's to its io
// draft); the ops resolve it and refuse a path outside the vault.
func IsPathBody(body string) bool {
	s := strings.TrimSpace(body)
	if s == "" || strings.ContainsAny(s, " \t\n\r[]()`") {
		return false
	}
	return strings.HasSuffix(s, ".md") || strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~/")
}
