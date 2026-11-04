package ops

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// Health of tracked documents: changes the owner has not seen for a while,
// documents no agent has read again lately, and comments nobody has settled.

// docsCheckDaysKey is the vault setting after which a watched document that
// no agent has read again is stale.
const docsCheckDaysKey = "docs.check_days"

// DocUnseenDays is how long a version may wait unseen, and DocCommentDays how
// long a comment may stay open, before health names it.
const (
	DocUnseenDays  = 7
	DocCommentDays = 7
)

// docsCheckDays returns docs.check_days, or 0 when off.
func docsCheckDays(s store.Store) (int, error) {
	conf, err := s.Config(docsCheckDaysKey)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(conf))
	if err != nil || n <= 0 {
		return 0, nil
	}
	return n, nil
}

// dayOf is the date part of an RFC 3339 time or a date.
func dayOf(s string) string {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.Format(dateLayout)
	}
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// trackedDocs adds the findings of watched documents. checkDays is
// docs.check_days; 0 is off.
func (h *healther) trackedDocs(checkDays int) {
	unseenCut := daysBefore(h.today, DocUnseenDays)
	commentCut := daysBefore(h.today, DocCommentDays)
	checkCut := daysBefore(h.today, checkDays)
	for id, d := range h.docs {
		m, ok := d.Meta.(*model.TrackedDoc)
		if !ok || m.Status != model.DocWatching {
			continue
		}
		goal := m.Project.Target()
		if !h.openGoals[goal] {
			goal = ""
		}
		if m.Latest > m.LastSeen {
			if v, ok := h.docs[versionID(id, m.LastSeen+1)].Meta.(*model.Transcript); ok && unseenCut != "" && dayOf(v.Source.Retrieved) < unseenCut {
				h.add(HealthDocUnseen, id, goal, fmt.Sprintf("%d version(s) not seen; the oldest was read on %s", m.Latest-m.LastSeen, dayOf(v.Source.Retrieved)),
					fmt.Sprintf("read the change: doc_diff {id: %s}, then mark it seen in the web app or `para doc seen %s`", id, id))
			}
		}
		if checkCut != "" && (m.Checked == "" || dayOf(m.Checked) < checkCut) {
			h.add(HealthDocStale, id, goal, fmt.Sprintf("not read again since %s (docs.check_days %d)", firstOf(dayOf(m.Checked), "it was tracked"), checkDays),
				fmt.Sprintf("read %s with your own tools and pass the text: doc_version_add {id: %s}", m.URL, id))
		}
		var old []string
		for _, c := range m.Comments {
			if c.Open() && commentCut != "" && dayOf(c.At) < commentCut {
				old = append(old, c.ID)
			}
		}
		if len(old) > 0 {
			h.add(HealthDocCommentOpen, id, goal, fmt.Sprintf("comment(s) %s open for more than %d days", strings.Join(old, ", "), DocCommentDays),
				fmt.Sprintf("turn each into an Ask, question, or commitment (doc_comment_file), or close it (doc_comment_resolve) on %s", id))
		}
	}
}
