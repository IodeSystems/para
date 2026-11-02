import { createFileRoute } from '@tanstack/react-router'
import { useEffect, useRef, useState } from 'react'
import Alert from '@mui/material/Alert'
import Chip from '@mui/material/Chip'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import TextField from '@mui/material/TextField'
import { useSearch } from '@/api/search'
import { Empty, PageTitle } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'

export const Route = createFileRoute('/_authed/search')({
  validateSearch: (s: Record<string, unknown>): { q?: string } => ({
    q: typeof s.q === 'string' && s.q !== '' ? s.q : undefined,
  }),
  component: Search,
})

function Search() {
  const { q = '' } = Route.useSearch()
  const navigate = Route.useNavigate()
  const [text, setText] = useState(q)
  const input = useRef<HTMLInputElement>(null)
  // `/` opens this screen; focus the field so typing starts at once.
  useEffect(() => input.current?.focus(), [])
  const hits = useSearch(q)
  return (
    <>
      <PageTitle>Search</PageTitle>
      <TextField
        inputRef={input}
        label="Search documents, summaries, and transcripts"
        type="search"
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter')
            void navigate({ search: (p) => ({ ...p, q: text.trim() || undefined }), replace: true })
        }}
        fullWidth
        sx={{ mb: 2 }}
        helperText="Press Enter to search."
      />
      {q !== '' && (
        <DataState q={hits}>
          {({ hits: list, degraded }) => (
            <>
              {degraded !== '' && (
                <Alert severity="info" sx={{ mb: 1 }}>
                  {degraded}
                </Alert>
              )}
              {list.length === 0 ? (
                <Empty>No hit for {q}.</Empty>
              ) : (
                <List disablePadding>
                  {list.map((h) => (
                    <ListItem
                      key={`${h.id}-${h.passage.slice(0, 20)}`}
                      disableGutters
                      divider
                      alignItems="flex-start"
                    >
                      <ListItemText
                        primary={<DocLink id={h.id}>{h.title || h.id}</DocLink>}
                        secondary={
                          <>
                            {h.passage}
                            {h.via && <> (text from {h.via})</>}
                            {h.caveat && <> [{h.caveat}]</>}
                          </>
                        }
                        slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
                      />
                      {h.match && (
                        <Chip size="small" variant="outlined" label={h.match} sx={{ mr: 0.5 }} />
                      )}
                      {h.unverified && <Chip size="small" color="warning" label="unverified" />}
                    </ListItem>
                  ))}
                </List>
              )}
            </>
          )}
        </DataState>
      )}
    </>
  )
}
