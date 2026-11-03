# para

para tracks what you owe and who you wait on. It keeps bounded goals, areas for ongoing responsibilities, tasks with dependencies, blockers with reasons, and people, in vaults of markdown. Plans and docs stay where they live, and para links to them.

It is not a documentation system, a pager, a calendar, or a sync tool. You direct the work, agents act through MCP, and proposals let you review what they intend.

## Install

Needs Go 1.26 or later, and pnpm for the web UI.

    make install       # builds the UI and para into ~/.local/bin

## First vault

    para init life ~/para/life
    para vault set-default life
    para                # prints the agenda

## With an agent

    para mcp --help-claude
    para mcp --help-dun
    para skill install
    para mcp --help-raglit

## Web UI

    para serve
    para serve login

## Learn more

    para guide          # use cases and where things go

## Develop

`bin/t` runs tests. `bin/dev` runs the stack. See `AGENTS.md`.
