---
type: source
created: 2026-11-04
system: tracker
vendor: github
status: active
via: cli
ref_url: "https://github.com/{ref}"
fetch:
  command: "gh issue view {url} --json title,body,state,comments"
scan_in:
  command: "gh search issues --involves @me --updated '>={since}' --json url,title,updatedAt"
  how: An issue or pull request that asks the owner for something is an io item, or a comment on a tracked_doc (doc_comment_add).
scan_out:
  command: "gh search prs --author @me --updated '>={since}' --json url,title,state"
  how: A pull request the owner opened that an approved outgoing draft names is io_send.
state:
  command: "gh pr view {url} --json state,reviewDecision,mergedAt"
cadence: daily
---

# Code reviews and issues

Issues and pull requests that involve the owner, through `gh`, which keeps its own login. `para doctor` checks `gh` is on PATH.
