---
type: source
created: 2026-11-04
system: calendar
vendor: gcal
status: active
via: mcp
ref_url: "https://calendar.google.com/calendar/u/0/r/eventedit/{ref}"
server: google-calendar
tools: [list_events, get_event]
fetch:
  tool: get_event
  args: '{"event_id": "{ref}"}'
scan_in:
  tool: list_events
  args: '{"time_min": "{since}"}'
  how: Pass the events of the next 14 days to calendar_compare; a new invitation the owner has not answered is an io item, direction in, medium meeting.
scan_out:
  how: Responses the owner sent to invitations since the last scan; a held meeting is close.
state:
  tool: get_event
  args: '{"event_id": "{ref}"}'
  how: Read the owner's response and the time; a moved meeting shows in calendar_compare as mismatched.
cadence: daily
---

# Work calendar

The owner's calendar. Reached through a Google Calendar MCP connector inside the agent. The sweep feeds `calendar_compare`, which writes nothing; fix what it finds with the meeting ops.
