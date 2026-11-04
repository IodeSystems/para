package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/iodesystems/para/internal/keys"
	"github.com/iodesystems/para/internal/netguard"
)

// SessionView says who the caller is. The PWA reads it to choose a screen.
type SessionView struct {
	Vault   string   `json:"vault"`
	Key     string   `json:"key"`
	Admin   bool     `json:"admin"`
	Scopes  []string `json:"scopes"`
	Surface string   `json:"surface"`
	// Expires is when the key ends, or absent. The session ends with it.
	Expires *time.Time `json:"expires,omitempty"`
	// Login: the key is a browser sign-in key, which renews near its expiry.
	Login bool `json:"login,omitempty"`
}

// SessionCreateBody is the answer to POST /auth/session and to POST
// /auth/session/renew.
type SessionCreateBody struct {
	SessionView
	// SessionExpires is when the cookie ends.
	SessionExpires time.Time `json:"session_expires"`
}

type sessionGetOutput struct{ Body SessionView }

type sessionCreateOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
	Body      SessionCreateBody
}

// SessionDeleteBody is the answer to POST /auth/session/delete.
type SessionDeleteBody struct {
	Deleted bool `json:"deleted"`
}

type sessionDeleteOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
	Body      SessionDeleteBody
}

func view(b *boundVault) SessionView {
	p := b.req.Principal
	scopes := p.Scopes
	if p.Admin {
		scopes = append([]string{netguard.ScopeAdmin}, netguard.Scopes...)
	}
	return SessionView{Vault: b.req.Vault, Key: p.Name, Admin: p.Admin, Scopes: scopes, Surface: b.surface, Expires: p.Expires, Login: p.Login}
}

// sessionCookie has no Domain, so it stays on this vault's host. Secure follows
// the scheme the browser used: a Secure cookie is dropped on plain http.
func sessionCookie(c netguard.Client, value string, maxAge int, exp time.Time) http.Cookie {
	return http.Cookie{
		Name: netguard.CookieName, Value: value, Path: "/", MaxAge: maxAge, Expires: exp,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: c.Proto == "https",
	}
}

// registerSession: a key becomes a cookie. The key travels in the Authorization
// header, so the gate has already checked it, and the same call works over
// REST and GraphQL.
func registerSession(r *registrar) {
	s := r.s
	raw(r, "authSessionGet", get, "/auth/session", netguard.ScopeAnyKey, "Show the caller's key, vault, and scopes",
		func(ctx context.Context, _ *noInput) (*sessionGetOutput, error) {
			b, err := s.bound(ctx)
			if err != nil {
				return nil, err
			}
			return &sessionGetOutput{Body: view(b)}, nil
		})
	raw(r, "authSessionCreate", post, "/auth/session", netguard.ScopeAnyKey, "Exchange a key in the Authorization header for a session cookie",
		func(ctx context.Context, _ *noInput) (*sessionCreateOutput, error) {
			b, err := s.bound(ctx)
			if err != nil {
				return nil, err
			}
			p := b.req.Principal
			if p.Session {
				return nil, huma.Error400BadRequest("already a session: send the key in the Authorization header")
			}
			tok, exp, err := b.t.keys.NewSession(keys.Identity{Name: p.Name, Admin: p.Admin, Expires: p.Expires})
			if err != nil {
				b.t.log.Error("session create", "vault", b.t.id, "error", err)
				return nil, huma.Error500InternalServerError("internal error")
			}
			out := &sessionCreateOutput{Body: SessionCreateBody{SessionView: view(b), SessionExpires: exp}}
			out.Body.Surface = surfacePWA
			out.SetCookie = sessionCookie(b.req.Client, tok, int(time.Until(exp).Seconds()), exp)
			return out, nil
		})
	raw(r, "authSessionRenew", post, "/auth/session/renew", netguard.ScopeAnyKey, "Replace the browser's login key near its expiry: a new key with the same scopes, the old one revoked",
		func(ctx context.Context, _ *noInput) (*sessionCreateOutput, error) {
			b, err := s.bound(ctx)
			if err != nil {
				return nil, err
			}
			return s.renew(ctx, b)
		})
	raw(r, "authSessionDelete", post, "/auth/session/delete", netguard.ScopeAnyKey, "End the caller's browser session",
		func(ctx context.Context, _ *noInput) (*sessionDeleteOutput, error) {
			b, err := s.bound(ctx)
			if err != nil {
				return nil, err
			}
			req := netguard.CookieFrom(ctx)
			if req != "" {
				if err := b.t.keys.EndSession(req); err != nil {
					b.t.log.Error("session delete", "vault", b.t.id, "error", err)
					return nil, huma.Error500InternalServerError("internal error")
				}
			}
			out := &sessionDeleteOutput{Body: SessionDeleteBody{Deleted: req != ""}}
			out.SetCookie = sessionCookie(b.req.Client, "", -1, time.Unix(1, 0))
			return out, nil
		})
}

// KeyView is a key without its hash.
type KeyView struct {
	Name    string     `json:"name"`
	Scopes  []string   `json:"scopes"`
	AllowIP []string   `json:"allow_ip"`
	Created time.Time  `json:"created"`
	Expires *time.Time `json:"expires,omitempty"`
	Revoked *time.Time `json:"revoked,omitempty"`
}

func keyView(k keys.Key) KeyView {
	if k.AllowIP == nil {
		k.AllowIP = []string{}
	}
	return KeyView{Name: k.Name, Scopes: k.Scopes, AllowIP: k.AllowIP, Created: k.Created, Expires: k.Expires, Revoked: k.Revoked}
}

// KeyListBody is the answer to the key list.
type KeyListBody struct {
	Keys []KeyView `json:"keys"`
}

type keyListOutput struct{ Body KeyListBody }

// KeyCreateInput is the body of a key create.
type KeyCreateInput struct {
	Name       string   `json:"name"`
	Scopes     []string `json:"scopes"`
	AllowIP    []string `json:"allow_ip,omitempty"`
	TTLSeconds int      `json:"ttl_seconds,omitempty"`
}

// KeyCreateBody holds the token. It is shown once.
type KeyCreateBody struct {
	Key   KeyView `json:"key"`
	Token string  `json:"token"`
}

type keyCreateOutput struct{ Body KeyCreateBody }

// KeyRevokeInput is the body of a key revoke.
type KeyRevokeInput struct {
	Name string `json:"name"`
}

type keyRevokeOutput struct{ Body KeyView }

// registerAdmin: key management, for the admin token only. The admin token
// works from this machine only, so these routes are local only.
func registerAdmin(r *registrar) {
	s := r.s
	raw(r, "adminKeyList", get, "/api/v1/admin/key/list", netguard.ScopeAdmin, "List keys of this vault",
		func(ctx context.Context, _ *noInput) (*keyListOutput, error) {
			b, err := s.bound(ctx)
			if err != nil {
				return nil, err
			}
			list, err := b.t.keys.List()
			if err != nil {
				return nil, huma.Error500InternalServerError("internal error")
			}
			out := &keyListOutput{Body: KeyListBody{Keys: make([]KeyView, 0, len(list))}}
			for _, k := range list {
				out.Body.Keys = append(out.Body.Keys, keyView(k))
			}
			return out, nil
		})
	raw(r, "adminKeyCreate", post, "/api/v1/admin/key/create", netguard.ScopeAdmin, "Create a key; the token is shown once",
		func(ctx context.Context, in *bodyInput[KeyCreateInput]) (*keyCreateOutput, error) {
			b, err := s.bound(ctx)
			if err != nil {
				return nil, err
			}
			if err := checkScopes(in.Body.Scopes); err != nil {
				return nil, err
			}
			k, tok, err := b.t.keys.Create(in.Body.Name, in.Body.Scopes, in.Body.AllowIP, time.Duration(in.Body.TTLSeconds)*time.Second)
			if errors.Is(err, keys.ErrExists) {
				return nil, huma.Error409Conflict(err.Error())
			}
			if err != nil {
				return nil, huma.Error400BadRequest(b.t.public(err))
			}
			return &keyCreateOutput{Body: KeyCreateBody{Key: keyView(k), Token: tok}}, nil
		})
	raw(r, "adminKeyRevoke", post, "/api/v1/admin/key/revoke", netguard.ScopeAdmin, "Revoke a key and its sessions",
		func(ctx context.Context, in *bodyInput[KeyRevokeInput]) (*keyRevokeOutput, error) {
			b, err := s.bound(ctx)
			if err != nil {
				return nil, err
			}
			k, err := b.t.keys.Revoke(in.Body.Name)
			if errors.Is(err, keys.ErrUnknown) {
				return nil, huma.Error404NotFound(err.Error())
			}
			if err != nil {
				return nil, huma.Error500InternalServerError("internal error")
			}
			return &keyRevokeOutput{Body: keyView(k)}, nil
		})
}

func checkScopes(list []string) error {
	for _, s := range list {
		ok := false
		for _, v := range netguard.Scopes {
			ok = ok || v == s
		}
		if !ok {
			return huma.Error400BadRequest("unknown scope " + s)
		}
	}
	return nil
}

// renew replaces the login key of a browser session. Only a person in the
// browser can renew: the caller must come by session cookie, and its key must
// be a login key. The new token never leaves the server; the browser gets a new
// session cookie for the new key. The event holds the two key names only.
func (s *Server) renew(ctx context.Context, b *boundVault) (*sessionCreateOutput, error) {
	p := b.req.Principal
	if !p.Session || p.Admin || !p.Login {
		return nil, huma.Error403Forbidden("only a browser session of a login key can renew: run para serve login")
	}
	k, _, err := b.t.keys.Renew(p.Name)
	switch {
	case errors.Is(err, keys.ErrNotRenewable):
		return nil, huma.Error409Conflict(b.t.public(err))
	case errors.Is(err, keys.ErrInvalid):
		return nil, huma.Error401Unauthorized("the key is no longer valid: run para serve login")
	case err != nil:
		b.t.log.Error("session renew", "vault", b.t.id, "error", err)
		return nil, huma.Error500InternalServerError("internal error")
	}
	tok, exp, err := b.t.keys.NewSession(keys.Identity{Name: k.Name, Expires: k.Expires})
	if err != nil {
		b.t.log.Error("session renew", "vault", b.t.id, "error", err)
		return nil, huma.Error500InternalServerError("internal error")
	}
	if old := netguard.CookieFrom(ctx); old != "" {
		_ = b.t.keys.EndSession(old)
	}
	if err := b.deps().Store.Record(ctx, "login_renew", "", p.Name, k.Name, ""); err != nil {
		b.t.log.Error("session renew event", "vault", b.t.id, "error", err)
	}
	v := view(b)
	v.Key, v.Scopes, v.Expires, v.Surface = k.Name, k.Scopes, k.Expires, surfacePWA
	out := &sessionCreateOutput{Body: SessionCreateBody{SessionView: v, SessionExpires: exp}}
	out.SetCookie = sessionCookie(b.req.Client, tok, int(time.Until(exp).Seconds()), exp)
	return out, nil
}
