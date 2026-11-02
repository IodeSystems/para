package vault

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/iodesystems/paradize/internal/egress"
	"github.com/iodesystems/paradize/internal/props"
)

// Egress checks a path out of the vault against vault.properties and logs an
// egress event (doc id and destination only). The event is written before the
// caller releases any content.
func (v *Vault) Egress(ctx context.Context, doc, dest string) (egress.Decision, error) {
	cfg, err := props.Load(filepath.Join(v.root, dirState, "vault.properties"))
	if err != nil {
		return egress.Decision{}, fmt.Errorf("vault: egress: %w", err)
	}
	d := egress.Check(cfg, dest)
	err = v.lock(ctx, func() error { return egress.Log(v.log, doc, dest, d) })
	if err != nil {
		return egress.Decision{}, fmt.Errorf("vault: egress: %w", err)
	}
	return d, nil
}
