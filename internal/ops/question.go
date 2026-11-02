package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// A question is something unknown that nobody has been asked yet, or a decision
// or an input that only the owner can give. It is not work: a task is work to
// do. A question has one scope (a project, a task, an area, or root for the whole
// vault), blocks the tasks that depend on it while it is open, and keeps its
// answer in the document where the owner and the agents will find it.

const (
	answerHeading  = "## Answer"
	droppedHeading = "## Dropped"
	// ScopeRootWord is the scope input and output word for a vault-level question.
	ScopeRootWord = "root"
)

// Scope types in QuestionView.ScopeType.
const (
	ScopeProject = "project"
	ScopeTask    = "task"
	ScopeArea    = "area"
	ScopeRoot    = "root"
)

// QuestionView is a question without its body. Scope is a document id or
// "root". Blocks holds the open tasks and requests that depend on it directly;
// Blocking counts everything that waits on it, through any chain.
type QuestionView struct {
	ID          string               `json:"id"`
	Folder      model.Folder         `json:"folder"`
	Title       string               `json:"title"`
	Kind        model.QuestionKind   `json:"kind"`
	Status      model.QuestionStatus `json:"status"`
	Scope       string               `json:"scope"`
	ScopeType   string               `json:"scope_type"`
	Options     []string             `json:"options"`
	Recommended string               `json:"recommended,omitempty"`
	RFX         string               `json:"rfx,omitempty"`
	Choice      string               `json:"choice,omitempty"`
	AnsweredBy  string               `json:"answered_by,omitempty"`
	Answered    model.Date           `json:"answered,omitempty"`
	Created     model.Date           `json:"created"`
	Blocks      []string             `json:"blocks"`
	Blocking    int                  `json:"blocking"`
	// DependsOn are the tasks, requests, and questions this one waits on.
	// WaitingOn is the part of DependsOn not yet resolved.
	DependsOn []string `json:"depends_on"`
	WaitingOn []string `json:"waiting_on"`
	// HasProposal is true when an agent proposed an answer that no person has
	// accepted yet. The question stays open and what waits on it stays blocked.
	HasProposal     bool                 `json:"has_proposal"`
	ProposedAnswer  string               `json:"proposed_answer,omitempty"`
	ProposedChoice  string               `json:"proposed_choice,omitempty"`
	ProposedBy      string               `json:"proposed_by,omitempty"`
	ProposedSources []model.AnswerSource `json:"proposed_sources,omitempty"`
	ProposedWhy     string               `json:"proposed_why,omitempty"`
	Proposed        model.Date           `json:"proposed,omitempty"`
	// RejectedReason is why a person rejected the last proposed answer.
	RejectedReason string `json:"rejected_reason,omitempty"`
	RejectedBy     string `json:"rejected_by,omitempty"`
	Rev            string `json:"rev"`
	// Deferred is true while a person has put the question off (itself, or its
	// goal or task): it keeps blocking what depends on it. DeferUntil is the
	// date or "someday".
	Deferred       bool   `json:"deferred"`
	DeferUntil     string `json:"defer_until,omitempty"`
	DeferredReason string `json:"deferred_reason,omitempty"`
	// AgentLooking is true while queued or claimed agent work looks for evidence
	// for this question. The question stays open.
	AgentLooking bool `json:"agent_looking"`
	// ScopeName is the title of Scope, for people. A root question has none.
	ScopeName string `json:"scope_name,omitempty" nameof:"Scope"`
}

func questionOpen(v QuestionView) bool { return v.Status == model.QuestionOpen }

func questionView(d model.Document) (QuestionView, error) {
	q, ok := d.Meta.(*model.Question)
	if !ok {
		return QuestionView{}, fmt.Errorf("%s is a %s, not a question", d.ID, d.Meta.Header().Type)
	}
	return QuestionView{
		ID: d.ID, Folder: d.Folder, Title: d.Title, Kind: q.AsKind(), Status: q.Status,
		Scope: scopeID(q), Options: append([]string{}, q.Options...), Recommended: q.Recommended,
		RFX: q.RFX.Target(), Choice: q.Choice, AnsweredBy: q.AnsweredBy, Answered: q.Answered,
		Created: q.Created, Blocks: []string{}, DependsOn: targets(q.DependsOn), WaitingOn: []string{}, Rev: d.Rev,
		HasProposal: q.HasProposal(), ProposedAnswer: q.ProposedAnswer, ProposedChoice: q.ProposedChoice,
		ProposedBy: q.ProposedBy, ProposedSources: q.ProposedSources, ProposedWhy: q.ProposedWhy, Proposed: q.Proposed,
		RejectedReason: q.RejectedReason, RejectedBy: q.RejectedBy,
		DeferUntil: q.DeferUntil, DeferredReason: q.DeferredReason,
	}, nil
}

func scopeID(q *model.Question) string {
	if q.IsRoot() {
		return ScopeRootWord
	}
	return q.Scope.Target()
}

func getQuestion(ctx context.Context, s store.Store, id string) (model.Document, *model.Question, error) {
	id = normID(id)
	if id == "" {
		return model.Document{}, nil, errors.New("id is empty")
	}
	d, err := s.Get(ctx, id)
	if err != nil {
		return d, nil, err
	}
	q, ok := d.Meta.(*model.Question)
	if !ok {
		return d, nil, fmt.Errorf("%s is a %s, not a question", id, d.Meta.Header().Type)
	}
	return d, q, nil
}

// qrow is a question with the project and area its scope falls in.
type qrow struct {
	v             QuestionView
	project, area string
}

// dependents maps a document id to the open tasks and requests that list it in
// depends_on, and tells which of those are open.
type dependents struct {
	by   map[string][]string
	open map[string]bool
	task map[string]TaskView
	// deps holds the dependencies of open requests and questions.
	deps map[string][]string
}

func loadDependents(ctx context.Context, s store.Store) (dependents, error) {
	dp := dependents{by: map[string][]string{}, open: map[string]bool{}, task: map[string]TaskView{}, deps: map[string][]string{}}
	tasks, err := listTasks(ctx, s, "", "")
	if err != nil {
		return dp, err
	}
	for _, t := range tasks {
		dp.task[t.ID] = t
		if taskTerminal(t.Status) {
			continue
		}
		dp.open[t.ID] = true
		for _, to := range t.DependsOn {
			dp.by[to] = append(dp.by[to], t.ID)
		}
	}
	refs, err := s.List(ctx, store.Filter{Type: model.TypeRFX})
	if err != nil {
		return dp, err
	}
	for _, r := range refs {
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return dp, err
		}
		m, ok := doc.Meta.(*model.RFX)
		if !ok || m.Status != model.RFXOpen {
			continue
		}
		dp.open[r.ID] = true
		for _, l := range m.DependsOn {
			dp.by[l.Target()] = append(dp.by[l.Target()], r.ID)
		}
		dp.deps[r.ID] = targets(m.DependsOn)
	}
	qrefs, err := s.List(ctx, store.Filter{Type: model.TypeQuestion})
	if err != nil {
		return dp, err
	}
	for _, r := range qrefs {
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return dp, err
		}
		m, ok := doc.Meta.(*model.Question)
		if !ok || m.Status != model.QuestionOpen {
			continue
		}
		dp.open[r.ID] = true
		for _, l := range m.DependsOn {
			dp.by[l.Target()] = append(dp.by[l.Target()], r.ID)
		}
		dp.deps[r.ID] = targets(m.DependsOn)
	}
	return dp, nil
}

// chain returns every open document that waits on id, directly or not.
func (dp dependents) chain(id string) []string {
	seen := map[string]bool{id: true}
	queue := []string{id}
	var out []string
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, u := range dp.by[cur] {
			if !seen[u] {
				seen[u] = true
				out = append(out, u)
				queue = append(queue, u)
			}
		}
	}
	return out
}

// loadQuestions returns every question with its blocks, open first, then by
// what it blocks (most first), then oldest, then id.
func loadQuestions(ctx context.Context, s store.Store) ([]qrow, error) {
	refs, err := s.List(ctx, store.Filter{Type: model.TypeQuestion})
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return nil, nil
	}
	dp, err := loadDependents(ctx, s)
	if err != nil {
		return nil, err
	}
	projArea := map[string]string{}
	prefs, err := s.List(ctx, store.Filter{Type: model.TypeProject})
	if err != nil {
		return nil, err
	}
	for _, r := range prefs {
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		if p, ok := doc.Meta.(*model.Project); ok {
			projArea[r.ID] = p.Area.Target()
		}
	}
	deferred, err := deferredSet(ctx, s, s.Now().Format(dateLayout))
	if err != nil {
		return nil, err
	}
	looking, err := investigating(ctx, s)
	if err != nil {
		return nil, err
	}
	out := make([]qrow, 0, len(refs))
	for _, r := range refs {
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		v, err := questionView(doc)
		if err != nil {
			return nil, err
		}
		v.Deferred = deferred[v.ID]
		v.AgentLooking = looking[v.ID]
		x := qrow{v: v}
		sd, gerr := scopeDoc(ctx, s, v.Scope)
		switch {
		case v.Scope == ScopeRootWord:
			x.v.ScopeType = ScopeRoot
		case gerr != nil:
			x.v.ScopeType = "missing"
		default:
			switch m := sd.Meta.(type) {
			case *model.Project:
				x.v.ScopeType, x.project, x.area = ScopeProject, sd.ID, m.Area.Target()
			case *model.Area:
				x.v.ScopeType, x.area = ScopeArea, sd.ID
			case *model.Task:
				x.v.ScopeType, x.project, x.area = ScopeTask, m.Project.Target(), m.Area.Target()
				if x.area == "" {
					x.area = projArea[x.project]
				}
			default:
				x.v.ScopeType = "missing"
			}
		}
		if questionOpen(x.v) {
			for _, to := range x.v.DependsOn {
				if dep, err := s.Get(ctx, to); err != nil || !resolved(dep) {
					x.v.WaitingOn = append(x.v.WaitingOn, to)
				}
			}
			x.v.Blocks = append(x.v.Blocks, dp.by[v.ID]...)
			slices.Sort(x.v.Blocks)
			x.v.Blocking = len(dp.chain(v.ID))
		}
		out = append(out, x)
	}
	status := func(v QuestionView) int { return slices.Index(model.QuestionStatuses, v.Status) }
	slices.SortStableFunc(out, func(a, b qrow) int {
		return cmp.Or(
			cmp.Compare(status(a.v), status(b.v)),
			cmp.Compare(min(len(a.v.WaitingOn), 1), min(len(b.v.WaitingOn), 1)), // ready before waiting
			cmp.Compare(b.v.Blocking, a.v.Blocking),
			cmp.Compare(a.v.Created, b.v.Created),
			cmp.Compare(a.v.ID, b.v.ID))
	})
	return out, nil
}

// openQuestions returns the open questions that keep accepts.
func openQuestions(ctx context.Context, s store.Store, keep func(qrow) bool) ([]QuestionView, error) {
	all, err := loadQuestions(ctx, s)
	if err != nil {
		return nil, err
	}
	out := []QuestionView{}
	for _, x := range all {
		if questionOpen(x.v) && keep(x) {
			out = append(out, x.v)
		}
	}
	return out, nil
}

func scopeDoc(ctx context.Context, s store.Store, id string) (model.Document, error) {
	if id == ScopeRootWord {
		return model.Document{}, nil
	}
	return s.Get(ctx, id)
}

// scopeLink checks that exactly one scope is given and that it names a project,
// a task, an area, or root.
func scopeLink(ctx context.Context, s store.Store, project, task, area string, root bool) (model.Link, error) {
	project, task, area = normID(project), normID(task), normID(area)
	var given []string
	for _, g := range []struct {
		name string
		set  bool
	}{{"project", project != ""}, {"task", task != ""}, {"area", area != ""}, {"root", root}} {
		if g.set {
			given = append(given, g.name)
		}
	}
	switch len(given) {
	case 0:
		return "", errors.New("a question needs a scope: give project, task, area, or root (for the whole vault)")
	case 1:
	default:
		return "", fmt.Errorf("a question has one scope: got %s", strings.Join(given, " and "))
	}
	switch {
	case root:
		return model.ScopeRoot, nil
	case project != "":
		doc, _, err := getProject(ctx, s, project)
		if err != nil {
			return "", fmt.Errorf("project %s: %w", project, err)
		}
		if doc.Folder == model.FolderArchive {
			return "", fmt.Errorf("project %s is archived", project)
		}
		return model.LinkTo(doc.ID), nil
	case task != "":
		doc, _, err := getTask(ctx, s, task)
		if err != nil {
			return "", fmt.Errorf("task %s: %w", task, err)
		}
		return model.LinkTo(doc.ID), nil
	}
	return areaLink(ctx, s, area)
}

func checkOptions(kind model.QuestionKind, options []string, recommended string) ([]string, string, error) {
	if kind != "" && !slices.Contains(model.QuestionKinds, kind) {
		return nil, "", fmt.Errorf("kind %q must be one of %v", kind, model.QuestionKinds)
	}
	var opts []string
	for _, o := range options {
		o = strings.Join(strings.Fields(o), " ")
		if o == "" {
			return nil, "", errors.New("an option is empty")
		}
		if slices.Contains(opts, o) {
			return nil, "", fmt.Errorf("option %q is listed twice", o)
		}
		opts = append(opts, o)
	}
	recommended = strings.Join(strings.Fields(recommended), " ")
	if recommended != "" && !slices.Contains(opts, recommended) {
		return nil, "", fmt.Errorf("recommended %q is not one of the options", recommended)
	}
	return opts, recommended, nil
}

// QuestionAddInput is the input of question_add. Exactly one of Project, Task,
// Area, and Root is required.
type QuestionAddInput struct {
	Text        string             `json:"text"`                  // the question; the first line is the title
	Project     string             `json:"project,omitempty"`     // scope: a project id
	Task        string             `json:"task,omitempty"`        // scope: a task id
	Area        string             `json:"area,omitempty"`        // scope: an area id
	Root        bool               `json:"root,omitempty"`        // scope: the whole vault
	Kind        model.QuestionKind `json:"kind,omitempty"`        // question (default), decision, or input
	Options     []string           `json:"options,omitempty"`     // the choices, when there is a fixed set
	Recommended string             `json:"recommended,omitempty"` // one of the options
}

// QuestionAddOutput is the output of question_add.
type QuestionAddOutput struct {
	ID     string       `json:"id"`
	Folder model.Folder `json:"folder"`
	Rev    string       `json:"rev"`
}

// QuestionAdd records an open question.
var QuestionAdd = define("question_add", "Record an open question, decision, or input the owner owes, with one scope (project, task, area, or root); a task is work to do, a question is something to answer", func(ctx context.Context, d Deps, in QuestionAddInput) (QuestionAddOutput, error) {
	title, body, err := splitCapture(in.Text)
	if err != nil {
		return QuestionAddOutput{}, fmt.Errorf("text: %w", err)
	}
	scope, err := scopeLink(ctx, d.Store, in.Project, in.Task, in.Area, in.Root)
	if err != nil {
		return QuestionAddOutput{}, err
	}
	opts, rec, err := checkOptions(in.Kind, in.Options, in.Recommended)
	if err != nil {
		return QuestionAddOutput{}, err
	}
	kind := in.Kind
	if kind == model.KindQuestion {
		kind = "" // the default is not written
	}
	doc, err := d.Store.Create(ctx, model.Document{
		Folder: model.FolderQuestions, Title: title, Body: body,
		Meta: &model.Question{
			Common: model.Common{Type: model.TypeQuestion}, Status: model.QuestionOpen, Kind: kind,
			Scope: scope, Options: opts, Recommended: rec,
		},
	})
	if err != nil {
		return QuestionAddOutput{}, err
	}
	return QuestionAddOutput{ID: doc.ID, Folder: doc.Folder, Rev: doc.Rev}, nil
})

// QuestionListInput is the input of question_list. Empty fields match all.
type QuestionListInput struct {
	Status  model.QuestionStatus `json:"status,omitempty" query:"status"`
	Project string               `json:"project,omitempty" query:"project"` // scoped to the project or to one of its tasks
	Task    string               `json:"task,omitempty" query:"task"`       // scoped to this task
	Area    string               `json:"area,omitempty" query:"area"`       // scoped to the area, to one of its projects, or to one of their tasks
	Root    bool                 `json:"root,omitempty" query:"root"`       // only vault-level questions
	All     bool                 `json:"all,omitempty" query:"all"`         // include answered and dropped questions
	// Deferred includes the questions a person put off (Later). They are left
	// out by default, as the agent lists leave them out; each is marked.
	Deferred bool `json:"deferred,omitempty" query:"deferred"`
}

// QuestionListOutput is the output of question_list.
type QuestionListOutput struct {
	Questions []QuestionView `json:"questions"`
}

// QuestionList lists open questions, the ones that block the most first.
var QuestionList = define("question_list", "List open questions, ordered by what they block", func(ctx context.Context, d Deps, in QuestionListInput) (QuestionListOutput, error) {
	if in.Status != "" && !slices.Contains(model.QuestionStatuses, in.Status) {
		return QuestionListOutput{}, fmt.Errorf("status %q must be one of %v", in.Status, model.QuestionStatuses)
	}
	all, err := loadQuestions(ctx, d.Store)
	if err != nil {
		return QuestionListOutput{}, err
	}
	project, task, area := normID(in.Project), normID(in.Task), normID(in.Area)
	out := QuestionListOutput{Questions: []QuestionView{}}
	for _, x := range all {
		switch {
		case in.Status != "" && x.v.Status != in.Status,
			in.Status == "" && !in.All && !questionOpen(x.v),
			project != "" && x.project != project,
			task != "" && x.v.Scope != task,
			area != "" && x.area != area,
			in.Root && x.v.ScopeType != ScopeRoot,
			!in.Deferred && x.v.Deferred && questionOpen(x.v):
			continue
		}
		out.Questions = append(out.Questions, x.v)
	}
	return out, nil
})

// QuestionShowInput is the input of question_show.
type QuestionShowInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// QuestionShowOutput is the output of question_show.
type QuestionShowOutput struct {
	Question QuestionView `json:"question"`
	Body     string       `json:"body"`
}

// QuestionShow returns one question with its body and answer.
var QuestionShow = define("question_show", "Show a question with its body and answer", func(ctx context.Context, d Deps, in QuestionShowInput) (QuestionShowOutput, error) {
	doc, _, err := getQuestion(ctx, d.Store, in.ID)
	if err != nil {
		return QuestionShowOutput{}, err
	}
	all, err := loadQuestions(ctx, d.Store)
	if err != nil {
		return QuestionShowOutput{}, err
	}
	for _, x := range all {
		if x.v.ID == doc.ID {
			return QuestionShowOutput{Question: x.v, Body: doc.Body}, nil
		}
	}
	v, err := questionView(doc)
	return QuestionShowOutput{Question: v, Body: doc.Body}, err
})

// QuestionAnswerInput is the input of question_answer.
type QuestionAnswerInput struct {
	ID     string `json:"id"`
	Choice string `json:"choice,omitempty"` // one of the options, or its number as listed
	Text   string `json:"text,omitempty"`   // the answer in words, or a free answer when the options do not fit. Never a secret: record where it is kept
	Why    string `json:"why,omitempty"`    // the reason; required for a decision
	Source string `json:"source,omitempty"` // where the answer came from: a URL, a meeting, a person
	By     string `json:"by,omitempty"`     // who answered; default the acting user
	// Sources is required from an agent: one or more URLs or vault document
	// ids, each with an optional quote. An agent's answer is only proposed.
	Sources []model.AnswerSource `json:"sources,omitempty"`
}

// QuestionAnswerOutput is the output of question_answer and question_drop.
type QuestionAnswerOutput struct {
	ID         string               `json:"id"`
	Status     model.QuestionStatus `json:"status"`
	Choice     string               `json:"choice,omitempty"`
	AnsweredBy string               `json:"answered_by,omitempty"`
	Answered   model.Date           `json:"answered,omitempty"`
	// Unblocked lists the open tasks and requests that had waited only on this
	// question and can now start.
	Unblocked []string `json:"unblocked"`
	// Proposed is true when an agent's answer was recorded as a proposal: the
	// question is still open until a person accepts it.
	Proposed bool   `json:"proposed,omitempty"`
	Rev      string `json:"rev"`
}

// QuestionAnswerInput gets Sources for an agent: see answerSources.

// reviewerKey carries the person who approved a proposal item to the op the
// item runs, so the answer it records names that person.
type reviewerKey struct{}

func withReviewer(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, reviewerKey{}, actor)
}

// agentSurface reports whether the caller is an agent: a surface that is not a
// person's. An approved proposal item is not an agent: a person approved it.
func agentSurface(d Deps) bool {
	s := d.Store.Info().Surface
	return !slices.Contains(PersonSurfaces, s) && s != surfaceProposal
}

// answerParts checks choice, text, and why against the question and returns
// them in their stored form.
func answerParts(q *model.Question, in QuestionAnswerInput) (choice, text, why string, err error) {
	choice = strings.Join(strings.Fields(in.Choice), " ")
	text = strings.TrimSpace(in.Text)
	why = strings.Join(strings.Fields(in.Why), " ")
	switch {
	case choice != "":
		if len(q.Options) == 0 {
			return "", "", "", errors.New("this question has no options: answer with text")
		}
		i := slices.IndexFunc(q.Options, func(o string) bool { return strings.EqualFold(o, choice) })
		if n, err := strconv.Atoi(choice); i < 0 && err == nil && n >= 1 && n <= len(q.Options) {
			i = n - 1 // the number of an option, as listed
		}
		if i < 0 {
			return "", "", "", fmt.Errorf("choice %q is not one of the options: %s (or answer with text)", choice, strings.Join(q.Options, " | "))
		}
		choice = q.Options[i]
	case text == "":
		return "", "", "", errors.New("an answer needs a choice or text")
	}
	if q.AsKind() == model.KindDecision && why == "" {
		return "", "", "", errors.New("a decision needs its reason: pass why")
	}
	return choice, text, why, nil
}

// resolveQuestion applies the answer to a question that is open and saves it.
// From an agent it records a proposed answer instead, and the question stays
// open. From an approved proposal item it answers, naming the approver.
func resolveQuestion(ctx context.Context, d Deps, in QuestionAnswerInput) (QuestionAnswerOutput, error) {
	doc, q, err := getQuestion(ctx, d.Store, in.ID)
	if err != nil {
		return QuestionAnswerOutput{}, err
	}
	if q.Status != model.QuestionOpen {
		return QuestionAnswerOutput{}, fmt.Errorf("%s is already %s", doc.ID, q.Status)
	}
	choice, text, why, err := answerParts(q, in)
	if err != nil {
		return QuestionAnswerOutput{}, err
	}
	info := d.Store.Info()
	agent := agentSurface(d)
	sources, err := answerSources(ctx, d, in.Sources, agent || info.Surface == surfaceProposal)
	if err != nil {
		return QuestionAnswerOutput{}, err
	}
	if err := lintText("text", text); err != nil {
		return QuestionAnswerOutput{}, err
	}
	today := d.Store.Now().Format(dateLayout)
	if agent {
		q.ProposedAnswer, q.ProposedChoice, q.ProposedBy = text, choice, info.Actor
		q.ProposedSources, q.ProposedWhy, q.Proposed = sources, why, model.Date(today)
		q.RejectedReason, q.RejectedBy = "", ""
		out, err := saveQuestion(ctx, d, doc, q)
		out.Proposed = true
		return out, err
	}
	by := strings.Join(strings.Fields(in.By), " ")
	if by == "" {
		by = info.Actor
	}
	if info.Surface == surfaceProposal {
		// A person approved the item: they answer, the agent proposed.
		q.ProposedBy, q.ProposedSources, q.Proposed = info.Actor, sources, model.Date(today)
		by, _ = ctx.Value(reviewerKey{}).(string)
		if by == "" {
			by = "owner"
		}
	}
	return writeAnswer(ctx, d, doc, q, answerRecord{choice: choice, text: text, why: why, source: in.Source, sources: sources, by: by})
}

type answerRecord struct {
	choice, text, why, source string
	sources                   []model.AnswerSource
	by                        string
}

// sourceLine is one cited source as a body line.
func sourceLine(s model.AnswerSource) string {
	ref := s.Ref
	if !strings.HasPrefix(ref, "http") && !strings.ContainsAny(ref, " \t") {
		ref = "[[" + ref + "]]"
	}
	if s.Quote != "" {
		ref += `: "` + s.Quote + `"`
	}
	return "- Source: " + ref
}

// writeAnswer marks q answered by the person in r.by and writes the answer
// block. A proposal that was accepted stays on the question as the record of
// who proposed it and from what, apart from its text.
func writeAnswer(ctx context.Context, d Deps, doc model.Document, q *model.Question, r answerRecord) (QuestionAnswerOutput, error) {
	info := d.Store.Info()
	today := d.Store.Now().Format(dateLayout)
	block := []string{}
	if r.text != "" {
		block = append(block, r.text, "")
	}
	if r.choice != "" {
		block = append(block, "- Choice: "+r.choice)
	}
	if r.why != "" {
		block = append(block, "- Why: "+r.why)
	}
	if src := strings.Join(strings.Fields(r.source), " "); src != "" {
		block = append(block, "- Source: "+src)
	}
	for _, s := range r.sources {
		block = append(block, sourceLine(s))
	}
	if q.ProposedBy != "" {
		block = append(block, "- Proposed by: "+q.ProposedBy)
	}
	block = append(block, fmt.Sprintf("- By: %s, via %s, on %s", r.by, info.Surface, today))
	doc.Body = appendToSection(doc.Body, answerHeading, strings.Join(block, "\n"))
	q.Status, q.Choice, q.AnsweredBy, q.Answered = model.QuestionAnswered, r.choice, r.by, model.Date(today)
	q.ProposedAnswer, q.ProposedChoice, q.ProposedWhy = "", "", ""
	return saveQuestion(ctx, d, doc, q)
}

func saveQuestion(ctx context.Context, d Deps, doc model.Document, q *model.Question) (QuestionAnswerOutput, error) {
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return QuestionAnswerOutput{}, err
	}
	un, err := unblockedBy(ctx, d.Store, doc.ID)
	if err != nil {
		return QuestionAnswerOutput{}, err
	}
	return QuestionAnswerOutput{ID: doc.ID, Status: q.Status, Choice: q.Choice, AnsweredBy: q.AnsweredBy,
		Answered: q.Answered, Unblocked: un, Rev: saved.Rev}, nil
}

// unblockedBy lists the open tasks and requests that depend on id and whose
// every dependency is now resolved. Dependents read the question's state each
// time, so nothing else has to change when it is answered.
func unblockedBy(ctx context.Context, s store.Store, id string) ([]string, error) {
	dp, err := loadDependents(ctx, s)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, u := range dp.by[id] {
		deps := dp.deps[u]
		if t, ok := dp.task[u]; ok {
			deps = t.DependsOn
		}
		waiting := false
		for _, to := range deps {
			dep, err := s.Get(ctx, to)
			if err != nil || !resolved(dep) {
				waiting = true
				break
			}
		}
		if !waiting {
			out = append(out, u)
		}
	}
	slices.Sort(out)
	return out, nil
}

// QuestionAnswer records the answer of an open question and marks it answered.
// Tasks that depend on it can start at once.
var QuestionAnswer = define("question_answer", "Answer an open question with a choice or text; a decision needs its reason. From an agent this only proposes an answer: cite at least one source (a URL or vault document id, with a quote); the owner accepts or rejects it, and the question stays open until then. A person may add sources too, and may use free text such as a meeting", func(ctx context.Context, d Deps, in QuestionAnswerInput) (QuestionAnswerOutput, error) {
	return resolveQuestion(ctx, d, in)
})

// QuestionDropInput is the input of question_drop.
type QuestionDropInput struct {
	ID     string `json:"id"`
	Reason string `json:"reason,omitempty"` // why nobody needs the answer now
}

// QuestionDrop marks an open question dropped: nobody needs the answer. Tasks
// that depended on it stop waiting.
var QuestionDrop = define("question_drop", "Drop an open question that nobody needs answered; tasks that depended on it stop waiting", func(ctx context.Context, d Deps, in QuestionDropInput) (QuestionAnswerOutput, error) {
	doc, q, err := getQuestion(ctx, d.Store, in.ID)
	if err != nil {
		return QuestionAnswerOutput{}, err
	}
	if q.Status != model.QuestionOpen {
		return QuestionAnswerOutput{}, fmt.Errorf("%s is already %s", doc.ID, q.Status)
	}
	reason := strings.Join(strings.Fields(in.Reason), " ")
	if reason == "" {
		reason = "No reason given."
	}
	doc.Body = appendToSection(doc.Body, droppedHeading, reason)
	q.Status = model.QuestionDropped
	return saveQuestion(ctx, d, doc, q)
})

// QuestionAskInput is the input of question_ask. It is rfx_add for a question:
// who to ask, how, and the route.
type QuestionAskInput struct {
	ID        string       `json:"id"`
	To        string       `json:"to"`                   // person or group id
	Via       model.Medium `json:"via"`                  // how it goes
	Route     string       `json:"route"`                // domain id
	Value     string       `json:"value,omitempty"`      // address or handle; default the person's preferred contact
	SelfServe string       `json:"self_serve,omitempty"` // what was checked first
	NextNudge string       `json:"next_nudge,omitempty"` // YYYY-MM-DD
}

// QuestionAskOutput is the output of question_ask.
type QuestionAskOutput struct {
	ID       string   `json:"id"`
	RFX      string   `json:"rfx"`
	Warnings []string `json:"warnings"`
	Rev      string   `json:"rev"`
}

// QuestionAsk turns an open question into an outbound request to a person,
// through rfx_add. The two link to each other. A reply with rfx_reply (outcome
// answered or no) answers the question. Para sends nothing.
var QuestionAsk = define("question_ask", "Ask a person an open question: makes an outbound request linked both ways; rfx_reply then answers the question", func(ctx context.Context, d Deps, in QuestionAskInput) (QuestionAskOutput, error) {
	doc, q, err := getQuestion(ctx, d.Store, in.ID)
	if err != nil {
		return QuestionAskOutput{}, err
	}
	if q.Status != model.QuestionOpen {
		return QuestionAskOutput{}, fmt.Errorf("%s is already %s", doc.ID, q.Status)
	}
	if q.RFX != "" {
		if _, m, err := getRFX(ctx, d, q.RFX.Target()); err == nil && m.Status == model.RFXOpen {
			return QuestionAskOutput{}, fmt.Errorf("%s was already asked in %s", doc.ID, q.RFX.Target())
		}
	}
	if strings.TrimSpace(in.To) == "" || in.Via == "" {
		return QuestionAskOutput{}, errors.New("to and via are required: who to ask, and by what medium")
	}
	ask := doc.Title
	if b := strings.TrimSpace(askBody(doc.Body)); b != "" {
		ask += "\n\n" + b
	}
	rin := RFXAddInput{
		Direction: model.RFXOut, Ask: ask, Route: in.Route, SelfServe: in.SelfServe, NextNudge: in.NextNudge,
		Targets:  []TargetInput{{To: in.To, Medium: in.Via, Value: in.Value}},
		Question: doc.ID,
	}
	if !q.IsRoot() && q.Scope.Target() != "" {
		if sd, err := d.Store.Get(ctx, q.Scope.Target()); err == nil {
			switch m := sd.Meta.(type) {
			case *model.Project:
				rin.Project = sd.ID
			case *model.Task:
				rin.Project = m.Project.Target()
			}
		}
	}
	r, err := RFXAdd.Run(ctx, d, rin)
	if err != nil {
		return QuestionAskOutput{}, err
	}
	q.RFX = model.LinkTo(r.ID)
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return QuestionAskOutput{}, fmt.Errorf("%s was made but the question was not linked to it: %w", r.ID, err)
	}
	return QuestionAskOutput{ID: doc.ID, RFX: r.ID, Warnings: r.Warnings, Rev: saved.Rev}, nil
})

// askBody is the body of a question without its answer sections.
func askBody(body string) string {
	for _, h := range []string{answerHeading, droppedHeading} {
		if i := strings.Index(body, h); i >= 0 {
			body = body[:i]
		}
	}
	return body
}

// answerFromReply answers the question of an RFX when a reply came in. A
// partial reply leaves the question open. From an agent the answer is only
// proposed; proposed says so.
func answerFromReply(ctx context.Context, d Deps, question, from string, in RFXReplyInput, sources []model.AnswerSource) (id string, proposed bool, err error) {
	if in.Outcome == ReplyPartial {
		return "", false, nil
	}
	doc, q, err := getQuestion(ctx, d.Store, question)
	if err != nil {
		return "", false, err
	}
	if q.Status != model.QuestionOpen {
		return "", false, nil
	}
	text := strings.TrimSpace(in.Text)
	why := strings.Join(strings.Fields(in.Why), " ")
	if why == "" && q.AsKind() == model.KindDecision {
		why = "Given in the reply."
	}
	if in.Outcome == ReplyNo {
		text = "No. " + text
	}
	out, err := resolveQuestion(ctx, d, QuestionAnswerInput{ID: doc.ID, Text: text, Why: why, Source: in.URL, By: from, Sources: sources})
	return out.ID, out.Proposed, err
}
