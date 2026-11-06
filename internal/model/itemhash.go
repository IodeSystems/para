package model

import "encoding/json"

// ItemHash is the hash of one version of an item: its frontmatter without
// prepared and updated, and its body. Map keys marshal sorted, so the same
// item gives the same hash. Agent work made for an item (fill-gap, prepare)
// records it, so the same version is not asked for twice.
func ItemHash(m Meta, body string) string {
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	var fields map[string]any
	if err := json.Unmarshal(b, &fields); err != nil {
		return ""
	}
	delete(fields, "prepared")
	delete(fields, "updated")
	out, err := json.Marshal(map[string]any{"meta": fields, "body": body})
	if err != nil {
		return ""
	}
	return hashOf(string(out))
}
