import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import TextField from '@mui/material/TextField'
import { useDismissHealth, useHealth } from '@/api/health'
import { Empty, PageTitle, Section, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { errorText } from '@/client/gqlClient'
import { dateWords } from '@/lib/when'

export const Route = createFileRoute('/_authed/health')({ component: Health })

type Finding = { id: string; kind: string; title: string }

function Health() {
  const q = useHealth()
  const dismiss = useDismissHealth()
  const [target, setTarget] = useState<Finding | null>(null)
  const [reason, setReason] = useState('')
  const close = () => {
    setTarget(null)
    setReason('')
    dismiss.reset()
  }
  return (
    <>
      <PageTitle subtitle="Gaps in the vault, by goal. Dismiss one that can stay as it is.">
        Health
      </PageTitle>
      <DataState q={q}>
        {(h) =>
          h.groups.length === 0 ? (
            <Empty>Nothing to flag.{h.dismissed > 0 ? ` ${h.dismissed} dismissed.` : ''}</Empty>
          ) : (
            <>
              {h.groups.map((g) => (
                <Section
                  key={g.goal}
                  title={g.due ? `${g.title} (due ${dateWords(g.due)})` : g.title}
                  count={g.findings.length}
                >
                  <List dense disablePadding>
                    {g.findings.map((f) => (
                      <ListItem key={`${f.kind}-${f.id}`} disableGutters>
                        <ListItemText
                          primary={<DocLink id={f.id}>{f.title || f.id}</DocLink>}
                          secondary={`${f.message}. Fix: ${f.fix}`}
                          slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
                        />
                        <StatusChip tone="quiet" label={f.kind} />
                        <Button
                          size="small"
                          onClick={() => setTarget({ id: f.id, kind: f.kind, title: f.title })}
                        >
                          Dismiss
                        </Button>
                      </ListItem>
                    ))}
                  </List>
                </Section>
              ))}
              {h.dismissed > 0 && <Empty>{h.dismissed} dismissed.</Empty>}
            </>
          )
        }
      </DataState>
      <Dialog open={target !== null} onClose={close} fullWidth maxWidth="sm">
        <form
          onSubmit={(e) => {
            e.preventDefault()
            if (!target) return
            dismiss.mutate(
              { id: target.id, kind: target.kind, reason: reason.trim() },
              { onSuccess: close },
            )
          }}
        >
          <DialogTitle>Dismiss {target?.kind}</DialogTitle>
          <DialogContent>
            {dismiss.isError && <Alert severity="error">{errorText(dismiss.error)}</Alert>}
            <TextField
              autoFocus
              fullWidth
              margin="dense"
              label="Why it can stay as it is"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
            />
          </DialogContent>
          <DialogActions>
            <Button onClick={close}>Cancel</Button>
            <Button
              type="submit"
              variant="contained"
              disabled={reason.trim() === '' || dismiss.isPending}
            >
              Dismiss
            </Button>
          </DialogActions>
        </form>
      </Dialog>
    </>
  )
}
