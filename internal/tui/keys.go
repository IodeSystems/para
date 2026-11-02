// Package tui is the terminal review and status surface. It reads and writes
// through internal/ops only, with surface "tui", and never sends anything.
package tui

import (
	"strings"
	"unicode"
)

// KeyStyle is the one style constant for a key letter: bold and underlined, as
// raw SGR so that it nests inside any other style without a full reset. Off
// clears only these two attributes. Changing it changes every key on every
// screen.
var KeyStyle = struct{ On, Off string }{On: "\x1b[1;4m", Off: "\x1b[22;24m"}

// keyed renders s as a key letter.
func keyed(s string) string { return KeyStyle.On + s + KeyStyle.Off }

// Global keys. Link keys never use these. Digits switch tabs.
const (
	reservedGlobals = "jkgq" // plus G, ?, /, esc, ; and the digits
	overflowPrefix  = ";"    // overflow keys are ";" and two letters
	alphabet        = "abcdefghijklmnopqrstuvwxyz"
)

// Target is something a key can follow. ID is the identity: two labels with
// one ID share one key. Pref is a preferred key for an action; it applies only
// when Label holds that letter.
type Target struct {
	ID    string
	Label string
	Pref  rune
	// Dim shows the target greyed: it has a key, and does not apply now.
	Dim bool
}

// Key is the key of one target. Code is one lowercase letter, or ";" and two
// letters. At is the rune index of the key letter inside the label, or -1 when
// the label holds no free letter: the key is then shown after the label.
type Key struct {
	Code string
	At   int
}

// Assign gives each target a key. It is a pure function of its inputs, so the
// same document always gets the same keys. Actions pick before links. Within
// each group the order of the slice decides. A target seen twice keeps its
// first key. reserved holds extra letters that are not available.
func Assign(actions, links []Target, reserved string) map[string]Key {
	used := map[rune]bool{}
	for _, r := range reservedGlobals + strings.ToLower(reserved) {
		used[r] = true
	}
	out := map[string]Key{}
	overflow := 0
	take := func(t Target, preferred bool) {
		if _, done := out[t.ID]; done {
			return
		}
		lab := []rune(t.Label)
		if preferred && t.Pref != 0 {
			for i, r := range lab {
				if unicode.ToLower(r) == t.Pref && !used[t.Pref] {
					used[t.Pref] = true
					out[t.ID] = Key{Code: string(t.Pref), At: i}
					return
				}
			}
		}
		for i, r := range lab {
			l := unicode.ToLower(r)
			if l >= 'a' && l <= 'z' && !used[l] {
				used[l] = true
				out[t.ID] = Key{Code: string(l), At: i}
				return
			}
		}
		for _, l := range alphabet {
			if !used[l] {
				used[l] = true
				out[t.ID] = Key{Code: string(l), At: -1}
				return
			}
		}
		n := len(alphabet)
		code := overflowPrefix + string(alphabet[(overflow/n)%n]) + string(alphabet[overflow%n])
		overflow++
		out[t.ID] = Key{Code: code, At: -1}
	}
	for _, t := range actions {
		take(t, true)
	}
	for _, t := range links {
		take(t, false)
	}
	return out
}

// Mark renders label with its key shown: bold and underline on the key letter, or
// the code after the label when the label holds no letter for it.
func Mark(label string, k Key) string {
	lab := []rune(label)
	if k.At >= 0 && k.At < len(lab) {
		return string(lab[:k.At]) + keyed(string(lab[k.At])) + string(lab[k.At+1:])
	}
	return label + " " + keyed(k.Code)
}
