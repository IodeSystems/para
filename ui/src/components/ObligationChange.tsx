import { useState } from 'react'
import { useRouteContext } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import FormControlLabel from '@mui/material/FormControlLabel'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { obligationChange } from '@/api/obligations'
import { revertReceipt } from '@/api/receipts'
import { errorText } from '@/client/gqlClient'
import { canDecide } from '@/client/session'
import { useToasts } from '@/components/ToastStack'
import { useThemeReview } from '@/components/ThemeReview'
import {
  ACTION_WORDS,
  changeBody,
  tellWords,
  type ChangeAction,
  type ChangeForm,
  type Party,
} from '@/lib/obligations'

type Target = {
  id: string
  title: string
  meeting: boolean
  // parties are who a change tells, with the medium; undefined when not loaded (a list row).
  parties?: readonly Party[]
  calendar?: boolean
}

const blank = (action: ChangeAction, meeting: boolean): ChangeForm => ({
  action,
  meeting,
  due: '',
  start: '',
  end: '',
  to: '',
  outcome: '',
  reason: '',
  because: '',
  notify: true,
})

// ChangeButtons are Push out, Redirect, and Cancel on an open obligation, for the owner. Each opens
// a small dialog: the new date or time, who has it now, or how it ends; a reason (required: the
// facts on the ground, or the item with a higher priority); and Tell them, on by default, listing
// who is told. The notices are drafts the owner approves. The toast's Undo takes the change and
// its drafts back.
export function ChangeButtons({ x, size = 'small' }: { x: Target; size?: 'small' | 'medium' }) {
  const { session } = useRouteContext({ from: '/_authed' })
  const [form, setForm] = useState<ChangeForm | null>(null)
  if (!canDecide(session)) return null
  return (
    <>
      <Stack direction="row" spacing={0.5} data-change-buttons={x.id}>
        {(Object.keys(ACTION_WORDS) as ChangeAction[]).map((a) => (
          <Button key={a} size={size} data-change={a} onClick={() => setForm(blank(a, x.meeting))}>
            {ACTION_WORDS[a]}
          </Button>
        ))}
      </Stack>
      {form && <ChangeDialog x={x} form={form} setForm={setForm} />}
    </>
  )
}

function ChangeDialog({
  x,
  form,
  setForm,
}: {
  x: Target
  form: ChangeForm
  setForm: (f: ChangeForm | null) => void
}) {
  const toasts = useToasts()
  const qc = useQueryClient()
  const review = useThemeReview()
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const set = (p: Partial<ChangeForm>) => setForm({ ...form, ...p })
  const name = x.title || x.id
  const submit = async () => {
    const { body, error } = changeBody(x.id, form)
    if (!body) {
      setErr(error ?? '')
      return
    }
    setBusy(true)
    setErr('')
    try {
      // A cancel of an obligation that open items hang on opens the review: Confirm cancels it
      // with the owner's drops and moves as one batch.
      if (
        form.action === 'cancel' &&
        (await review({ theme: x.id, title: name, end: { kind: 'change', change: body } }))
      ) {
        setForm(null)
        return
      }
      const out = await obligationChange(body)
      setForm(null)
      await qc.invalidateQueries()
      const told = out.notices.length
      toasts.push({
        text: `${ACTION_WORDS[form.action]}: ${name}${told > 0 ? `; ${told} notice${told === 1 ? '' : 's'} to approve` : ''}`,
        undo: out.receipt
          ? async () => {
              await revertReceipt(out.receipt ?? '')
              await qc.invalidateQueries()
            }
          : undefined,
        note: out.receipt ? undefined : 'This change left no receipt to undo.',
      })
    } catch (e) {
      setErr(errorText(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog
      open
      onClose={() => setForm(null)}
      fullWidth
      maxWidth="xs"
      data-change-dialog={form.action}
    >
      <DialogTitle>
        {ACTION_WORDS[form.action]}: {name}
      </DialogTitle>
      <DialogContent onKeyDown={(e) => e.stopPropagation()}>
        <Stack spacing={1.5} sx={{ pt: 1 }}>
          {form.action === 'push' && !form.meeting && (
            <TextField
              type="date"
              label="New date"
              size="small"
              value={form.due}
              onChange={(e) => set({ due: e.target.value })}
              slotProps={{ inputLabel: { shrink: true } }}
            />
          )}
          {form.action === 'push' && form.meeting && (
            <>
              <TextField
                type="datetime-local"
                label="New start"
                size="small"
                value={form.start}
                onChange={(e) => set({ start: e.target.value })}
                slotProps={{ inputLabel: { shrink: true } }}
              />
              <TextField
                type="datetime-local"
                label="New end (optional)"
                size="small"
                value={form.end}
                onChange={(e) => set({ end: e.target.value })}
                slotProps={{ inputLabel: { shrink: true } }}
              />
            </>
          )}
          {form.action === 'redirect' && (
            <TextField
              label="Who has it now, or where it went"
              helperText="A person id, agent:<name>, an item id, or a URL"
              size="small"
              value={form.to}
              onChange={(e) => set({ to: e.target.value })}
            />
          )}
          {form.action === 'cancel' && (
            <TextField
              select
              label="How it ends"
              size="small"
              value={form.outcome}
              onChange={(e) => set({ outcome: e.target.value as ChangeForm['outcome'] })}
            >
              <MenuItem value="">{x.meeting ? 'Cancelled' : 'Declined: I say no'}</MenuItem>
              {!x.meeting && <MenuItem value="dropped">Dropped: nobody needs it now</MenuItem>}
            </TextField>
          )}
          <TextField
            multiline
            minRows={2}
            autoFocus
            label="Why (the facts on the ground)"
            size="small"
            value={form.reason}
            onChange={(e) => set({ reason: e.target.value })}
          />
          <TextField
            label="Or: the item with a higher priority (id)"
            size="small"
            value={form.because}
            onChange={(e) => set({ because: e.target.value })}
          />
          <FormControlLabel
            control={
              <Checkbox
                checked={form.notify}
                onChange={(e) => set({ notify: e.target.checked })}
                data-tell-them
              />
            }
            label="Tell them"
          />
          {form.notify && (
            <Typography variant="caption" color="text.secondary" data-tell-who>
              {x.parties === undefined
                ? 'A draft goes to each person it is owed to, for you to approve.'
                : x.parties.length === 0
                  ? 'Nobody is named on it: no notice goes out.'
                  : `Drafts for you to approve: ${tellWords(x.parties)}${x.calendar && form.action !== 'redirect' ? '; and the calendar change' : ''}.`}
            </Typography>
          )}
          {err && (
            <Typography variant="body2" color="error">
              {err}
            </Typography>
          )}
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={() => setForm(null)}>Back</Button>
        <Button
          variant="contained"
          disabled={busy}
          onClick={() => void submit()}
          data-change-submit
        >
          {ACTION_WORDS[form.action]}
        </Button>
      </DialogActions>
    </Dialog>
  )
}
