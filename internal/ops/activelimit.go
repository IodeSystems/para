package ops

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// Active goal limit. The vault turns it on by setting goals.active_limit in
// vault.properties; unset, zero, negative, or not a number leaves it off. A
// goal is active when its status is active and it is not deferred.
const activeLimitKey = "goals.active_limit"

// activeGoalLimit returns the limit, or 0 when off.
func activeGoalLimit(s store.Store) (int, error) {
	conf, err := s.Config(activeLimitKey)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(conf))
	if err != nil || n <= 0 {
		return 0, nil
	}
	return n, nil
}

// isActiveGoal reports whether d is a goal that counts toward the limit on the
// date today (YYYY-MM-DD).
func isActiveGoal(d model.Document, today string) bool {
	p, ok := d.Meta.(*model.Project)
	return ok && p.Status == model.ProjectActive && !p.IsDeferred(today)
}

// activeGoalCount counts the active goals of the vault.
func activeGoalCount(ctx context.Context, s store.Store, today string) (int, error) {
	refs, err := s.List(ctx, store.Filter{Type: model.TypeProject})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range refs {
		d, err := s.Get(ctx, r.ID)
		if err != nil {
			return 0, err
		}
		if isActiveGoal(d, today) {
			n++
		}
	}
	return n, nil
}

// activeLimitWarning returns a warning line when the active goals exceed the
// limit, else "". It is for an op that just made a goal active.
func activeLimitWarning(ctx context.Context, s store.Store) (string, error) {
	limit, err := activeGoalLimit(s)
	if err != nil || limit == 0 {
		return "", err
	}
	n, err := activeGoalCount(ctx, s, s.Now().Format(dateLayout))
	if err != nil || n <= limit {
		return "", err
	}
	return fmt.Sprintf("active goals: %d, over the vault limit of %d (goals.active_limit); defer or drop one (see health: too_many_active_goals)", n, limit), nil
}

// tooManyActive adds a too_many_active_goals finding when the active goals
// exceed the limit. It names the goals lowest in value order as candidates to
// defer. rank maps a goal id to its place in the value order.
func (h *healther) tooManyActive(s store.Store, rank map[string]int) error {
	limit, err := activeGoalLimit(s)
	if err != nil || limit == 0 {
		return err
	}
	var ids []string
	for id := range h.openGoals {
		ids = append(ids, id)
	}
	if len(ids) <= limit {
		return nil
	}
	slices.SortFunc(ids, func(a, b string) int {
		return cmp.Or(cmp.Compare(rankOr(rank, a), rankOr(rank, b)), cmp.Compare(a, b))
	})
	// Lowest value first: the tail of the order, at most as many as the excess.
	excess := len(ids) - limit
	lowest := ids[len(ids)-1]
	var cands []string
	for i := len(ids) - 1; i >= 0 && len(cands) < max(excess, 3); i-- {
		cands = append(cands, fmt.Sprintf("%s (%s)", h.docs[ids[i]].Title, ids[i]))
	}
	h.add(HealthTooManyActive, lowest, "",
		fmt.Sprintf("%d active goals, over the limit of %d (goals.active_limit); lowest in value order, candidates to defer: %s", len(ids), limit, strings.Join(cands, "; ")),
		"the owner defers a goal (later {ids: [<goal>]}) or closes one; an agent asks")
	return nil
}
