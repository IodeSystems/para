---
type: source
created: 2026-11-04
system: wiki
vendor: gdocs
status: active
via: mcp
ref_url: "https://docs.google.com/document/d/{ref}"
server: google-drive
tools: [search_files, read_file_content, get_file_metadata]
fetch:
  tool: read_file_content
  args: '{"file_id": "{ref}"}'
  how: Convert the text to para-md (no raw HTML) before doc_version_add.
scan_in:
  tool: search_files
  query: "modifiedTime > '{since}' and mimeType = 'application/vnd.google-apps.document'"
  args: '{"query": "{query}"}'
  how: For each changed document that is a tracked_doc (via.source this source, via.ref the file id), read it and pass the text with doc_version_add; record new comments with doc_comment_add.
scan_out:
  how: Documents the owner shared or edited since the last scan that match an approved outgoing draft; report the edit with io_send, then record the new version with doc_version_add.
state:
  tool: get_file_metadata
  args: '{"file_id": "{ref}"}'
cadence: daily
---

# Shared documents

Google Docs the owner follows. Reached through a Google Drive MCP connector inside the agent. Track a document once with `doc_track` (via {source: this id, ref: the file id}); each sweep passes the text again.
