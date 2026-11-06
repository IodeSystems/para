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
import { AgentReplyNote } from '@/components/FindContext'
import { CopyButton } from '@/components/CopyButton'
import { DataState } from '@/components/DataState'
import { hitKindLabel } from '@/lib/hitkind'
import { Markdown } from '@/components/Markdown'
import { withoutSection } from '@/lib/markdown'
import { QuestionPanel, TaskPanel } from '@/components/QuestionPanel'
import { ReceiptSection } from '@/components/Receipts'
import type { Para_CopyInputAs } from '@/gql/graphql'
import { MergedFrom, NotesEditor } from '@/components/NotesEditor'
import { ItemComposer } from '@/components/ItemComposer'
import { ItemActions } from '@/components/AgendaActions'
import { PrioritySelect } from '@/components/PrioritySelect'
import { isClosed, rowKind } from '@/lib/agenda'

export const Route = createFileRoute('/_authed/doc/$id')({ component: Doc })

// withSourceLink turns "Read the source." in a summary stub into a link to the source.
function withSourceLink(body: string, url: string | null | undefined): string {
  return url ? body.replace('Read the source.', `[Read the source](${url}).`) : body
}

const FORMATS: Para_CopyInputAs[] = ['plain', 'email', 'slack', 'jira']

function Doc() {
  const { id } = Route.useParams()
  // Opening the item, or the agent work itself, marks its finished agent work seen.
  useSeenOnOpen(id)
  const q = useDoc(id)
  const copy = useCopyText()
  const [as, setAs] = useState<Para_CopyInputAs>('plain')
  return (
    <DataState q={q}>
      {(d) => (
        <>
          <PageTitle
            action={
              <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                {d.meta.type === 'proposal' && (
                  <LinkButton
                    size="small"
                    variant="outlined"
                    to="/proposals/$id"
                    params={{ id: d.id }}
                  >
                    Open the proposal
                  </LinkButton>
                )}
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
                {d.meta.type === 'tracked_doc' && (
                  <LinkButton size="small" variant="outlined" to="/docs/$id" params={{ id: d.id }}>
                    Open versions and comments
                  </LinkButton>
                )}
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
          <Stack direction="row" sx={{ mb: 2, flexWrap: 'wrap', gap: 0.5 }}>
            <StatusChip tone="quiet" label={hitKindLabel(d.meta.type)} />
            {d.meta.status && <StateChip state={d.meta.status} />}
            {d.meta.verified === false && (
              <Typography variant="caption" color="text.secondary" sx={{ fontStyle: 'italic' }}>
                unverified: no person has checked this summary against its source
              </Typography>
            )}
          </Stack>
          {d.meta.type === 'group' && <PrioritySelect id={d.id} value={d.meta.priority} />}
          <AgentReplyNote id={d.id} />
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
          {d.meta.type === 'task' && <TaskPanel id={d.id} />}
          <ReceiptSection doc={d.id} title="Agent writes" />
          {copy.isError && (
            <Alert severity="error" sx={{ mb: 2 }}>
              The server refused the copy.
            </Alert>
          )}
          <Markdown source={withSourceLink(withoutSection(d.body, 'Notes'), d.meta.source?.url)} />
          <MergedFrom id={id} />
          <NotesEditor id={id} />
          <ItemComposer id={d.id} title={d.title} meta={d.meta} />
        </>
      )}
    </DataState>
  )
}
