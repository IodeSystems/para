import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useIncident, useIncidentNote, useIncidentStatus } from '@/api/areas'
import { errorText } from '@/client/gqlClient'
import { Empty, PageTitle, Section } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { PointerLinks } from '@/components/PointerLinks'
import { LinkButton } from '@/components/links'
import { Markdown } from '@/components/Markdown'
import { TaskRows } from '@/components/TaskRows'
import type { Para_IncidentStatusInputStatus } from '@/gql/graphql'

export const Route = createFileRoute('/_authed/incidents/$id')({ component: IncidentPage })

const NEXT: Record<string, { to: Para_IncidentStatusInputStatus; label: string }[]> = {
  open: [
    { to: 'mitigated', label: 'Mark mitigated' },
    { to: 'resolved', label: 'Mark resolved' },
  ],
  mitigated: [
    { to: 'resolved', label: 'Mark resolved' },
    { to: 'open', label: 'Reopen' },
  ],
  resolved: [{ to: 'open', label: 'Reopen' }],
}

function IncidentPage() {
  const { id } = Route.useParams()
  const q = useIncident(id)
  const note = useIncidentNote()
  const status = useIncidentStatus()
  const [text, setText] = useState('')
  const err = note.error ?? status.error
  return (
    <DataState q={q}>
      {(d) => {
        const i = d.incident
        return (
          <>
            <PageTitle>{i.title || id}</PageTitle>
            <Stack direction="row" spacing={1} useFlexGap sx={{ mb: 2, flexWrap: 'wrap' }}>
              <Chip
                size="small"
                label={i.severity}
                color={
                  i.severity === 'sev1' ? 'error' : i.severity === 'sev2' ? 'warning' : 'default'
                }
              />
              <Chip size="small" variant="outlined" label={i.status} />
              <Chip size="small" variant="outlined" label={`started ${i.started}`} />
              {i.ended && <Chip size="small" variant="outlined" label={`ended ${i.ended}`} />}
              {i.area && (
                <LinkButton size="small" to="/areas/$id" params={{ id: i.area }}>
                  area {i.area}
                </LinkButton>
              )}
            </Stack>
            {err && (
              <Alert severity="error" sx={{ mb: 2 }}>
                {errorText(err)}
              </Alert>
            )}
            <Stack direction="row" spacing={1} useFlexGap sx={{ mb: 2, flexWrap: 'wrap' }}>
              {(NEXT[i.status] ?? []).map((n) => (
                <Button
                  key={n.to}
                  size="small"
                  variant="outlined"
                  disabled={status.isPending}
                  onClick={() => status.mutate({ id, status: n.to })}
                >
                  {n.label}
                </Button>
              ))}
            </Stack>
            <Section title="Timeline">
              {d.body.trim() === '' ? <Empty>No entry yet.</Empty> : <Markdown source={d.body} />}
              <Stack
                component="form"
                direction="row"
                spacing={1}
                sx={{ mt: 2 }}
                onSubmit={(e) => {
                  e.preventDefault()
                  if (text.trim() === '') return
                  note.mutate({ id, text: text.trim() }, { onSuccess: () => setText('') })
                }}
              >
                <TextField
                  label="Add to the timeline"
                  value={text}
                  onChange={(e) => setText(e.target.value)}
                  size="small"
                  fullWidth
                />
                <Button
                  type="submit"
                  variant="contained"
                  disabled={note.isPending || text.trim() === ''}
                >
                  Add
                </Button>
              </Stack>
            </Section>
            <PointerLinks links={i.links} />
            <Section title="Follow-ups" count={d.followUps.length}>
              {d.followUps.length === 0 ? (
                <Typography variant="body2" color="text.secondary">
                  No task follows from this incident.
                </Typography>
              ) : (
                <TaskRows tasks={d.followUps} />
              )}
            </Section>
          </>
        )
      }}
    </DataState>
  )
}
