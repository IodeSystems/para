---
type: source
created: 2026-11-04
system: chat
vendor: slack
status: active
via: cli
fetch:
  command: "slackcli messages thread {ref}"
scan_in:
  command: "slackcli search messages --query '{query}' --after {since_date}"
  query: "to:me"
  how: Each direct message or mention not in the vault is one io item, direction in, medium slack.
scan_out:
  command: "slackcli search messages --query 'from:me' --after {since_date}"
  how: A message that answers an open Ask is rfx_reply; one that sends an io draft is io_send.
cadence: daily
---

# Team chat

Direct messages and mentions. `slackcli` stands for the Slack command line tool installed on this machine: replace it and the flags with yours. The tool keeps its own login; no token goes in the vault. `para doctor` checks the command is on PATH.
