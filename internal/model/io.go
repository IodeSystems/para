package model

import "strings"

// CaptureText is the text of an inbox capture as written. inbox_add made the
// first line the title and the rest the body, and kept a long first line whole
// in the body with the title cut and ending in "...".
func CaptureText(title, body string) string {
	title, body = strings.TrimSpace(title), strings.TrimSpace(body)
	if cut, ok := strings.CutSuffix(title, "..."); ok && strings.HasPrefix(body, cut) {
		return body
	}
	if body == "" {
		return title
	}
	if title == "" {
		return body
	}
	return title + "\n\n" + body
}
