// Package guide holds the text that tells people and agents what para is, what
// it is not, and how to use it. One source feeds `para --help`, `para
// --help-extended`, `para bootstrap`, the MCP prompts, and the MCP server
// instructions, so the surfaces cannot disagree. A test in internal/cli parses
// every "$ para ..." line against the command tree and checks every backticked
// tool name against ops.All.
//
// Para does not run meetings. The ingest text works from a transcript the
// owner gives it, or from the notes the owner wrote on the meeting (agent work
// of kind meeting-notes).
package guide

import (
	_ "embed"
	"strings"
)

var (
	//go:embed intro.txt
	introText string
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
	//go:embed raglit.txt
	raglitHelp string
	//go:embed import.txt
	importText string
	//go:embed ingest.txt
	ingestSource string
	//go:embed align.txt
	alignVault string
	//go:embed grill.txt
	grillGoal string
	//go:embed sweep.txt
	sweep string
	//go:embed loop.txt
	loop string
	//go:embed io.txt
	ioText string
)

// ruleStart begins the last paragraph of intro.txt: the one rule.
const ruleStart = "The rule:"

// Intro says what para is and is not, in under 12 lines.
func Intro() string { return trim(introText[:strings.Index(introText, ruleStart)]) }

// Rule is the one rule: what para tracks, and where plans and docs stay.
func Rule() string { return trim(introText[strings.Index(introText, ruleStart):]) }

// Orient is the MCP prompt "orient": what para is and is not, the rule, and
// how to work a vault.
func Orient() string { return Intro() + "\n\n" + Rule() + "\n\n" + trim(orient) }

// Bootstrap is the interview an agent runs to fill a new vault. Both the MCP
// prompt "bootstrap" and `para bootstrap` print it.
func Bootstrap() string { return trim(bootstrap) + "\n\n" + Rule() }

// ImportCorpus is the MCP prompt "import_corpus" and `guide` topic import_corpus:
// how the owner and an agent import and sort a corpus.
func ImportCorpus() string { return trim(importText) + "\n\n" + Rule() }

// IngestSource is the MCP prompt "ingest_source": read one source with your own
// tools and update the vault. Its parts cover a meeting transcript, the
// owner's notes on a meeting, and the owner's update on an item.
func IngestSource() string { return trim(ingestSource) + "\n\n" + Rule() }

// AlignVault is the MCP prompt "align_vault": compare a goal, an area, or the
// vault with reality and fix the differences; its part Health findings
// triages `health`.
func AlignVault() string { return trim(alignVault) + "\n\n" + Rule() }

// GrillGoal is the MCP prompt "grill_goal": interview the owner on a goal's
// risks, unknowns, owners, policies, and assumptions; draft or challenge its
// brief; recheck what might no longer be true.
func GrillGoal() string { return trim(grillGoal) + "\n\n" + Rule() }

// Sweep is the MCP prompt "sweep": declare the sources you reach, read the
// due ones with your own tools, write what you found, and record the sweep;
// its part Tracked documents tracks, refreshes, and edits remote documents.
func Sweep() string { return trim(sweep) + "\n\n" + Rule() }

// Loop is the MCP prompt "loop": declare sources, then claim and end agent
// work with request_next and request_done until the queue is empty. Its parts
// Produce and Delegate cover a recurring artifact you owe and work handed to
// another agent.
func Loop() string { return trim(loop) + "\n\n" + Rule() }

// IO is the MCP prompt "io": set up the owner's sources,
// scan them and report what came in, give the numbers per source, and the
// pre-send check every send follows.
func IO() string { return trim(ioText) + "\n\n" + Rule() }

// Raglit is `para mcp --help-raglit`: connecting a vault to a raglit daemon
// for meaning search.
func Raglit() string { return trim(raglitHelp) }

// Extended is `para --help-extended`: use cases with real commands, and a
// "where does this go?" table.
func Extended() string { return Intro() + "\n\n" + trim(extended) }

// Topics are the names `op_run {op: guide, topic}` and `para guide <topic>`
// take: the MCP prompts, in the order the server lists them.
var Topics = []string{"loop", "orient", "bootstrap", "import_corpus", "ingest_source", "align_vault", "grill_goal", "sweep", "io"}

// Topic returns the text of a prompt by name: the same text the MCP prompt
// serves (orient without the vault's pending upgrade note).
func Topic(name string) (string, bool) {
	switch name {
	case "loop":
		return Loop(), true
	case "orient":
		return Orient(), true
	case "bootstrap":
		return Bootstrap(), true
	case "import_corpus":
		return ImportCorpus(), true
	case "ingest_source":
		return IngestSource(), true
	case "align_vault":
		return AlignVault(), true
	case "grill_goal":
		return GrillGoal(), true
	case "sweep":
		return Sweep(), true
	case "io":
		return IO(), true
	}
	return "", false
}

// Skill is SKILL.md for the Agent Skills format, read by Claude Code and dun.
// It is a trigger: the guide text lives in the MCP prompts, and `op_run {op:
// guide, topic: loop}` returns the loop.
func Skill() string {
	return "---\nname: para\ndescription: \"Track what the owner owes and who they wait on, in para vaults: tasks, blockers, open questions and decisions, requests to people, areas, and policies. Use it when work touches para, and when the owner runs /para (for example /loop 30m /para) to work the agent queue.\"\n---\n\n" +
		"# para\n\n" + trim(skill) + "\n"
}

// SkillRetired names reference files earlier versions installed beside
// SKILL.md, whose text is now served by the MCP prompts and `op_run {op:
// guide}`. `para skill install` removes them so no stale copy is read.
var SkillRetired = []string{"update.md", "validate.md", "track.md", "delegate.md",
	"import.md", "ingest.md", "align.md", "grill.md", "sweep.md", "loop.md", "io.md"}

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
