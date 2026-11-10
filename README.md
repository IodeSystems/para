# para

para tracks what you owe and who you wait on. It keeps bounded goals, areas for ongoing responsibilities, tasks with dependencies, blockers with reasons, and people, in vaults of markdown. Plans and docs stay where they live, and para links to them.

It is not a documentation system, a pager, a calendar, or a sync tool. You direct the work, agents act through MCP, and you approve what goes out of the vault.

## Install with an agent

Open your agent (Claude Code or another) in this repo and ask it to install para. It follows the Installation section of `AGENTS.md`. It will ask you for a vault name, a path, and whether this machine is work or personal.

## Install by hand

Needs git, make, Go (version in `go.mod`), node, and pnpm (version in `ui/package.json`).

    git clone https://github.com/IodeSystems/para.git && cd para
    bin/doctor         # checks the tools above and that ~/.local/bin is on PATH
    make install       # builds the UI and para into ~/.local/bin
    para setup         # doctor, machine kind, first vault, service, sign-in, optional agent

Fix every `FAIL` line from `bin/doctor` before `make install`. `BINDIR=<dir>` installs somewhere else.

`para setup` asks each question on a terminal and takes a flag for each (`para setup --help`). A rerun skips what is done. Work and personal vaults never share a machine: setup records the machine kind, and `para init` refuses without one (`para init life ~/para/life --kind work --default` records it too).

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
