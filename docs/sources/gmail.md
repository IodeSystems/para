---
type: source
created: 2026-11-04
system: gmail
status: active
via: mcp
ref_url: "https://mail.google.com/mail/u/0/#all/{ref}"
server: gmail
tools: [search_threads, get_thread, list_drafts]
fetch:
  tool: get_thread
  args: '{"thread_id": "{ref}"}'
scan_in:
  tool: search_threads
  query: "in:inbox after:{since_date} -from:me"
  args: '{"query": "{query}"}'
  how: Each thread not in the vault is one io item, direction in, via {source, ref} with the thread id.
scan_out:
  tool: search_threads
  query: "in:sent after:{since_date}"
  args: '{"query": "{query}"}'
  how: A sent thread that matches an io draft is io_send; a reply to an open Ask is rfx_reply.
state:
  tool: get_thread
  args: '{"thread_id": "{ref}"}'
  how: Read whether the thread has a reply after the owner's last message.
cadence: daily
---

# Work mailbox

Mail that comes to the owner and mail the owner sends. Reached through a Gmail MCP connector inside the agent (for example a claude.ai connector): para and `para doctor` cannot check it. Rename the id to the mailbox, set `reached_by` to the agents that have the connector, and adjust the queries (Gmail may want the date as YYYY/MM/DD).
