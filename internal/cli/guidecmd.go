package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/iodesystems/para/internal/atomicfile"
	"github.com/iodesystems/para/internal/guide"
	"github.com/iodesystems/para/internal/ops"
)

// newGuideCmd is `para guide [topic]`. With no topic it prints what
// --help-extended prints (no vault needed); with one it runs the op guide.
func newGuideCmd(l *loader) *cobra.Command {
	cmd := opCommand(l, ops.Guide, binding[ops.GuideInput, ops.GuideOutput]{
		Use:  "guide [topic]",
		Args: cobra.MaximumNArgs(1),
		Bind: func(args []string, in *ops.GuideInput) error { in.Topic = strings.Join(args, ""); return nil },
		Text: func(w io.Writer, out ops.GuideOutput) error {
			_, err := fmt.Fprintln(w, out.Text)
			return err
		},
	})
	cmd.Short = "Print use cases with real commands, or the guide text of a topic: " + strings.Join(guide.Topics, ", ")
	run := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if asJSON, _ := cmd.Flags().GetBool("json"); len(args) == 0 && !asJSON {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), guide.Extended())
			return err
		}
		return run(cmd, args)
	}
	return cmd
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

// installSkill writes the skill under dir/para, removes the reference files
// of earlier versions, and records version. It returns the files written.
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
