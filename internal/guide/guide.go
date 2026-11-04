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
	//go:embed healthtriage.txt
	healthTriage string
	//go:embed importsteps.txt
	importSteps string
	//go:embed ingest.txt
	ingestSource string
	//go:embed align.txt
	alignVault string
	//go:embed sortupdate.txt
	sortUpdate string
	//go:embed validategoal.txt
	validateGoal string
	//go:embed grill.txt
	grillGoal string
	//go:embed track.txt
	trackDoc string
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

// ReviewCycle is the MCP prompt "review_cycle": how an agent asks the owner
// through a proposal and records the decision from the chat.
func ReviewCycle() string { return trim(reviewCycle) + "\n\n" + Rule() }

// ProcessMeeting is the MCP prompt "process_meeting": vault updates from a
// transcript.
func ProcessMeeting() string { return trim(processMeeting) + "\n\n" + Rule() }

// JournalLOD is the MCP prompt "journal_lod": how to write the day, week, and
// month summaries of a journal.
func JournalLOD() string { return trim(journalLOD) + "\n\n" + Rule() }

// ImportCorpus is the MCP prompt "import_corpus":
// how the owner and an agent import and sort a corpus.
func ImportCorpus() string { return trim(importCorpus) + "\n\n" + Rule() }

// HealthTriage is the MCP prompt "health_triage": settle health findings with
// evidence and fix them.
func HealthTriage() string { return trim(healthTriage) + "\n\n" + Rule() }

// IngestSource is the MCP prompt "ingest_source": read one source with your own
// tools and update the vault.
func IngestSource() string { return trim(ingestSource) + "\n\n" + Rule() }

// AlignVault is the MCP prompt "align_vault": compare a goal, an area, or the
// vault with reality and fix the differences.
func AlignVault() string { return trim(alignVault) + "\n\n" + Rule() }

// SortUpdate is the MCP prompt "sort_update": sort an owner's update on a task
// into typed items.
func SortUpdate() string { return trim(sortUpdate) + "\n\n" + Rule() }

// ValidateGoal is the MCP prompt "validate_goal": draft or challenge a goal's
// brief with sources, and argue against it.
func ValidateGoal() string { return trim(validateGoal) + "\n\n" + Rule() }

// GrillGoal is the MCP prompt "grill_goal": interview the owner on a goal's
// risks, unknowns, owners, policies, and assumptions, and recheck what might
// no longer be true.
func GrillGoal() string { return trim(grillGoal) + "\n\n" + Rule() }

// TrackDoc is the MCP prompt "track_doc": track a remote document, refresh
// it with the text read again, and carry out an approved edit of it.
func TrackDoc() string { return trim(trackDoc) + "\n\n" + Rule() }

// Raglit is `para mcp --help-raglit`: connecting a vault to a raglit daemon
// for meaning search.
func Raglit() string { return trim(raglitHelp) }

// Extended is `para --help-extended`: use cases with real commands, and a
// "where does this go?" table.
func Extended() string { return Intro() + "\n\n" + trim(extended) }

// Skill is SKILL.md for the Agent Skills format, read by Claude Code and dun.
func Skill() string {
	return "---\nname: para\ndescription: \"Track what the owner owes and who they wait on, in para vaults: tasks, blockers, open questions and decisions, requests to people, areas, and policies. Use `/para import <folder|file>` to bring notes, goals, or a tracker export into a vault, `/para ingest <url|file|text>` to take in one source, and `/para align <goal|area|vault>` to compare the vault with reality, `/para update <task>` to sort the owner's update on a task, `/para validate <goal>` to draft or challenge a goal's brief, `/para grill <goal>` to interview the owner on a goal's risks, unknowns, owners, and policies, and `/para recheck <goal|area|task|vault>` to check again what might no longer be true, and `/para track <url>` to follow a remote document's versions and comments.\"\nargument-hint: \"[import <folder|file> | ingest <url|file|text> | align <goal|area|vault> | update <task> | validate <goal> | grill <goal> | recheck <scope> | track <url> | health]\"\n---\n\n" +
		"# para\n\n" + skillRoute + "\n\n" + trim(skill) + "\n\n" + Orient() + "\n"
}

// skillRoute reads the skill's arguments. Other subcommands can come here.
const skillRoute = "If the arguments are `import <path>`, read `import.md` beside this file and follow it for that path, a folder or a single file. If they are `ingest <url|file|text>`, read `ingest.md` beside this file and follow it for that one source. If they are `align <goal|area|vault>`, read `align.md` beside this file and follow it for that scope. If they are `update <task>`, read `update.md` beside this file and follow it for that task. If they are `validate <goal>`, read `validate.md` beside this file and follow it for that goal. If they are `grill <goal>`, read `grill.md` beside this file and follow it for that goal. If they are `recheck <scope>`, read `grill.md` beside this file and follow its Recheck part for that scope: goal, area, or task ids, or the word vault for all. If they are `track <url>`, read `track.md` beside this file and follow it for that document. With no arguments, work as below."

// SkillImportFile is the name of the import steps, beside SKILL.md.
const SkillImportFile = "import.md"

// SkillImport is the import reference file of the skill, loaded only when import
// is asked.
func SkillImport() string { return trim(importSteps) + "\n\n" + Rule() + "\n" }

// SkillIngestFile and SkillAlignFile are the names of the ingest and align
// steps, beside SKILL.md.
const (
	SkillIngestFile   = "ingest.md"
	SkillAlignFile    = "align.md"
	SkillUpdateFile   = "update.md"
	SkillValidateFile = "validate.md"
	SkillGrillFile    = "grill.md"
	SkillTrackFile    = "track.md"
)

// SkillIngest is the ingest reference file of the skill. It is the text of the
// prompt ingest_source.
func SkillIngest() string { return IngestSource() + "\n" }

// SkillAlign is the align reference file of the skill. It is the text of the
// prompt align_vault.
func SkillAlign() string { return AlignVault() + "\n" }

// SkillUpdate is the update reference file of the skill. It is the text of the
// prompt sort_update.
func SkillUpdate() string { return SortUpdate() + "\n" }

// SkillValidate is the validate reference file of the skill. It is the text of
// the prompt validate_goal.
func SkillValidate() string { return ValidateGoal() + "\n" }

// SkillGrill is the grill and recheck reference file of the skill. It is the
// text of the prompt grill_goal.
func SkillGrill() string { return GrillGoal() + "\n" }

// SkillTrack is the track reference file of the skill. It is the text of the
// prompt track_doc.
func SkillTrack() string { return TrackDoc() + "\n" }

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
