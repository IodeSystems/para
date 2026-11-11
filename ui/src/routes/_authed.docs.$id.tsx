import { createFileRoute } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Link from '@mui/material/Link'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useState } from 'react'
import { useSeenOnOpen } from '@/api/requests'
import { useCopyText, useDoc } from '@/api/doc'
import { LinkButton } from '@/components/links'
import { Details, PageTitle, StateChip, StatusChip } from '@/components/Bits'
import { AgentLooking } from '@/components/FindContext'
import { ItemDates } from '@/components/ItemDates'
import { CopyButton } from '@/components/CopyButton'
import { DataState } from '@/components/DataState'
import { hitKindLabel } from '@/lib/hitkind'
import { Markdown } from '@/components/Markdown'
import { withoutSection } from '@/lib/markdown'
import { QuestionPanel, TaskPanel } from '@/components/QuestionPanel'
import { IncidentPanel } from '@/components/IncidentPanel'
import { TrackedDoc } from '@/components/TrackedDoc'
import { canDecide } from '@/client/session'
import { ItemContext } from '@/components/ItemContext'
import { ItemsUnder } from '@/components/ItemsUnder'
import type { Para_CopyInputAs } from '@/gql/graphql'
import { MergedFrom, NotesEditor } from '@/components/NotesEditor'
import { ItemLog } from '@/components/ItemLog'
import { ItemComposer } from '@/components/ItemComposer'
import { ItemDraftProvider } from '@/components/ItemDraft'
import { ItemActions } from '@/components/AgendaActions'
import { PrioritySelect } from '@/components/PrioritySelect'
import { isClosed, rowKind } from '@/lib/agenda'

export const Route = createFileRoute('/_authed/docs/$id')({ component: Doc })

// CONTEXT_TYPES are the work items whose page starts with what the item is for.
const CONTEXT_TYPES = ['task', 'obligation', 'question', 'rfx', 'project', 'incident', 'meeting']

// UNDER_TYPES are the items others hang on: a parent task, an obligation or meeting, a question.
const UNDER_TYPES = ['task', 'obligation', 'meeting', 'question']

// withSourceLink turns "Read the source." in a summary stub into a link to the source.
function withSourceLink(body: string, url: string | null | undefined): string {
  return url ? body.replace('Read the source.', `[Read the source](${url}).`) : body
}

const FORMATS: Para_CopyInputAs[] = ['plain', 'email', 'slack', 'jira']

function Doc() {
  const { id } = Route.useParams()
  // Opening the item, or the agent work itself, marks its finished agent work seen.
  useSeenOnOpen(id)
  const { session } = Route.useRouteContext()
  const q = useDoc(id)
  const copy = useCopyText()
  const [as, setAs] = useState<Para_CopyInputAs>('plain')
  return (
    <DataState q={q}>
      {(d) => (
        <ItemDraftProvider id={d.id}>
          <PageTitle
            pin={{ id: d.id }}
            action={
              <Stack
                direction="row"
                useFlexGap
                sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 1, minWidth: 0 }}
              >
                {d.meta.type === 'source' && (
                  <LinkButton
                    size="small"
                    variant="outlined"
                    to="/sources/$id"
                    params={{ id: d.id }}
                  >
                    Open the source
                  </LinkButton>
                )}
                <ItemActions
                  x={{ id: d.id, title: d.title, kind: rowKind(d.meta.type, d.meta.kind) }}
                  closed={isClosed(
                    d.meta.status ?? (typeof d.meta.state === 'string' ? d.meta.state : null),
                  )}
                  deferred={!!d.meta.deferUntil || (d.meta.deferredAfter?.length ?? 0) > 0}
                  open={false}
                />
                {
                  <>
                    <TextField
                      select
                      size="small"
                      value={as}
                      onChange={(e) => setAs(e.target.value as Para_CopyInputAs)}
                      aria-label="Format to copy"
                      sx={{ minWidth: 96 }}
                    >
                      {FORMATS.map((f) => (
                        <MenuItem key={f} value={f}>
                          {f}
                        </MenuItem>
                      ))}
                    </TextField>
                    <CopyButton
                      label="Copy"
                      getText={async () => (await copy.mutateAsync({ id, as })).text}
                    />
                  </>
                }
              </Stack>
            }
          >
            {d.title || d.id}
          </PageTitle>
          <Stack direction="row" sx={{ mb: 2, flexWrap: 'wrap', gap: 0.5, alignItems: 'center' }}>
            <StatusChip tone="quiet" label={hitKindLabel(d.meta.type)} />
            {d.meta.status && <StateChip state={d.meta.status} />}
            <AgentLooking id={d.id} />
            <ItemDates created={d.meta.created} updated={d.meta.updated} />
            {d.meta.verified === false && (
              <Typography variant="caption" color="text.secondary" sx={{ fontStyle: 'italic' }}>
                unverified: no person has checked this summary against its source
              </Typography>
            )}
          </Stack>
          {d.meta.type === 'group' && <PrioritySelect id={d.id} value={d.meta.priority} />}
          {CONTEXT_TYPES.includes(d.meta.type) && <ItemContext id={d.id} />}
          {UNDER_TYPES.includes(d.meta.type) && <ItemsUnder theme={d.id} />}
          {d.meta.source?.url && (
            <Box sx={{ mb: 2 }}>
              <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
                Source:{' '}
                <Link href={d.meta.source.url} target="_blank" rel="noreferrer noopener">
                  {d.meta.source.url}
                </Link>
              </Typography>
              <Details>
                Fidelity: {d.meta.source.fidelity}. Source state: {d.meta.source.status}.
                {d.meta.source.checked ? ` Last checked ${d.meta.source.checked}.` : ''}
              </Details>
            </Box>
          )}
          {d.meta.type === 'question' && <QuestionPanel id={d.id} />}
          {(d.meta.type === 'incident' ||
            (d.meta.type === 'task' && d.meta.kind === 'incident')) && <IncidentPanel id={d.id} />}
          {d.meta.type === 'task' && d.meta.kind !== 'incident' && <TaskPanel id={d.id} />}
          {copy.isError && (
            <Alert severity="error" sx={{ mb: 2 }}>
              The server refused the copy.
            </Alert>
          )}
          <Markdown source={withSourceLink(withoutSection(d.body, 'Notes'), d.meta.source?.url)} />
          {d.meta.type === 'tracked_doc' && <TrackedDoc id={d.id} decide={canDecide(session)} />}
          <MergedFrom id={id} />
          <NotesEditor id={id} />
          <ItemLog id={d.id} />
          <ItemComposer id={d.id} title={d.title} meta={d.meta} />
        </ItemDraftProvider>
      )}
    </DataState>
  )
}
