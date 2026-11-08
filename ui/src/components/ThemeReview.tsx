import { createContext, useContext, useState, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import ToggleButton from '@mui/material/ToggleButton'
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup'
import Typography from '@mui/material/Typography'
import useMediaQuery from '@mui/material/useMediaQuery'
import type { Theme } from '@mui/material/styles'
import { useAreas } from '@/api/areas'
import { bulk } from '@/api/bulk'
import { themeClose, themeItems, type ThemeItemRow } from '@/api/theme'
import { errorText } from '@/client/gqlClient'
import { DocLink } from '@/components/DocLink'
import { PartOfLinks } from '@/components/PartOf'
import { useToasts } from '@/components/ToastStack'
import {
  confirmText,
  left,
  mark,
  markRest,
  reviewDoneText,
  themeCloseBody,
  type Marks,
  type ThemeEnd,
} from '@/lib/theme'

// ThemeReview: when the owner closes a goal, an obligation, or a question that open items hang on,
// the close does not run at once. A list opens instead: each item Drop or Keep (Keep may also move
// it to an area), with Drop the rest and Keep the rest. Nothing changes until Confirm, which closes
// the theme and applies the drops and moves as one batch (theme_close); the toast's Undo takes the
// whole batch back, the theme's close too.

type Review = {
  theme: string
  title: string
  end: ThemeEnd
  items: ThemeItemRow[]
  onDone?: () => void
}

type Start = (r: Omit<Review, 'items'>) => Promise<boolean>

const Ctx = createContext<Start | null>(null)

// useThemeReview returns start: it reads what hangs on the theme and, when anything does, opens
// the review and resolves true (the caller must not close the theme itself). It resolves false
// when nothing hangs on it, or outside the provider.
export function useThemeReview(): Start {
  const s = useContext(Ctx)
  return s ?? (() => Promise.resolve(false))
}

export function ThemeReviewProvider({ children }: { children: ReactNode }) {
  const [review, setReview] = useState<Review | null>(null)
  const start: Start = async (r) => {
    // A failed read does not stop the close: it runs as it did before the review.
    const t = await themeItems(r.theme).catch(() => null)
    if (!t || t.items.length === 0) return false
    setReview({ ...r, title: r.title || t.title, items: t.items })
    return true
  }
  return (
    <Ctx.Provider value={start}>
      {children}
      {review && <ReviewDialog r={review} onClose={() => setReview(null)} />}
    </Ctx.Provider>
  )
}

function ReviewDialog({ r, onClose }: { r: Review; onClose: () => void }) {
  const toasts = useToasts()
  const qc = useQueryClient()
  const areas = useAreas()
  const phone = useMediaQuery((t: Theme) => t.breakpoints.down('sm'))
  const [marks, setMarks] = useState<Marks>(new Map())
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const ids = r.items.map((x) => x.id)
  const rest = left(marks, ids)
  const closing = r.end.kind !== 'closed'

  const confirm = async () => {
    setBusy(true)
    setErr('')
    try {
      const out = await themeClose(themeCloseBody(r.theme, r.end, marks, ids))
      const batch = out.batch
      toasts.push({
        text: reviewDoneText(r.title, !!out.closed, out),
        undo: batch
          ? async () => {
              await bulk({ verb: 'undo', batch })
              await qc.invalidateQueries()
            }
          : undefined,
        note: batch ? 'Undo takes back the close and every drop and move.' : 'Nothing changed.',
      })
      onClose()
      r.onDone?.()
      await qc.invalidateQueries()
    } catch (e) {
      setErr(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open onClose={() => !busy && onClose()} fullWidth maxWidth="sm" fullScreen={phone}>
      <DialogTitle>
        {closing ? `Before you close ${r.title}` : `What hangs on ${r.title}`}
      </DialogTitle>
      <DialogContent dividers data-theme-review={r.theme}>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
          {r.items.length === 1 ? 'One open item hangs' : `${r.items.length} open items hang`} on
          it. Mark each Drop or Keep; nothing changes until Confirm.
        </Typography>
        <Stack direction="row" spacing={1} sx={{ mb: 1 }}>
          <Button
            size="small"
            data-act="drop-rest"
            disabled={rest.length === 0}
            onClick={() => setMarks(markRest(marks, ids, 'drop'))}
          >
            Drop the rest
          </Button>
          <Button
            size="small"
            data-act="keep-rest"
            disabled={rest.length === 0}
            onClick={() => setMarks(markRest(marks, ids, 'keep'))}
          >
            Keep the rest
          </Button>
        </Stack>
        <List dense disablePadding>
          {r.items.map((x) => {
            const m = marks.get(x.id)
            return (
              <ListItem
                key={x.id}
                divider
                disableGutters
                sx={{ display: 'block' }}
                data-review={x.id}
              >
                <ListItemText
                  primary={<DocLink id={x.id}>{x.title || x.id}</DocLink>}
                  secondary={
                    <>
                      {x.whyText}
                      <br />
                      <PartOfLinks parts={x.partOf} omit={r.theme} />
                    </>
                  }
                  slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
                />
                <Stack
                  direction="row"
                  useFlexGap
                  sx={{ flexWrap: 'wrap', gap: 1, alignItems: 'center', mt: 0.5 }}
                >
                  <ToggleButtonGroup
                    size="small"
                    exclusive
                    value={m?.mark ?? null}
                    onChange={(_e, v: 'drop' | 'keep' | null) => {
                      if (v) setMarks(mark(marks, x.id, { mark: v }))
                    }}
                  >
                    <ToggleButton value="drop" data-mark="drop">
                      Drop
                    </ToggleButton>
                    <ToggleButton value="keep" data-mark="keep">
                      Keep
                    </ToggleButton>
                  </ToggleButtonGroup>
                  {m?.mark === 'keep' &&
                    (x.type === 'task' || x.type === 'obligation' || x.type === 'rfx') && (
                      <TextField
                        select
                        size="small"
                        label="Move to area"
                        sx={{ minWidth: 160 }}
                        value={m.area ?? ''}
                        onChange={(e) =>
                          setMarks(
                            mark(marks, x.id, { mark: 'keep', area: e.target.value || undefined }),
                          )
                        }
                      >
                        <MenuItem value="">Leave where it is</MenuItem>
                        {(areas.data ?? []).map((a) => (
                          <MenuItem key={a.id} value={a.id}>
                            {a.title || a.id}
                          </MenuItem>
                        ))}
                      </TextField>
                    )}
                </Stack>
              </ListItem>
            )
          })}
        </List>
        {err && (
          <Alert severity="error" sx={{ mt: 1 }} onClose={() => setErr('')}>
            {err}
          </Alert>
        )}
      </DialogContent>
      <DialogActions sx={{ flexWrap: 'wrap', gap: 1 }}>
        <Typography variant="body2" sx={{ mr: 'auto', pl: 1 }} data-review-left={rest.length}>
          {confirmText(marks, ids)}
        </Typography>
        <Button color="inherit" disabled={busy} onClick={onClose}>
          {closing ? 'Cancel' : 'Later'}
        </Button>
        <Button
          variant="contained"
          data-act="review-confirm"
          disabled={busy || rest.length > 0}
          onClick={() => void confirm()}
        >
          Confirm
        </Button>
      </DialogActions>
    </Dialog>
  )
}
