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

// ImportCorpus is the MCP prompt "import_corpus" and the skill's import.md:
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

// IO is the MCP prompt "io" and the skill's io.md: set up the owner's sources,
// scan them and report what came in, give the numbers per source, and the
// pre-send check every send follows.
func IO() string { return trim(ioText) + "\n\n" + Rule() }

// Raglit is `para mcp --help-raglit`: connecting a vault to a raglit daemon
// for meaning search.
func Raglit() string { return trim(raglitHelp) }

// Extended is `para --help-extended`: use cases with real commands, and a
// "where does this go?" table.
func Extended() string { return Intro() + "\n\n" + trim(extended) }

// Skill is SKILL.md for the Agent Skills format, read by Claude Code and dun.
func Skill() string {
	return "---\nname: para\ndescription: \"Track what the owner owes and who they wait on, in para vaults: tasks, blockers, open questions and decisions, requests to people, areas, and policies. Use `/para import <folder|file>` to bring notes, goals, or a tracker export into a vault, `/para ingest <url|file|text>` to take in one source, and `/para align <goal|area|vault>` to compare the vault with reality, `/para update <task>` to sort the owner's update on a task, `/para validate <goal>` to draft or challenge a goal's brief, `/para grill <goal>` to interview the owner on a goal's risks, unknowns, owners, and policies, and `/para recheck <goal|area|task|vault>` to check again what might no longer be true, and `/para track <url>` to follow a remote document's versions and comments, and `/para io setup` to set up the owner's mail, chat, wiki, tracker, and calendar sources, `/para io [source…]` (or `/para sweep`) to read them with your own tools, bring the vault up to date, and report what came in and what it became (replies owed are obligations), and `/para io stats` for the numbers per source, and `/para delegate <task>` to hand work to another agent over chat and stage what it would send as the owner for review, and `/para loop` to work the agent queue (sweeps, what the owner asked, other agent work) until it is empty; pair it with `/loop 30m /para loop`.\"\nargument-hint: \"[import <folder|file> | ingest <url|file|text> | align <goal|area|vault> | update <task> | validate <goal> | grill <goal> | recheck <scope> | track <url> | io [setup | stats | source…] | sweep [source…] | delegate <task> | loop | health]\"\n---\n\n" +
		"# para\n\n" + skillRoute + "\n\n" + trim(skill) + "\n\n" + Orient() + "\n"
}

// skillRoute reads the skill's arguments. Other subcommands can come here.
const skillRoute = "If the arguments are `import <path>`, read `import.md` beside this file and follow it for that path, a folder or a single file. " +
	"If they are `ingest <url|file|text>`, read `ingest.md` beside this file and follow it for that one source. " +
	"If they are `update <task>`, read `ingest.md` and follow its part An update on an item for that task. " +
	"If they are `align <goal|area|vault>`, read `align.md` beside this file and follow it for that scope. " +
	"If they are `health`, read `align.md` and follow its part Health findings. " +
	"If they are `grill <goal>`, read `grill.md` beside this file and follow it for that goal. " +
	"If they are `validate <goal>`, read `grill.md` and follow its part Brief for that goal. " +
	"If they are `recheck <scope>`, read `grill.md` and follow its part Recheck for that scope: goal, area, or task ids, or the word vault for all. " +
	"If they are `io setup`, read `io.md` beside this file and follow its part Setup. " +
	"If they are `io stats`, read `io.md` and follow its part Stats. " +
	"If they are `io`, with or without source ids, read `io.md` and follow its part Scan for those sources, or every declared source that is due. " +
	"If they are `sweep`, with or without source ids, read `io.md` beside this file and follow its part Scan, as for `io`. " +
	"If they are `track <url>`, read `sweep.md` and follow its part Tracked documents for that document. " +
	"If they are `loop`, read `loop.md` beside this file and follow it until the agent queue is empty. " +
	"If they are `delegate <task>`, read `loop.md` and follow its part Delegate for that task. " +
	"With no arguments, work as below."

// The reference files of the skill, beside SKILL.md, each loaded only when
// its argument is asked.
const (
	SkillImportFile = "import.md"
	SkillIngestFile = "ingest.md"
	SkillAlignFile  = "align.md"
	SkillGrillFile  = "grill.md"
	SkillSweepFile  = "sweep.md"
	SkillLoopFile   = "loop.md"
	SkillIOFile     = "io.md"
)

// SkillRetired names reference files earlier versions installed beside
// SKILL.md, whose text is now part of another file. `para skill install`
// removes them so no stale copy is read.
var SkillRetired = []string{"update.md", "validate.md", "track.md", "delegate.md"}

// SkillImport is the import reference file of the skill. It is the text of
// the prompt import_corpus.
func SkillImport() string { return ImportCorpus() + "\n" }

// SkillIngest is the ingest reference file of the skill. It is the text of the
// prompt ingest_source.
func SkillIngest() string { return IngestSource() + "\n" }

// SkillAlign is the align reference file of the skill. It is the text of the
// prompt align_vault.
func SkillAlign() string { return AlignVault() + "\n" }

// SkillGrill is the grill, validate, and recheck reference file of the skill.
// It is the text of the prompt grill_goal.
func SkillGrill() string { return GrillGoal() + "\n" }

// SkillSweep is the sweep and track reference file of the skill. It is the
// text of the prompt sweep.
func SkillSweep() string { return Sweep() + "\n" }

// SkillLoop is the loop, produce, and delegate reference file of the skill.
// It is the text of the prompt loop.
func SkillLoop() string { return Loop() + "\n" }

// SkillIO is the io reference file of the skill: setup, scan, stats, and the
// send rule. It is the text of the prompt io.
func SkillIO() string { return IO() + "\n" }

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
