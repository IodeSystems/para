import { useState } from 'react'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import OpenInNewIcon from '@mui/icons-material/OpenInNew'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { useRouteContext } from '@tanstack/react-router'
import { canDecide } from '@/client/session'
import { useQueryClient } from '@tanstack/react-query'
import { closeItem } from '@/api/composer'
import { useTask } from '@/api/tasks'
import { errorText } from '@/client/gqlClient'
import { CopyButton } from '@/components/CopyButton'
import { useToasts } from '@/components/ToastStack'
import { hasHow, openableUrl, stepLines, type How } from '@/lib/doit'

const mono = { fontFamily: 'ui-monospace, monospace', fontSize: 13 } as const

// DoIt is the "Do it" block of a task: Open the page, the command in a box with Copy, the steps,
// and Done. Drop it wherever a task row expands. With no how it shows only Done (or nothing, with
// done={false}). Done is the owner's item_close; a closed task cannot be reopened here, so the toast
// says so.
export function DoIt({
  id,
  title,
  how,
  open = true,
  done = true,
}: {
  id: string
  title: string
  how: How
  open?: boolean
  done?: boolean
}) {
  const { session } = useRouteContext({ from: '/_authed' })
  const qc = useQueryClient()
  const toasts = useToasts()
  const [busy, setBusy] = useState(false)
  const url = openableUrl(how.doUrl)
  const steps = stepLines(how.steps)
  const cmd = how.doCommand?.trim()
  const showDone = done && open && canDecide(session)
  if (!hasHow(how) && !showDone) return null
  const markDone = async () => {
    setBusy(true)
    try {
      await closeItem(id)
      await qc.invalidateQueries()
      toasts.push({
        text: `Marked done: ${title}`,
        note: 'Marking done cannot be undone here. Ask an agent to reopen it.',
      })
    } catch (e) {
      toasts.push({ text: `Could not mark done: ${title}: ${errorText(e)}`, tone: 'error' })
    } finally {
      setBusy(false)
    }
  }
  return (
    <Box data-do-it={id} sx={{ my: 0.5 }}>
      {hasHow(how) && (
        <Typography variant="subtitle2" component="h4">
          Do it
        </Typography>
      )}
      {steps.length > 0 && (
        <Box component="ol" aria-label="Steps" sx={{ my: 0.5, pl: 3, fontSize: 14 }}>
          {steps.map((s, i) => (
            <li key={i}>{s}</li>
          ))}
        </Box>
      )}
      {cmd && (
        <Stack direction="row" spacing={1} sx={{ alignItems: 'flex-start', my: 0.5 }}>
          <Box
            component="pre"
            aria-label="Command"
            sx={{
              ...mono,
              m: 0,
              p: 1,
              flexGrow: 1,
              whiteSpace: 'pre-wrap',
              overflowWrap: 'anywhere',
              bgcolor: 'action.hover',
              borderRadius: 1,
            }}
          >
            {cmd}
          </Box>
          <CopyButton getText={() => cmd} label="Copy command" />
        </Stack>
      )}
      <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: 'wrap', mt: 0.5 }}>
        {url && (
          <Button
            size="small"
            variant="outlined"
            href={url}
            target="_blank"
            rel="noopener noreferrer"
            endIcon={<OpenInNewIcon fontSize="small" />}
          >
            Open the page
          </Button>
        )}
        {showDone && (
          <Button size="small" variant="contained" disabled={busy} onClick={() => void markDone()}>
            Done
          </Button>
        )}
      </Stack>
    </Box>
  )
}

// DoItTask loads a task and shows its DoIt block. It shows nothing while the task loads or when the
// document is not a task.
export function DoItTask({ id, done = true }: { id: string; done?: boolean }) {
  const q = useTask(id)
  const t = q.data
  if (!t) return null
  const open = t.status !== 'done' && t.status !== 'dropped'
  return (
    <DoIt
      id={t.id}
      title={t.title}
      open={open}
      done={done}
      how={{ doUrl: t.doUrl, doCommand: t.doCommand, steps: t.steps }}
    />
  )
}
