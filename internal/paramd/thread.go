package paramd

import (
	"fmt"
	"regexp"
	"strings"
)

// Thread key grammar. A key is an item id, #tag, or channel:<source>/<thread>.
var (
	threadIDRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	threadTagRe  = regexp.MustCompile(`^#[a-z0-9][a-z0-9_-]*$`)
	threadChanRe = regexp.MustCompile(`^channel:([^\s/:]+)/(\S+)$`)
)

// Thread key kinds returned by ParseThreadKey.
const (
	ThreadKeyItem    = "item"
	ThreadKeyTag     = "tag"
	ThreadKeyChannel = "channel"
)

// ParseThreadKey splits a thread key into its kind and value. The value is the
// item id, the tag without "#", or "<source>/<thread>". A key must already be
// in canonical form (a tag is lower case, with "-" for spaces).
func ParseThreadKey(s string) (kind, value string, err error) {
	switch {
	case s == "" || strings.TrimSpace(s) != s:
		return "", "", fmt.Errorf("paramd: parse thread key %q: empty or padded", s)
	case strings.HasPrefix(s, "#"):
		if !threadTagRe.MatchString(s) {
			return "", "", fmt.Errorf("paramd: parse thread key %q: tag must be #lower-case-words", s)
		}
		return ThreadKeyTag, s[1:], nil
	case strings.HasPrefix(s, "channel:"):
		m := threadChanRe.FindStringSubmatch(s)
		if m == nil {
			return "", "", fmt.Errorf("paramd: parse thread key %q: channel must be channel:<source-id>/<thread-id>", s)
		}
		return ThreadKeyChannel, m[1] + "/" + m[2], nil
	case threadIDRe.MatchString(s):
		return ThreadKeyItem, s, nil
	}
	return "", "", fmt.Errorf("paramd: parse thread key %q: not an item id, #tag, or channel:<source>/<thread>", s)
}
