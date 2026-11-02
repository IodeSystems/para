package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/parasolve/internal/ops"
)

// newGateCmd is `para gate`: the check a harness hook runs before an outward act.
func newGateCmd(l *loader) *cobra.Command {
	var bodyFile string
	cmd := &cobra.Command{Use: "gate", Short: "Check an outward act against what the owner approved", Args: cobra.NoArgs}
	cmd.AddCommand(opCommand(l, ops.GateCheck, binding[ops.GateCheckInput, ops.GateCheckOutput]{
		Use:  "check --kind <kind> --target <t> --body-file <f>",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.GateCheckInput) {
			fs.StringVar(&in.Kind, "kind", "", "the subtype, such as pr or email; default any")
			fs.StringVar(&in.Target, "target", "", "where it lands (required)")
			fs.StringVar(&in.Subject, "subject", "", "the subject, when it has one")
			fs.StringVar(&bodyFile, "body-file", "", "file holding the exact text (required)")
		},
		Bind: func(_ []string, in *ops.GateCheckInput) error {
			if in.Target == "" || bodyFile == "" {
				return errors.New("gate check: --target and --body-file are required")
			}
			b, err := os.ReadFile(bodyFile)
			if err != nil {
				return fmt.Errorf("gate check: %w", err)
			}
			in.Body = string(b)
			return nil
		},
		Text: func(w io.Writer, out ops.GateCheckOutput) error {
			p := &printer{w: w}
			if out.Approved {
				p.f("approved\t%s\t%s\n", out.Proposal, out.Item)
			}
			return p.err
		},
	}))
	return cmd
}

// hookTool is one outward command a hook knows. Body flags name where the text
// is; a flag ending in -file reads that file. Target flags name where it lands.
type hookTool struct {
	words  []string // the words the command starts with
	kind   string
	target []string
	body   []string
}

var hookTools = []hookTool{
	{[]string{"gh", "pr", "create"}, "pr", []string{"--repo", "-R"}, []string{"--body-file", "-F", "--body", "-b"}},
	{[]string{"gh", "issue", "create"}, "ticket", []string{"--repo", "-R"}, []string{"--body-file", "-F", "--body", "-b"}},
	{[]string{"gh", "pr", "comment"}, "comment", []string{"--repo", "-R"}, []string{"--body-file", "-F", "--body", "-b"}},
	{[]string{"slack"}, "slack", []string{"--channel", "-c"}, []string{"--text", "-t", "--message", "-m"}},
	{[]string{"slackcli"}, "slack", []string{"--channel", "-c"}, []string{"--text", "-t", "--message", "-m"}},
	{[]string{"acli"}, "jira", []string{"--project", "--key"}, []string{"--description", "--body", "--comment"}},
}

// acliWrites are the acli words that change something. Other acli calls read.
var acliWrites = []string{"create", "edit", "update", "transition", "comment", "delete", "assign"}

// newHookCmd is `para hook`: a PreToolUse hook that blocks known outward commands
// unless the owner approved their exact text.
func newHookCmd(l *loader) *cobra.Command {
	var client string
	cmd := &cobra.Command{Use: "hook", Short: "A harness hook that blocks outward commands unless approved", Args: cobra.NoArgs}
	print := &cobra.Command{
		Use:   "print",
		Short: "Print the hook settings for a harness",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if client != "claude" && client != "dun" {
				return errors.New("hook print: --client must be claude or dun")
			}
			_, err := fmt.Fprint(cmd.OutOrStdout(), hookSettings(client))
			return err
		},
	}
	print.Flags().StringVar(&client, "client", "", "claude or dun (required)")
	check := &cobra.Command{
		Use:   "check",
		Short: "Read one tool call as JSON on stdin; exit 2 to block it",
		Long:  "Harnesses run this before a tool call. It exits 0 for a command it does not know, and for one whose exact text the owner approved. Otherwise it exits 2 with the reason on stderr. A failure to reach the vault blocks.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			in, ok, why := hookCall(cmd.InOrStdin())
			if why != "" {
				return blockHook(why)
			}
			if !ok {
				return nil
			}
			if err := l.load(); err != nil {
				return blockHook(err.Error())
			}
			deps, err := l.deps()
			if err != nil {
				return blockHook(err.Error())
			}
			out, err := ops.GateCheck.Run(cmd.Context(), deps, in)
			if err != nil {
				return blockHook(err.Error())
			}
			if !out.Approved {
				return blockHook("para: " + out.Reason + ". File a proposal with proposal_add, and carry it out after the owner approves. The text must match exactly")
			}
			return nil
		},
	}
	cmd.AddCommand(print, check)
	return cmd
}

// blockError exits 2, which a harness reads as block.
type blockError struct{ msg string }

func (e blockError) Error() string { return e.msg }

// ExitCode is the process exit code of a blocked call.
func (e blockError) ExitCode() int { return 2 }

func blockHook(msg string) error { return blockError{msg} }

func hookSettings(client string) string {
	if client == "dun" {
		return "# Add to the hooks of your dun settings. The tool JSON arrives on stdin; exit 2 blocks.\n" +
			"# This form is untested against a real dun session.\n" +
			"{\n  \"hooks\": {\n    \"PreToolUse\": [\n      {\"matcher\": \"Bash\", \"command\": \"para hook check\"}\n    ]\n  }\n}\n"
	}
	return "{\n  \"hooks\": {\n    \"PreToolUse\": [\n      {\n        \"matcher\": \"Bash|mcp__.*form_input\",\n        \"hooks\": [{\"type\": \"command\", \"command\": \"para hook check\"}]\n      }\n    ]\n  }\n}\n"
}

// hookCall reads a harness's tool call. ok is false for a call the gate does not
// cover. why is set when the call is outward and cannot be checked.
func hookCall(r io.Reader) (in ops.GateCheckInput, ok bool, why string) {
	var call struct {
		Tool  string         `json:"tool_name"`
		Input map[string]any `json:"tool_input"`
	}
	if err := json.NewDecoder(r).Decode(&call); err != nil {
		return in, false, "para hook: tool call is not JSON"
	}
	if strings.Contains(call.Tool, "form_input") {
		value, _ := call.Input["value"].(string)
		ref, _ := call.Input["ref"].(string)
		return ops.GateCheckInput{Kind: "browser", Target: ref, Body: value}, true, ""
	}
	command, _ := call.Input["command"].(string)
	for _, line := range strings.FieldsFunc(command, func(r rune) bool { return r == '\n' || r == ';' }) {
		for _, part := range strings.Split(strings.ReplaceAll(line, "&&", "|"), "|") {
			words := shellWords(part)
			for _, t := range hookTools {
				if !startsWith(words, t.words) || !isWrite(t, words) {
					continue
				}
				in, why := toolInput(t, words)
				return in, why == "", why
			}
		}
	}
	return in, false, ""
}

func isWrite(t hookTool, words []string) bool {
	if t.words[0] != "acli" {
		return true
	}
	for _, w := range words {
		for _, a := range acliWrites {
			if w == a {
				return true
			}
		}
	}
	return false
}

func startsWith(words, prefix []string) bool {
	if len(words) < len(prefix) {
		return false
	}
	for i, p := range prefix {
		if words[i] != p {
			return false
		}
	}
	return true
}

func toolInput(t hookTool, words []string) (ops.GateCheckInput, string) {
	in := ops.GateCheckInput{Kind: t.kind}
	flag := func(names []string) (string, string, bool) {
		for i, w := range words {
			for _, n := range names {
				if w == n && i+1 < len(words) {
					return n, words[i+1], true
				}
				if v, ok := strings.CutPrefix(w, n+"="); ok {
					return n, v, true
				}
			}
		}
		return "", "", false
	}
	_, target, hasTarget := flag(t.target)
	if !hasTarget {
		return in, fmt.Sprintf("para: %s needs an explicit target (%s) so the gate can check it", strings.Join(t.words, " "), t.target[0])
	}
	in.Target = target
	name, body, hasBody := flag(t.body)
	if !hasBody {
		return in, fmt.Sprintf("para: %s needs its text in a flag (%s) so the gate can check it", strings.Join(t.words, " "), t.body[0])
	}
	if strings.HasSuffix(name, "-file") || name == "-F" {
		b, err := os.ReadFile(body)
		if err != nil {
			return in, "para: cannot read " + body
		}
		body = string(b)
	}
	in.Body = body
	return in, ""
}

// shellWords splits a command as a shell does, for quotes and backslashes. It
// does not expand anything.
func shellWords(s string) []string {
	var out []string
	var b strings.Builder
	var quote rune
	started := false
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, started = r, true
		case r == ' ' || r == '\t':
			if started {
				out = append(out, b.String())
				b.Reset()
				started = false
			}
		default:
			b.WriteRune(r)
			started = true
		}
	}
	if started {
		out = append(out, b.String())
	}
	return out
}
