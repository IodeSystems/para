import { createFileRoute } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import Chip from '@mui/material/Chip'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useState } from 'react'
import { useCopyText, useDoc } from '@/api/doc'
import { PageTitle } from '@/components/Bits'
import { CopyButton } from '@/components/CopyButton'
import { DataState } from '@/components/DataState'
import { Markdown } from '@/components/Markdown'
import type { Para_CopyInputAs } from '@/gql/graphql'

export const Route = createFileRoute('/_authed/doc/$id')({ component: Doc })

const FORMATS: Para_CopyInputAs[] = ['plain', 'email', 'slack', 'jira']

function Doc() {
  const { id } = Route.useParams()
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
            <Chip size="small" label={d.meta.type} />
            <Chip size="small" label={d.folder} variant="outlined" />
            {d.meta.status && <Chip size="small" label={d.meta.status} variant="outlined" />}
            {d.meta.verified === false && <Chip size="small" color="warning" label="unverified" />}
          </Stack>
          {d.meta.source?.url && (
            <Typography variant="body2" sx={{ mb: 2, overflowWrap: 'anywhere' }}>
              Source: {d.meta.source.url} ({d.meta.source.fidelity})
            </Typography>
          )}
          {copy.isError && (
            <Alert severity="error" sx={{ mb: 2 }}>
              The server refused the copy.
            </Alert>
          )}
          <Markdown source={d.body} />
        </>
      )}
    </DataState>
  )
}
