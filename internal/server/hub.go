package server

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/iodesystems/parasolve/internal/atomicfile"
	"github.com/iodesystems/parasolve/internal/keys"
	"github.com/iodesystems/parasolve/internal/netguard"
	"github.com/iodesystems/parasolve/internal/registry"
	"github.com/iodesystems/parasolve/internal/store/local"
	"github.com/iodesystems/parasolve/internal/vault"
)

// Hub is every vault this process serves. A vault opens once, on first use, and
// stays open: `para init` while the server runs needs no restart.
//
// Actor choice: a Store binds its actor when opened, and a request's actor is
// its key name. The Hub keeps one open Store per vault and derives a per-request
// copy with local.Store.As. That is a struct copy with no file access. The
// vault lock is a file lock taken per write on its own descriptor, so the
// copies exclude each other.
type Hub struct {
	install registry.Install
	log     *slog.Logger
	mu      sync.Mutex
	open    map[string]*Tenant
}

// NewHub returns the Hub of an install.
func NewHub(in registry.Install, log *slog.Logger) *Hub {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Hub{install: in, log: log, open: map[string]*Tenant{}}
}

// ServerDir is the per-machine server state of a vault: admin.token, net.rules,
// keys, and sessions. It is outside the vault tree, so it never syncs.
func ServerDir(in registry.Install, id string) string {
	return filepath.Join(in.Home, "vaults", id, "server")
}

// Lookup returns the tenant of a vault id. A closed vault is gone on the next
// request. The id comes from a Host header, so it is only compared with the
// registered list, never joined into a path.
func (h *Hub) Lookup(id string) (netguard.Tenant, bool) {
	t, err := h.tenant(id)
	if err != nil {
		h.log.Error("vault unavailable", "vault", id, "error", err)
		return nil, false
	}
	if t == nil {
		return nil, false
	}
	return t, true
}

func (h *Hub) tenant(id string) (*Tenant, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if t, ok := h.open[id]; ok {
		if _, err := os.Stat(t.ref); err == nil {
			return t, nil
		}
		delete(h.open, id)
		return nil, nil
	}
	vaults, err := h.install.List()
	if err != nil {
		return nil, err
	}
	for _, v := range vaults {
		if v.ID == id {
			t, err := h.openVault(v)
			if err != nil {
				return nil, err
			}
			h.open[id] = t
			return t, nil
		}
	}
	return nil, nil
}

// Prepare opens every registered vault and creates its server state, so a bad
// vault stops the start, not a later request.
func (h *Hub) Prepare() ([]string, error) {
	vaults, err := h.install.List()
	if err != nil {
		return nil, fmt.Errorf("server: prepare: %w", err)
	}
	ids := make([]string, 0, len(vaults))
	for _, v := range vaults {
		if _, err := h.tenant(v.ID); err != nil {
			return nil, fmt.Errorf("server: prepare: vault %s: %w", v.ID, err)
		}
		ids = append(ids, v.ID)
	}
	return ids, nil
}

func (h *Hub) openVault(v registry.Vault) (*Tenant, error) {
	install, err := h.install.ID()
	if err != nil {
		return nil, err
	}
	state := filepath.Join(h.install.Home, "vaults", v.ID)
	st, err := local.Open(v.Path, vault.Options{ID: v.ID, Install: install, StateDir: state,
		Warn: func(m string) { h.log.Warn("event log", "vault", v.ID, "note", m) }})
	if err != nil {
		return nil, fmt.Errorf("vault %s: %w", v.ID, err)
	}
	dir := ServerDir(h.install, v.ID)
	ks := keys.New(dir)
	created, err := ks.Ensure()
	if err != nil {
		return nil, err
	}
	if created {
		h.log.Info("admin token created", "vault", v.ID)
	}
	if err := EnsureRules(dir); err != nil {
		return nil, err
	}
	return &Tenant{
		id: v.ID, store: st, keys: ks, rulesPath: filepath.Join(dir, "net.rules"),
		ref: filepath.Join(state, "vault.ref"), hide: []string{filepath.Clean(v.Path), h.install.Home}, log: h.log,
	}, nil
}

// EnsureRules writes the default net.rules once.
func EnsureRules(dir string) error {
	p := filepath.Join(dir, "net.rules")
	if _, err := os.Stat(p); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("server: net.rules: %w", err)
	}
	if err := atomicfile.Write(p, []byte(netguard.DefaultRules), 0o600); err != nil {
		return fmt.Errorf("server: net.rules: %w", err)
	}
	return nil
}

// Tenant is one open vault with its keys and rules.
type Tenant struct {
	id        string
	store     *local.Store
	keys      *keys.Store
	rulesPath string
	ref       string
	hide      []string
	log       *slog.Logger
}

// Rules reads net.rules on each request, so an edit needs no restart.
func (t *Tenant) Rules() (*netguard.Rules, error) { return netguard.LoadRules(t.rulesPath) }

// Authenticate resolves a credential. Keys belong to one vault: this tenant's
// directory is the only one searched. The admin token works from this machine
// only, and an IP-pinned key from its addresses only.
func (t *Tenant) Authenticate(cred netguard.Credential, c netguard.Client) (netguard.Principal, error) {
	var id keys.Identity
	var err error
	if cred.Cookie {
		id, err = t.keys.Session(cred.Token)
	} else {
		id, err = t.keys.Bearer(cred.Token)
	}
	if err != nil {
		return netguard.Principal{}, netguard.ErrUnauthenticated
	}
	if id.Admin && !c.Local {
		return netguard.Principal{}, netguard.ErrForbidden
	}
	if !id.AllowsIP(c.Addr) {
		return netguard.Principal{}, netguard.ErrForbidden
	}
	return netguard.Principal{Name: id.Name, Scopes: id.Scopes, Admin: id.Admin, Session: id.Session, Expires: id.Expires}, nil
}
