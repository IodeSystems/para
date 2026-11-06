---
type: obligation
created: 2026-11-01
kind: meeting
state: open
schedule: series
recur: weekly
scheduled: "2026-11-04T10:00:00-05:00"
end: 2026-11-04T10:30:00-05:00
with:
  - "[[jordan-example]]"
optional: true
attend_default: yes
rfx:
  - "[[rfx-launch-window]]"
tags: []
---

# Launch window sync with Jordan

An obligation is something the owner owes or must attend: kind `meeting` (people at a time) or `commitment` (something owed, `owed_to` the people). `schedule` is `once` (by `due`), `at` (`scheduled` to `end`, RFC 3339 with the zone it was booked in), `series` (by `recur`: each occurrence is its own file, made when the one before closes, with `occurrence_of` and `previous`), or `ongoing` (no date, reviewed on `cadence`). `state` is `open` or `closed` with an `outcome`: done (held), skipped, dropped, declined. An obligation an agent owes is a recurring artifact: `owed_by: [agent:<name>]` and `produce: {kind: journal|report|message|doc|other, template: "[[note-id]]" or the text, to: ["[[group-id]]"], medium: email}`; tick asks the agent for it when each occurrence comes due, and the owner approves it. Para does not write a calendar, record, or transcribe.

## Questions

- [[rfx-launch-window]] Can the portal launch before the audit?

## Notes

What was said, in your words, and the action items as tasks.
