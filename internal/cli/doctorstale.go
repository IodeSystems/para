package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

// staleServerNote says whether the running server is an older build than this
// binary. It reports "" when it cannot tell: an unstamped build has no version
// to compare.
func staleServerNote(running, installed string) string {
	if running == "" || installed == "" || running == "dev" || installed == "dev" || running == installed {
		return ""
	}
	return fmt.Sprintf("the running server is %s but the installed binary is %s: run para service restart", running, installed)
}

// staleMCPPids lists the pids of `para mcp` processes whose binary was replaced
// after they started. It reads procRoot (/proc), so it can tell only on Linux
// and only for processes this user may inspect. A process it cannot inspect is
// not listed.
func staleMCPPids(procRoot string) []int {
	if runtime.GOOS != "linux" {
		return nil
	}
	ents, err := os.ReadDir(procRoot)
	if err != nil {
		return nil
	}
	var out []int
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		exe, err := os.Readlink(filepath.Join(procRoot, e.Name(), "exe"))
		if err != nil || !strings.HasSuffix(exe, " (deleted)") || filepath.Base(strings.TrimSuffix(exe, " (deleted)")) != "para" {
			continue
		}
		cmd, err := os.ReadFile(filepath.Join(procRoot, e.Name(), "cmdline"))
		if err != nil {
			continue
		}
		if args := strings.Split(strings.TrimRight(string(cmd), "\x00"), "\x00"); slices.Contains(args[1:], "mcp") {
			out = append(out, pid)
		}
	}
	slices.Sort(out)
	return out
}

func doctorStaleMCP(r *report, procRoot string) {
	pids := staleMCPPids(procRoot)
	if len(pids) == 0 {
		return
	}
	ids := make([]string, len(pids))
	for i, p := range pids {
		ids[i] = strconv.Itoa(p)
	}
	r.warn("mcp servers", fmt.Sprintf("%d run a para binary that was replaced (pid %s): reconnect para in the client (Claude Code: /mcp); a tool call there reports the same", len(pids), strings.Join(ids, ", ")))
}
