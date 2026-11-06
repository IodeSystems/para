package ops

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// Outward acts seen in a sweep. Para executes nothing, so it cannot stop an
// agent from sending as the owner; it can notice afterwards. A sweep's
// scan_out reads what went out (sent mail, posted comments, pushes). The
// sweeping agent passes each act it cannot match to a draft, an Ask, or an
// interaction as outward on sweep_record; para keeps it in the source body
// under "## Outward seen", and health names every one with no record:
// no receipt and no sent io item or interaction at that URL. A later record clears the finding.

const (
	outwardHeading = "## Outward seen"
	maxOutwardSeen = 200
)

// SweepOutward is one outward act a sweep saw in scan_out.
type SweepOutward struct {
	URL  string `json:"url"`          // where the act can be seen
	What string `json:"what"`         // what it is, in words: "comment on PR 12"
	At   string `json:"at,omitempty"` // when it happened, RFC 3339; default the scan time
}

// outwardSeen is one line of "## Outward seen": "- <at> <url> <what>".
type outwardSeen struct{ at, url, what string }

func (o outwardSeen) line() string {
	return strings.TrimSpace("- " + o.at + " " + o.url + " " + o.what)
}

// splitSourceBody cuts a source body into the text before para's sections,
// the outward acts seen, and the last sweep line.
func splitSourceBody(body string) (head string, seen []outwardSeen, last string) {
	body = "\n" + body
	if i := strings.Index(body, "\n"+lastSweepHeading+"\n"); i >= 0 {
		last = strings.TrimSpace(body[i+len(lastSweepHeading)+2:])
		body = body[:i]
	}
	if i := strings.Index(body, "\n"+outwardHeading+"\n"); i >= 0 {
		for _, l := range strings.Split(body[i+len(outwardHeading)+2:], "\n") {
			f := strings.Fields(strings.TrimPrefix(strings.TrimSpace(l), "- "))
			if !strings.HasPrefix(strings.TrimSpace(l), "- ") || len(f) < 2 {
				continue
			}
			seen = append(seen, outwardSeen{at: f[0], url: f[1], what: strings.Join(f[2:], " ")})
		}
		body = body[:i]
	}
	return strings.TrimSpace(body), seen, last
}

// joinSourceBody is the inverse of splitSourceBody.
func joinSourceBody(head string, seen []outwardSeen, last string) string {
	body := head
	if len(seen) > 0 {
		lines := make([]string, len(seen))
		for i, o := range seen {
			lines[i] = o.line()
		}
		body = appendSection(body, strings.TrimPrefix(outwardHeading, "## "), strings.Join(lines, "\n"))
	}
	if last != "" {
		body = appendSection(body, strings.TrimPrefix(lastSweepHeading, "## "), last)
	}
	return body
}

// checkOutward checks the acts of a sweep and fills each time.
func checkOutward(list []SweepOutward, at string) ([]outwardSeen, error) {
	out := make([]outwardSeen, 0, len(list))
	for _, o := range list {
		u, err := checkURL(o.URL)
		if err != nil {
			return nil, fmt.Errorf("outward: %w", err)
		}
		what := strings.Join(strings.Fields(o.What), " ")
		if what == "" {
			return nil, fmt.Errorf("outward %s: what is empty: say what the act is", u)
		}
		if err := lintText("outward what", what); err != nil {
			return nil, err
		}
		when := at
		if s := strings.TrimSpace(o.At); s != "" {
			t, err := time.Parse(time.RFC3339Nano, s)
			if err != nil {
				return nil, fmt.Errorf("outward %s: at %q: want an RFC 3339 time", u, s)
			}
			when = t.UTC().Format(time.RFC3339)
		}
		out = append(out, outwardSeen{at: when, url: u, what: what})
	}
	return out, nil
}

// mergeOutward adds acts not seen before (by URL), keeping the newest
// maxOutwardSeen.
func mergeOutward(old, add []outwardSeen) []outwardSeen {
	for _, o := range add {
		if !slices.ContainsFunc(old, func(x outwardSeen) bool { return sameURL(x.url, o.url) }) {
			old = append(old, o)
		}
	}
	if len(old) > maxOutwardSeen {
		old = old[len(old)-maxOutwardSeen:]
	}
	return old
}

func sameURL(a, b string) bool { return normURL(a) == normURL(b) }

func normURL(u string) string { return strings.TrimRight(strings.TrimSpace(u), "/") }

// recordedURLs lists every URL the vault holds a record of an outward act
// at: a receipt's verify or sources, a sent io item's ref, an interaction's
// url.
func recordedURLs(docs map[string]model.Document) map[string]bool {
	out := map[string]bool{}
	add := func(u string) {
		if u = normURL(u); u != "" {
			out[u] = true
		}
	}
	for _, d := range docs {
		switch m := d.Meta.(type) {
		case *model.Receipt:
			if m.Undone != "" {
				continue
			}
			add(m.Verify)
			for _, s := range m.Sources {
				add(s)
			}
		case *model.IO:
			if m.Direction == model.IOOut && m.State == model.IOSent {
				add(m.Ref)
			}
			if m.Logged() { // an interaction before format 10
				add(m.URL)
			}
		case *model.Interaction:
			add(m.URL)
		}
	}
	return out
}

// unreviewedOutward lists the acts seen in a source body with no record.
func unreviewedOutward(body string, recorded map[string]bool) []outwardSeen {
	_, seen, _ := splitSourceBody(body)
	var out []outwardSeen
	for _, o := range seen {
		if !recorded[normURL(o.url)] {
			out = append(out, o)
		}
	}
	return out
}
