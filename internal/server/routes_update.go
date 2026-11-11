package server

import (
	"context"

	"github.com/iodesystems/para/internal/netguard"
)

// UpdateStatus is where this machine stands on para releases, for the web
// banner: the running version, the newest release seen, the switches, and
// the last auto-install. It holds versions and times only.
type UpdateStatus struct {
	Running string `json:"running"`
	// Available is the newest release seen when it is newer than Running.
	Available string `json:"available,omitempty"`
	Summary   string `json:"summary,omitempty"`
	// Notice is the one line to show, "" when nothing is newer.
	Notice  string `json:"notice,omitempty"`
	Release bool   `json:"release"` // a release build; a checkout build is never replaced
	Check   bool   `json:"check"`   // upgrade.check
	Auto    bool   `json:"auto"`    // upgrade.auto
	Checked string `json:"checked,omitempty"`
	// CheckError is why the last check failed: the full line for a key with
	// the admin scope, only its category (network, signature, egress, other)
	// for any other key. Doctor and the CLI show the full line.
	CheckError string `json:"check_error,omitempty"`
	CheckKind  string `json:"-"`
	// The last auto-install, when its target is the running version.
	InstalledFrom string `json:"installed_from,omitempty"`
	InstalledTo   string `json:"installed_to,omitempty"`
	InstalledAt   string `json:"installed_at,omitempty"`
}

type updateStatusOutput struct{ Body UpdateStatus }

func registerUpdate(r *registrar) {
	s := r.s
	raw(r, "updateStatus", get, "/api/v1/update/status", netguard.ScopeVaultRead, "Show whether a newer para release is available and whether it installs by itself",
		func(ctx context.Context, _ *noInput) (*updateStatusOutput, error) {
			b, err := s.bound(ctx)
			if err != nil {
				return nil, err
			}
			out := UpdateStatus{Running: s.opt.Version}
			if s.opt.Update != nil {
				out = s.opt.Update()
			}
			if out.CheckError != "" && !b.req.Principal.Has(netguard.ScopeAdmin) {
				out.CheckError = out.CheckKind
				if out.CheckError == "" {
					out.CheckError = "other"
				}
			}
			return &updateStatusOutput{Body: out}, nil
		})
}
