// Package egress is the check and the log for every path out of a vault
// (plan.md, Isolation). A record holds the document id and the destination
// only, never content. Para is not DLP: an agent with shell access bypasses
// this.
package egress

import (
	"fmt"
	"strings"

	"github.com/iodesystems/parasolve/internal/events"
)

// Op is the event op of an egress record.
const Op = "egress"

// Config keys in vault.properties.
const (
	KeyDefault = "egress.default" // "allow" or "deny"; anything else means deny
	KeyMarking = "classification" // text printed on copied documents
	KeyExport  = "egress.export"  // optional allowlist of export transports, comma separated; unset allows all
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
	d.Allowed = d.Default == "allow" || strings.HasPrefix(dest, DestCopy)
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
