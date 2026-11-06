import { useState } from 'react'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useNavigate } from '@tanstack/react-router'
import { useAddRecurring } from '@/api/meetings'
import { Section } from '@/components/Bits'
import { DocLink } from '@/components/DocLink'
import type { Para_ProduceInputMedium } from '@/gql/graphql'
import {
  artifactState,
  KIND_WORD,
  templateId,
  type ProduceInfo,
  type ProduceKind,
  type RecurringInput,
} from '@/lib/recurring'

// ProduceSection shows who owes the artifact, its template, and the last one made with its state.
export function ProduceSection({ id, p }: { id: string; p: ProduceInfo }) {
  const tpl = templateId(p.template)
  const a = p.artifact
  return (
    <Section title="Recurring artifact">
      <Stack spacing={0.75} data-produce={id}>
        <Typography variant="body2">
          {KIND_WORD[p.kind] ?? p.kind}, owed by <span data-produce-agent>{p.agent}</span>
          {p.builtin === 'journal-summary' && ' (made by para while journal.agent is set)'}
        </Typography>
        <Typography variant="body2" data-produce-template>
          Template:{' '}
          {tpl ? (
            <DocLink id={tpl}>{p.templateName || tpl}</DocLink>
          ) : p.template ? (
            <span style={{ whiteSpace: 'pre-wrap' }}>{p.template}</span>
          ) : (
            'none'
          )}
        </Typography>
        {p.kind === 'message' && (p.to?.length ?? 0) > 0 && (
          <Typography variant="body2">
            To: {(p.to ?? []).map((t) => templateId(t) ?? t).join(', ')}
            {p.medium && ` by ${p.medium}`}
          </Typography>
        )}
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center', flexWrap: 'wrap' }}>
          <Typography variant="body2">Last artifact:</Typography>
          {a ? <DocLink id={a.id}>{a.title || a.id}</DocLink> : null}
          <Chip
            size="small"
            data-produce-state
            color={p.ready ? 'success' : 'default'}
            label={p.ready ? `ready: ${artifactState(p)}` : artifactState(p)}
          />
          {a && a.occurrence !== id && (
            <Typography variant="caption" color="text.secondary">
              from <DocLink id={a.occurrence}>the occurrence before</DocLink>
            </Typography>
          )}
        </Stack>
      </Stack>
    </Section>
  )
}

const REPEATS = ['daily', 'weekly', 'monthly', 'quarterly', 'yearly']
const MEDIUMS: Para_ProduceInputMedium[] = ['email', 'slack', 'teams', 'other']

const EMPTY: RecurringInput = {
  title: '',
  agent: '',
  kind: 'report',
  repeat: 'weekly',
  ongoing: false,
  due: '',
  template: '',
  to: [],
  medium: '',
}

// NewRecurringButton opens the form that makes a recurring artifact: an obligation an agent owes
// (obligation_add with produce). The agent prepares each one; you approve it.
export function NewRecurringButton() {
  const [open, setOpen] = useState(false)
  const [f, setF] = useState<RecurringInput>(EMPTY)
  const [to, setTo] = useState('')
  const add = useAddRecurring()
  const navigate = useNavigate()
  const set = <K extends keyof RecurringInput>(k: K, v: RecurringInput[K]) =>
    setF((x) => ({ ...x, [k]: v }))
  const close = () => {
    setOpen(false)
    add.reset()
  }
  const submit = () => {
    const toList = to
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)
    add.mutate(
      { ...f, to: toList },
      {
        onSuccess: (out) => {
          setOpen(false)
          setF(EMPTY)
          setTo('')
          void navigate({ to: '/obligations/$id', params: { id: out.id } })
        },
      },
    )
  }
  return (
    <>
      <Button size="small" variant="outlined" onClick={() => setOpen(true)} data-new-recurring>
        New recurring artifact
      </Button>
      <Dialog open={open} onClose={close} fullWidth maxWidth="sm">
        <DialogTitle>New recurring artifact</DialogTitle>
        <DialogContent>
          <Stack spacing={2} sx={{ pt: 1 }}>
            <Typography variant="body2" color="text.secondary">
              An agent prepares it each time it comes due; you approve it. A message is never sent
              before you approve its text.
            </Typography>
            <TextField
              label="Title"
              required
              value={f.title}
              onChange={(e) => set('title', e.target.value)}
              slotProps={{ htmlInput: { 'data-field': 'title' } }}
            />
            <Stack direction="row" spacing={2}>
              <TextField
                select
                label="What"
                value={f.kind}
                onChange={(e) => set('kind', e.target.value as ProduceKind)}
                sx={{ minWidth: 160 }}
                slotProps={{ htmlInput: { 'data-field': 'kind' } }}
              >
                <MenuItem value="report">Report</MenuItem>
                <MenuItem value="message">Message</MenuItem>
                <MenuItem value="doc">Document</MenuItem>
                <MenuItem value="other">Other</MenuItem>
              </TextField>
              <TextField
                label="Agent"
                placeholder="agent name, or empty for any agent"
                value={f.agent}
                onChange={(e) => set('agent', e.target.value)}
                fullWidth
                slotProps={{ htmlInput: { 'data-field': 'agent' } }}
              />
            </Stack>
            <Stack direction="row" spacing={2}>
              <TextField
                select
                label="Schedule"
                value={f.ongoing ? 'ongoing' : 'series'}
                onChange={(e) => set('ongoing', e.target.value === 'ongoing')}
                sx={{ minWidth: 160 }}
              >
                <MenuItem value="series">Each</MenuItem>
                <MenuItem value="ongoing">Review every</MenuItem>
              </TextField>
              <TextField
                select
                label="How often"
                value={f.repeat}
                onChange={(e) => set('repeat', e.target.value)}
                sx={{ minWidth: 140 }}
                slotProps={{ htmlInput: { 'data-field': 'repeat' } }}
              >
                {REPEATS.map((r) => (
                  <MenuItem key={r} value={r}>
                    {r}
                  </MenuItem>
                ))}
              </TextField>
              {!f.ongoing && (
                <TextField
                  label="First due"
                  type="date"
                  value={f.due}
                  onChange={(e) => set('due', e.target.value)}
                  slotProps={{ inputLabel: { shrink: true }, htmlInput: { 'data-field': 'due' } }}
                />
              )}
            </Stack>
            <TextField
              label="Template"
              placeholder="a document id (such as a note), or the text itself"
              multiline
              minRows={2}
              value={f.template}
              onChange={(e) => set('template', e.target.value)}
              slotProps={{ htmlInput: { 'data-field': 'template' } }}
            />
            {f.kind === 'message' && (
              <Stack direction="row" spacing={2}>
                <TextField
                  label="To"
                  required
                  placeholder="person or group ids, comma separated"
                  value={to}
                  onChange={(e) => setTo(e.target.value)}
                  fullWidth
                  slotProps={{ htmlInput: { 'data-field': 'to' } }}
                />
                <TextField
                  select
                  label="By"
                  value={f.medium}
                  onChange={(e) => set('medium', e.target.value as Para_ProduceInputMedium | '')}
                  sx={{ minWidth: 120 }}
                >
                  <MenuItem value="">any</MenuItem>
                  {MEDIUMS.map((m) => (
                    <MenuItem key={m} value={m}>
                      {m}
                    </MenuItem>
                  ))}
                </TextField>
              </Stack>
            )}
            {add.isError && (
              <Alert severity="error" data-recurring-error>
                {add.error instanceof Error ? add.error.message : 'The server refused it.'}
              </Alert>
            )}
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={close}>Cancel</Button>
          <Button
            variant="contained"
            onClick={submit}
            disabled={add.isPending || f.title.trim() === ''}
            data-recurring-submit
          >
            Make it
          </Button>
        </DialogActions>
      </Dialog>
    </>
  )
}
