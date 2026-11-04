package vault

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/iodesystems/para/internal/egress"
	"github.com/iodesystems/para/internal/props"
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

// Record appends one event under the vault lock: one writer per event file.
func (v *Vault) Record(ctx context.Context, op, doc, from, to, hash string) error {
	err := v.lock(ctx, func() error { return v.log.AppendHash(op, doc, from, to, hash) })
	if err != nil {
		return fmt.Errorf("vault: record: %w", err)
	}
	return nil
}
