package server

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/netguard"
	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/paramd"
	"github.com/iodesystems/para/internal/review"
	"github.com/iodesystems/para/internal/store"
	"github.com/iodesystems/para/internal/today"
)

// Routes for the PWA that are not ops. An op is something an agent can also do
// through MCP. These are not: review state belongs to the person, and an agent
// must not be able to accept its own changes. Each route is a view of one vault.

// UITodayBody is the Today screen of one vault.
type UITodayBody struct {
	Date  string           `json:"date"`
	Today today.VaultToday `json:"today"`
}

type uiTodayOutput struct{ Body UITodayBody }

// ReviewNeed is something that waits for the owner.
type ReviewNeed struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Title string `json:"title"`
}

// ReviewGroup is the events one actor made since the review position.
type ReviewGroup struct {
	Actor  string        `json:"actor"`
	Events []ReviewEvent `json:"events"`
}

// ReviewEvent is an event with the title of the document it touched, for people.
// DocName is empty when the document is gone.
type ReviewEvent struct {
	model.Event
	DocName string `json:"doc_name,omitempty"`
}

// UIReviewBody is the Review screen. Position is the time of the last accept,
// empty before the first.
type UIReviewBody struct {
	Needs    []ReviewNeed  `json:"needs"`
	Groups   []ReviewGroup `json:"groups"`
	Count    int           `json:"count"`
	Position string        `json:"position"`
	CanUndo  bool          `json:"can_undo"`
}

type uiReviewOutput struct{ Body UIReviewBody }

// UIReviewAcceptInput accepts every event up to and including At (RFC 3339).
// An empty At accepts everything now listed.
type UIReviewAcceptInput struct {
	At string `json:"at,omitempty"`
}

type uiReviewMoveOutput struct{ Body UIReviewBody }

// UIReviewReopenInput has no fields.
type UIReviewReopenInput struct{}

// UIDocInput selects a document.
type UIDocInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

type uiDocOutput struct{ Body model.Document }

// UIDocField is one field a person may edit on a document, with its value as
// the edit form shows it.
type UIDocField struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Enum     []string `json:"enum,omitempty"`
	Required bool     `json:"required,omitempty"`
	Value    string   `json:"value"`
	// Choices are the documents a link or links field may point at, by title.
	Choices []UIDocChoice `json:"choices,omitempty"`
}

// UIDocChoice is one document a link field can point at.
type UIDocChoice struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type"`
}

// UIDocFieldsBody lists the editable fields of a document. Editable is false
// for a type with its own ops.
type UIDocFieldsBody struct {
	Editable bool         `json:"editable"`
	Fields   []UIDocField `json:"fields"`
}

type uiDocFieldsOutput struct{ Body UIDocFieldsBody }

// UILiveSubscribeBody is a signed cap for the vault's event channel.
type UILiveSubscribeBody struct {
	Token   string `json:"token"`
	Channel string `json:"channel"`
	Exp     string `json:"exp"` // RFC 3339
}

type uiLiveOutput struct{ Body UILiveSubscribeBody }

func registerUI(r *registrar) {
	s := r.s
	raw(r, "uiToday", get, "/api/v1/ui/today", netguard.ScopeVaultRead, "Show what needs attention in this vault today",
		func(ctx context.Context, _ *noInput) (*uiTodayOutput, error) {
			b, err := s.bound(ctx)
			if err != nil {
				return nil, err
			}
			date := b.t.store.Now().Format("2006-01-02")
			// today.Read walks the whole install. Only this vault's entry is kept.
			rep, err := today.Read(ctx, b.t.install, b.req.Principal.Name, b.surface, date)
			if err != nil {
				b.t.log.Error("ui today", "vault", b.t.id, "error", err)
				return nil, huma.Error500InternalServerError("internal error")
			}
			for _, v := range rep.Vaults {
				if v.Vault == b.t.id {
					return &uiTodayOutput{Body: UITodayBody{Date: rep.Date, Today: v}}, nil
				}
			}
			return nil, huma.Error404NotFound("vault not found")
		})
	raw(r, "uiReview", get, "/api/v1/ui/review", netguard.ScopeVaultRead, "Show what others changed since the last accept",
		func(ctx context.Context, _ *noInput) (*uiReviewOutput, error) {
			b, err := s.bound(ctx)
			if err != nil {
				return nil, err
			}
			body, err := s.review(ctx, b)
			if err != nil {
				return nil, err
			}
			return &uiReviewOutput{Body: body}, nil
		})
	// Accept and reopen change the owner's review position. Only a browser
	// session can: a key cannot accept what its own agent did.
	raw(r, "uiReviewAccept", post, "/api/v1/ui/review/accept", netguard.ScopeVaultWrite, "Accept events up to a time",
		func(ctx context.Context, in *bodyInput[UIReviewAcceptInput]) (*uiReviewMoveOutput, error) {
			b, err := s.bound(ctx)
			if err != nil {
				return nil, err
			}
			if err := mustBeSession(b); err != nil {
				return nil, err
			}
			pos, err := review.LoadPosition(b.t.state)
			if err != nil {
				return nil, huma.Error500InternalServerError("internal error")
			}
			var at time.Time
			if in.Body.At != "" {
				var perr error
				if at, perr = time.Parse(time.RFC3339Nano, in.Body.At); perr != nil {
					return nil, huma.Error400BadRequest("at: want an RFC 3339 time")
				}
			} else {
				cur, err := s.review(ctx, b)
				if err != nil {
					return nil, err
				}
				for _, g := range cur.Groups {
					for _, e := range g.Events {
						if e.TS.After(at) {
							at = e.TS
						}
					}
				}
			}
			if err := review.SavePosition(b.t.state, pos.Accept(at)); err != nil {
				b.t.log.Error("review accept", "vault", b.t.id, "error", err)
				return nil, huma.Error500InternalServerError("internal error")
			}
			body, err := s.review(ctx, b)
			if err != nil {
				return nil, err
			}
			return &uiReviewMoveOutput{Body: body}, nil
		})
	raw(r, "uiReviewReopen", post, "/api/v1/ui/review/reopen", netguard.ScopeVaultWrite, "Undo the last accept",
		func(ctx context.Context, _ *noInput) (*uiReviewMoveOutput, error) {
			b, err := s.bound(ctx)
			if err != nil {
				return nil, err
			}
			if err := mustBeSession(b); err != nil {
				return nil, err
			}
			pos, err := review.LoadPosition(b.t.state)
			if err != nil {
				return nil, huma.Error500InternalServerError("internal error")
			}
			if err := review.SavePosition(b.t.state, pos.Reopen()); err != nil {
				b.t.log.Error("review reopen", "vault", b.t.id, "error", err)
				return nil, huma.Error500InternalServerError("internal error")
			}
			body, err := s.review(ctx, b)
			if err != nil {
				return nil, err
			}
			return &uiReviewMoveOutput{Body: body}, nil
		})
	raw(r, "uiDoc", get, "/api/v1/ui/doc", netguard.ScopeVaultRead, "Read one document with its body",
		func(ctx context.Context, in *UIDocInput) (*uiDocOutput, error) {
			b, err := s.bound(ctx)
			if err != nil {
				return nil, err
			}
			d, err := b.t.store.As(b.req.Principal.Name, b.surface).Get(ctx, in.ID)
			if err != nil {
				return nil, b.t.apiError(err)
			}
			return &uiDocOutput{Body: d}, nil
		})
	raw(r, "uiDocFields", get, "/api/v1/ui/doc/fields", netguard.ScopeVaultRead, "List the fields of a document a person may edit, with their values",
		func(ctx context.Context, in *UIDocInput) (*uiDocFieldsOutput, error) {
			b, err := s.bound(ctx)
			if err != nil {
				return nil, err
			}
			d, err := b.t.store.As(b.req.Principal.Name, b.surface).Get(ctx, in.ID)
			if err != nil {
				return nil, b.t.apiError(err)
			}
			text, err := d.FieldText()
			if err != nil {
				return nil, b.t.apiError(err)
			}
			out := UIDocFieldsBody{Fields: []UIDocField{}}
			var choices []UIDocChoice
			for _, f := range paramd.EditableFields(string(d.Meta.Header().Type)) {
				out.Editable = true
				uf := UIDocField{Name: f.Name, Kind: f.Kind, Enum: f.Enum, Required: f.Required, Value: text[f.Name]}
				if f.Kind == "link" || f.Kind == "links" {
					if choices == nil {
						refs, err := b.t.store.List(ctx, store.Filter{})
						if err != nil {
							return nil, b.t.apiError(err)
						}
						choices = []UIDocChoice{}
						for _, r := range refs {
							if r.ID != in.ID && r.Title != "" {
								choices = append(choices, UIDocChoice{ID: r.ID, Title: r.Title, Type: string(r.Type)})
							}
						}
					}
					uf.Choices = choices
				}
				out.Fields = append(out.Fields, uf)
			}
			return &uiDocFieldsOutput{Body: out}, nil
		})
	raw(r, "uiLiveSubscribe", post, "/api/v1/ui/live/subscribe", netguard.ScopeVaultRead, "Mint a cap for this vault's live event channel",
		func(ctx context.Context, _ *noInput) (*uiLiveOutput, error) {
			b, err := s.bound(ctx)
			if err != nil {
				return nil, err
			}
			tok, ch, exp, err := s.live.Mint(b.t.id, time.Now())
			if err != nil {
				b.t.log.Error("live mint", "vault", b.t.id, "error", err)
				return nil, huma.Error500InternalServerError("internal error")
			}
			return &uiLiveOutput{Body: UILiveSubscribeBody{Token: tok, Channel: ch, Exp: time.Unix(exp, 0).UTC().Format(time.RFC3339)}}, nil
		})
}

func mustBeSession(b *boundVault) error {
	if !b.req.Principal.Session {
		return huma.Error403Forbidden("review position changes need a browser session")
	}
	return nil
}

// review builds the Review screen from the same rules as the CLI.
func (s *Server) review(ctx context.Context, b *boundVault) (UIReviewBody, error) {
	deps := b.deps()
	var snap review.Input
	snap.Drafts = map[string]string{}
	fail := func(what string, err error) (UIReviewBody, error) {
		b.t.log.Error("ui review", "vault", b.t.id, "section", what, "error", b.t.public(err))
		return UIReviewBody{}, b.t.apiError(err)
	}
	rfx, err := ops.RFXList.Run(ctx, deps, ops.RFXListInput{})
	if err != nil {
		return fail("rfx", err)
	}
	snap.RFX = rfx.Items
	for _, x := range rfx.Items {
		if x.Status != model.RFXOpen || x.Direction != model.RFXOut {
			continue
		}
		if sh, err := ops.RFXShow.Run(ctx, deps, ops.RFXShowInput{ID: x.ID}); err == nil && sh.Draft != "" {
			snap.Drafts[x.ID] = sh.Draft
		}
	}
	pol, err := ops.PolicyList.Run(ctx, deps, ops.PolicyListInput{})
	if err != nil {
		return fail("policies", err)
	}
	snap.Policies = pol.Policies
	req, err := ops.RequestList.Run(ctx, deps, ops.RequestListInput{})
	if err != nil {
		return fail("requests", err)
	}
	snap.Requests = req.Items
	ev, err := ops.EventsTail.Run(ctx, deps, ops.EventsTailInput{Limit: reviewWindow})
	if err != nil {
		return fail("events", err)
	}
	snap.Events = ev.Events
	pos, err := review.LoadPosition(b.t.state)
	if err != nil {
		return fail("position", err)
	}
	rd := review.Build(snap, pos)
	out := UIReviewBody{Needs: []ReviewNeed{}, Groups: []ReviewGroup{}, Count: rd.Count, CanUndo: !pos.Previous.Equal(pos.At)}
	if !pos.At.IsZero() {
		out.Position = pos.At.UTC().Format(time.RFC3339Nano)
	}
	for _, n := range rd.Needs {
		out.Needs = append(out.Needs, ReviewNeed{Kind: n.Kind, ID: n.ID, Title: n.Title})
	}
	titles := map[string]string{}
	if refs, err := deps.Store.List(ctx, store.Filter{All: true}); err == nil {
		for _, r := range refs {
			titles[r.ID] = r.Title
		}
	}
	for _, g := range rd.Groups {
		evs := make([]ReviewEvent, 0, len(g.Events))
		for _, e := range g.Events {
			evs = append(evs, ReviewEvent{Event: e, DocName: titles[e.Doc]})
		}
		out.Groups = append(out.Groups, ReviewGroup{Actor: g.Actor, Events: evs})
	}
	return out, nil
}

// reviewWindow is how many recent events Review reads, .
const reviewWindow = 2000
