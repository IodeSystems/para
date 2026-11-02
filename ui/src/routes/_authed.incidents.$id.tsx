import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import { useAreas, useIncident, useIncidentNote, useIncidentStatus } from '@/api/areas'
import { errorText } from '@/client/gqlClient'
import { Empty, PageTitle, Section, StateChip, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { PointerLinks } from '@/components/PointerLinks'
import { LinkButton } from '@/components/links'
import { Markdown } from '@/components/Markdown'
import { nameOr } from '@/lib/names'
import { toneOfSeverity } from '@/lib/status'
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
  const areas = useAreas()
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
              <StatusChip tone={toneOfSeverity(i.severity)} label={i.severity} />
              <StateChip state={i.status} tone={i.status === 'open' ? 'attn' : undefined} />
              <StatusChip tone="quiet" label={`started ${i.started}`} />
              {i.ended && <StatusChip tone="quiet" label={`ended ${i.ended}`} />}
              {i.area && (
                <LinkButton size="small" to="/areas/$id" params={{ id: i.area }}>
                  area {nameOr(areas.data?.find((a) => a.id === i.area)?.title, i.area)}
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
            <Section title="Follow-ups" count={d.followUps.length} hideEmpty>
              <TaskRows tasks={d.followUps} />
            </Section>
          </>
        )
      }}
    </DataState>
  )
}
