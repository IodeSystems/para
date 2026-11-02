package ops

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/iodesystems/paradize/internal/model"
	"github.com/iodesystems/paradize/internal/paramd"
	"github.com/iodesystems/paradize/internal/store"
)

// An interaction is one logged contact with people. The user writes it, or
// asks para to. Para reads no messages and captures nothing by itself.

// WarnSummaryLong is the warning for an interaction body over the limit.
var WarnSummaryLong = fmt.Sprintf("summary is over %d characters: write it in your own words and link the original", paramd.MaxSummaryLen)

// InteractionAddInput is the input of interaction_add.
type InteractionAddInput struct {
	Summary    string                     `json:"summary"` // first line is the title; your words, not a copy
	With       []string                   `json:"with"`    // person or group ids
	Medium     model.Medium               `json:"medium"`
	Direction  model.InteractionDirection `json:"direction"`
	Visibility model.Visibility           `json:"visibility,omitempty"` // default private
	URL        string                     `json:"url,omitempty"`        // link to the original
	Date       string                     `json:"date,omitempty"`       // YYYY-MM-DD; default the local date
}

// InteractionAddOutput is the output of interaction_add.
type InteractionAddOutput struct {
	ID       string       `json:"id"`
	Folder   model.Folder `json:"folder"`
	Rev      string       `json:"rev"`
	Warnings []string     `json:"warnings"`
}

// InteractionAdd records an interaction.
var InteractionAdd = define("interaction_add", "Log an interaction with people: a summary in your words and a link", func(ctx context.Context, d Deps, in InteractionAddInput) (InteractionAddOutput, error) {
	title, body, err := splitCapture(in.Summary)
	if err != nil {
		return InteractionAddOutput{}, fmt.Errorf("summary: %w", err)
	}
	if len(in.With) == 0 {
		return InteractionAddOutput{}, errors.New("with is empty: name at least one person or group")
	}
	var with []model.Link
	for _, w := range in.With {
		l, err := linkTo(ctx, d, "with", w, model.TypePerson, model.TypeGroup)
		if err != nil {
			return InteractionAddOutput{}, err
		}
		if l == "" {
			return InteractionAddOutput{}, errors.New("with: empty id")
		}
		if !slices.Contains(with, l) {
			with = append(with, l)
		}
	}
	if err := checkMedium(in.Medium); err != nil {
		return InteractionAddOutput{}, err
	}
	if !slices.Contains(model.InteractionDirections, in.Direction) {
		return InteractionAddOutput{}, fmt.Errorf("direction %q: want one of %v", in.Direction, model.InteractionDirections)
	}
	vis := in.Visibility
	if vis == "" {
		vis = model.VisibilityPrivate
	}
	if !slices.Contains(model.Visibilities, vis) {
		return InteractionAddOutput{}, fmt.Errorf("visibility %q: want public or private", vis)
	}
	date, err := dateOrToday(in.Date)
	if err != nil {
		return InteractionAddOutput{}, err
	}
	u := strings.TrimSpace(in.URL)
	if u != "" {
		if pu, err := url.Parse(u); err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
			return InteractionAddOutput{}, fmt.Errorf("url %q: want an absolute http(s) URL", u)
		}
	}
	doc, err := d.Store.Create(ctx, model.Document{
		Folder: model.FolderInteractions, Title: title, Body: body,
		Meta: &model.Interaction{
			Common: model.Common{Type: model.TypeInteraction},
			Date:   model.Date(date), With: with, Medium: in.Medium, Direction: in.Direction, Visibility: vis, URL: u,
		},
	})
	if err != nil {
		return InteractionAddOutput{}, err
	}
	out := InteractionAddOutput{ID: doc.ID, Folder: doc.Folder, Rev: doc.Rev, Warnings: []string{}}
	if utf8.RuneCountInString(body) > paramd.MaxSummaryLen {
		out.Warnings = append(out.Warnings, WarnSummaryLong)
	}
	return out, nil
})

// InteractionSummary is one interaction in a list.
type InteractionSummary struct {
	ID         string                     `json:"id"`
	Title      string                     `json:"title"`
	Date       string                     `json:"date"`
	With       []string                   `json:"with"`
	Medium     model.Medium               `json:"medium"`
	Direction  model.InteractionDirection `json:"direction"`
	Visibility model.Visibility           `json:"visibility"`
	URL        string                     `json:"url"`
}

// loadInteractions returns every interaction, newest first.
func loadInteractions(ctx context.Context, d Deps) ([]InteractionSummary, error) {
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeInteraction})
	if err != nil {
		return nil, err
	}
	out := make([]InteractionSummary, 0, len(refs))
	for _, r := range refs {
		doc, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		m, ok := doc.Meta.(*model.Interaction)
		if !ok {
			continue
		}
		out = append(out, InteractionSummary{
			ID: doc.ID, Title: doc.Title, Date: string(m.Date), With: targets(m.With), Medium: m.Medium,
			Direction: m.Direction, Visibility: m.Visibility, URL: m.URL,
		})
	}
	slices.SortStableFunc(out, func(a, b InteractionSummary) int {
		if c := strings.Compare(b.Date, a.Date); c != 0 {
			return c
		}
		return strings.Compare(b.ID, a.ID)
	})
	return out, nil
}

// InteractionListInput is the input of interaction_list. Empty fields match all.
type InteractionListInput struct {
	With      string                     `json:"with,omitempty"` // person or group id
	Medium    model.Medium               `json:"medium,omitempty"`
	Direction model.InteractionDirection `json:"direction,omitempty"`
	Since     string                     `json:"since,omitempty"` // YYYY-MM-DD, inclusive
	Limit     int                        `json:"limit,omitempty"` // 0 means all
}

// InteractionListOutput is the output of interaction_list.
type InteractionListOutput struct {
	Items []InteractionSummary `json:"items"`
}

// InteractionList lists interactions, newest first.
var InteractionList = define("interaction_list", "List logged interactions, newest first", func(ctx context.Context, d Deps, in InteractionListInput) (InteractionListOutput, error) {
	if err := optionalDate(in.Since); err != nil {
		return InteractionListOutput{}, fmt.Errorf("since: %w", err)
	}
	if in.Limit < 0 {
		return InteractionListOutput{}, errors.New("limit is negative")
	}
	all, err := loadInteractions(ctx, d)
	if err != nil {
		return InteractionListOutput{}, err
	}
	with := normID(in.With)
	out := InteractionListOutput{Items: []InteractionSummary{}}
	for _, s := range all {
		if (with == "" || slices.Contains(s.With, with)) &&
			(in.Medium == "" || s.Medium == in.Medium) &&
			(in.Direction == "" || s.Direction == in.Direction) &&
			(in.Since == "" || s.Date >= in.Since) {
			out.Items = append(out.Items, s)
		}
	}
	if in.Limit > 0 && len(out.Items) > in.Limit {
		out.Items = out.Items[:in.Limit]
	}
	return out, nil
})

// InteractionShowInput is the input of interaction_show.
type InteractionShowInput struct {
	ID string `json:"id"`
}

// InteractionShowOutput is the output of interaction_show.
type InteractionShowOutput struct {
	Doc model.Document `json:"doc"`
}

// InteractionShow shows one interaction.
var InteractionShow = define("interaction_show", "Show one logged interaction", func(ctx context.Context, d Deps, in InteractionShowInput) (InteractionShowOutput, error) {
	doc, err := d.Store.Get(ctx, normID(in.ID))
	if err != nil {
		return InteractionShowOutput{}, err
	}
	if got := doc.Meta.Header().Type; got != model.TypeInteraction {
		return InteractionShowOutput{}, fmt.Errorf("%s is %s, not interaction", doc.ID, got)
	}
	return InteractionShowOutput{Doc: doc}, nil
})
