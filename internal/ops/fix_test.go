package ops

import "testing"

// agentFix is a vault with a person (owner) and an MCP agent on one install,
// one note (source) and one goal (proj).
type agentFix struct {
	t            *testing.T
	root         string
	owner, agent Deps
	source, proj string
}

func newAgentFix(t *testing.T) *agentFix {
	t.Helper()
	rv := newReqVault(t)
	f := &agentFix{t: t, root: rv.root, owner: rv.as("user", "cli", "install-a"), agent: rv.as("claude", "mcp", "install-a")}
	note, err := NoteAdd.Run(t.Context(), f.owner, NoteAddInput{Title: "Sync notes", Body: "Ship it."})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := ProjectAdd.Run(t.Context(), f.owner, ProjectAddInput{DoneWhen: "Done.", Title: "Portal", Owner: "Jordan Example"})
	if err != nil {
		t.Fatal(err)
	}
	f.source, f.proj = note.ID, proj.ID
	return f
}
