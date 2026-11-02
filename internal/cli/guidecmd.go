package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/iodesystems/parasolve/internal/atomicfile"
	"github.com/iodesystems/parasolve/internal/guide"
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

// newSkillCmd prints or installs the para skill. Claude Code and dun both read
// ~/.claude/skills, and the file comes from internal/guide, so reinstalling
// after an upgrade is all that keeps it current.
func newSkillCmd() *cobra.Command {
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
		Short: "Write the skill to ~/.claude/skills/para, read by Claude Code and dun",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dir == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("skill: %w", err)
				}
				dir = filepath.Join(home, ".claude", "skills")
			}
			target := filepath.Join(dir, "para")
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("skill: %w", err)
			}
			file := filepath.Join(target, "SKILL.md")
			if err := atomicfile.Write(file, []byte(guide.Skill()), 0o644); err != nil {
				return fmt.Errorf("skill: %w", err)
			}
			extra := filepath.Join(target, guide.SkillImportFile)
			if err := atomicfile.Write(extra, []byte(guide.SkillImport()), 0o644); err != nil {
				return fmt.Errorf("skill: %w", err)
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "installed", file, extra)
			return err
		},
	}
	install.Flags().StringVar(&dir, "dir", "", "skills directory (default ~/.claude/skills)")
	cmd.AddCommand(install)
	return cmd
}
