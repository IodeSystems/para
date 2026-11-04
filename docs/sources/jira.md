---
type: source
created: 2026-11-04
system: jira
status: active
via: cli
fetch:
  command: "acli jira workitem view {ref} --json"
scan_in:
  command: "acli jira workitem search --jql '{query}' --json"
  query: 'assignee = currentUser() AND updated >= "{since_date}"'
  how: A work item assigned to the owner is a task, or an io item when it needs sorting; a comment on a tracked work item is doc_comment_add.
scan_out:
  command: "acli jira workitem search --jql '{query}' --json"
  query: 'reporter = currentUser() AND updated >= "{since_date}"'
  how: A work item the owner opened that answers an Ask or an approved action is rfx_reply or proposal_executed.
state:
  command: "acli jira workitem view {ref} --fields status --json"
cadence: daily
---

# Work tracker

Work items through the Atlassian CLI `acli`, which keeps its own login. Check the flags with `acli jira workitem --help` on this machine. `para doctor` checks `acli` is on PATH.
