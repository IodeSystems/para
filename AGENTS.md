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
- A vault opens only on a machine that lists its domain (`vault.domain` vs `machine.domains` in `$PARA_HOME/install.properties`); a corp vault stays on the corp machine. Only the owner changes `machine.domains`, by CLI; no op or MCP tool does. Para has no sync feature, but a vault must stay correct under any file-sync tool: per-machine state lives in `$PARA_HOME/vaults/<id>/`, never in the vault.
- Agents write through MCP. Agents get rendered text only; export transports are CLI only.
- SQLite is a rebuildable cache, not the source of truth.
- Para does not fetch from outside systems. Agents compose it with other MCP servers and pass a URL and text.
- Policy summaries are never rewritten by `para policy refresh`.
- Port: `para serve` is 7700.

## Installation
Follow in order. Stop and tell the owner at any failure; do not work around it.
1. `bin/doctor` (read-only): git, make, Go, node, pnpm versions, and `~/.local/bin` on PATH. For each `FAIL` line, tell the owner. Do not install system packages or edit shell profiles without asking.
2. `make install` from the repo root. Builds the UI, installs `~/.local/bin/para` (`BINDIR` in `Makefile`), restarts the service if one runs.
3. `command -v para` must print `~/.local/bin/para`. If another para shadows it, tell the owner.
4. Ask the owner first: the domain of each vault (who controls its data: `life`, `iode`, `corp`; a vault opens only on a machine that lists its domain, and a corp vault stays on the corp machine), vault id, vault path, install the service (y/n), hook up an agent (none, claude, dun, or generic; optional, default none). Optionally run `para setup --dry-run ...` first: it prints every step and write and changes nothing. Then run `para setup [--domain <vault>=<domain> ...|--domains-default] [--allow-domain <d>] --vault <id> --path <path> --service|--no-service [--agent <kind>]` and report its output to the owner. It runs doctor, marks each registered vault with its domain and records machine.domains, creates the first vault, installs the service and signs in, and hooks up the agent. Stop at any `fail` line and tell the owner. `--agent claude` runs `claude mcp add` and `para skill install` (a skill of another para version is reinstalled on any run); `--agent generic --agent-name <name>` prints a name, URL, and token once. Other harnesses: `para help dun`.
5. Tell the owner to open their agent (restart the session, check `/mcp`) and run the `bootstrap` prompt for the first interview.

Do not: use OAuth, put secrets in a vault, push to any remote, run `para service install` or start `para serve` without asking, or edit vault files by hand.

## Build gates before done
`go build ./...` · `go vet ./...` · `bin/t` · `cd ui && pnpm lint && pnpm typecheck && pnpm build`.

## Style
Match surrounding code. Errors wrapped `pkg: method: %w`.

## Git
Fast-forward only, no merge commits. Run each build gate as its own step and read the result before committing or merging. Never push without being asked. Vaults hold no secrets, and no real company or personal data goes in this repo: fixtures are synthetic.
