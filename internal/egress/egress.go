// Package egress is the check and the log for every path out of a vault
// (plan.md, Isolation). A record holds the document id and the destination
// only, never content. Para is not DLP: an agent with shell access bypasses
// this.
package egress

import (
	"context"
	"fmt"
	"strings"

	"github.com/iodesystems/para/internal/events"
)

// Op is the event op of an egress record.
const Op = "egress"

// Config keys in vault.properties.
const (
	KeyDefault = "egress.default" // "allow" or "deny"; anything else means deny
	KeyMarking = "classification" // text printed on copied documents
	KeyExport  = "egress.export"  // optional allowlist of export transports, comma separated; unset allows all
	KeyUpdate  = "egress.update"  // "deny" blocks this machine's update check and install while the vault is served; anything else allows
	KeyPublish = "egress.publish" // "allow" lets publish runs out under egress.default=deny; anything else follows egress.default
)

// UnmarkedMarking is printed when the vault sets no classification.
const UnmarkedMarking = "UNMARKED"

// Destination prefixes. A copy is the swivel path: the user pastes it by
// hand, so it stays allowed when the default is deny, and is logged.
const (
	DestCopy = "copy:" // copy:plain, copy:clipboard
	// DestExport is an export transport: export:ssh, export:clipboard. It is
	// the chair-swivel path between machines, so it stays allowed under
	// egress.default=deny. KeyExport can restrict it.
	DestExport = "export:"
)

// DestRaglit is a query to the vault's raglit daemon (raglit:search). The
// owner opts in per machine with `para raglit connect`, so the destination
// stays allowed under egress.default=deny, like copy and export. The record
// holds no query text.
const DestRaglit = "raglit:"

// DestAgent is a file of the vault returned to an agent over MCP or a key
// (agent:mcp, agent:api), such as an attachment an agent reads with
// attachment_read. An agent reads vault documents the same way, so it stays
// allowed under egress.default=deny; the record holds the path, never content.
const DestAgent = "agent:"

// DestPublish is a publish set written to a folder on this machine
// (publish:<set>). A run moves many documents at once, so it follows
// egress.default: under deny it runs only when egress.publish is allow.
// One record covers a run.
const DestPublish = "publish:"

// DestUpdate is the update check of this machine (update:manifest) and the
// download of a release (update:release): the signed manifest and archive of
// para itself, nothing from a vault. egress.default does not govern it: it
// is allowed unless the vault sets egress.update=deny. Each vault the
// machine serves logs it; one vault with egress.update=deny stops it
// (update.Auto). The machine's own switches are upgrade.check and
// upgrade.auto in install.properties.
const DestUpdate = "update:"

// Decision is the outcome of Check.
type Decision struct {
	Default string `json:"default"` // egress.default as applied: "allow" or "deny"
	Allowed bool   `json:"allowed"`
	Marking string `json:"marking"` // classification marking for the document
}

// Check applies the vault configuration to a destination. It never reads
// document content.
func Check(cfg map[string]string, dest string) Decision {
	d := Decision{Default: "deny", Marking: UnmarkedMarking}
	if strings.EqualFold(strings.TrimSpace(cfg[KeyDefault]), "allow") {
		d.Default = "allow"
	}
	if m := strings.TrimSpace(cfg[KeyMarking]); m != "" {
		d.Marking = m
	}
	d.Allowed = d.Default == "allow" || strings.HasPrefix(dest, DestCopy) || strings.HasPrefix(dest, DestRaglit) || strings.HasPrefix(dest, DestAgent) ||
		(strings.HasPrefix(dest, DestPublish) && strings.EqualFold(strings.TrimSpace(cfg[KeyPublish]), "allow"))
	if strings.HasPrefix(dest, DestUpdate) {
		d.Allowed = !strings.EqualFold(strings.TrimSpace(cfg[KeyUpdate]), "deny")
	}
	if kind, ok := strings.CutPrefix(dest, DestExport); ok {
		d.Allowed = exportAllowed(cfg[KeyExport], kind)
	}
	return d
}

// Log appends one egress event: doc id, the default in force, and the
// destination.
func Log(l *events.Log, doc, dest string, d Decision) error {
	if err := l.Append(Op, doc, d.Default, dest); err != nil {
		return fmt.Errorf("egress: log: %w", err)
	}
	return nil
}

// Recorder appends one event with a content hash, as store.Store.Record does.
type Recorder func(ctx context.Context, op, doc, from, to, hash string) error

// LogRun checks dest and logs one egress record for a run that moves many
// documents at once: run names it (such as the set), and hash is the hash of
// the run's manifest of document ids and content hashes. The record holds no
// content. A denied run is logged too.
func LogRun(ctx context.Context, cfg map[string]string, rec Recorder, run, dest, hash string) (Decision, error) {
	d := Check(cfg, dest)
	if err := rec(ctx, Op, run, d.Default, dest, hash); err != nil {
		return d, fmt.Errorf("egress: log run: %w", err)
	}
	return d, nil
}

// exportAllowed applies the egress.export allowlist. An unset or blank list
// allows every transport.
func exportAllowed(list, kind string) bool {
	if strings.TrimSpace(list) == "" {
		return true
	}
	for _, k := range strings.Split(list, ",") {
		if strings.EqualFold(strings.TrimSpace(k), kind) {
			return true
		}
	}
	return false
}
