import { createFileRoute } from '@tanstack/react-router'
import { useEffect, useRef, useState, type ReactElement } from 'react'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import AssignmentOutlinedIcon from '@mui/icons-material/AssignmentOutlined'
import CallSplitIcon from '@mui/icons-material/CallSplit'
import DescriptionOutlinedIcon from '@mui/icons-material/DescriptionOutlined'
import FolderOutlinedIcon from '@mui/icons-material/FolderOutlined'
import GavelIcon from '@mui/icons-material/Gavel'
import HelpOutlinedIcon from '@mui/icons-material/HelpOutlined'
import MicNoneOutlinedIcon from '@mui/icons-material/MicNoneOutlined'
import PersonOutlinedIcon from '@mui/icons-material/PersonOutlined'
import SendOutlinedIcon from '@mui/icons-material/SendOutlined'
import { useAreas } from '@/api/areas'
import { useSearch } from '@/api/search'
import { useWork } from '@/api/work'
import { Empty, PageTitle, StatusChip } from '@/components/Bits'
import { SearchState } from '@/components/SearchState'
import { DocLink } from '@/components/DocLink'
import { TextLink } from '@/components/links'
import { highlight, readable } from '@/lib/highlight'
import { hitKindLabel } from '@/lib/hitkind'
import { nameOr } from '@/lib/names'

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
  'io',
]

const SINCE = [
  { value: '1d', label: 'Today' },
  { value: '7d', label: 'Last 7 days' },
  { value: '30d', label: 'Last 30 days' },
  { value: '90d', label: 'Last 90 days' },
]

const ICON: Record<string, ReactElement> = {
  task: <AssignmentOutlinedIcon fontSize="inherit" />,
  project: <FolderOutlinedIcon fontSize="inherit" />,
  area: <FolderOutlinedIcon fontSize="inherit" />,
  question: <HelpOutlinedIcon fontSize="inherit" />,
  rfx: <SendOutlinedIcon fontSize="inherit" />,
  person: <PersonOutlinedIcon fontSize="inherit" />,
  policy: <GavelIcon fontSize="inherit" />,
  summary: <GavelIcon fontSize="inherit" />,
  constraint: <GavelIcon fontSize="inherit" />,
  transcript: <MicNoneOutlinedIcon fontSize="inherit" />,
  proposal: <CallSplitIcon fontSize="inherit" />,
}

const DEBOUNCE_MS = 250

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
  const areas = useAreas()
  const work = useWork()
  const hits = useSearch(q, { type, status, area, project, since })
  const set = (patch: Params) =>
    void navigate({ search: (p) => ({ ...p, ...patch }), replace: true })

  // Results follow the typing: the URL takes the text a moment after the last key.
  const timer = useRef<number | undefined>(undefined)
  useEffect(() => () => window.clearTimeout(timer.current), [])
  const onText = (v: string) => {
    setText(v)
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => set({ q: v.trim() || undefined }), DEBOUNCE_MS)
  }

  const sinceOptions = SINCE.some((o) => o.value === since)
    ? SINCE
    : since
      ? [...SINCE, { value: since, label: since }]
      : SINCE
  return (
    <>
      <PageTitle>Search</PageTitle>
      <TextField
        id="search-input"
        inputRef={input}
        label="Search documents, summaries, and transcripts"
        type="search"
        value={text}
        onChange={(e) => onText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            window.clearTimeout(timer.current)
            set({ q: text.trim() || undefined })
          }
        }}
        autoFocus
        fullWidth
        sx={{ mb: 1.5 }}
        helperText="Results appear as you type. Press / to come back here."
      />
      <Stack direction="row" spacing={1} useFlexGap sx={{ mb: 2, flexWrap: 'wrap' }}>
        <TextField
          select
          size="small"
          label="Type"
          value={type ?? ''}
          onChange={(e) => set({ type: e.target.value || undefined })}
          sx={{ flex: '1 1 130px' }}
        >
          <MenuItem value="">Any</MenuItem>
          {TYPES.map((t) => (
            <MenuItem key={t} value={t}>
              {hitKindLabel(t)}
            </MenuItem>
          ))}
        </TextField>
        <TextField
          select
          size="small"
          label="Goal"
          value={project ?? ''}
          onChange={(e) => set({ project: e.target.value || undefined })}
          sx={{ flex: '1 1 130px' }}
        >
          <MenuItem value="">Any</MenuItem>
          {(work.data?.projects ?? []).map((p) => (
            <MenuItem key={p.id} value={p.id}>
              {p.title || nameOr('', p.id)}
            </MenuItem>
          ))}
        </TextField>
        <TextField
          select
          size="small"
          label="Area"
          value={area ?? ''}
          onChange={(e) => set({ area: e.target.value || undefined })}
          sx={{ flex: '1 1 130px' }}
        >
          <MenuItem value="">Any</MenuItem>
          {(areas.data ?? []).map((a) => (
            <MenuItem key={a.id} value={a.id}>
              {a.title || nameOr('', a.id)}
            </MenuItem>
          ))}
        </TextField>
        <TextField
          select
          size="small"
          label="Updated"
          value={since ?? ''}
          onChange={(e) => set({ since: e.target.value || undefined })}
          sx={{ flex: '1 1 130px' }}
        >
          <MenuItem value="">Any time</MenuItem>
          {sinceOptions.map((o) => (
            <MenuItem key={o.value} value={o.value}>
              {o.label}
            </MenuItem>
          ))}
        </TextField>
        <TextField
          size="small"
          label="Status"
          defaultValue={status ?? ''}
          key={status ?? ''}
          onBlur={(e) => {
            if ((e.target.value.trim() || undefined) !== status)
              set({ status: e.target.value.trim() || undefined })
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && e.target instanceof HTMLInputElement)
              set({ status: e.target.value.trim() || undefined })
          }}
          sx={{ flex: '1 1 130px' }}
        />
      </Stack>
      {q !== '' && (
        <SearchState q={hits} text={q} typing={text.trim() !== q}>
          {({ hits: list, degraded }) => (
            <>
              {degraded !== '' && (
                <Alert severity="info" sx={{ mb: 1 }}>
                  {degraded}
                </Alert>
              )}
              <Typography
                variant="body2"
                color="text.secondary"
                role="status"
                aria-live="polite"
                sx={{ mb: 1 }}
              >
                {list.length === 0
                  ? `No results for “${q}”.`
                  : `${list.length} ${list.length === 1 ? 'result' : 'results'} for “${q}”`}
              </Typography>
              {list.length === 0 ? (
                <Empty>Try fewer words, or clear the filters.</Empty>
              ) : (
                <List disablePadding>
                  {list.map((h) => (
                    <ListItem
                      key={`${h.id}-${h.match ?? ''}-${h.passage.slice(0, 20)}`}
                      disableGutters
                      divider
                      sx={{ display: 'block' }}
                    >
                      <Stack
                        direction="row"
                        spacing={0.75}
                        sx={{ alignItems: 'center', color: 'text.secondary', fontSize: 14 }}
                      >
                        <Box sx={{ display: 'flex' }} aria-hidden>
                          {ICON[h.kind] ?? <DescriptionOutlinedIcon fontSize="inherit" />}
                        </Box>
                        <Typography variant="caption">{hitKindLabel(h.kind)}</Typography>
                        {h.match === 'meaning' && <StatusChip tone="quiet" label="by meaning" />}
                        {h.unverified && <StatusChip tone="attn" label="unverified" />}
                      </Stack>
                      <Typography sx={{ fontWeight: 500, overflowWrap: 'anywhere' }}>
                        {h.kind === 'proposal' ? (
                          <TextLink to="/proposals/$id" params={{ id: h.id }} underline="hover">
                            {h.title || nameOr('', h.id)}
                          </TextLink>
                        ) : (
                          <DocLink id={h.id}>{h.title || nameOr('', h.id)}</DocLink>
                        )}
                      </Typography>
                      <Typography
                        variant="body2"
                        color="text.secondary"
                        sx={{ overflowWrap: 'anywhere' }}
                      >
                        {highlight(h.snippet || h.passage, h.marks).map((p, i) =>
                          p.mark ? (
                            <Box
                              component="mark"
                              key={i}
                              sx={{
                                bgcolor: 'rgba(255, 193, 7, 0.35)',
                                color: 'text.primary',
                                borderRadius: 0.5,
                                px: 0.25,
                              }}
                            >
                              {readable(p.text)}
                            </Box>
                          ) : (
                            <span key={i}>{readable(p.text)}</span>
                          ),
                        )}
                        {h.via && <> (text from {nameOr('', h.via)})</>}
                      </Typography>
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
        </SearchState>
      )}
    </>
  )
}
