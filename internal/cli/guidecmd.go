package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/iodesystems/para/internal/atomicfile"
	"github.com/iodesystems/para/internal/guide"
)

// newGuideCmd prints what --help-extended prints, for anyone who looks for a
// command.
func newGuideCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "guide",
		Short: "Print use cases with real commands, and where things go",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), guide.Extended())
			return err
		},
	}
}

// skillVersionFile sits beside SKILL.md and holds the version of the para
// binary that wrote the skill, so setup and doctor can tell a stale skill.
const skillVersionFile = ".para-version"

func defaultSkillsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("skill: %w", err)
	}
	return filepath.Join(home, ".claude", "skills"), nil
}

// installedSkill returns the version recorded beside the installed skill, and
// whether SKILL.md is there. A skill with no version file has version "".
func installedSkill(dir string) (version string, ok bool) {
	if dir == "" {
		var err error
		if dir, err = defaultSkillsDir(); err != nil {
			return "", false
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "para", "SKILL.md")); err != nil {
		return "", false
	}
	b, _ := os.ReadFile(filepath.Join(dir, "para", skillVersionFile))
	return strings.TrimSpace(string(b)), true
}

// installSkill writes the skill and its reference files under dir/para and
// records version. It returns the files written.
func installSkill(dir, version string) ([]any, error) {
	if dir == "" {
		var err error
		if dir, err = defaultSkillsDir(); err != nil {
			return nil, err
		}
	}
	target := filepath.Join(dir, "para")
	if err := os.MkdirAll(target, 0o755); err != nil {
		return nil, fmt.Errorf("skill: %w", err)
	}
	file := filepath.Join(target, "SKILL.md")
	if err := atomicfile.Write(file, []byte(guide.Skill()), 0o644); err != nil {
		return nil, fmt.Errorf("skill: %w", err)
	}
	installed := []any{"installed", file}
	for _, f := range []struct{ name, text string }{
		{guide.SkillImportFile, guide.SkillImport()},
		{guide.SkillIngestFile, guide.SkillIngest()},
		{guide.SkillAlignFile, guide.SkillAlign()},
		{guide.SkillGrillFile, guide.SkillGrill()},
		{guide.SkillSweepFile, guide.SkillSweep()},
		{guide.SkillLoopFile, guide.SkillLoop()},
		{guide.SkillIOFile, guide.SkillIO()},
	} {
		extra := filepath.Join(target, f.name)
		if err := atomicfile.Write(extra, []byte(f.text), 0o644); err != nil {
			return nil, fmt.Errorf("skill: %w", err)
		}
		installed = append(installed, extra)
	}
	for _, name := range guide.SkillRetired {
		if err := os.Remove(filepath.Join(target, name)); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("skill: %w", err)
		}
	}
	if err := atomicfile.Write(filepath.Join(target, skillVersionFile), []byte(version+"\n"), 0o644); err != nil {
		return nil, fmt.Errorf("skill: %w", err)
	}
	return installed, nil
}

// newSkillCmd prints or installs the para skill. Claude Code and dun both read
// ~/.claude/skills, and the file comes from internal/guide, so reinstalling
// after an upgrade is all that keeps it current. The install records the para
// version beside the skill; para setup reinstalls when it differs.
func newSkillCmd(l *loader) *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Print the para skill for agents, as SKILL.md",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprint(cmd.OutOrStdout(), guide.Skill())
			return err
		},
	}
	install := &cobra.Command{
		Use:   "install",
		Short: "Write the skill to ~/.claude/skills/para, read by Claude Code and dun, with the para version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			installed, err := installSkill(dir, l.version)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), installed...)
			return err
		},
	}
	install.Flags().StringVar(&dir, "dir", "", "skills directory (default ~/.claude/skills)")
	cmd.AddCommand(install)
	return cmd
}
