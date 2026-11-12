package ops

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUnknownFieldNamesTheShape(t *testing.T) {
	cases := []struct {
		op         Entry
		input      string
		hint, want string
	}{
		{IOApprove, `{"id":"p","owner_says":"yes"}`, "(did you mean owner_said?)", "owner_said"},
		{Search, `{"q":"x"}`, "(did you mean query?)", "query"},
		{Defer, `{"ids":["t"],"date":"2026-12-01"}`, "(did you mean until?)", "until"},
		// Agents' calls: id inside sources[], ref inside evidence. The hint
		// names the field in the object that had it, not the top-level id.
		{Close, `{"id":"q","outcome":"answered","answer":"eu-west","sources":[{"id":"io-x","kind":"io"}]}`, `"id" in sources[] (did you mean sources[].ref?)`, "sources[]{ref*"},
		{Close, `{"id":"t","outcome":"done","evidence":{"quote":"q","ref":"https://chat.example.test/t/1"}}`, `"ref" in evidence (did you mean evidence.locator?)`, "locator*"},
		{Close, `{"id":"t","ref":"https://chat.example.test/t/1"}`, "(did you mean sources[].ref?)", "sources[]"},
	}
	for _, c := range cases {
		_, err := c.op.Invoke(ctx, Deps{}, json.RawMessage(c.input))
		if err == nil {
			t.Fatalf("%s: unknown field accepted", c.op.Name())
		}
		msg := err.Error()
		if strings.ContainsAny(msg, "\r\n") {
			t.Errorf("%s: error is not one line: %q", c.op.Name(), msg)
		}
		if !strings.Contains(msg, "unknown field") || !strings.Contains(msg, c.want) || !strings.Contains(msg, c.hint) {
			t.Errorf("%s: error = %q, want %q and %q", c.op.Name(), msg, c.hint, c.want)
		}
	}
}

func TestInputShapeMarksEnumsAndWrites(t *testing.T) {
	s := InputShape(ProposalList)
	if !strings.Contains(s, "status open|sent_back|closed|superseded") {
		t.Errorf("proposal_list shape = %q", s)
	}
	if strings.Contains(s, "write:") {
		t.Errorf("a read has write fields: %q", s)
	}
	if s := InputShape(TaskAdd); !strings.Contains(s, "write: why*") {
		t.Errorf("task_add shape = %q", s)
	}
}
