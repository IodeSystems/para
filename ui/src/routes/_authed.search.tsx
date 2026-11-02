import { createFileRoute } from '@tanstack/react-router'
import { useEffect, useRef, useState } from 'react'
import Alert from '@mui/material/Alert'
import Chip from '@mui/material/Chip'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useSearch } from '@/api/search'
import { Empty, PageTitle } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'

type Params = {
  q?: string
  type?: string
  status?: string
  area?: string
  project?: string
  since?: string
}

const str = (v: unknown): string | undefined => (typeof v === 'string' && v !== '' ? v : undefined)

// The document types a search can be limited to.
const TYPES = [
  'task',
  'project',
  'area',
  'question',
  'incident',
  'note',
  'rfx',
  'person',
  'policy',
  'transcript',
  'constraint',
  'convention',
  'proposal',
  'request',
  'journal',
  'inbox',
]

export const Route = createFileRoute('/_authed/search')({
  validateSearch: (s: Record<string, unknown>): Params => ({
    q: str(s.q),
    type: str(s.type),
    status: str(s.status),
    area: str(s.area),
    project: str(s.project),
    since: str(s.since),
  }),
  component: Search,
})

function Search() {
  const { q = '', type, status, area, project, since } = Route.useSearch()
  const navigate = Route.useNavigate()
  const [text, setText] = useState(q)
  const input = useRef<HTMLInputElement>(null)
  // `/` opens this screen; focus the field so typing starts at once.
  useEffect(() => input.current?.focus(), [])
  const hits = useSearch(q, { type, status, area, project, since })
  const set = (patch: Params) =>
    void navigate({ search: (p) => ({ ...p, ...patch }), replace: true })
  const field = (label: string, key: keyof Params, value: string | undefined, help?: string) => (
    <TextField
      key={key}
      label={label}
      size="small"
      defaultValue={value ?? ''}
      // The URL changes when the field loses focus or Enter is pressed, not on each key.
      onBlur={(e) => {
        if ((e.target.value.trim() || undefined) !== value)
          set({ [key]: e.target.value.trim() || undefined })
      }}
      onKeyDown={(e) => {
        if (e.key === 'Enter' && e.target instanceof HTMLInputElement)
          set({ [key]: e.target.value.trim() || undefined })
      }}
      helperText={help}
      sx={{ flex: '1 1 140px' }}
    />
  )
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
          if (e.key === 'Enter') set({ q: text.trim() || undefined })
        }}
        fullWidth
        sx={{ mb: 1.5 }}
        helperText="Press Enter to search."
      />
      <Stack direction="row" spacing={1} useFlexGap sx={{ mb: 2, flexWrap: 'wrap' }}>
        <TextField
          select
          size="small"
          label="Type"
          value={type ?? ''}
          onChange={(e) => set({ type: e.target.value || undefined })}
          sx={{ flex: '1 1 140px' }}
        >
          <MenuItem value="">Any</MenuItem>
          {TYPES.map((t) => (
            <MenuItem key={t} value={t}>
              {t}
            </MenuItem>
          ))}
        </TextField>
        {field('Status', 'status', status, 'todo, active, open')}
        {field('Area id', 'area', area)}
        {field('Project id', 'project', project)}
        {field('Updated since', 'since', since, '2026-10-01, 7d, 2w')}
      </Stack>
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
                      key={`${h.id}-${h.match ?? ''}-${h.passage.slice(0, 20)}`}
                      disableGutters
                      divider
                      alignItems="flex-start"
                      sx={{ display: 'block' }}
                    >
                      <Stack direction="row" spacing={0.5} sx={{ alignItems: 'flex-start' }}>
                        <ListItemText
                          primary={<DocLink id={h.id}>{h.title || h.id}</DocLink>}
                          secondary={
                            <>
                              {h.passage}
                              {h.via && <> (text from {h.via})</>}
                            </>
                          }
                          slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
                        />
                        {h.match && (
                          <Chip
                            size="small"
                            variant="outlined"
                            color={h.match === 'meaning' ? 'secondary' : 'default'}
                            label={h.match}
                          />
                        )}
                        {h.unverified && <Chip size="small" color="warning" label="unverified" />}
                      </Stack>
                      {h.caveat && (
                        <Typography
                          variant="caption"
                          color="text.secondary"
                          sx={{ display: 'block' }}
                        >
                          Caveat: {h.caveat}
                        </Typography>
                      )}
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
