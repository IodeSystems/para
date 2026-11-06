package ops

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// The inbox digest is the part of a day's journal (section inbox-digest)
// that lists the incoming messages of that day that did not need the owner
// (inbound.go): id, sender, subject or first line, and link. A message a
// triage rule muted is counted, not listed. The messages stay in the Inbox,
// under Digest, until filed or dropped.

const (
	inboxDigestSection = "inbox-digest"
	inboxDigestHeading = "### Inbox digest"
)

// inboxDigest is the digest of the incoming messages that came in
// [start, end); nil when there are none.
func inboxDigest(ctx context.Context, d Deps, start, end time.Time) ([]string, error) {
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeIO})
	if err != nil || len(refs) == 0 {
		return nil, err
	}
	sources, err := loadSources(ctx, d.Store)
	if err != nil {
		return nil, err
	}
	// The verdict reads senders and sources only: no full snapshot.
	sn := &Snapshot{ctx: ctx, s: d.Store, docs: map[string]model.Document{}, missing: map[string]bool{}}
	var lines []string
	muted := 0
	for _, r := range refs {
		doc, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		m, ok := doc.Meta.(*model.IO)
		if !ok || m.Direction != model.IOIn || !cameIn(doc, m, start, end) {
			continue
		}
		if yes, _ := sn.inboundVerdict(m); yes {
			continue
		}
		if m.State == model.IODropped && strings.HasPrefix(m.Reason, "muted by ") {
			muted++
			continue
		}
		line := fmt.Sprintf("- [[%s]] from %s: %s", doc.ID, firstOf(m.From, "unknown"), firstOf(doc.Title, "(no subject)"))
		if link := digestLink(sources, m); link != "" {
			line += " " + link
		}
		if !m.State.Open() {
			line += " (" + string(m.State) + ")"
		}
		lines = append(lines, line)
	}
	if muted > 0 {
		lines = append(lines, fmt.Sprintf("- %d muted by triage rules", muted))
	}
	if len(lines) == 0 {
		return nil, nil
	}
	return append([]string{inboxDigestHeading}, lines...), nil
}

// cameIn reports whether the message came in [start, end): its at time, else
// its created day.
func cameIn(doc model.Document, m *model.IO, start, end time.Time) bool {
	if t, err := time.Parse(time.RFC3339Nano, m.At); err == nil {
		return !t.Before(start) && t.Before(end)
	}
	day := string(m.Created)
	return day >= start.Format(dateLayout) && day < end.Format(dateLayout)
}

// digestLink is the message's link: its ref, else its via ref through the
// source's ref_url.
func digestLink(sources []model.Document, m *model.IO) string {
	if m.Ref != "" {
		return m.Ref
	}
	if src := ioSource(sources, m.Via); src != nil {
		return fillRefURL(src.Meta.(*model.SourceDoc).RefURL, m.Via.Ref)
	}
	return ""
}
