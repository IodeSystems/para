package ops

import (
	"testing"
)

// clockFix is an agentFix whose clock moves (rv.advance). It starts on
// 2026-11-01 09:00 UTC.
func clockFix(t *testing.T) (*agentFix, *reqVault) {
	t.Helper()
	rv := newReqVault(t)
	f := &agentFix{t: t, root: rv.root, owner: rv.as("user", "cli", "install-a"), agent: rv.as("claude", "mcp", "install-a")}
	f.proj = must(t, ProjectAdd, f.owner, ProjectAddInput{DoneWhen: "Done.", Title: "Portal", Owner: "Jordan Example"}).ID
	f.source = must(t, NoteAdd, f.owner, NoteAddInput{Title: "Sync notes", Body: "Ship it."}).ID
	return f, rv
}

func pendingOf(t *testing.T, d Deps, id string) int {
	t.Helper()
	for _, v := range must(t, ProposalList, d, ProposalListInput{}).Items {
		if v.ID == id {
			return v.Pending
		}
	}
	t.Fatalf("no proposal %s", id)
	return 0
}
