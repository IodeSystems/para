package ops

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/iodesystems/para/internal/opstats"
)

// OpsStatsInput is the input of ops_stats.
type OpsStatsInput struct {
	Since   string `json:"since,omitempty" query:"since"`     // YYYY-MM-DD; counts only calls last made on or after it
	Surface string `json:"surface,omitempty" query:"surface"` // mcp-stdio, mcp-http, web, or cli
}

// OpStat is the calls of one op.
type OpStat struct {
	Op       string         `json:"op"`
	Calls    int            `json:"calls"`
	Last     string         `json:"last,omitempty"`
	Surfaces map[string]int `json:"surfaces,omitempty"`
	Actors   map[string]int `json:"actors,omitempty"`
}

// OpsStatsOutput is the output of ops_stats. Every op is listed, one with no
// calls too.
type OpsStatsOutput struct {
	Ops []OpStat `json:"ops"`
}

// OpsStats counts op calls on this machine, from the file in the vault's
// state directory. The count of a surface, actor, and op is cumulative; since
// keeps the counts last made on or after that date, so calls before it that
// were not repeated read as none.
var OpsStats = defineRead("ops_stats", "Count the calls of each op on this machine, by surface and actor; an op with no calls shows with zero", func(_ context.Context, d Deps, in OpsStatsInput) (OpsStatsOutput, error) {
	var since time.Time
	if in.Since != "" {
		t, err := time.Parse("2006-01-02", in.Since)
		if err != nil {
			return OpsStatsOutput{}, fmt.Errorf("since: want YYYY-MM-DD, got %q", in.Since)
		}
		since = t
	}
	entries, err := opstats.Read(d.Store.Info().StateDir)
	if err != nil {
		return OpsStatsOutput{}, err
	}
	by := map[string]*OpStat{}
	for _, e := range All() {
		by[e.Name()] = &OpStat{Op: e.Name()}
	}
	for _, e := range entries {
		if in.Surface != "" && e.Surface != in.Surface || e.Last.Before(since) {
			continue
		}
		s := by[e.Op]
		if s == nil {
			s = &OpStat{Op: e.Op}
			by[e.Op] = s
		}
		s.Calls += e.Count
		if l := e.Last.Format(time.RFC3339); l > s.Last {
			s.Last = l
		}
		if s.Surfaces == nil {
			s.Surfaces, s.Actors = map[string]int{}, map[string]int{}
		}
		s.Surfaces[e.Surface] += e.Count
		s.Actors[e.Actor] += e.Count
	}
	out := OpsStatsOutput{Ops: make([]OpStat, 0, len(by))}
	for _, s := range by {
		out.Ops = append(out.Ops, *s)
	}
	sort.Slice(out.Ops, func(i, j int) bool {
		if out.Ops[i].Calls != out.Ops[j].Calls {
			return out.Ops[i].Calls > out.Ops[j].Calls
		}
		return out.Ops[i].Op < out.Ops[j].Op
	})
	return out, nil
})
