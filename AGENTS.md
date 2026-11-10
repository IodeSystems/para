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
- Agents write through MCP. Agents get rendered text only; export transports are CLI only.
- SQLite is a rebuildable cache, not the source of truth.
- Para does not fetch from outside systems. Agents compose it with other MCP servers and pass a URL and text.
- Policy summaries are never rewritten by `para policy refresh`.
- Port: `para serve` is 7700.

## Installation
Follow in order. Stop and tell the owner at any failure; do not work around it.
1. Prerequisites: `go version` must meet the `go` line in `go.mod`; `pnpm --version` must work. If one is missing, tell the owner. Do not install system packages without asking.
2. `make install` from the repo root. Builds the UI, installs `~/.local/bin/para` (`BINDIR` in `Makefile`), restarts the service if one runs.
3. `command -v para`. If empty, `~/.local/bin` is not on PATH: tell the owner to add it. Do not edit shell profiles unasked.
4. `para doctor` is read-only. Fix `fail` lines with the owner; report `warn` lines to the owner.
5. First vault, only after asking the owner three things: its id, its path, and whether this machine is work or personal. Work and personal never share a machine: run `para vault list` first; if it lists vaults, ask the owner which kind they are, and stop if they are the other kind. Then `para init <id> <path>` and `para vault set-default <id>`. There is no trust setting; the rule is per machine.
6. Agent wiring. Run the line `para help claude` prints for the new vault, then `para skill install`. The line is `claude mcp add --scope user para-<id> -- para mcp --vault <id> --actor claude`, with the full binary path. Other harnesses: `para help dun`.
7. Verify: `para` prints the agenda, `para doctor` shows no `fail`. Tell the owner to restart the session and check `/mcp`.

Do not: use OAuth, put secrets in a vault, push to any remote, run `para service install` or start `para serve` without asking, or edit vault files by hand.

## Build gates before done
`go build ./...` · `go vet ./...` · `bin/t` · `cd ui && pnpm lint && pnpm typecheck && pnpm build`.

## Style
Match surrounding code. Errors wrapped `pkg: method: %w`.

## Git
Fast-forward only, no merge commits. Run each build gate as its own step and read the result before committing or merging. Never push without being asked. Vaults hold no secrets, and no real company or personal data goes in this repo: fixtures are synthetic.
