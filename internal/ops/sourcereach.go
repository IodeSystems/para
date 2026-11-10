package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/reach"
	"github.com/iodesystems/para/internal/store"
)

// Which agent reaches which source is a fact about this machine and the
// agent's harness, not about the vault: a claude.ai connector exists only
// inside that agent. So a declaration lives in the vault's state directory on
// this machine ($PARA_HOME/vaults/<id>/reach/<actor>.json), one file per
// actor, never in the vault, and a sync tool never carries it.
//
// reached_by in the source document is what the owner wrote down. An agent's
// declaration adds its actor to reached_by only when the owner turned on
// sources.learn_reach: reached_by is shared vault state the owner reads as
// "who can reach this", and a declaration is the agent's own claim. Off by
// default, a declaration changes nothing in the vault; it still lets the
// agent claim sweeps of the source, which is what scheduling needs.

const (
	reachDaysConfig   = "sources.reach_days"
	learnReachConfig  = "sources.learn_reach"
	defaultReachDays  = 7
	maxDeclaredSource = 200
)

// Declaration is one actor's statement of the sources it reaches.
type Declaration = reach.Declaration

// reachDays returns sources.reach_days, default 7.
func reachDays(s store.Store) (int, error) {
	v, err := s.Config(reachDaysConfig)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return defaultReachDays, nil
	}
	return n, nil
}

// reachCut is the oldest time a fresh declaration may have.
func reachCut(now time.Time, days int) time.Time {
	return now.Add(-time.Duration(days) * 24 * time.Hour)
}

func readDeclarations(stateDir string) ([]Declaration, error) { return reach.Read(stateDir) }

// declaredBy returns the declaration of actor, if any.
func declaredBy(decl []Declaration, actor string) (Declaration, bool) {
	for _, d := range decl {
		if d.Actor == actor {
			return d, true
		}
	}
	return Declaration{}, false
}

// reaches reports whether actor declared source within the cut.
func reaches(decl []Declaration, actor, source string, cut time.Time) bool {
	d, ok := declaredBy(decl, actor)
	return ok && d.Fresh(cut) && slices.Contains(d.Sources, source)
}

// anyReaches reports whether any actor declared source within the cut.
func anyReaches(decl []Declaration, source string, cut time.Time) bool {
	for _, d := range decl {
		if d.Fresh(cut) && slices.Contains(d.Sources, source) {
			return true
		}
	}
	return false
}

// sourceReach answers, for the request queue, whether actor may claim a
// sweep of source now. It reads this machine's declarations.
func sourceReach(d Deps, actor, source string) (bool, error) {
	days, err := reachDays(d.Store)
	if err != nil {
		return false, err
	}
	decl, err := readDeclarations(d.Store.Info().StateDir)
	if err != nil {
		return false, err
	}
	cut := reachCut(d.Store.Now(), days)
	if actor == "" {
		return anyReaches(decl, source, cut), nil
	}
	return reaches(decl, actor, source, cut), nil
}

// AgentSourcesInput is the input of agent_sources.
type AgentSourcesInput struct {
	// Sources are the ids of the source documents you can reach now with your
	// own tools. It replaces what you declared before; empty declares none.
	Sources []string `json:"sources"`
}

// AgentSourcesOutput is the output of agent_sources.
type AgentSourcesOutput struct {
	Actor   string   `json:"actor"`
	At      string   `json:"at"`
	Sources []string `json:"sources"`
	// Unknown are ids that name no source document; they are not recorded.
	Unknown []string `json:"unknown"`
	// Hint is set when Unknown is not empty: how to add a source.
	Hint string `json:"hint,omitempty"`
	// Due are the declared sources a sweep is due for: run sweep_plan.
	Due []string `json:"due"`
	// Learned are the sources whose reached_by gained your actor, only when
	// the owner turned on sources.learn_reach.
	Learned    []string `json:"learned"`
	LearnReach bool     `json:"learn_reach"`
	ReachDays  int      `json:"reach_days"`
}

// AgentSources records which sources the calling agent reaches.
var AgentSources = defineSafe("agent_sources", "At session start, declare the source ids you can reach now with your own tools (MCP connectors, CLIs): it replaces your earlier declaration and lasts sources.reach_days. Only an agent that declared a source can claim its scheduled sweep. Kept on this machine, not in the vault", func(ctx context.Context, d Deps, in AgentSourcesInput) (AgentSourcesOutput, error) {
	info := d.Store.Info()
	if info.Actor == "" {
		return AgentSourcesOutput{}, errors.New("no actor: the surface did not name the caller")
	}
	if info.StateDir == "" {
		return AgentSourcesOutput{}, errors.New("no state directory: declarations live on this machine, outside the vault; this surface has none")
	}
	if len(in.Sources) > maxDeclaredSource {
		return AgentSourcesOutput{}, fmt.Errorf("sources: at most %d", maxDeclaredSource)
	}
	days, err := reachDays(d.Store)
	if err != nil {
		return AgentSourcesOutput{}, err
	}
	learn, err := d.Store.Config(learnReachConfig)
	if err != nil {
		return AgentSourcesOutput{}, err
	}
	now := d.Store.Now().UTC()
	today := now.Local().Format(dateLayout)
	out := AgentSourcesOutput{Actor: info.Actor, At: now.Format(time.RFC3339), Sources: []string{}, Unknown: []string{}, Due: []string{},
		Learned: []string{}, LearnReach: strings.TrimSpace(learn) == "true", ReachDays: days}
	for _, raw := range in.Sources {
		id := normID(raw)
		if id == "" || slices.Contains(out.Sources, id) {
			continue
		}
		doc, err := d.Store.Get(ctx, id)
		if errors.Is(err, model.ErrNotFound) {
			out.Unknown = append(out.Unknown, id)
			continue
		}
		if err != nil {
			return out, err
		}
		m, ok := doc.Meta.(*model.SourceDoc)
		if !ok {
			out.Unknown = append(out.Unknown, id)
			continue
		}
		out.Sources = append(out.Sources, doc.ID)
		if m.Status == model.SourceActive {
			if due := sourceDue(m); due != "" && due <= today {
				out.Due = append(out.Due, doc.ID)
			}
		}
	}
	if len(out.Unknown) > 0 {
		out.Hint = noSourceHint(ctx, d.Store, out.Unknown[0])
	}
	if err := reach.Write(info.StateDir, Declaration{Actor: info.Actor, Install: info.Install, At: out.At, Sources: out.Sources}); err != nil {
		return out, err
	}
	if !out.LearnReach {
		return out, nil
	}
	for _, id := range out.Sources {
		doc, m, err := getSource(ctx, d.Store, id)
		if err != nil {
			return out, err
		}
		if slices.Contains(m.ReachedBy, info.Actor) {
			continue
		}
		m.ReachedBy = append(m.ReachedBy, info.Actor)
		if _, err := d.Store.Update(ctx, doc); err != nil {
			return out, err
		}
		out.Learned = append(out.Learned, id)
	}
	return out, nil
})
