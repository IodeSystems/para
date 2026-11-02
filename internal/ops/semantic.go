package ops

import (
	"context"
	"errors"
	"path"
	"strings"

	"github.com/iodesystems/parasolve/internal/egress"
	"github.com/iodesystems/parasolve/internal/index"
	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/raglit"
)

const defaultSearchLimit = 10

// Hit.Match values.
const (
	MatchExact   = "exact"
	MatchMeaning = "meaning"
)

// semantic is a raglit search in flight beside the exact one.
type semantic struct {
	cancel context.CancelFunc
	done   chan result
}

type result struct {
	hits []raglit.Hit
	err  error
}

// startSemantic starts the raglit search when this machine has a config for
// the vault. It returns nil when it has none. A failure to start is reported
// by merge as a degraded result, not as an error.
func startSemantic(ctx context.Context, d Deps, query string, limit int) *semantic {
	if strings.TrimSpace(query) == "" {
		return nil
	}
	cfg, ok, err := raglit.Load(d.Store.Info().StateDir)
	if !ok && err == nil {
		return nil
	}
	s := &semantic{done: make(chan result, 1)}
	if err != nil {
		s.cancel = func() {}
		s.done <- result{err: err}
		return s
	}
	// The query leaves the machine: log that, and fail closed when the log
	// cannot be written. The record holds no query text.
	if dec, err := d.Store.Egress(ctx, "", egress.DestRaglit+"search"); err != nil || !dec.Allowed {
		s.cancel = func() {}
		s.done <- result{err: errors.New("egress record failed")}
		return s
	}
	cctx, cancel := context.WithTimeout(ctx, raglit.Timeout)
	s.cancel = cancel
	go func() {
		// Fragments, not documents: ask for more than the limit so that
		// deduplication still leaves enough documents.
		hits, err := cfg.Search(cctx, query, limit*3)
		s.done <- result{hits: hits, err: err}
	}()
	return s
}

// merge waits for the meaning search and appends its hits to the exact ones.
func (s *semantic) merge(ctx context.Context, d Deps, q index.Query, limit int, out SearchOutput) SearchOutput {
	defer s.cancel()
	var r result
	select {
	case r = <-s.done:
	case <-ctx.Done():
		r.err = ctx.Err()
	}
	if r.err != nil {
		out.Degraded = "exact only: " + degradedReason(r.err)
		return out
	}
	out.Hits = mergeMeaning(ctx, d, q, limit, out.Hits, r.hits)
	return out
}

func degradedReason(err error) string {
	if err.Error() == "egress record failed" {
		return "could not log the query leaving"
	}
	if strings.Contains(err.Error(), "raglit: load") {
		return "raglit.json unreadable"
	}
	return raglit.Reason(err)
}

// mergeMeaning puts exact hits first, then the raglit hits by rank. Raglit
// hits are fragments: one hit per document. Scores are not compared, since
// the scales differ.
func mergeMeaning(ctx context.Context, d Deps, q index.Query, limit int, exact []index.Hit, raw []raglit.Hit) []index.Hit {
	have := map[string]bool{}
	for _, h := range exact {
		have[h.ID] = true
	}
	type pick struct {
		hit   raglit.Hit
		via   string
		class int
		rank  int
	}
	best := map[string]*pick{}
	var order []string
	for rank, h := range raw {
		id, via := resolveID(ctx, d, h.Rel)
		if id == "" || have[id] {
			continue
		}
		c := 0
		switch h.Origin {
		case "":
		case "frontmatter":
			c = 2
		default:
			c = 1
		}
		if p, ok := best[id]; !ok {
			best[id] = &pick{hit: h, via: via, class: c, rank: rank}
			order = append(order, id)
		} else if c < p.class {
			// A body hit beats a caption, and a caption beats frontmatter.
			p.hit, p.via, p.class = h, via, c
		}
	}
	out := exact
	for _, id := range order {
		if len(out) >= limit {
			break
		}
		p := best[id]
		doc, err := d.Store.Get(ctx, id)
		if err != nil || !q.Match(doc) {
			continue // not in this vault (any more), or filtered out
		}
		h := index.HitFor(doc, raglit.Clean(p.hit.Snippet, 300))
		h.Match, h.Via = MatchMeaning, p.via
		h.Caveat = raglit.Clean(p.hit.Caveat, 200)
		if h.Caveat == "" {
			switch p.class {
			case 1:
				h.Caveat = "a generated description, not the document's words"
			case 2:
				h.Caveat = "matched the frontmatter, not the text"
			}
		}
		out = append(out, h)
	}
	return out
}

// resolveID maps a raglit display path to a document id of this vault. The
// path is untrusted: only its last element is used. A transcript
// (<id>.source.md) maps to its summary <id> when that exists, and via names
// the transcript.
func resolveID(ctx context.Context, d Deps, rel string) (id, via string) {
	base := strings.TrimSuffix(path.Base(strings.ReplaceAll(rel, "\\", "/")), ".md")
	if base == "" || base == "." || base == ".." || !strings.HasSuffix(strings.ToLower(path.Base(rel)), ".md") {
		return "", ""
	}
	if sum, ok := strings.CutSuffix(base, ".source"); ok && sum != "" {
		if doc, err := d.Store.Get(ctx, sum); err == nil && doc.Meta.Header().Type != model.TypeTranscript {
			return sum, base
		}
	}
	return base, ""
}
