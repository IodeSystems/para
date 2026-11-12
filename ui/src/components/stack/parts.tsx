import { useState } from 'react'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { diffKind, splitDiff } from '@/lib/diff'

// Pieces shared by the diff view and the Mark Done form.

export const mono = { fontFamily: 'ui-monospace, monospace', fontSize: 13 } as const
export const block = {
  ...mono,
  my: 1,
  p: 1,
  bgcolor: 'action.hover',
  borderRadius: 1,
  whiteSpace: 'pre-wrap',
  overflowWrap: 'anywhere',
} as const

// DiffBlock draws diff lines, one per row, monospace: added green, removed red.
export function DiffBlock({ lines, label }: { lines: string[]; label?: string }) {
  return (
    <Box role="group" aria-label={label} sx={{ ...mono, m: 0, overflowX: 'auto' }}>
      {splitDiff(lines).map((l, n) => {
        const k = diffKind(l)
        return (
          <Box
            key={n}
            data-diff={k}
            sx={{
              whiteSpace: 'pre-wrap',
              overflowWrap: 'anywhere',
              minHeight: '1.4em',
              color: k === 'add' ? 'success.main' : k === 'del' ? 'error.main' : 'text.secondary',
            }}
          >
            {l}
          </Box>
        )
      })}
    </Box>
  )
}

export type DoneFields = { url: string; locator: string; text: string; note: string }

// DoneForm is the inline form of Mark Done: where the result is, the final text, and a note. It
// is part of the page, not a browser dialog. Confirm takes effect at once.
export function DoneForm({
  start,
  textLabel,
  withNote,
  busy,
  error,
  onConfirm,
  onCancel,
}: {
  start: string
  textLabel: string
  withNote: boolean
  busy: boolean
  error?: string
  onConfirm: (f: DoneFields) => void
  onCancel: () => void
}) {
  const [f, setF] = useState<DoneFields>({ url: '', locator: '', text: start, note: '' })
  const set = (k: keyof DoneFields) => (e: { target: { value: string } }) =>
    setF((x) => ({ ...x, [k]: e.target.value }))
  return (
    <Box
      data-done-form
      role="group"
      aria-label="Mark Done"
      sx={{ my: 1, p: 1.5, border: 1, borderColor: 'divider', borderRadius: 1 }}
    >
      <Typography variant="body2" sx={{ mb: 1 }}>
        You did this yourself. Para marks it done, and no agent does it again.
      </Typography>
      <Stack spacing={1}>
        <TextField
          size="small"
          autoFocus
          label="Link to the result (optional)"
          value={f.url}
          onChange={set('url')}
        />
        <TextField
          size="small"
          label="Where it is, with no link (optional)"
          value={f.locator}
          onChange={set('locator')}
        />
        <TextField
          size="small"
          multiline
          minRows={2}
          label={textLabel}
          value={f.text}
          onChange={set('text')}
        />
        {withNote && (
          <TextField size="small" label="Note (optional)" value={f.note} onChange={set('note')} />
        )}
      </Stack>
      {error && (
        <Alert severity="error" sx={{ mt: 1 }}>
          {error}
        </Alert>
      )}
      <Stack direction="row" spacing={1} sx={{ mt: 1 }}>
        <Button size="small" variant="contained" disabled={busy} onClick={() => onConfirm(f)}>
          Confirm done
        </Button>
        <Button size="small" onClick={onCancel}>
          Cancel
        </Button>
      </Stack>
    </Box>
  )
}
