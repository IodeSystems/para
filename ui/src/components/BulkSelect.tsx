import { createContext, useContext, useState, type ReactNode } from 'react'
import { useRouteContext } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Paper from '@mui/material/Paper'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { bulk } from '@/api/bulk'
import { canDecide } from '@/client/session'
import { errorText } from '@/client/gqlClient'
import { LaterButton } from '@/components/stack/LaterButton'
import { useToasts } from '@/components/ToastStack'
import {
  allState,
  bulkBody,
  doneText,
  keepShown,
  mustReason,
  previewText,
  refusals,
  toggle,
  toggleAll,
  type BulkAction,
  type BulkBody,
  type BulkResult,
} from '@/lib/bulk'

// Select mode on a list the owner works through: checkboxes on the rows, "N selected", and a bar
// with Done, Drop, Later, and Reopen for all of them at once. The bar's box is the reason, as an
// item's composer is for its own buttons. Each action first runs dry and shows what would change
// and what is refused; Apply runs it, and the toast's Undo takes the whole batch back. A session
// that may not decide (no review:decide) sees no select mode.

type Sel = { sel: ReadonlySet<string>; flip: (id: string) => void }

const SelCtx = createContext<Sel | null>(null)

// SelectBox is a row's checkbox; it shows only in select mode.
export function SelectBox({ id, title }: { id: string; title?: string | null }) {
  const s = useContext(SelCtx)
  if (!s) return null
  return (
    <Checkbox
      size="small"
      sx={{ p: 0.5, alignSelf: 'center' }}
      checked={s.sel.has(id)}
      onChange={() => s.flip(id)}
      slotProps={{ input: { 'aria-label': `Select ${title || id}` } }}
      data-select={id}
    />
  )
}

type Pending = { action: BulkAction; body: BulkBody; preview: BulkResult }

// BulkList wraps a list whose rows carry SelectBox. ids are the rows it shows, in order.
export function BulkList({ ids: rows, children }: { ids: readonly string[]; children: ReactNode }) {
  // A dated agenda can show one item on two days: it is one choice.
  const ids = [...new Set(rows)]
  const { session } = useRouteContext({ from: '/_authed' })
  const toasts = useToasts()
  const qc = useQueryClient()
  const [on, setOn] = useState(false)
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [reason, setReason] = useState('')
  const [busy, setBusy] = useState(false)
  const [pending, setPending] = useState<Pending | null>(null)
  const [error, setError] = useState('')
  if (!canDecide(session) || ids.length === 0) return <>{children}</>
  const sel = keepShown(picked, ids)
  const chosen = ids.filter((id) => sel.has(id))
  const all = allState(sel, ids)

  const stop = () => {
    setOn(false)
    setPicked(new Set())
    setReason('')
    setPending(null)
    setError('')
  }

  // preview runs the action dry and shows what it found before anything is written.
  const preview = async (action: BulkAction, until = '') => {
    const body = bulkBody(action, chosen, reason, until)
    setBusy(true)
    setError('')
    try {
      setPending({ action, body, preview: await bulk({ ...body, dryRun: true }) })
    } catch (e) {
      setError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  const apply = async () => {
    if (!pending) return
    setBusy(true)
    try {
      const r = await bulk(pending.body)
      const batch = r.batch
      toasts.push({
        text: doneText(pending.action, r),
        undo: batch
          ? async () => {
              await bulk({ verb: 'undo', batch })
              await qc.invalidateQueries()
            }
          : undefined,
        note: batch ? 'Undo takes back every item of this batch.' : 'Nothing changed.',
      })
      stop()
      await qc.invalidateQueries()
    } catch (e) {
      setError(errorText(e))
      setPending(null)
    } finally {
      setBusy(false)
    }
  }

  if (!on)
    return (
      <>
        <Stack direction="row" sx={{ justifyContent: 'flex-end' }}>
          <Button size="small" color="inherit" data-act="select" onClick={() => setOn(true)}>
            Select
          </Button>
        </Stack>
        {children}
      </>
    )

  const none = chosen.length === 0
  const refused = pending ? refusals(pending.preview) : []
  return (
    <SelCtx.Provider value={{ sel, flip: (id) => setPicked(toggle(sel, id)) }}>
      <Paper
        variant="outlined"
        sx={{ p: 1, mb: 1, position: 'sticky', top: 0, zIndex: 2 }}
        data-bulk-bar
      >
        <Stack direction="row" useFlexGap sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 0.5 }}>
          <Checkbox
            size="small"
            sx={{ p: 0.5 }}
            checked={all === 'all'}
            indeterminate={all === 'some'}
            onChange={() => setPicked(toggleAll(sel, ids))}
            slotProps={{ input: { 'aria-label': 'Select all' } }}
          />
          <Typography variant="body2" sx={{ mr: 1 }} data-selected={chosen.length}>
            {chosen.length} selected
          </Typography>
          <Button
            size="small"
            data-act="bulk-done"
            disabled={busy || none}
            onClick={() => void preview('done')}
          >
            Done
          </Button>
          <Button
            size="small"
            data-act="bulk-drop"
            disabled={busy || none || !reason.trim()}
            title={reason.trim() ? undefined : 'Say why in the box first'}
            onClick={() => void preview('drop')}
          >
            Drop
          </Button>
          <LaterButton disabled={busy || none} onLater={(u) => void preview('later', u)} />
          <Button
            size="small"
            data-act="bulk-reopen"
            disabled={busy || none}
            onClick={() => void preview('reopen')}
          >
            Reopen
          </Button>
          <Button size="small" color="inherit" data-act="select-cancel" onClick={stop}>
            Cancel
          </Button>
        </Stack>
        <TextField
          size="small"
          fullWidth
          sx={{ mt: 1 }}
          label="Reason for all of them (a drop needs one)"
          value={reason}
          onChange={(e) => setReason(e.target.value)}
        />
        {error && (
          <Alert severity="error" sx={{ mt: 1 }} onClose={() => setError('')}>
            {error}
          </Alert>
        )}
      </Paper>
      {children}
      <Dialog open={!!pending} onClose={() => !busy && setPending(null)} fullWidth maxWidth="sm">
        <DialogTitle>Check before you apply</DialogTitle>
        <DialogContent>
          {pending && (
            <>
              <Typography variant="body2" data-bulk-preview>
                {previewText(pending.action, pending.preview)}
              </Typography>
              {refused.length > 0 && (
                <List dense disablePadding>
                  {refused.map((x) => (
                    <ListItem key={x.id} disableGutters data-refused={x.id}>
                      <ListItemText
                        primary={x.title}
                        secondary={x.why}
                        slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
                      />
                    </ListItem>
                  ))}
                </List>
              )}
              {mustReason(pending.action) && (
                <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
                  Reason: {reason.trim()}
                </Typography>
              )}
            </>
          )}
        </DialogContent>
        <DialogActions>
          <Button color="inherit" disabled={busy} onClick={() => setPending(null)}>
            Back
          </Button>
          <Button
            variant="contained"
            data-act="bulk-apply"
            disabled={busy || !pending || pending.preview.applied === 0}
            onClick={() => void apply()}
          >
            Apply
          </Button>
        </DialogActions>
      </Dialog>
    </SelCtx.Provider>
  )
}
