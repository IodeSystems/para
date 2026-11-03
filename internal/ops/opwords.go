package ops

import "strings"

// opWords turns an op name into words: task_depends_add is "Task depends add".
func opWords(op string) string {
	s := strings.ReplaceAll(op, "_", " ")
	if s == "" {
		return "Change"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
