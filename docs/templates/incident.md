---
type: incident
created: 2026-11-01
severity: sev2
status: open
started: 2026-11-01T09:00:00Z
area: "[[atlas-service]]"
links:
  - label: Dashboard
    kind: dashboard
    target: https://grafana.acme.example/d/atlas
  - label: Runbook
    kind: runbook
    target: ~/doc/atlas/runbook.md
tags: []
---

# One-line summary of the fault

What is known: impact, scope, who is affected. Para is not a pager: alerts stay in monitoring, and this record holds what happened and what changes afterward.

## Timeline

- 2026-11-01T09:00:00Z opened sev2
- 2026-11-01T09:12:00Z Rolled back the 08:40 deploy

Add lines with `para incident note <id> <text>`. Follow-up tasks are tasks with `incident: "[[this-incident]]"`; make one with `para task add --incident <id>`.
