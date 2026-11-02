package ops

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

const (
	// journalAgentConfig is the vault property that names the agent that
	// writes journal narratives. Without it tick makes the mechanical
	// journals only and queues no agent work.
	journalAgentConfig = "journal.agent"
	// JournalCatchUpDays bounds how far back tick looks for a journal that was
	// never made, such as after downtime.
	JournalCatchUpDays = 14
)

// journalPeriods lists the periods tick looks after, oldest first and lower
// levels first, so a week is made after its days. A period is in scope when it
// has ended and its end is within the catch-up window. The clock's own zone
// decides where a day starts, so a DST day is 23 or 25 hours and no day is
// skipped or made twice.
func journalPeriods(now time.Time) []period {
	loc := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	window := today.AddDate(0, 0, -JournalCatchUpDays)
	var out []period
	for day := window; day.Before(today); day = day.AddDate(0, 0, 1) {
		out = append(out, newPeriod(model.JournalDay, day))
	}
	for _, scope := range []model.JournalScope{model.JournalWeek, model.JournalMonth} {
		var last string
		// Walk every day from a month before the window: the first period
		// can start before it and still end inside it.
		for day := window.AddDate(0, -1, 0); day.Before(today); day = day.AddDate(0, 0, 1) {
			p := newPeriod(scope, day)
			if p.id == last {
				continue
			}
			last = p.id
			if !p.end.After(today) && p.end.After(window) {
				out = append(out, p)
			}
		}
	}
	return out
}

// tickJournals makes the journals that are due and, when journal.agent is
// set, one request per journal for a narrative. A journal that exists is not
// rewritten, so a pass is safe to repeat.
func tickJournals(ctx context.Context, d Deps, out *TickOutput) error {
	now := d.Store.Now()
	periods := journalPeriods(now)
	have, err := journalIDs(ctx, d.Store)
	if err != nil {
		return err
	}
	for _, p := range periods {
		if have[p.id] {
			continue
		}
		if _, err := journal(ctx, d, JournalInput{Date: p.start.Format(dateLayout)}, p.scope); err != nil {
			return fmt.Errorf("%s: %w", p.id, err)
		}
		have[p.id] = true
		out.Journals = append(out.Journals, p.id)
	}
	agent, err := d.Store.Config(journalAgentConfig)
	if err != nil {
		return err
	}
	agent = strings.TrimPrefix(strings.TrimSpace(agent), AgentPrefix)
	if agent == "" {
		return nil
	}
	if strings.ContainsAny(agent, " \t\r\n") {
		out.Warnings = append(out.Warnings, fmt.Sprintf("%s=%q: want one agent name; no journal requests made", journalAgentConfig, agent))
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
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for _, p := range periods {
		if asked[p.id] || !have[p.id] {
			continue
		}
		// A month waits for its last week to end, so the weeks it summarizes
		// exist and are whole.
		if kids := p.children(); p.scope == model.JournalMonth && len(kids) > 0 && kids[len(kids)-1].end.After(today) {
			continue
		}
		doc, err := d.Store.Get(ctx, p.id)
		if err != nil {
			return err
		}
		if strings.TrimSpace(sectionText(doc.Body, SummaryAgentSection)) != "" {
			continue
		}
		// A period with no events has nothing to tell; the agent would spend
		// tokens to write "quiet".
		if events(doc.Body) == 0 {
			continue
		}
		if p.scope == model.JournalMonth {
			// The month was made before its last week; list that week now.
			if _, err := journal(ctx, d, JournalInput{Date: p.start.Format(dateLayout)}, p.scope); err != nil {
				return fmt.Errorf("%s: %w", p.id, err)
			}
		}
		ask := fmt.Sprintf("%s\n\nWrite the %s summary of [[%s]] for %s%s.\n"+
			"Use the MCP prompt journal_lod. Call journal_summary with id %s and no text to see what to read, then again with the text. "+
			"A week reads the day summaries and a month reads the week summaries; if a lower journal lacks a summary-agent section, write that one first.",
			p.requestTitle(), p.scope, p.id, AgentPrefix, agent, p.id)
		title, rest, _ := strings.Cut(ask, "\n")
		res, err := RequestAdd.Run(ctx, d, RequestAddInput{Title: title, Ask: strings.TrimSpace(rest), Links: []string{p.id}})
		if err != nil {
			out.Warnings = append(out.Warnings, "journal "+p.id+": no request made: "+err.Error())
			continue
		}
		asked[p.id] = true
		out.noteRequest(ctx, d, res.ID)
	}
	return nil
}

// events reads the count back from the journal's own summary section.
func events(body string) int {
	if m := eventsLineRe.FindStringSubmatch(sectionText(body, "summary")); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// journalIDs returns the ids of the journals in the vault.
func journalIDs(ctx context.Context, s store.Store) (map[string]bool, error) {
	refs, err := s.List(ctx, store.Filter{Folder: model.FolderJournal, Type: model.TypeJournal})
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(refs))
	for _, r := range refs {
		out[r.ID] = true
	}
	return out, nil
}

// requestTitle is the plain title of the request for a period's narrative.
func (p period) requestTitle() string {
	last := p.end.AddDate(0, 0, -1)
	switch p.scope {
	case model.JournalWeek:
		if p.start.Month() == last.Month() {
			return fmt.Sprintf("Write the %s %d to %d week summary", p.start.Format("Jan"), p.start.Day(), last.Day())
		}
		return fmt.Sprintf("Write the %s to %s week summary", p.start.Format("Jan 2"), last.Format("Jan 2"))
	case model.JournalMonth:
		return fmt.Sprintf("Write the %s month summary", p.start.Format("January 2006"))
	}
	return fmt.Sprintf("Write the %s day summary", p.start.Format("Jan 2"))
}
