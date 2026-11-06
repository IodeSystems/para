import { useState } from 'react'
import { useRouteContext } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Typography from '@mui/material/Typography'
import { useBrief } from '@/api/brief'
import { useNotes } from '@/api/notes'
import { sendToAgent } from '@/api/verbs'
import { canDecide } from '@/client/session'
import { errorText } from '@/client/gqlClient'
import { Empty } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { Markdown } from '@/components/Markdown'
import { lineSecondary, personGroups, type BriefLine } from '@/lib/brief'

function Lines({ lines }: { lines: BriefLine[] }) {
  return (
    <List dense disablePadding>
      {lines.map((l) => (
        <ListItem key={l.id} disableGutters data-brief-line={l.id}>
          <ListItemText
            primary={<DocLink id={l.id}>{l.title || l.id}</DocLink>}
            secondary={lineSecondary(l)}
            slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
          />
        </ListItem>
      ))}
    </List>
  )
}

function Heading({ children }: { children: string }) {
  return (
    <Typography variant="subtitle2" component="h3" sx={{ mt: 1 }}>
      {children}
    </Typography>
  )
}

// BriefBody is the brief of one meeting: per person what is open with them, what it is linked to,
// and what came of the last occurrence. The server reads it from the vault only.
export function BriefBody({ id }: { id: string }) {
  const q = useBrief(id)
  // A meeting with no one named (a hold) has nothing to brief: the server says so.
  if (q.isError && /names no people/.test(errorText(q.error)))
    return <Empty>Nobody is named, so there is nothing to brief.</Empty>
  return (
    <DataState q={q}>
      {(b) => (
        <Box data-brief={id}>
          {b.doneWhen && (
            <Typography variant="body2">
              <b>Done when:</b> {b.doneWhen}
            </Typography>
          )}
          {b.agenda && <Markdown source={b.agenda} />}
          {b.people.map((p, i) => {
            const groups = personGroups(p)
            return (
              <Box key={p.id || `text-${i}`} data-brief-person={p.id || p.text || ''}>
                <Heading>{p.name}</Heading>
                {!p.resolved && (
                  <Typography variant="body2" color="text.secondary" data-brief-unresolved>
                    Unresolved: {p.unresolved}
                  </Typography>
                )}
                {p.resolved && groups.length === 0 && <Empty>Nothing open with them.</Empty>}
                {groups.map((g) => (
                  <Box key={g.title} sx={{ pl: 1 }}>
                    <Typography variant="caption" color="text.secondary">
                      {g.title}
                    </Typography>
                    <Lines lines={g.lines} />
                  </Box>
                ))}
              </Box>
            )
          })}
          {b.linked.length > 0 && (
            <>
              <Heading>Linked</Heading>
              <Lines lines={b.linked} />
            </>
          )}
          {b.previous && (
            <Box data-brief-previous={b.previous.id}>
              <Heading>Last time</Heading>
              <Typography variant="body2">
                <DocLink id={b.previous.id}>{b.previous.title || b.previous.id}</DocLink>
                {b.previous.skipped.length > 0 &&
                  ` - not held since: ${b.previous.skipped.length} occurrence${b.previous.skipped.length === 1 ? '' : 's'}`}
              </Typography>
              {b.previous.notes && <Markdown source={b.previous.notes} />}
              {b.previous.outcome && <Markdown source={b.previous.outcome} />}
            </Box>
          )}
        </Box>
      )}
    </DataState>
  )
}

// FileNotesButton sends a meeting's notes to an agent to file: decisions as answered questions,
// action items as tasks, asks as Asks, then the meeting closes as held (send_to_agent file_notes).
export function FileNotesButton({ id, open }: { id: string; open: boolean }) {
  const { session } = useRouteContext({ from: '/_authed' })
  const notes = useNotes(id)
  const [busy, setBusy] = useState(false)
  const [sent, setSent] = useState('')
  const [err, setErr] = useState('')
  if (!canDecide(session) || !open) return null
  const has = !!notes.data?.md
  const send = async () => {
    setBusy(true)
    setErr('')
    try {
      const r = await sendToAgent({ id, fileNotes: true })
      const req = r.result.request
      setSent(typeof req === 'string' && req ? req : 'queued')
    } catch (e) {
      setErr(errorText(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Box sx={{ mb: 2 }}>
      <Button
        size="small"
        variant="outlined"
        disabled={busy || !has || !!sent}
        onClick={() => void send()}
        data-act="file-notes"
      >
        Send to agent to file
      </Button>
      {!has && (
        <Typography variant="caption" color="text.secondary" sx={{ ml: 1 }}>
          Write notes first.
        </Typography>
      )}
      {sent && (
        <Alert severity="success" sx={{ mt: 1 }} data-file-notes-sent={sent}>
          Sent to an agent ({sent}). It files the decisions, action items, and asks, then closes the
          meeting as held.
        </Alert>
      )}
      {err && (
        <Alert severity="error" sx={{ mt: 1 }}>
          {err}
        </Alert>
      )}
    </Box>
  )
}
