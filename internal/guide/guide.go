// Package guide holds the text that tells people and agents what para is, what
// it is not, and how to use it. One source feeds `para --help`, `para
// --help-extended`, `para bootstrap`, the MCP prompts, and the MCP server
// instructions, so the surfaces cannot disagree. A test in internal/cli parses
// every "$ para ..." line against the command tree and checks every backticked
// tool name against ops.All.
//
// Meetings are left out of the texts on purpose: para does not run them.
package guide

import (
	_ "embed"
	"strings"
)

var (
	//go:embed intro.txt
	intro string
	//go:embed rule.txt
	rule string
	//go:embed orient.txt
	orient string
	//go:embed bootstrap.txt
	bootstrap string
	//go:embed extended.txt
	extended string
	//go:embed harness_claude.txt
	harnessClaude string
	//go:embed harness_dun.txt
	harnessDun string
)

// Intro says what para is and is not, in under 12 lines.
func Intro() string { return trim(intro) }

// Rule is the one rule: what para tracks, and where plans and docs stay.
func Rule() string { return trim(rule) }

// Orient is the MCP prompt "orient": what para is and is not, the rule, and
// how to work a vault.
func Orient() string { return Intro() + "\n\n" + Rule() + "\n\n" + trim(orient) }

// Bootstrap is the interview an agent runs to fill a new vault. Both the MCP
// prompt "bootstrap" and `para bootstrap` print it.
func Bootstrap() string { return trim(bootstrap) + "\n\n" + Rule() }

// Extended is `para --help-extended`: use cases with real commands, and a
// "where does this go?" table.
func Extended() string { return Intro() + "\n\n" + trim(extended) }

// Harness is the setup text for an agent harness, claude or dun. config is the
// server entries for this install.
func Harness(client, config string) string {
	text := harnessDun
	if client == "claude" {
		text = harnessClaude
	}
	return strings.ReplaceAll(trim(text), "{{config}}", strings.TrimRight(config, "\n"))
}

func trim(s string) string { return strings.TrimSpace(s) }
