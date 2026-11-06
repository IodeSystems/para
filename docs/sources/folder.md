---
type: source
created: 2026-11-04
system: other
vendor: folder
status: active
via: cli
fetch:
  command: "cat {ref}"
scan_in:
  command: "find {query} -type f -newermt {since}"
  query: ~/Documents/para-drop
  how: Each new file is one io item, direction in, medium other, via {source, ref} with the file path.
cadence: daily
---

# Drop folder

A local folder the owner drops notes and exports into. Replace the path in `query`. `para doctor` checks `cat` and `find` are on PATH.
