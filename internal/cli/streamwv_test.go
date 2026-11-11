package cli

// streamWVParityCases: the generic write verbs add, edit, link, and tag, on
// the tasks seedStreamGVParity makes.
func streamWVParityCases() map[string]parityCase {
	return map[string]parityCase{
		"add":  {`{"type":"task","title":"GV add a","parent":"project-gv-seed","fields":{"done_when":"It is done."}}`, []string{"add", "task", "GV add b", "--parent", "project-gv-seed", "--field", "done_when=It is done."}, false},
		"edit": {`{"id":"task-gv-edit-a","fields":{"effort":"s"}}`, []string{"edit", "task-gv-edit-b", "--field", "effort=s"}, false},
		"link": {`{"from":"task-gv-link-a","to":"task-gv-send"}`, []string{"link", "task-gv-link-b", "task-gv-send"}, false},
		"tag":  {`{"ids":["task-gv-tag-a"],"add":["socials"]}`, []string{"tag", "task-gv-tag-b", "--add", "socials"}, false},
		"work": {`{"action":"ask","title":"Check backups","text":"Check the backups."}`, []string{"work", "ask", "--title", "Check backups", "--text", "Check the backups."}, false},
		"undo": {`{"receipt":"receipt-rc-g"}`, []string{"undo", "receipt-rc-h"}, false},
		// ops_stats counts its own call, so only the shape is compared.
		"ops_stats": {`{}`, []string{"ops", "stats"}, false},
	}
}
