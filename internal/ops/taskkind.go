package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// Kinds of task. A meeting is a task with a time, people, and the questions it
// should settle; a call is a task with a person and attempts; work with and a
// commitment are tasks with a person and a way to tell they are done.

// CallAttemptCap is how many attempts at a call make it stuck: the owner
// decides whether to keep trying.
const CallAttemptCap = 3

// Call results, the result of a task_attempt.
const (
	CallNoAnswer    = "no_answer"
	CallLeftMessage = "left_message"
	CallReached     = "reached"
)

var callResults = []string{CallNoAnswer, CallLeftMessage, CallReached}

// callWhat is the "what" of an attempt line that is a call attempt.
const callWhat = "call"

// callAttempts counts the call attempts in the Attempts section of a body and
// returns the result of the latest. A line reads "- time actor: call: result",
// where the result may continue with "; retry <when>".
func callAttempts(body string) (int, string) {
	n, last := 0, ""
	for _, l := range sectionLines(body, "## Attempts") {
		parts := strings.SplitN(strings.TrimPrefix(l, "- "), ": ", 3)
		if len(parts) < 3 || parts[1] != callWhat {
			continue
		}
		n++
		last, _, _ = strings.Cut(parts[2], ";")
		last, _, _ = strings.Cut(last, " (sources:")
		last = strings.TrimSpace(last)
	}
	return n, last
}

// checkWhen validates scheduled: a date YYYY-MM-DD, or an RFC 3339 time with a
// zone. It returns the value as stored and whether it has a time.
func checkWhen(field, s string) (string, bool, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Format(time.RFC3339), true, nil
	}
	if _, err := time.Parse(dateLayout, s); err == nil {
		return s, false, nil
	}
	return "", false, fmt.Errorf("%s %q: want a date YYYY-MM-DD, or a time with a zone such as 2026-11-04T10:00:00-05:00", field, s)
}

// checkKindFields validates the fields a kind brings and returns them as
// stored. Kind commitment keeps its older rule: a due date is needed.
func checkKindFields(ctx context.Context, d Deps, kind model.TaskKind, in TaskAddInput, due model.Date) (scheduled, end string, with []model.Link, err error) {
	if in.Scheduled == "" && in.End != "" {
		return "", "", nil, errors.New("end needs scheduled")
	}
	if in.Scheduled != "" {
		var timed bool
		if scheduled, timed, err = checkWhen("scheduled", in.Scheduled); err != nil {
			return
		}
		switch {
		case in.End == "":
		case !timed:
			return "", "", nil, errors.New("end: scheduled is a date; give it a start time to give an end")
		default:
			if scheduled, end, err = checkSlot(scheduled, in.End); err != nil {
				return "", "", nil, err
			}
		}
	}
	if with, err = meetingLinks(ctx, d, "with", in.With, model.TypePerson, model.TypeGroup); err != nil {
		return
	}
	switch kind {
	case model.KindMeeting, model.KindCall, model.KindWorkWith:
		if len(with) == 0 {
			return "", "", nil, fmt.Errorf("a %s is with someone: pass with (a person or group id)", kind)
		}
		if kind == model.KindWorkWith && strings.TrimSpace(in.DoneWhen) == "" {
			return "", "", nil, errors.New("work_with needs done_when: how we know the work is done")
		}
	case model.KindCommitment:
		if due == "" {
			return "", "", nil, errors.New("a commitment needs a due date")
		}
	}
	return scheduled, end, with, nil
}

// kindOf resolves kind and the older commitment flag.
func kindOf(in TaskAddInput) (model.TaskKind, error) {
	kind := model.TaskKind(strings.TrimSpace(string(in.Kind)))
	if in.Commitment {
		if kind != "" && kind != model.KindCommitment {
			return "", fmt.Errorf("commitment and kind %s disagree", kind)
		}
		kind = model.KindCommitment
	}
	if kind == "" || kind == model.KindTask {
		return "", nil
	}
	if !slices.Contains(model.TaskKinds, kind) {
		return "", fmt.Errorf("unknown kind %q: want one of %v", kind, model.TaskKinds)
	}
	return kind, nil
}

// needsOutcome reports whether closing a task of this kind states how it ended.
func needsOutcome(t *model.Task) bool {
	k := t.EffectiveKind()
	return k == model.KindWorkWith || k == model.KindCommitment
}

// TaskAttemptInput is the input of task_attempt.
type TaskAttemptInput struct {
	ID     string `json:"id"`
	Result string `json:"result"`          // no_answer, left_message, or reached
	Retry  string `json:"retry,omitempty"` // when to try again: a date or a time with a zone; needed unless reached
	Note   string `json:"note,omitempty"`
}

// TaskAttemptOutput is the output of task_attempt.
type TaskAttemptOutput struct {
	ID        string `json:"id"`
	Attempts  int    `json:"attempts"`
	Result    string `json:"result"`
	Scheduled string `json:"scheduled,omitempty"`
	Stuck     bool   `json:"stuck"`
	Rev       string `json:"rev"`
}

// TaskAttempt logs one attempt at a call in the task's Attempts section and
// moves its scheduled time to the retry. The old time stays in the line. At the
// cap the call is stuck: it shows as stuck until it is reached or closed.
var TaskAttempt = defineSafe("task_attempt", "Log an attempt at a call task: no_answer, left_message, or reached; the retry date or time reschedules it. Three attempts without reaching the person make it stuck", func(ctx context.Context, d Deps, in TaskAttemptInput) (TaskAttemptOutput, error) {
	if !slices.Contains(callResults, in.Result) {
		return TaskAttemptOutput{}, fmt.Errorf("result %q: want one of %v", in.Result, callResults)
	}
	doc, t, err := getTask(ctx, d.Store, normID(in.ID))
	if err != nil {
		return TaskAttemptOutput{}, err
	}
	if t.EffectiveKind() != model.KindCall {
		return TaskAttemptOutput{}, fmt.Errorf("%s is a %s, not a call", doc.ID, t.EffectiveKind())
	}
	if taskTerminal(t.Status) {
		return TaskAttemptOutput{}, fmt.Errorf("%s is already %s", doc.ID, t.Status)
	}
	retry, timed := "", false
	if r := strings.TrimSpace(in.Retry); r != "" {
		if retry, timed, err = checkWhen("retry", r); err != nil {
			return TaskAttemptOutput{}, err
		}
	}
	n, _ := callAttempts(doc.Body)
	n++
	capped := n >= CallAttemptCap && in.Result != CallReached
	if in.Result != CallReached && retry == "" && !capped {
		return TaskAttemptOutput{}, errors.New("retry: say when to try again (a date or a time with a zone)")
	}
	result := in.Result
	if retry != "" && in.Result != CallReached {
		result += "; retry " + retry
		if t.Scheduled != "" {
			result += " (was " + t.Scheduled + ")"
		}
		t.Scheduled = retry
		if !timed || t.End != "" {
			t.End = ""
		}
	}
	if note := strings.Join(strings.Fields(in.Note), " "); note != "" {
		result += "; " + note
	}
	line, err := oneLine("attempt", attemptLine(d, callWhat, result, nil))
	if err != nil {
		return TaskAttemptOutput{}, err
	}
	addAttemptLine(&doc, line)
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return TaskAttemptOutput{}, fmt.Errorf("task_attempt: %w", err)
	}
	return TaskAttemptOutput{ID: doc.ID, Attempts: n, Result: in.Result, Scheduled: t.Scheduled, Stuck: capped, Rev: saved.Rev}, nil
})
