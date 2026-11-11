# para

para tracks what you owe and who you wait on. It keeps bounded goals, areas for ongoing responsibilities, tasks with dependencies, blockers with reasons, and people, in vaults of markdown. Plans and docs stay where they live, and para links to them.

It is not a documentation system, a pager, a calendar, or a sync tool. You direct the work, agents act through MCP, and you approve what goes out of the vault.

## Install with an agent

Open your agent (Claude Code or another) in this repo and ask it to install para. It follows the Installation section of `AGENTS.md`. It will ask you for a vault name, a path, and the domain of each vault (who controls its data, such as `life`, `iode`, or `corp`).

## Install by hand

Needs git, make, Go (version in `go.mod`), node, and pnpm (version in `ui/package.json`).

    git clone https://github.com/IodeSystems/para.git && cd para
    bin/doctor         # checks the tools above and that ~/.local/bin is on PATH
    make install       # builds the UI and para into ~/.local/bin
    para setup         # doctor, domains, first vault, service, sign-in, optional agent

Fix every `FAIL` line from `bin/doctor` before `make install`. `BINDIR=<dir>` installs somewhere else.

Or install a release (linux and macOS, amd64 and arm64) from https://github.com/IodeSystems/para/releases: download `para_<os>_<arch>.tar.gz`, `para-manifest.json`, and `para-manifest.json.minisig`, check the signature with `minisign -V -p release/minisign.pub -m para-manifest.json` (key ID 2CC27A171DE03D42), compare the archive's sha256 with the manifest, and put `para` from the archive in `~/.local/bin`. Then `para setup`. Later upgrades:

    para upgrade              # from the checkout this para was built from, else the newest signed release
    para upgrade --release    # the newest signed release, even with a checkout

`para upgrade` checks the release's signature and sha256 before it replaces the binary, refuses an older release (`--force` installs it), and restarts the service. Then, for each vault, it prints the dry run of the pending automated steps and asks before it runs them (`--yes` runs them, `--no-vault-steps` skips them).

`para setup` asks each question on a terminal and takes a flag for each (`para setup --help`). A rerun skips what is done. `para setup --dry-run` prints every step and every write and changes nothing. A vault has a domain (`vault.domain` in `.para/vault.properties`, one lowercase word) and a machine lists the domains it holds (`machine.domains` in `$PARA_HOME/install.properties`, never in a vault). A vault opens only on a machine that lists its domain, so a corp vault stays on the corp machine while one machine may hold several (`iode`, `life`). `para init life ~/para/life --domain life` records both; the default domain is the vault id. Only the owner changes `machine.domains`, by CLI (`para setup --allow-domain <d>`), never through MCP.

## With an agent (optional)

    para help claude    # setup for Claude Code: MCP servers, skill, tracked asks, /para loop, status
    para help dun       # the same for dun (also: para mcp --help-claude, --help-dun)
    para skill install
    para mcp --help-raglit
    para --actor claude --why "reason" <command>   # an agent with only a shell

## Vault settings

    para vault config list
    para vault config set goals.active_limit 5

## Web UI

    para serve
    para login            # same as para serve login (needs the server running); sign this browser in to every vault for 30 days; it renews while used
    para serve sessions   # list login keys; para serve logout revokes them

## Learn more

    para guide          # use cases and where things go

## Develop

`bin/t` runs tests. `bin/dev` runs the stack. See `AGENTS.md`.
