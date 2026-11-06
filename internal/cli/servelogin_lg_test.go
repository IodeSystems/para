package cli

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/ops"
)

func TestLoginDays(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	for _, d := range []string{"0", "91"} {
		if _, _, err := e.run("serve", "login", "--print", "--days", d); err == nil || !strings.Contains(err.Error(), "--days") {
			t.Errorf("--days %s: %v", d, err)
		}
	}
}

func TestOpener(t *testing.T) {
	none := func(string) string { return "" }
	found := func(string) (string, error) { return "/usr/bin/x", nil }
	missing := func(string) (string, error) { return "", os.ErrNotExist }
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	for _, tc := range []struct {
		goos   string
		getenv func(string) string
		look   func(string) (string, error)
		reason string
	}{
		{"linux", none, found, ops.OpenNoDisplay},
		{"linux", env(map[string]string{"SSH_CONNECTION": "1 2 3 4"}), found, ops.OpenRemote},
		{"linux", env(map[string]string{"DISPLAY": ":0"}), missing, ops.OpenNoOpener},
		{"linux", env(map[string]string{"WAYLAND_DISPLAY": "wayland-0"}), found, ""},
		{"darwin", none, found, ""},
		{"plan9", none, found, ops.OpenNoOpener},
	} {
		_, err := opener(tc.goos, tc.getenv, tc.look)
		var oe *ops.OpenError
		switch {
		case tc.reason == "" && err != nil:
			t.Errorf("%s: %v", tc.goos, err)
		case tc.reason != "" && (!errors.As(err, &oe) || oe.Reason != tc.reason):
			t.Errorf("%s: %v, want %s", tc.goos, err, tc.reason)
		}
	}
}
