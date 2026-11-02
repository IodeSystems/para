package raglit

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestSaveLoadRemove(t *testing.T) {
	dir := t.TempDir() + "/vaults/acme"
	if _, ok, err := Load(dir); ok || err != nil {
		t.Fatalf("empty: %v %v", ok, err)
	}
	want := Config{URL: "http://127.0.0.1:7770", Project: "acme", Token: "rk_x"}
	if err := Save(dir, want); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(Path(dir))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, %v", st.Mode(), err)
	}
	got, ok, err := Load(dir)
	if err != nil || !ok || got != want {
		t.Errorf("load = %+v %v %v", got, ok, err)
	}
	if got.MCPURL() != "http://127.0.0.1:7770/mcp/acme" {
		t.Errorf("MCPURL = %s", got.MCPURL())
	}
	if err := os.WriteFile(Path(dir), []byte(`{"url":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(dir); err == nil {
		t.Error("an incomplete config loaded")
	}
	if err := Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := Remove(dir); err != nil {
		t.Errorf("second remove: %v", err)
	}
}

func TestReason(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{context.DeadlineExceeded, "raglit timed out"},
		{errors.New("transport error: authorization required"), "raglit refused the token"},
		{errors.New("dial tcp 127.0.0.1:1: connect: connection refused"), "raglit unreachable"},
		{ErrBadReply, "raglit reply unreadable"},
		{errors.New("something else with /secret/path"), "raglit failed"},
	} {
		if got := Reason(c.err); got != c.want {
			t.Errorf("Reason(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

func TestClean(t *testing.T) {
	got := Clean("a\x1b[0m\x00 b\n\tc "+strings.Repeat("é", 20), 10)
	if got != "a[0m b c é..." {
		t.Errorf("Clean = %q", got)
	}
}
