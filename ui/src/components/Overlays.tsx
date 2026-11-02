import { useState } from 'react'
import { useNavigate, useRouteContext, useRouterState, useSearch } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Drawer from '@mui/material/Drawer'
import List from '@mui/material/List'
import ListItemButton from '@mui/material/ListItemButton'
import ListItemText from '@mui/material/ListItemText'
import Divider from '@mui/material/Divider'
import Button from '@mui/material/Button'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import MenuItem from '@mui/material/MenuItem'
import Snackbar from '@mui/material/Snackbar'
import useMediaQuery from '@mui/material/useMediaQuery'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useAreas } from '@/api/areas'
import { useCapture, useInboxItem, useTriage } from '@/api/inbox'
import { usePeople } from '@/api/people'
import { useOpenProposals } from '@/api/proposals'
import { useWork } from '@/api/work'
import { useRequestAdd } from '@/api/requests'
import { useRfxReply } from '@/api/rfx'
import { errorText } from '@/client/gqlClient'
import { Pick, type Option } from '@/components/Pick'
import { Frame, useClose } from '@/components/OverlayFrame'
import { ProposalDialogs } from '@/components/ProposalDialogs'
import { QuestionDialogs } from '@/components/QuestionDialogs'
import { TABS } from '@/lib/keys'
import { UI_COMMIT } from '@/buildStamp'
import { Details, StatusChip } from '@/components/Bits'

import type { Para_InboxTriageInputKind, Para_InboxTriageInputTo } from '@/gql/graphql'

export function Overlays() {
  const s = useSearch({ strict: false })
  const [toast, setToast] = useState('')
  return (
    <>
      {s.menu && <NavDrawer />}
      {s.capture && <CaptureDialog seed={s.seed ?? ''} onDone={setToast} />}
      {s.request && <RequestDialog onDone={setToast} />}
      {s.help && <HelpDialog />}
      {s.triage && <TriageDialog id={s.triage} onDone={setToast} />}
      {s.reply && <ReplyDialog id={s.reply} onDone={setToast} />}
      <ProposalDialogs s={s} onDone={setToast} />
      <QuestionDialogs s={s} onDone={setToast} />
      {/* The toast sits under the top bar, where Capture is, and never over page text. */}
      <Snackbar
        open={toast !== ''}
        autoHideDuration={7000}
        onClose={() => setToast('')}
        anchorOrigin={{ vertical: 'top', horizontal: 'right' }}
        sx={{ top: { xs: 60, md: 108 } }}
      >
        <Alert severity="success" variant="filled" onClose={() => setToast('')}>
          {toast}
        </Alert>
      </Snackbar>
    </>
  )
}

function CaptureDialog({ seed, onDone }: { seed: string; onDone: (m: string) => void }) {
  const close = useClose()
  const cap = useCapture()
  const [text, setText] = useState(seed)
  const submit = () => {
    if (text.trim() === '') return
    cap.mutate(text.trim(), {
      onSuccess: () => {
        onDone('Captured to the inbox')
        close()
      },
    })
  }
  return (
    <Frame
      title="Capture"
      onSubmit={submit}
      actions={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="submit" variant="contained" disabled={cap.isPending || text.trim() === ''}>
            Capture
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {cap.isError && <Alert severity="error">{errorText(cap.error)}</Alert>}
        <TextField
          label="What is on your mind?"
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) submit()
          }}
          multiline
          minRows={3}
          autoFocus
          fullWidth
        />
        <Typography variant="caption" color="text.secondary">
          Goes to the inbox. Sort it later. Ctrl+Enter captures.
        </Typography>
      </Stack>
    </Frame>
  )
}

function RequestDialog({ onDone }: { onDone: (m: string) => void }) {
  const close = useClose()
  const add = useRequestAdd()
  const [ask, setAsk] = useState('')
  return (
    <Frame
      title="Request for an agent"
      onSubmit={() =>
        add.mutate(ask.trim(), {
          onSuccess: () => {
            onDone('Request queued')
            close()
          },
        })
      }
      actions={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="submit" variant="contained" disabled={add.isPending || ask.trim() === ''}>
            Queue
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {add.isError && <Alert severity="error">{errorText(add.error)}</Alert>}
        <TextField
          label="Ask"
          value={ask}
          onChange={(e) => setAsk(e.target.value)}
          multiline
          minRows={3}
          autoFocus
          fullWidth
        />
        <Typography variant="caption" color="text.secondary">
          Para starts no agent. An agent that polls the queue claims it and its result lands in
          Review.
        </Typography>
      </Stack>
    </Frame>
  )
}

function HelpDialog() {
  const close = useClose()
  const { session } = useRouteContext({ from: '/_authed' })
  // A touch screen has no keyboard to show: the section is left out.
  const touch = useMediaQuery('(hover: none), (pointer: coarse), (max-width: 599px)')
  return (
    <Dialog open onClose={close} fullWidth maxWidth="xs" aria-labelledby="help-title">
      <DialogTitle id="help-title">{touch ? 'Help' : 'Keyboard'}</DialogTitle>
      <DialogContent>
        {!touch && (
          <>
            <Stack component="dl" spacing={0.5} sx={{ m: 0 }}>
              {TABS.map((t) => (
                <Row key={t.key} k={t.key} v={t.label} />
              ))}
              <Row k="/" v="Search" />
              <Row k="c" v="Capture" />
              <Row k="?" v="This help" />
              <Row k="Esc" v="Close a dialog" />
            </Stack>
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
              Keys do nothing while you type in a field.
            </Typography>
            <Divider sx={{ my: 2 }} />
          </>
        )}
        <Typography variant="subtitle2" component="h3" sx={{ mb: 0.5 }}>
          Colors
        </Typography>
        <Stack direction="row" useFlexGap sx={{ flexWrap: 'wrap', gap: 0.5 }}>
          <StatusChip tone="good" label="finished" />
          <StatusChip tone="wait" label="in progress or with someone" />
          <StatusChip tone="attn" label="needs a look" />
          <StatusChip tone="bad" label="blocked or late" />
          <StatusChip tone="quiet" label="plain state" />
        </Stack>
        <Divider sx={{ my: 2 }} />
        <Details summary="About">
          Vault {session.vault}. Signed in with key {session.key}. Build {UI_COMMIT}.
        </Details>
      </DialogContent>
      <DialogActions>
        <Button onClick={close}>Close</Button>
      </DialogActions>
    </Dialog>
  )
}

// NavDrawer lists every screen when the tab bar is folded away. It is a URL overlay like the rest.
function NavDrawer() {
  const close = useClose()
  const path = useRouterState({ select: (st) => st.location.pathname })
  const navigate = useNavigate()
  const { open } = useOpenProposals()
  return (
    <Drawer open onClose={close} anchor="left" aria-label="Screens">
      <Box sx={{ width: 280 }} role="navigation">
        <List>
          {TABS.map((t) => (
            <ListItemButton
              key={t.to}
              component="a"
              href={t.to}
              selected={path === t.to || path.startsWith(`${t.to}/`)}
              onClick={(e) => {
                // Let the router do it: a plain navigation here would reload the app.
                e.preventDefault()
                void navigate({ href: t.to })
              }}
            >
              <ListItemText
                primary={t.label}
                secondary={
                  t.to === '/review' && open.length > 0
                    ? `${open.length} proposal${open.length === 1 ? '' : 's'} to decide`
                    : t.hint
                }
              />
              <Typography
                component="kbd"
                variant="caption"
                color="text.secondary"
                sx={{ fontFamily: 'ui-monospace, monospace' }}
              >
                {t.key}
              </Typography>
            </ListItemButton>
          ))}
        </List>
      </Box>
    </Drawer>
  )
}

function Row({ k, v }: { k: string; v: string }) {
  return (
    <Stack direction="row" spacing={2} sx={{ alignItems: 'baseline' }}>
      <Typography component="dt" sx={{ fontFamily: 'ui-monospace, monospace', width: 40 }}>
        {k}
      </Typography>
      <Typography component="dd" sx={{ m: 0 }}>
        {v}
      </Typography>
    </Stack>
  )
}

const TRIAGE_TARGETS: { value: Para_InboxTriageInputTo; label: string }[] = [
  { value: 'task', label: 'Task' },
  { value: 'project', label: 'Project' },
  { value: 'question', label: 'Question (needs a scope)' },
  { value: 'rfx', label: 'RFX (ask someone)' },
  { value: 'note', label: 'Note on a person' },
  { value: 'drop', label: 'Drop (archive)' },
]

type QuestionScopeType = 'project' | 'area' | 'task' | 'root'

function TriageDialog({ id, onDone }: { id: string; onDone: (m: string) => void }) {
  const close = useClose()
  const item = useInboxItem(id)
  const triage = useTriage()
  const work = useWork()
  const areas = useAreas()
  const people = usePeople()
  const projects: Option[] = (work.data?.projects ?? []).filter((p) => p.status !== 'done')
  const openTasks: Option[] = (work.data?.tasks ?? []).filter(
    (t) => t.status !== 'done' && t.status !== 'dropped',
  )
  const [to, setTo] = useState<Para_InboxTriageInputTo>('task')
  const [project, setProject] = useState('')
  const [person, setPerson] = useState('')
  const [route, setRoute] = useState('')
  const [scope, setScope] = useState<QuestionScopeType>('project')
  const [scopeId, setScopeId] = useState('')
  const [kind, setKind] = useState<Para_InboxTriageInputKind>('question')
  const [options, setOptions] = useState('')
  const [recommended, setRecommended] = useState('')
  const question = to === 'question'
  // A question has exactly one scope: a project, an area, a task, or the vault itself.
  const scopeOk = !question || scope === 'root' || scopeId.trim() !== ''
  const personOk = to !== 'note' || person !== ''
  const submit = () =>
    triage.mutate(
      {
        id,
        to,
        project:
          to === 'task' ? project : question && scope === 'project' ? scopeId.trim() : undefined,
        area: question && scope === 'area' ? scopeId.trim() : undefined,
        task: question && scope === 'task' ? scopeId.trim() : undefined,
        root: question && scope === 'root' ? true : undefined,
        kind: question ? kind : undefined,
        options: question
          ? options
              .split('\n')
              .map((o) => o.trim())
              .filter(Boolean)
          : undefined,
        recommended: question ? recommended.trim() : undefined,
        person: to === 'note' ? person : undefined,
        route: to === 'rfx' ? route : undefined,
      },
      {
        onSuccess: (r) => {
          onDone(r.warnings.length ? `Sorted. ${r.warnings.join(' ')}` : `Sorted into ${r.to}`)
          close()
        },
      },
    )
  const scopeOptions: Option[] =
    scope === 'project' ? projects : scope === 'area' ? (areas.data ?? []) : openTasks
  return (
    <Frame
      title="Sort inbox item"
      onSubmit={submit}
      actions={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button
            type="submit"
            variant="contained"
            disabled={triage.isPending || !scopeOk || !personOk}
          >
            Sort
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        <Typography sx={{ whiteSpace: 'pre-wrap' }}>
          {item.data?.title ?? (item.isError ? errorText(item.error) : 'Loading')}
        </Typography>
        {triage.isError && <Alert severity="error">{errorText(triage.error)}</Alert>}
        <TextField
          select
          label="Turn into"
          value={to}
          onChange={(e) => setTo(e.target.value as Para_InboxTriageInputTo)}
        >
          {TRIAGE_TARGETS.map((t) => (
            <MenuItem key={t.value} value={t.value}>
              {t.label}
            </MenuItem>
          ))}
        </TextField>
        {to === 'task' && (
          <Pick
            label="Project (optional)"
            value={project}
            onChange={setProject}
            options={projects}
            none="No project"
          />
        )}
        {question && (
          <>
            <TextField
              select
              label="Scope (what this question is about)"
              value={scope}
              onChange={(e) => {
                setScope(e.target.value as QuestionScopeType)
                setScopeId('')
              }}
            >
              <MenuItem value="project">A project</MenuItem>
              <MenuItem value="area">An area</MenuItem>
              <MenuItem value="task">A task</MenuItem>
              <MenuItem value="root">The whole vault</MenuItem>
            </TextField>
            {scope !== 'root' && (
              <Pick
                label={
                  scope === 'project'
                    ? 'Which project'
                    : scope === 'area'
                      ? 'Which area'
                      : 'Which task'
                }
                value={scopeId}
                onChange={setScopeId}
                options={scopeOptions}
                required
              />
            )}
            <TextField
              select
              label="Kind"
              value={kind}
              onChange={(e) => setKind(e.target.value as Para_InboxTriageInputKind)}
            >
              <MenuItem value="question">Question</MenuItem>
              <MenuItem value="decision">Decision (options and a reason)</MenuItem>
              <MenuItem value="input">Input (someone gives you a value)</MenuItem>
            </TextField>
            <TextField
              label="Options (one per line, optional)"
              value={options}
              onChange={(e) => setOptions(e.target.value)}
              multiline
              minRows={2}
            />
            <TextField
              label="Recommended option (optional)"
              value={recommended}
              onChange={(e) => setRecommended(e.target.value)}
            />
          </>
        )}
        {to === 'note' && (
          <Pick
            label="Which person"
            value={person}
            onChange={setPerson}
            options={people.data ?? []}
            required
          />
        )}
        {to === 'rfx' && (
          <TextField
            label="Route (who or where to ask)"
            value={route}
            onChange={(e) => setRoute(e.target.value)}
          />
        )}
      </Stack>
    </Frame>
  )
}

function ReplyDialog({ id, onDone }: { id: string; onDone: (m: string) => void }) {
  const close = useClose()
  const reply = useRfxReply()
  const [from, setFrom] = useState('')
  const [outcome, setOutcome] = useState<'answered' | 'partial' | 'no'>('answered')
  const [text, setText] = useState('')
  const [why, setWhy] = useState('')
  const ok = from.trim() !== '' && text.trim() !== '' && (outcome !== 'no' || why.trim() !== '')
  return (
    <Frame
      title="Record a reply"
      onSubmit={() =>
        reply.mutate(
          { id, from: from.trim(), outcome, text: text.trim(), why: why.trim() },
          {
            onSuccess: (r) => {
              onDone(
                r.constraint
                  ? `Reply recorded. Constraint ${r.constraint} created.`
                  : 'Reply recorded',
              )
              close()
            },
          },
        )
      }
      actions={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button type="submit" variant="contained" disabled={reply.isPending || !ok}>
            Record
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {reply.isError && <Alert severity="error">{errorText(reply.error)}</Alert>}
        <TextField
          label="Who replied (name)"
          value={from}
          onChange={(e) => setFrom(e.target.value)}
          required
          autoFocus
        />
        <TextField
          select
          label="Outcome"
          value={outcome}
          onChange={(e) => setOutcome(e.target.value as typeof outcome)}
        >
          <MenuItem value="answered">Answered</MenuItem>
          <MenuItem value="partial">Partly answered</MenuItem>
          <MenuItem value="no">No (creates a constraint)</MenuItem>
        </TextField>
        <TextField
          label="What they said"
          value={text}
          onChange={(e) => setText(e.target.value)}
          multiline
          minRows={3}
          required
        />
        {outcome === 'no' && (
          <TextField label="Why" value={why} onChange={(e) => setWhy(e.target.value)} required />
        )}
      </Stack>
    </Frame>
  )
}
