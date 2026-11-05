import { useQueryClient } from '@tanstack/react-query'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { useSent } from '@/api/proposals'
import { agentUnsend } from '@/api/composer'
import { errorText } from '@/client/gqlClient'
import { Empty, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { useToasts } from '@/components/ToastStack'
import { humanTime } from '@/lib/when'
import { sentStatus } from '@/lib/sent'

// SentList is every piece of agent work the owner sent with Send to agent, newest first: the item,
// the note, the status, and how it ended. Work no agent has claimed can be taken back.
export function SentList({ decide }: { decide: boolean }) {
  const q = useSent()
  const qc = useQueryClient()
  const toasts = useToasts()
  const takeBack = async (request: string, name: string) => {
    try {
      await agentUnsend(request)
      toasts.push({ text: `Taken back: ${name}` })
    } catch (e) {
      toasts.push({ text: `Could not take it back: ${errorText(e)}`, tone: 'error' })
    }
    await qc.invalidateQueries()
  }
  return (
    <DataState q={q}>
      {(items) =>
        items.length === 0 ? (
          <Empty>You have sent nothing to an agent yet.</Empty>
        ) : (
          <List aria-label="Sent to agent" disablePadding>
            {items.map((it) => {
              const s = sentStatus(it)
              return (
                <ListItem
                  key={it.request}
                  divider
                  disableGutters
                  data-sent={it.request}
                  sx={{ display: 'block' }}
                >
                  <Stack
                    direction="row"
                    spacing={1}
                    sx={{ alignItems: 'center', flexWrap: 'wrap' }}
                  >
                    <Box sx={{ flexGrow: 1, minWidth: 0 }}>
                      <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
                        {it.item ? (
                          <DocLink id={it.item}>{it.itemName || it.item}</DocLink>
                        ) : (
                          it.title
                        )}
                      </Typography>
                      {it.note && (
                        <Typography
                          variant="body2"
                          color="text.secondary"
                          sx={{ overflowWrap: 'anywhere' }}
                        >
                          You asked: {it.note}
                        </Typography>
                      )}
                      {it.summary && (
                        <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
                          Agent: {it.summary}
                        </Typography>
                      )}
                      <Stack
                        direction="row"
                        spacing={1}
                        useFlexGap
                        sx={{ mt: 0.25, flexWrap: 'wrap', alignItems: 'center' }}
                      >
                        <StatusChip tone={s.tone} label={s.label} />
                        <Typography variant="caption" color="text.secondary">
                          {humanTime(it.queuedAt)}
                        </Typography>
                        <Typography variant="caption">
                          <DocLink id={it.request}>the request</DocLink>
                        </Typography>
                        {(it.results ?? []).map((r) => (
                          <Typography key={r} variant="caption">
                            <DocLink id={r}>{r}</DocLink>
                          </Typography>
                        ))}
                      </Stack>
                    </Box>
                    {decide && it.undoable && (
                      <Button
                        size="small"
                        onClick={() => void takeBack(it.request, it.itemName || it.title)}
                      >
                        Take back
                      </Button>
                    )}
                  </Stack>
                </ListItem>
              )
            })}
          </List>
        )
      }
    </DataState>
  )
}
