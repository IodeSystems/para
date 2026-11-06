import { useState } from 'react'
import { useRouteContext } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import Button from '@mui/material/Button'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import { meetingRsvp, type Attend } from '@/api/meetings'
import { StatusChip } from '@/components/Bits'
import { TextLink } from '@/components/links'
import { useToasts } from '@/components/ToastStack'
import { canDecide } from '@/client/session'
import { errorText } from '@/client/gqlClient'
import { attendSaid, meetingWhen } from '@/lib/meeting'

// Attendance on the agenda. An optional meeting asks nothing: it shows quietly, with one tap to
// say the owner goes (Attend) or not (Skip). A skipped meeting leaves the list for a collapsed
// line. Each tap shows a toast with Undo, which puts the answer before it back.

export type AttendRow = {
  id: string
  title: string
  at?: string | null
  until?: string | null
  attend?: string | null
}

// AttendButtons are Attend and Skip for one meeting; without review:decide there are none.
export function AttendButtons({
  id,
  title,
  skipOnly,
}: {
  id: string
  title: string
  skipOnly?: boolean
}) {
  const { session } = useRouteContext({ from: '/_authed' })
  const toasts = useToasts()
  const qc = useQueryClient()
  const [busy, setBusy] = useState(false)
  if (!canDecide(session)) return null
  const set = async (attend: Attend) => {
    setBusy(true)
    try {
      const res = await meetingRsvp(id, attend)
      await qc.invalidateQueries()
      const before: Attend = res.previous.attend ?? 'unset'
      toasts.push({
        text: attendSaid(attend, res.title || title),
        undo: async () => {
          await meetingRsvp(id, before)
          await qc.invalidateQueries()
        },
      })
    } catch (e) {
      toasts.push({ text: `Could not change ${title}: ${errorText(e)}`, tone: 'error' })
    } finally {
      setBusy(false)
    }
  }
  return (
    <Stack direction="row" spacing={0.5}>
      {!skipOnly && (
        <Button size="small" disabled={busy} onClick={() => void set('yes')}>
          Attend
        </Button>
      )}
      <Button
        size="small"
        color="inherit"
        disabled={busy}
        onClick={() => void set(skipOnly ? 'unset' : 'no')}
      >
        {skipOnly ? 'Unskip' : 'Skip'}
      </Button>
    </Stack>
  )
}

// OptionalMeetings lists optional meetings the owner has not said they go to, muted.
export function OptionalMeetings({ items }: { items: AttendRow[] }) {
  if (items.length === 0) return null
  return (
    <List dense disablePadding aria-label="Optional meetings" sx={{ opacity: 0.75 }}>
      {items.map((m) => (
        <ListItem key={m.id} disableGutters>
          <ListItemText
            primary={
              <TextLink
                to="/obligations/$id"
                params={{ id: m.id }}
                underline="hover"
                color="text.secondary"
              >
                {m.title}
              </TextLink>
            }
            secondary={[meetingWhen(m.at, m.until), m.attend === 'maybe' && 'maybe']
              .filter(Boolean)
              .join(' - ')}
          />
          <StatusChip tone="quiet" label="optional" />
          <AttendButtons id={m.id} title={m.title} />
        </ListItem>
      ))}
    </List>
  )
}

// OptionalLine is one collapsed line for the optional meetings the owner has not said they go to.
// They ask nothing and count for nothing (needs_you); open, the line lists them with Attend.
export function OptionalLine({ items }: { items: AttendRow[] }) {
  const [open, setOpen] = useState(false)
  if (items.length === 0) return null
  return (
    <>
      <Button size="small" color="inherit" aria-expanded={open} onClick={() => setOpen(!open)}>
        {items.length} optional
      </Button>
      {open && <OptionalMeetings items={items} />}
    </>
  )
}

// SkippedLine is one collapsed line for the meetings the owner skips; open, it lists them.
export function SkippedLine({ items }: { items: AttendRow[] }) {
  const [open, setOpen] = useState(false)
  if (items.length === 0) return null
  return (
    <>
      <Button size="small" color="inherit" aria-expanded={open} onClick={() => setOpen(!open)}>
        {items.length} skipped
      </Button>
      {open && (
        <List dense disablePadding aria-label="Skipped meetings" sx={{ opacity: 0.6 }}>
          {items.map((m) => (
            <ListItem key={m.id} disableGutters>
              <ListItemText
                primary={
                  <TextLink
                    to="/obligations/$id"
                    params={{ id: m.id }}
                    underline="hover"
                    color="text.secondary"
                  >
                    {m.title}
                  </TextLink>
                }
                secondary={meetingWhen(m.at, m.until)}
              />
              <AttendButtons id={m.id} title={m.title} skipOnly />
            </ListItem>
          ))}
        </List>
      )}
    </>
  )
}
