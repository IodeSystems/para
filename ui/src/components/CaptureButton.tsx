import { useState } from 'react'
import Alert from '@mui/material/Alert'
import Autocomplete from '@mui/material/Autocomplete'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import FormControlLabel from '@mui/material/FormControlLabel'
import MenuItem from '@mui/material/MenuItem'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import ToggleButton from '@mui/material/ToggleButton'
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup'
import Typography from '@mui/material/Typography'
import { useNavigate } from '@tanstack/react-router'
import { useCapture } from '@/api/context'
import { useAreas } from '@/api/areas'
import { usePeople } from '@/api/people'
import { errorText } from '@/client/gqlClient'
import { useToasts } from '@/components/ToastStack'
import {
  captureCall,
  captureReady,
  fileTaskCall,
  firstLine,
  type Capture,
  type CaptureKind,
} from '@/lib/context'

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
  task: { on: false, title: '', due: '', area: '' },
})

// CaptureButton logs what reached the owner outside a connected source: a conversation they had in
// person or by voice (verbal), or something they carried over by hand from another system (swivel).
// A conversation with people in the directory is a logged contact; anything else is an incoming
// item in the inbox, to file into a task like any other. "Make a task of it" files it into a new
// task at once, so the task's Context shows where it came from.
export function CaptureButton() {
  const [open, setOpen] = useState(false)
  const [c, setC] = useState<Capture>(empty('verbal'))
  const capture = useCapture()
  const people = usePeople()
  const areas = useAreas()
  const toasts = useToasts()
  const navigate = useNavigate()
  const options: Who[] = (people.data ?? []).map((p) => ({ id: p.id, name: p.title || p.id }))
  const set = <K extends keyof Capture>(k: K, v: Capture[K]) => setC((x) => ({ ...x, [k]: v }))
  const close = () => {
    setOpen(false)
    capture.reset()
  }
  const setTask = (patch: Partial<NonNullable<Capture['task']>>) =>
    setC((x) => ({ ...x, task: { on: false, title: '', due: '', area: '', ...x.task, ...patch } }))
  const submit = () => {
    const call = captureCall(c)
    const task = fileTaskCall(c)
    capture.mutate(
      { call, task },
      {
        onSuccess: (out) => {
          setOpen(false)
          setC(empty(c.kind))
          if (out.became) {
            toasts.push({
              text: `Logged and made a task: ${c.task?.title.trim() || firstLine(c.summary)}`,
              note: 'It shows where it came from. This cannot be undone here.',
            })
          }
          void navigate(
            out.became
              ? { to: '/docs/$id', params: { id: out.became } }
              : call.op === 'io_add'
                ? { to: '/io/$id', params: { id: out.id } }
                : { to: '/docs/$id', params: { id: out.id } },
          )
        },
      },
    )
  }
  return (
    <>
      <Button size="small" variant="outlined" onClick={() => setOpen(true)} data-capture>
        Log a conversation or hand-carried item
      </Button>
      <Dialog open={open} onClose={close} fullWidth maxWidth="sm">
        <DialogTitle>Log it</DialogTitle>
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
              minRows={3}
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
            <FormControlLabel
              control={
                <Checkbox
                  checked={!!c.task?.on}
                  onChange={(e) => setTask({ on: e.target.checked })}
                  slotProps={{ input: { 'data-capture-task': '' } as object }}
                />
              }
              label="Make a task of it"
            />
            {c.task?.on && (
              <Stack spacing={2} data-capture-task-fields>
                <TextField
                  label="Task title"
                  placeholder={firstLine(c.summary) || 'the first line of the summary'}
                  value={c.task.title}
                  onChange={(e) => setTask({ title: e.target.value })}
                  slotProps={{ htmlInput: { 'data-field': 'task-title' } }}
                />
                <TextField
                  select
                  required
                  label="Area"
                  value={c.task.area}
                  onChange={(e) => setTask({ area: e.target.value })}
                  helperText={areas.isError ? errorText(areas.error) : undefined}
                  slotProps={{ htmlInput: { 'data-field': 'task-area' } }}
                >
                  {(areas.data ?? []).map((a) => (
                    <MenuItem key={a.id} value={a.id}>
                      {a.title || a.id}
                    </MenuItem>
                  ))}
                </TextField>
                <TextField
                  label="Due (optional)"
                  type="date"
                  value={c.task.due}
                  onChange={(e) => setTask({ due: e.target.value })}
                  slotProps={{
                    inputLabel: { shrink: true },
                    htmlInput: { 'data-field': 'task-due' },
                  }}
                />
              </Stack>
            )}
            <Typography variant="body2" color="text.secondary">
              {c.task?.on
                ? 'Logged, then filed into a new task. The task shows where it came from.'
                : c.kind === 'verbal' && c.with.length > 0 && c.with.every((w) => w.id)
                  ? 'Logged as a contact with these people.'
                  : 'Goes to your inbox: file it into a task or drop it there.'}
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
            Save
          </Button>
        </DialogActions>
      </Dialog>
    </>
  )
}
