package cli

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/props"
	"github.com/iodesystems/para/internal/registry"
	"github.com/iodesystems/para/internal/update"
)

// The auto-install swaps the binary, records it, files each vault's steps as
// agent work (it runs none unless upgrade.vault_steps=auto), then restarts
// the service: the same new-binary calls as para upgrade --release.
func TestAutoInstallFilesVaultStepsUnlessAuto(t *testing.T) {
	for name, c := range map[string]struct {
		steps   string
		applied bool
	}{
		"default files them": {},
		"vault_steps=auto":   {steps: "auto", applied: true},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.ok("init", "acme", filepath.Join(e.dir, "acme"))
			if c.steps != "" {
				p := filepath.Join(e.home, "install.properties")
				cfg, err := props.Load(p)
				if err != nil {
					t.Fatal(err)
				}
				cfg[update.KeyVaultSteps] = c.steps
				if err := os.WriteFile(p, props.Format(cfg), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			log := filepath.Join(e.dir, "new.log")
			u := releaseServer(t, "para", "v0.4.0", "para v0.4.0 manifest", fakeReleaseBin(log), e.dir)
			l := &loader{version: "v0.3.0", install: registry.Install{Home: e.home}, env: func(string) string { return "" }}
			if err := autoInstall(context.Background(), l, slog.New(slog.DiscardHandler), u); err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(log)
			calls := string(b)
			for _, want := range []string{"--vault acme vault upgrade --file", "service status", "service restart"} {
				if !strings.Contains(calls, want) {
					t.Errorf("calls lack %q:\n%s", want, calls)
				}
			}
			if i, j := strings.Index(calls, "upgrade --file"), strings.Index(calls, "service restart"); i < 0 || j < i {
				t.Errorf("the service restarted before the vault steps were filed:\n%s", calls)
			}
			if got := strings.Contains(calls, "--vault acme vault upgrade --unattended\n"); got != c.applied {
				t.Errorf("steps applied = %v, want %v:\n%s", got, c.applied, calls)
			}
			if st := update.LoadState(u.StatePath); st.InstalledFrom != "v0.3.0" || st.InstalledTo != "v0.4.0" {
				t.Errorf("state %+v", st)
			}
		})
	}
}

// TestAutoInstallRefreshesTheSkill: an install reinstalls a skill that para
// skill install wrote, with the new binary, before the service restarts; a
// skill without the version marker is left alone.
func TestAutoInstallRefreshesTheSkill(t *testing.T) {
	for name, marker := range map[string]bool{"with marker": true, "without marker": false} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.ok("init", "acme", filepath.Join(e.dir, "acme"))
			if _, err := installSkill("", "v0.3.0"); err != nil {
				t.Fatal(err)
			}
			if !marker {
				dir, _ := defaultSkillsDir()
				if err := os.Remove(filepath.Join(dir, "para", skillVersionFile)); err != nil {
					t.Fatal(err)
				}
			}
			log := filepath.Join(e.dir, "new.log")
			u := releaseServer(t, "para", "v0.4.0", "para v0.4.0 manifest", fakeReleaseBin(log), e.dir)
			l := &loader{version: "v0.3.0", install: registry.Install{Home: e.home}, env: func(string) string { return "" }}
			if err := autoInstall(context.Background(), l, slog.New(slog.DiscardHandler), u); err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(log)
			calls := string(b)
			i, j := strings.Index(calls, "skill install"), strings.Index(calls, "service restart")
			if marker && (i < 0 || j < i) {
				t.Errorf("want skill install before the restart:\n%s", calls)
			}
			if !marker && i >= 0 {
				t.Errorf("a skill without the marker was reinstalled:\n%s", calls)
			}
		})
	}
}
