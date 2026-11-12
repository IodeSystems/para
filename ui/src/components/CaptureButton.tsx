import { useEffect, useState } from 'react'
import Alert from '@mui/material/Alert'
import Autocomplete from '@mui/material/Autocomplete'
import Button from '@mui/material/Button'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import ToggleButton from '@mui/material/ToggleButton'
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import CampaignOutlinedIcon from '@mui/icons-material/CampaignOutlined'
import { useNavigate } from '@tanstack/react-router'
import { useCapture } from '@/api/context'
import { usePeople } from '@/api/people'
import { errorText } from '@/client/gqlClient'
import { useToasts } from '@/components/ToastStack'
import {
  captureCall,
  captureReady,
  type Capture,
  type CaptureKind,
} from '@/lib/context'
import { tellToast, TELL_OPEN } from '@/lib/tell'
import { requestEditTurn } from '@/lib/threadEdit'
import { withdrawTurn } from '@/api/thread'

type Who = { id?: string; name: string }

function today(): string {
  const d = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

const empty = (kind: CaptureKind): Capture => ({
  kind,
  with: [],
  from: '',
  date: today(),
  summary: '',
  url: '',
})

// TellAgent is the app bar button, and the t key, that open the capture dialog from any page. The
// owner says what happened; it is saved as an io item first, then the agent decides what it is.
// A read-only key writes nothing: it gets no button and the t key does nothing.
export function TellAgent({ decide }: { decide: boolean }) {
  const [open, setOpen] = useState(false)
  useEffect(() => {
    const on = () => setOpen(true)
    window.addEventListener(TELL_OPEN, on)
    return () => window.removeEventListener(TELL_OPEN, on)
  }, [])
  if (!decide) return null
  return (
    <>
      <Tooltip title="Capture what happened; your agent decides what it is (t)">
        <Button
          color="inherit"
          size="small"
          startIcon={<CampaignOutlinedIcon />}
          onClick={() => setOpen(true)}
          data-tell-agent
          sx={{ flexShrink: 0, whiteSpace: 'nowrap', minWidth: 0 }}
        >
          Capture
        </Button>
      </Tooltip>
      <CaptureDialog open={open} onClose={() => setOpen(false)} />
    </>
  )
}

// CaptureDialog: Send saves the words as an io item, then posts a thread turn for the agent.
export function CaptureDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [c, setC] = useState<Capture>(empty('verbal'))
  const capture = useCapture()
  const people = usePeople()
  const toasts = useToasts()
  const navigate = useNavigate()
  const options: Who[] = (people.data ?? []).map((p) => ({ id: p.id, name: p.title || p.id }))
  const set = <K extends keyof Capture>(k: K, v: Capture[K]) => setC((x) => ({ ...x, [k]: v }))
  const close = () => {
    onClose()
    capture.reset()
  }
  const submit = () => {
    const call = captureCall(c)
    capture.mutate(
      { call, tell: c.summary },
      {
        onSuccess: (out) => {
          onClose()
          setC(empty(c.kind))
          if (out.told) {
            const request = out.told.request
            toasts.push(
              tellToast(out.id, out.told, {
                // Undo withdraws the turn (the saved words stay as an io item); Edit opens the
                // turn's words in the thread of that item.
                undo: request ? async () => void (await withdrawTurn(request)) : undefined,
                edit: request
                  ? () => {
                      void navigate({ to: '/io/$id', params: { id: out.id } })
                      requestEditTurn(request)
                    }
                  : undefined,
              }),
            )
          } else
            toasts.push({
              text: `Saved, but your agent was not told: ${out.tellError ?? 'unknown error'}`,
              tone: 'error',
              link: { label: 'Open the item', io: out.id },
              target: { id: out.id, kind: 'io' },
              dwell: 15000,
            })
          void navigate({ to: '/io/$id', params: { id: out.id } })
        },
      },
    )
  }
  return (
    <>
      <Dialog open={open} onClose={close} fullWidth maxWidth="sm">
        <DialogTitle>Capture</DialogTitle>
        <DialogContent>
          <Stack spacing={2} sx={{ pt: 1 }}>
            <ToggleButtonGroup
              exclusive
              size="small"
              value={c.kind}
              onChange={(_, v: CaptureKind | null) => v && set('kind', v)}
              aria-label="What"
            >
              <ToggleButton value="verbal" data-capture-kind="verbal">
                Conversation
              </ToggleButton>
              <ToggleButton value="swivel" data-capture-kind="swivel">
                Carried over by hand
              </ToggleButton>
            </ToggleButtonGroup>
            {c.kind === 'verbal' ? (
              <Autocomplete<Who, true, false, true>
                multiple
                freeSolo
                options={options}
                value={c.with}
                getOptionLabel={(o) => (typeof o === 'string' ? o : o.name)}
                isOptionEqualToValue={(a, b) =>
                  typeof b !== 'string' && a.id === b.id && a.name === b.name
                }
                onChange={(_, v) =>
                  set(
                    'with',
                    v.map((x) => (typeof x === 'string' ? { name: x } : x)),
                  )
                }
                renderInput={(p) => (
                  <TextField {...p} label="With" placeholder="a person, or a name" />
                )}
              />
            ) : (
              <TextField
                label="From"
                required
                placeholder="the system it came from, or its URL"
                value={c.from}
                onChange={(e) => set('from', e.target.value)}
                slotProps={{ htmlInput: { 'data-field': 'from' } }}
              />
            )}
            <TextField
              label="Date"
              type="date"
              value={c.date}
              onChange={(e) => set('date', e.target.value)}
              slotProps={{ inputLabel: { shrink: true }, htmlInput: { 'data-field': 'date' } }}
            />
            <TextField
              label="Summary"
              required
              multiline
              minRows={8}
              maxRows={24}
              placeholder="what was said or what you carried over, in your words"
              value={c.summary}
              onChange={(e) => set('summary', e.target.value)}
              slotProps={{ htmlInput: { 'data-field': 'summary' } }}
            />
            <TextField
              label="Link"
              placeholder="optional: where the original is"
              value={c.url}
              onChange={(e) => set('url', e.target.value)}
              slotProps={{ htmlInput: { 'data-field': 'url' } }}
            />
            <Typography variant="body2" color="text.secondary">
              Saved as an incoming item in your words first, then your agent decides what it is. It
              may ask you in the thread.
            </Typography>
            {capture.isError && <Alert severity="error">{errorText(capture.error)}</Alert>}
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={close}>Cancel</Button>
          <Button
            variant="contained"
            disabled={!captureReady(c) || capture.isPending}
            onClick={submit}
            data-capture-save
          >
            Send
          </Button>
        </DialogActions>
      </Dialog>
    </>
  )
}
