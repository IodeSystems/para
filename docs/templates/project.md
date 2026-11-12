---
type: project
created: 2026-11-01
status: active
owner: Jordan Example
area: "[[health]]"
state_line: Roof done; the permit is next.
state_date: 2026-11-01
cadence: monthly
links:
  - label: Plan
    kind: plan
    target: https://wiki.acme.example/plans/home
kpis:
  - name: Monthly power use
    unit: kWh
    target: 400
    direction: down
    cadence: monthly
    owner: self
    readings:
      - date: 2026-10-31
        value: 455
        source: https://utility.acme.example/bill/2026-10
tags: []
---

# Project title

An ongoing body of work with no end date. `state_line` is one line on where it stands; para sets `state_date` when the line changes. `status` is `active` (the Projects view lists it), `paused`, or `retired`. Work reaches a project through its goals: a goal names its project with `project`. A note, an Ask, an obligation, or a tracked document may link a project with `project`. A cron is an obligation series with `project` and `makes`. A goal that waits is `parked` with `resume_when`; it shows in the project's icebox. `area` is optional: the area whose rules apply. `kpis` are named numbers with a `target`, a `direction` (`up`, `down`, or `hold` with `tolerance`), a `cadence`, an optional `owner` (`self`: a Next card while it is off target or stale; `agent:<name>`: agent work for that agent; a person id: shown only), and dated `readings`, each with a `source`; the file keeps the last 12 per KPI and tick moves older ones to `archive/<project>-kpis-<year>.md`.

## Notes
