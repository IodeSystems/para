package mcpserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/iodesystems/para/changes"
)

// ChangedLine is added to the server instructions when the binary differs from
// the one this vault's last MCP session ran.
const ChangedLine = "para changed since your last session; call changes"

// seenRecord is what the last MCP session of a vault saw. It lives in the
// per-machine state directory, never in the vault: another machine's binary is
// its own business.
type seenRecord struct {
	Key     string `json:"key"`     // binary version and newest release notes
	Release string `json:"release"` // newest release this session could read
	From    string `json:"from"`    // the release before the change, for the changes tool
}

// checkSeenVersion is checkSeen that also returns the binary version the last
// session ran, when it differs from version ("" otherwise).
func checkSeenVersion(path, version string) (changed bool, from, oldVersion string) {
	if path == "" {
		return false, "", ""
	}
	rec := seenRecord{Key: version + "@" + changes.Latest(), Release: changes.Latest()}
	var old seenRecord
	if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &old) == nil && old.Key != "" {
		if old.Key == rec.Key {
			return false, old.From, ""
		}
		if v, _, ok := strings.Cut(old.Key, "@"); ok && v != version {
			oldVersion = v
		}
		// The notice shows only when changes would list something: the same
		// check changes makes, releases after the last one this session saw.
		from = old.Release
		if rs, err := changes.Since(from); err == nil && len(rs) > 0 {
			changed = true
		}
		rec.From = from
	}
	if b, err := json.Marshal(rec); err == nil {
		_ = os.MkdirAll(filepath.Dir(path), 0o700)
		_ = os.WriteFile(path, b, 0o600)
	}
	return changed, from, oldVersion
}
