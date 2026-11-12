import { useState } from 'react'
import { useRouteContext } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useSaveStateLine } from '@/api/projects'
import { errorText } from '@/client/gqlClient'
import { canDecide } from '@/client/session'

// A state line older than this is stale (health: project_state_stale).
export const STALE_DAYS = 14

// ageWords is how old a state line is, from the server's day count (-1: no date).
export function ageWords(days: number): string {
  if (days < 0) return 'no date'
  if (days === 0) return 'today'
  if (days === 1) return '1 day ago'
  return `${days} days ago`
}

// StateLine shows the state line and how old it is.
export function StateLine({ line, ageDays }: { line: string | null | undefined; ageDays: number }) {
  const text = (line ?? '').trim()
  const stale = text !== '' && ageDays > STALE_DAYS
  return (
    <Typography
      sx={{ fontSize: '1rem', overflowWrap: 'anywhere' }}
      color={text ? 'text.primary' : 'text.secondary'}
    >
      {text || 'No state line yet.'}
      {text !== '' && (
        <Typography
          component="span"
          color={stale ? 'warning.main' : 'text.secondary'}
          sx={{ fontSize: '1rem' }}
        >
          {' - '}
          {ageWords(ageDays)}
          {stale && ', stale'}
        </Typography>
      )}
    </Typography>
  )
}

// StateLineEditor is the project page's state line. With review:decide the owner edits it in place
// (edit, fields state_line only); the server dates it today.
export function StateLineEditor({
  id,
  line,
  ageDays,
}: {
  id: string
  line: string | null | undefined
  ageDays: number
}) {
  const { session } = useRouteContext({ from: '/_authed' })
  const decide = canDecide(session)
  const save = useSaveStateLine(id)
  const [draft, setDraft] = useState<string | null>(null)
  const current = (line ?? '').trim()
  if (draft === null) {
    return (
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }} data-state-line={id}>
        <StateLine line={line} ageDays={ageDays} />
        {decide && (
          <Button
            size="small"
            variant="outlined"
            onClick={() => {
              save.reset()
              setDraft(current)
            }}
            data-state-act="edit"
          >
            Edit
          </Button>
        )}
      </Stack>
    )
  }
  const text = draft.trim()
  const submit = async () => {
    try {
      await save.mutateAsync(text)
      setDraft(null)
    } catch {
      // The error shows below; the draft is kept.
    }
  }
  return (
    <Stack spacing={1} data-state-line={id}>
      <TextField
        label="State line: where the project stands"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && text && text !== current) void submit()
          if (e.key === 'Escape') setDraft(null)
        }}
        autoFocus
        fullWidth
      />
      {save.isError && <Alert severity="error">{errorText(save.error)}</Alert>}
      <Stack direction="row" spacing={1}>
        <Button
          size="small"
          variant="contained"
          onClick={() => void submit()}
          disabled={save.isPending || text === '' || text === current}
          data-state-act="save"
        >
          Save state line
        </Button>
        <Button size="small" onClick={() => setDraft(null)} data-state-act="cancel">
          Cancel
        </Button>
      </Stack>
    </Stack>
  )
}
