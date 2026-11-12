package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/recur"
)

// Tick is the one scheduled pass. Para has no scheduler of its own: `para tick`
// runs from cron or a timer, and `para serve` runs it on a timer. It is not an
// op, so no agent surface can call it.
//
// A synced vault lives on more than one machine, and the vault lock does not
// cross machines, so two machines running tick would each make the same
// recurring task. Tick therefore runs only on the install the vault names with
// the property request.runner, and says why when it does not. A pass is safe
// to repeat: the work it does leaves state that the next pass sees.
//
// Tick acts as actor and surface "tick". Whether a request it makes is queued
// or only proposed follows the vault's request.surfaces, like any other
// surface, so queueing agent work from a timer is a choice the owner makes by
// adding "tick" to that list.

// TickSurface is the actor and surface of a tick.
const TickSurface = "tick"

// TickInput is the input of Tick.
type TickInput struct{}

// TickOutput is what one pass did.
type TickOutput struct {
	Vault string `json:"vault"`
	Ran   bool   `json:"ran"`
	// Skipped says why nothing ran: this install is not the runner.
	Skipped string `json:"skipped,omitempty"`
	// NoRunner is set when the vault names no runner. That is a setup error:
	// tick refuses to guess which machine does the work.
	NoRunner bool   `json:"no_runner,omitempty"`
	Runner   string `json:"runner,omitempty"`
	// Revived lists the recurring tasks made because one was done by hand with
	// no next occurrence; Requests lists the requests made for recurring tasks
	// assigned to an agent; Proposed counts those left for a person to accept.
	Revived  []string `json:"revived"`
	Requests []string `json:"requests"`
	// Journals lists the mechanical journals tick made this pass. The
	// narrative is produce work (Produce) of the built-in journal-summary
	// obligation, made when journal.agent is set.
	Journals []string `json:"journals"`
	// Produce lists the produce requests tick made this pass, one per due
	// occurrence of an obligation an agent owes (produce.go); they are in
	// Requests too. Occurrences lists the next occurrences of those series
	// made this pass; Defaults the built-in obligations made (the daily
	// journal summary).
	Produce     []string `json:"produce"`
	Occurrences []string `json:"occurrences"`
	Defaults    []string `json:"defaults"`
	// Sweeps lists the sweep requests tick made this pass, one per due
	// source with none open; they are in Requests too.
	Sweeps   []string `json:"sweeps"`
	Proposed int      `json:"proposed"`
	// Crons lists the cron runs this pass made a task for, as
	// "<occurrence>: <task>" (cron.go).
	Crons []string `json:"crons"`
	// CronsPaused lists the due cron runs this pass skipped because the
	// cron's goal is parked, as "<occurrence>: goal parked; next <date>".
	CronsPaused []string `json:"crons_paused"`
	// Gaps lists the fill-gap requests tick made this pass (gap.go); they
	// are in Requests too.
	Gaps []string `json:"gaps"`
	// Reasons lists the reason checks tick made this pass (reasontick.go):
	// investigate work for an open item with no reason; they are in
	// Requests too.
	Reasons []string `json:"reasons"`
	// Replies lists the reply obligations tick made this pass for incoming
	// messages that expect a reply and had none (ioreply.go).
	Replies []string `json:"replies"`
	// Promises lists the obligations whose promise this pass set, as
	// "<id>: <kept|renegotiated|broken>" (promise.go).
	Promises []string `json:"promises"`
	// Prepares lists the prepare requests tick made this pass (prepared.go);
	// they are in Requests too.
	Prepares []string `json:"prepares"`
	// NotQueued says why tick made no fill-gap, prepare, or produce work: it cannot
	// queue work, and proposed work would ask the owner to approve each.
	NotQueued string `json:"not_queued,omitempty"`
	// Nudges lists open requests whose next_nudge is today or earlier. Today and
	// agenda show them; tick only names them.
	Nudges []string `json:"nudges"`
	// Goals lists the goals whose pivot date has come or whose signal reading
	// is due, as "<id>: <reason>". The agenda shows them with the reason.
	Goals []string `json:"goals"`
	// MeetingsSkipped lists the past optional meetings this pass marked
	// skipped (attend no): the owner made no attend choice. Each has a
	// receipt that undoes it.
	MeetingsSkipped []string `json:"meetings_skipped"`
	// Followed lists the items put off until an obligation starts or ends
	// whose date this pass moved, or that came back because the obligation
	// closed: for a change made by hand (deferfollow.go).
	Followed []string `json:"followed"`
	// Changes counts the files reconcile found changed outside para.
	Changes  int      `json:"changes"`
	Warnings []string `json:"warnings"`
	// Error is set when this vault could not be ticked (para tick --all goes on
	// to the next vault).
	Error string `json:"error,omitempty"`
}

// Failed makes the command exit non-zero when the vault names no runner.
func (o TickOutput) Failed() error {
	if o.NoRunner {
		return errors.New(o.Skipped)
	}
	if o.Error != "" {
		return errors.New(o.Error)
	}
	return nil
}

// Tick runs one pass over the vault of d. The store should act as surface
// tick; Tick does that itself.
func Tick(ctx context.Context, d Deps, _ TickInput) (TickOutput, error) {
	base := d.Store
	info := base.Info()
	out := TickOutput{Vault: base.VaultID(), Revived: []string{}, Requests: []string{}, Journals: []string{}, Produce: []string{}, Occurrences: []string{}, Defaults: []string{}, Sweeps: []string{}, Gaps: []string{}, Reasons: []string{}, Replies: []string{}, Promises: []string{}, Prepares: []string{}, Nudges: []string{}, Goals: []string{}, MeetingsSkipped: []string{}, Followed: []string{}, Crons: []string{}, CronsPaused: []string{}, Warnings: []string{}}
	runner, err := base.Config(runnerConfig)
	if err != nil {
		return out, fmt.Errorf("tick: %w", err)
	}
	runner = strings.TrimSpace(runner)
	out.Runner = runner
	switch {
	case info.Install == "":
		return out, errors.New("tick: no install id")
	case runner == "":
		out.NoRunner = true
		out.Skipped = fmt.Sprintf("vault %s names no runner: on the machine that should tick, run: para vault config set request.runner %s", out.Vault, info.Install)
		return out, nil
	case runner != info.Install:
		out.Skipped = fmt.Sprintf("this install (%s) is not the runner (%s)", info.Install, runner)
		return out, nil
	}
	out.Ran = true
	d.Store = base.ActAs(TickSurface, TickSurface)

	rec, err := d.Store.Reconcile(ctx)
	if err != nil {
		return out, fmt.Errorf("tick: reconcile: %w", err)
	}
	out.Changes = len(rec.Changes)

	if err := tickRecurring(ctx, d, &out); err != nil {
		return out, fmt.Errorf("tick: recurring: %w", err)
	}
	if err := tickCrons(ctx, d, &out); err != nil {
		return out, fmt.Errorf("tick: crons: %w", err)
	}
	if err := tickRequests(ctx, d, &out); err != nil {
		return out, fmt.Errorf("tick: requests: %w", err)
	}
	if err := tickSweeps(ctx, d, &out); err != nil {
		return out, fmt.Errorf("tick: sweeps: %w", err)
	}
	if err := tickGaps(ctx, d, &out); err != nil {
		return out, fmt.Errorf("tick: gaps: %w", err)
	}
	if err := tickReasons(ctx, d, &out); err != nil {
		return out, fmt.Errorf("tick: reasons: %w", err)
	}
	if err := tickPrepare(ctx, d, &out); err != nil {
		return out, fmt.Errorf("tick: prepare: %w", err)
	}
	if err := tickJournals(ctx, d, &out); err != nil {
		return out, fmt.Errorf("tick: journals: %w", err)
	}
	if err := tickProduce(ctx, d, &out); err != nil {
		return out, fmt.Errorf("tick: produce: %w", err)
	}
	if err := tickNudges(ctx, d, &out); err != nil {
		return out, fmt.Errorf("tick: nudges: %w", err)
	}
	if err := tickGoals(ctx, d, &out); err != nil {
		return out, fmt.Errorf("tick: goals: %w", err)
	}
	if err := tickOptionalMeetings(ctx, d, &out); err != nil {
		return out, fmt.Errorf("tick: optional meetings: %w", err)
	}
	if err := tickReplies(ctx, d, &out); err != nil {
		return out, fmt.Errorf("tick: replies: %w", err)
	}
	if err := tickPromises(ctx, d, &out); err != nil {
		return out, fmt.Errorf("tick: promises: %w", err)
	}
	if out.Followed, err = followAll(ctx, d); err != nil {
		return out, fmt.Errorf("tick: follow obligations: %w", err)
	}
	return out, nil
}

// notQueued notes that tick made no gap, prepare, or produce work because it
// cannot queue.
func (o *TickOutput) notQueued() {
	o.NotQueued = "gap, prepare, and produce work is made only when tick can queue it: add tick to the vault setting request.surfaces"
}

// tickGoals names the goals whose pivot date has come or whose signal reading
// is due. Tick only names them, like nudges: the agenda holds the rows.
func tickGoals(ctx context.Context, d Deps, out *TickOutput) error {
	today := d.Store.Now().Local().Format(recur.Layout)
	ws, err := goalWatches(ctx, d.Store, today)
	if err != nil {
		return err
	}
	for _, w := range ws {
		if w.Date <= today {
			out.Goals = append(out.Goals, w.ID+": "+w.Reason)
		}
	}
	return nil
}

// tickRecurring makes the next occurrence of a recurring task that was marked
// done without one, such as by editing the file. A task dropped by hand ends
// its recurrence: only done revives it.
func tickRecurring(ctx context.Context, d Deps, out *TickOutput) error {
	tasks, err := listTasks(ctx, d.Store, "", "")
	if err != nil {
		return err
	}
	hasNext := map[string]bool{}
	for _, t := range tasks {
		if t.Previous != "" {
			hasNext[t.Previous] = true
		}
	}
	for _, t := range tasks {
		if t.Status != model.TaskDone || t.Recur == "" || hasNext[t.ID] {
			continue
		}
		doc, tm, err := getTask(ctx, d.Store, t.ID)
		if err != nil {
			return err
		}
		rule, ok := recurRule(tm.Recur)
		if !ok {
			continue
		}
		next, req, warns, err := makeNext(ctx, d, doc, tm, rule)
		if err != nil {
			return fmt.Errorf("%s: %w", t.ID, err)
		}
		out.Warnings = append(out.Warnings, warns...)
		if next != "" && !hasNext[t.ID] {
			out.Revived = append(out.Revived, next)
			hasNext[t.ID] = true
		}
		if req != "" {
			out.noteRequest(ctx, d, req)
		}
	}
	return nil
}

// noteRequest records a request tick made, and whether it is only proposed.
func (o *TickOutput) noteRequest(ctx context.Context, d Deps, id string) {
	o.Requests = append(o.Requests, id)
	if _, r, err := getRequest(ctx, d.Store, id); err == nil && r.Status == model.RequestProposed {
		o.Proposed++
	}
}

// tickRequests makes the request for an open recurring task assigned to an
// agent that is due and has none. A task that has any request, of any status,
// is left alone: a request the owner cancelled or a run that failed is not
// started again by a timer.
func tickRequests(ctx context.Context, d Deps, out *TickOutput) error {
	tasks, err := listTasks(ctx, d.Store, "", "")
	if err != nil {
		return err
	}
	var due []TaskView
	today := d.Store.Now().Local().Format(recur.Layout)
	for _, t := range tasks {
		if t.Status != model.TaskTodo || t.Recur == "" || !strings.HasPrefix(t.Assignee, AgentPrefix) {
			continue
		}
		if t.Due != "" && string(t.Due) > today {
			continue
		}
		if _, err := recur.Parse(t.Recur); err != nil {
			continue
		}
		due = append(due, t)
	}
	if len(due) == 0 {
		return nil
	}
	reqs, err := loadRequests(ctx, d.Store)
	if err != nil {
		return err
	}
	asked := map[string]bool{}
	for _, r := range reqs {
		for _, l := range r.Meta.(*model.Request).Links {
			asked[l.Target()] = true
		}
	}
	for _, t := range due {
		if asked[t.ID] {
			continue
		}
		doc, err := d.Store.Get(ctx, t.ID)
		if err != nil {
			return err
		}
		id, warns := agentRequest(ctx, d, doc, nil)
		out.Warnings = append(out.Warnings, warns...)
		if id == "" {
			continue
		}
		out.noteRequest(ctx, d, id)
	}
	return nil
}

// tickNudges names the open requests whose nudge date has come.
func tickNudges(ctx context.Context, d Deps, out *TickOutput) error {
	rfx, err := loadRFX(ctx, d)
	if err != nil {
		return err
	}
	today := d.Store.Now().Local().Format(recur.Layout)
	for _, r := range rfx {
		if r.Status == model.RFXOpen && r.NextNudge != "" && r.NextNudge <= today {
			out.Nudges = append(out.Nudges, r.ID)
		}
	}
	slices.Sort(out.Nudges)
	return nil
}

// SweepKind is the kind of a request to sweep one source.
const SweepKind = "sweep"

// tickSweeps makes one sweep request for each active source that is due and
// has no open sweep request. Only an agent that declared the source can
// claim it (request_next); unclaimed, it stays in the queue, and tick makes
// no second one. Like any request tick makes, it is queued only when
// request.surfaces lists tick, else proposed for a person to accept.
func tickSweeps(ctx context.Context, d Deps, out *TickOutput) error {
	sources, err := loadSources(ctx, d.Store)
	if err != nil || len(sources) == 0 {
		return err
	}
	open, err := openSweeps(ctx, d.Store)
	if err != nil {
		return err
	}
	today := d.Store.Now().Local().Format(recur.Layout)
	for _, doc := range sources {
		m := doc.Meta.(*model.SourceDoc)
		if m.Status != model.SourceActive || open[doc.ID] != "" {
			continue
		}
		if due := sourceDue(m); due == "" || due > today {
			continue
		}
		id, err := sweepRequest(ctx, d, doc)
		if err != nil {
			return fmt.Errorf("%s: %w", doc.ID, err)
		}
		out.Sweeps = append(out.Sweeps, id)
		out.noteRequest(ctx, d, id)
	}
	return nil
}

// sweepRequest makes the request to sweep one source, through request_add.
func sweepRequest(ctx context.Context, d Deps, src model.Document) (string, error) {
	ask := fmt.Sprintf("Sweep the source %s. Claim this only if you reach it (agent_sources). "+
		"Run sweep_plan with sources [%s], read it with your own tools from since, write what you find with the usual ops, "+
		"and end with sweep_record with source %s and request, the id of this request.", src.Title, src.ID, src.ID)
	out, err := addRequest(ctx, d, RequestAddInput{Title: "Sweep " + src.Title, Ask: ask, Kind: SweepKind}, model.LinkTo(src.ID))
	return out.ID, err
}
