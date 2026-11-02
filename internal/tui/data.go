package tui

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/iodesystems/paradize/internal/model"
	"github.com/iodesystems/paradize/internal/ops"
	"github.com/iodesystems/paradize/internal/registry"
	"github.com/iodesystems/paradize/internal/store/local"
	"github.com/iodesystems/paradize/internal/today"
	"github.com/iodesystems/paradize/internal/vault"
)

// Surface and actor of every write the TUI makes.
const (
	Surface = "tui"
	Actor   = "user"
)

// Env is what the TUI runs against: one install, one open vault, and the way
// to open another. Tests build one over a temporary vault.
type Env struct {
	Install registry.Install
	Vault   registry.Vault
	Deps    ops.Deps
	// StateDir is the per-machine directory for this vault. The review
	// position lives there.
	StateDir string
	Now      func() time.Time
	// Getenv reads the environment; nil means os.Getenv.
	Getenv func(string) string
}

// OpenEnv opens vault v of the install as the TUI.
func OpenEnv(in registry.Install, v registry.Vault) (Env, error) {
	id, err := in.ID()
	if err != nil {
		return Env{}, fmt.Errorf("tui: open: %w", err)
	}
	state := filepath.Join(in.Home, "vaults", v.ID)
	s, err := local.Open(v.Path, vault.Options{ID: v.ID, Actor: Actor, Surface: Surface, Install: id, StateDir: state})
	if err != nil {
		return Env{}, fmt.Errorf("tui: open vault %s: %w", v.ID, err)
	}
	return Env{Install: in, Vault: v, Deps: ops.Deps{Store: s}, StateDir: state, Now: time.Now}, nil
}

func (e Env) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e Env) getenv(k string) string {
	if e.Getenv != nil {
		return e.Getenv(k)
	}
	return os.Getenv(k)
}

// Snapshot is everything the screens show, read through ops in one pass. A
// section that fails to load is empty and noted in Errs.
type Snapshot struct {
	Vault  string
	Loaded time.Time
	Today  today.VaultToday
	Others []today.VaultToday // the install's other vaults, summary only
	Date   string

	Next       []ops.TaskView
	Cycles     [][]string
	Waiting    []ops.WaitingItem
	RFX        []ops.RFXSummary
	Drafts     map[string]string // open RFX id -> draft text
	Requests   []ops.RequestView
	Inbox      []ops.InboxItem
	Projects   []ops.ProjectView
	Tasks      []ops.TaskView
	Graph      ops.GraphOutput
	People     []model.Ref
	Policies   []ops.PolicyView
	Rules      []ops.Rule
	Exceptions []ops.ExceptionView
	Constrs    []ops.ConstraintView
	Events     []model.Event
	LintErrors int
	Errs       []string
}

const eventWindow = 2000

// Load reads a snapshot. It never returns an error: a failing section is
// recorded in Errs so the other screens stay usable.
func Load(ctx context.Context, e Env) Snapshot {
	s := Snapshot{Vault: e.Vault.ID, Loaded: e.now(), Date: e.now().Format("2006-01-02"), Drafts: map[string]string{}}
	note := func(what string, err error) bool {
		if err != nil {
			s.Errs = append(s.Errs, what+": "+err.Error())
			return false
		}
		return true
	}
	if rep, err := today.Read(ctx, e.Install, Actor, Surface, s.Date); note("today", err) {
		for _, v := range rep.Vaults {
			if v.Vault == e.Vault.ID {
				s.Today = v
			} else {
				s.Others = append(s.Others, v)
			}
		}
	}
	if o, err := ops.Next.Run(ctx, e.Deps, ops.NextInput{}); note("next", err) {
		s.Next, s.Cycles = o.Tasks, o.Cycles
	}
	if o, err := ops.RFXWaiting.Run(ctx, e.Deps, ops.RFXWaitingInput{Today: s.Date}); note("rfx waiting", err) {
		s.Waiting = o.Items
	}
	if o, err := ops.RFXList.Run(ctx, e.Deps, ops.RFXListInput{}); note("rfx list", err) {
		s.RFX = o.Items
		for _, r := range o.Items {
			if r.Status != model.RFXOpen || r.Direction != model.RFXOut {
				continue
			}
			if sh, err := ops.RFXShow.Run(ctx, e.Deps, ops.RFXShowInput{ID: r.ID}); err == nil && sh.Draft != "" {
				s.Drafts[r.ID] = sh.Draft
			}
		}
	}
	if o, err := ops.RequestList.Run(ctx, e.Deps, ops.RequestListInput{}); note("requests", err) {
		s.Requests = o.Items
	}
	if o, err := ops.InboxList.Run(ctx, e.Deps, ops.InboxListInput{}); note("inbox", err) {
		s.Inbox = o.Items
	}
	if o, err := ops.ProjectList.Run(ctx, e.Deps, ops.ProjectListInput{}); note("projects", err) {
		s.Projects = o.Projects
	}
	if o, err := ops.TaskList.Run(ctx, e.Deps, ops.TaskListInput{}); note("tasks", err) {
		s.Tasks = o.Tasks
	}
	if o, err := ops.Graph.Run(ctx, e.Deps, ops.GraphInput{}); note("graph", err) {
		s.Graph = o
	}
	if o, err := ops.OrgPersonList.Run(ctx, e.Deps, ops.OrgListInput{}); note("people", err) {
		s.People = o.Items
	}
	if o, err := ops.PolicyList.Run(ctx, e.Deps, ops.PolicyListInput{}); note("policies", err) {
		s.Policies = o.Policies
	}
	if o, err := ops.RuleList.Run(ctx, e.Deps, ops.RuleListInput{}); note("rules", err) {
		s.Rules = o.Rules
	}
	if o, err := ops.ExceptionList.Run(ctx, e.Deps, ops.ExceptionListInput{}); note("exceptions", err) {
		s.Exceptions = o.Exceptions
	}
	if o, err := ops.ConstraintList.Run(ctx, e.Deps, ops.ConstraintListInput{}); note("constraints", err) {
		s.Constrs = o.Constraints
	}
	if o, err := ops.EventsTail.Run(ctx, e.Deps, ops.EventsTailInput{Limit: eventWindow}); note("events", err) {
		s.Events = o.Events
	}
	if o, err := ops.Lint.Run(ctx, e.Deps, ops.LintInput{}); note("lint", err) {
		s.LintErrors = o.Errors
	}
	return s
}

// Fingerprint summarises the vault files so a poll can see a change: count,
// total size, and newest modification time. It skips .git and .para.
func Fingerprint(root string) string {
	var n, size int64
	var newest time.Time
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if b := d.Name(); p != root && (b == ".git" || b == ".para") {
				return filepath.SkipDir
			}
			return nil
		}
		if info, err := d.Info(); err == nil {
			n++
			size += info.Size()
			if info.ModTime().After(newest) {
				newest = info.ModTime()
			}
		}
		return nil
	})
	return fmt.Sprintf("%d/%d/%d", n, size, newest.UnixNano())
}

// docSource is the para-md text of a document: frontmatter, title, body.
func docSource(d model.Document) string {
	b, err := model.Render(d)
	if err != nil {
		return "# " + d.Title + "\n\n" + d.Body
	}
	return string(b)
}

// label shortens an id for a row when there is no title.
func titleOr(title, id string) string {
	if strings.TrimSpace(title) == "" {
		return id
	}
	return title
}
