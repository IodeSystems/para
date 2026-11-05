package cli

// streamSZParityCases are the parity rows of the snooze and topic streams:
// feedback, tags, and deferral of dossier items.
func streamSZParityCases() map[string]parityCase {
	return map[string]parityCase{
		"feedback_add": {`{"text":"The decide error did not name the field.","kind":"bug","tool":"proposal_decide"}`,
			[]string{"feedback", "add", "The defer error did not name the field.", "--kind", "bug", "--tool", "defer"}, false},
		"tag_add":      {`{"ids":["task-d1"],"tags":["socials"]}`, []string{"tag", "add", "socials", "--id", "task-s1"}, false},
		"tag_remove":   {`{"ids":["task-d1"],"tags":["socials"]}`, []string{"tag", "remove", "socials", "--id", "task-s1"}, false},
		"tag_list":     {`{}`, []string{"tag", "list"}, true},
		"stack_later":  {`{"questions":["question-oc-a"],"until":"2999-01-01"}`, []string{"stack", "later", "--question", "question-oc-b", "--until", "2999-01-01"}, false},
		"stack_return": {`{"questions":["question-oc-a"]}`, []string{"stack", "return", "--question", "question-oc-b"}, false},
		"sent_list":    {`{}`, []string{"sent", "list"}, false},
	}
}
