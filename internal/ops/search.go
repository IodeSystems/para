package ops

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/index"
	"github.com/iodesystems/para/internal/model"
)

// SearchInput is the input of search. The filters narrow the hits; an empty
// filter matches all.
type SearchInput struct {
	Query     string `json:"query" query:"query" required:"true"`
	Limit     int    `json:"limit,omitempty" query:"limit"` // 0 means 10
	Type      string `json:"type,omitempty" query:"type" doc:"document type, such as task, project, note, policy"`
	Status    string `json:"status,omitempty" query:"status" doc:"document status, such as todo or active; open means any status that is not final"`
	Area      string `json:"area,omitempty" query:"area" doc:"id of an area; matches documents that link to it"`
	Project   string `json:"project,omitempty" query:"project" doc:"id of a project; matches documents that link to it"`
	Since     string `json:"since,omitempty" query:"since" doc:"updated on or after this date (YYYY-MM-DD), or within a span such as 7d or 2w"`
	Prefix    bool   `json:"prefix,omitempty" query:"prefix" doc:"the last word matches the start of a word, for a search box that runs as a person types"`
	ExactOnly bool   `json:"exact_only,omitempty" query:"exact_only" doc:"skip the meaning search: nothing leaves this machine"`
}

var sinceSpan = regexp.MustCompile(`^(\d{1,4})([dw])$`)

// ParseSince turns a --since value into a YYYY-MM-DD date. It accepts a date,
// or a span back from now: 7d, 2w.
func ParseSince(s string, now time.Time) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if m := sinceSpan.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		if m[2] == "w" {
			n *= 7
		}
		return now.AddDate(0, 0, -n).Format("2006-01-02"), nil
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return "", fmt.Errorf("since %q: want YYYY-MM-DD or a span such as 7d or 2w", s)
	}
	return s, nil
}

// ParseQuery reads a search box: words, plus type:, status:, area:, project:
// and since: tokens that become filters. A token with an unknown key, or an
// empty value, stays part of the query text. The result has no Limit.
func ParseQuery(text string) SearchInput {
	var in SearchInput
	var words []string
	for _, w := range strings.Fields(text) {
		key, val, ok := strings.Cut(w, ":")
		dst := map[string]*string{"type": &in.Type, "status": &in.Status, "area": &in.Area, "project": &in.Project, "since": &in.Since}[key]
		if !ok || val == "" || dst == nil {
			words = append(words, w)
			continue
		}
		*dst = val
	}
	in.Query = strings.Join(words, " ")
	return in
}

// SearchOutput is the output of search.
type SearchOutput struct {
	Hits []index.Hit `json:"hits"`
	// Degraded is set when meaning search is configured and did not answer:
	// "exact only: <reason>". The hits are the exact ones, and complete.
	Degraded string `json:"degraded,omitempty"`
}

// Search finds documents by text, and by meaning when the vault is connected
// to a raglit daemon on this machine. A hit says whether it is a transcript or a
// summary, and carries the source URL, so a reader can cite the original.
var Search = defineRead("search", "Search documents, transcripts and summaries by text", func(ctx context.Context, d Deps, in SearchInput) (SearchOutput, error) {
	q := index.Query{Text: in.Query, Limit: in.Limit, Type: in.Type, Status: in.Status, Area: in.Area, Project: in.Project, Prefix: in.Prefix}
	if in.Type != "" && !slices.Contains(model.Types, model.Type(in.Type)) {
		return SearchOutput{}, fmt.Errorf("type %q: want one of %v", in.Type, model.Types)
	}
	since, err := ParseSince(in.Since, d.Store.Now())
	if err != nil {
		return SearchOutput{}, err
	}
	q.Since = since
	limit := q.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	// The exact search always runs. The meaning search, when configured,
	// runs beside it and may only add hits.
	// An exact-only search stays on this machine: a box that runs on every
	// pause in typing must not send partial words out or fill the egress log.
	var sem *semantic
	if !in.ExactOnly {
		sem = startSemantic(ctx, d, in.Query, limit)
	}
	hits, err := d.Store.Search(ctx, q)
	if err != nil {
		if sem != nil {
			sem.cancel()
		}
		return SearchOutput{}, err
	}
	if hits == nil {
		hits = []index.Hit{}
	}
	for i := range hits {
		hits[i].Match = MatchExact
	}
	out := SearchOutput{Hits: hits}
	if sem != nil {
		out = sem.merge(ctx, d, q, limit, out)
	}
	n := &namer{ctx: ctx, s: d.Store}
	readableHits(out.Hits, n.name)
	return out, nil
})

// IndexRebuildInput is the input of index_rebuild.
type IndexRebuildInput struct{}

// IndexRebuild drops the search index and builds it again from the markdown.
var IndexRebuild = defineSafe("index_rebuild", "Rebuild the search index from the documents", func(ctx context.Context, d Deps, _ IndexRebuildInput) (index.Stats, error) {
	return d.Store.IndexRebuild(ctx)
})
