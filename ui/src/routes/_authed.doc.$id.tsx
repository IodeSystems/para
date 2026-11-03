import { createFileRoute } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Link from '@mui/material/Link'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useState } from 'react'
import { useCopyText, useDoc, useDocFields } from '@/api/doc'
import { LinkButton } from '@/components/links'
import { Details, PageTitle, StateChip, StatusChip } from '@/components/Bits'
import { AgentReplyNote } from '@/components/FindContext'
import { CopyButton } from '@/components/CopyButton'
import { DataState } from '@/components/DataState'
import { hitKindLabel } from '@/lib/hitkind'
import { Markdown } from '@/components/Markdown'
import { QuestionPanel, TaskPanel } from '@/components/QuestionPanel'
import { ReceiptSection } from '@/components/Receipts'
import type { Para_CopyInputAs } from '@/gql/graphql'

export const Route = createFileRoute('/_authed/doc/$id')({ component: Doc })

// withSourceLink turns "Read the source." in a summary stub into a link to the source.
function withSourceLink(body: string, url: string | null | undefined): string {
  return url ? body.replace('Read the source.', `[Read the source](${url}).`) : body
}

const FORMATS: Para_CopyInputAs[] = ['plain', 'email', 'slack', 'jira']

function Doc() {
  const { id } = Route.useParams()
  const q = useDoc(id)
  const copy = useCopyText()
  const fields = useDocFields(id)
  const [as, setAs] = useState<Para_CopyInputAs>('plain')
  return (
    <DataState q={q}>
      {(d) => (
        <>
          <PageTitle
            action={
              <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                {(d.meta.type === 'note' || fields.data?.editable) && (
                  <LinkButton
                    size="small"
                    variant="outlined"
                    to="."
                    title="Change the text yourself"
                    search={(p) =>
                      d.meta.type === 'note' ? { ...p, noteedit: d.id } : { ...p, docedit: d.id }
                    }
                  >
                    Edit
                  </LinkButton>
                )}
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
          <Markdown source={withSourceLink(d.body, d.meta.source?.url)} />
        </>
      )}
    </DataState>
  )
}
