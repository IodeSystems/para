# AGENTS.md — para

Agent brief. Purpose + invariants + pointers only; do not duplicate README or plan here.

## What this is
Task and blocker tracker for a single contributor. Vaults of markdown (Obsidian format). Git is optional. CLI `para`, MCP `para mcp`, server `para serve`, web UI.

## Read first
- `plan/`, when present in your checkout: current state, decisions, active work.
- `README.md` — layout and commands.
- `para guide`: use cases. Repo class: Tool.

## Invariants (from plan.md)
- All vault writes go through `internal/vault`. Hand edits are detected and logged as `external_edit`.
- `events/YYYY-MM.<install>.jsonl` is append-only, one writer per file: ids, metadata, and hashes only, never content.
- Every path out goes through `internal/egress` and logs an event.
- Request gate order: client IP, Host, IP rule, token, scope, Origin. Deny by default. No `--insecure`.
- `X-Forwarded-For` only from exact `net.trusted_proxies`. Do not use chi `RealIP` or joko's `resolveClientIP`.
- No OAuth, inbound or outbound.
- Work and personal never share a machine. Para has no sync feature, but a vault must stay correct under any file-sync tool: per-machine state lives in `$PARA_HOME/vaults/<id>/`, never in the vault.
- TUI is for review and status. Agents write through MCP. Agents get rendered text only; export transports are CLI only.
- Tests that use tmux use a private socket (`tmux -L <name>`). Never touch the default tmux server.
- SQLite is a rebuildable cache, not the source of truth.
- Para does not fetch from outside systems. Agents compose it with other MCP servers and pass a URL and text.
- Policy summaries are never rewritten by `para policy refresh`.
- Port: `para serve` is 7700.

## Build gates before done
`go build ./...` · `go vet ./...` · `bin/t` · `cd ui && pnpm lint && pnpm typecheck && pnpm build`.

## Style
Match surrounding code. Errors wrapped `pkg: method: %w`.

## Git
Fast-forward only, no merge commits. Run each build gate as its own step and read the result before committing or merging. Never push without being asked. Vaults hold no secrets, and no real company or personal data goes in this repo: fixtures are synthetic.
