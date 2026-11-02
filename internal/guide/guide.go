// Package guide holds the text that tells people and agents what para is, what
// it is not, and how to use it. One source feeds `para --help`, `para
// --help-extended`, `para bootstrap`, the MCP prompts, and the MCP server
// instructions, so the surfaces cannot disagree. A test in internal/cli parses
// every "$ para ..." line against the command tree and checks every backticked
// tool name against ops.All.
//
// Para does not run meetings. The one text about them is ProcessMeeting, which
// works from a transcript the owner gives it.
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
	//go:embed skill.txt
	skill string
	//go:embed harness_claude.txt
	harnessClaude string
	//go:embed harness_dun.txt
	harnessDun string
	//go:embed reviewcycle.txt
	reviewCycle string
	//go:embed processmeeting.txt
	processMeeting string
	//go:embed journallod.txt
	journalLOD string
	//go:embed raglit.txt
	raglitHelp string
	//go:embed importcorpus.txt
	importCorpus string
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

// ReviewCycle is the MCP prompt "review_cycle": how an agent works with the
// owner through proposals.
func ReviewCycle() string { return trim(reviewCycle) + "\n\n" + Rule() }

// ProcessMeeting is the MCP prompt "process_meeting": one proposal from a
// transcript.
func ProcessMeeting() string { return trim(processMeeting) + "\n\n" + Rule() }

// JournalLOD is the MCP prompt "journal_lod": how to write the day, week, and
// month summaries of a journal.
func JournalLOD() string { return trim(journalLOD) + "\n\n" + Rule() }

// ImportCorpus is the MCP prompt "import_corpus" and the text of `para import --help`:
// how the owner and an agent import and sort a corpus.
func ImportCorpus() string { return trim(importCorpus) + "\n\n" + Rule() }

// Raglit is `para mcp --help-raglit`: connecting a vault to a raglit daemon
// for meaning search.
func Raglit() string { return trim(raglitHelp) }

// Extended is `para --help-extended`: use cases with real commands, and a
// "where does this go?" table.
func Extended() string { return Intro() + "\n\n" + trim(extended) }

// Skill is SKILL.md for the Agent Skills format, read by Claude Code and dun.
func Skill() string {
	return "---\nname: para\ndescription: \"Track what the owner owes and who they wait on, in para vaults: tasks, blockers, open questions and decisions, requests to people, areas, policies, and proposals for review.\"\n---\n\n" +
		"# para\n\n" + trim(skill) + "\n\n" + Orient() + "\n"
}

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
