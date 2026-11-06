---
type: source
created: 2026-11-04
system: wiki
vendor: confluence
status: active
via: cli
ref_url: "https://example.atlassian.net/wiki/pages/viewpage.action?pageId={ref}"
fetch:
  command: "acli confluence page view --id {ref} --body-format storage --json"
  how: Convert the body to para-md (no raw HTML) before doc_version_add.
scan_in:
  how: For each tracked_doc whose via.source is this source, fetch it and pass the text with doc_version_add; record new comments with doc_comment_add.
scan_out:
  how: A page edit that an approved doc edit names is proposal_executed with the text after the edit.
cadence: weekly
---

# Team wiki

Wiki pages the owner follows, through the Atlassian CLI `acli`. Check the flags with `acli confluence --help` on this machine. `para doctor` checks `acli` is on PATH.
