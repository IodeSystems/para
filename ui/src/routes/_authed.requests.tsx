import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { LinkButton } from '@/components/links'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import { useLooking, useRequestAccept, useRequestCancel, useRequests } from '@/api/requests'
import { RowMenu } from '@/components/RowMenu'
import { AgentLooking, findContextItem } from '@/components/FindContext'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import {
  useRequestAccept,
  useRequestCancel,
  useRequestChanges,
  useRequests,
  useRespond,
} from '@/api/requests'
import { Empty, PageTitle, StateChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { errorText } from '@/client/gqlClient'
import { DocLink } from '@/components/DocLink'
import { humanTime } from '@/lib/when'
import { joinNames, nameList, stripIds, withNames } from '@/lib/names'

// What each status says to a person.
const STATUS_WORDS: Record<string, string> = {
  queued: 'waiting for an agent',
  claimed: 'an agent has it',
  proposed: 'proposal ready',
  done: 'done',
  failed: 'failed',
  cancelled: 'cancelled',
}

export const Route = createFileRoute('/_authed/requests')({ component: Requests })

// Respond reopens finished work as the next turn. What the work changed comes from events.
function Respond({ id }: { id: string }) {
  const [open, setOpen] = useState(false)
  const [kind, setKind] = useState<'change' | 'clarify' | 'revert'>('change')
  const [text, setText] = useState('')
  const respond = useRespond()
  const changes = useRequestChanges(id, open)
  if (!open) {
    return (
      <Button size="small" onClick={() => setOpen(true)} sx={{ ml: 1 }}>
        Respond
      </Button>
    )
  }
  return (
    <Stack spacing={1} sx={{ ml: 1, minWidth: 240 }}>
      {respond.isError && <Alert severity="error">{errorText(respond.error)}</Alert>}
      {(changes.data ?? []).map((c) => (
        <DocLink key={c.receipt} id={c.doc}>
          {c.docName || c.doc}
        </DocLink>
      ))}
      <TextField
        select
        size="small"
        label="Response"
        value={kind}
        onChange={(e) => setKind(e.target.value as typeof kind)}
      >
        <MenuItem value="change">Change</MenuItem>
        <MenuItem value="clarify">Clarify</MenuItem>
        <MenuItem value="revert">Revert</MenuItem>
      </TextField>
      <TextField
        size="small"
        label="What to do"
        value={text}
        onChange={(e) => setText(e.target.value)}
      />
      <Button
        size="small"
        variant="contained"
        disabled={respond.isPending || text.trim() === ''}
        onClick={() => respond.mutate({ id, kind, text: text.trim() })}
      >
        Send
      </Button>
    </Stack>
  )
}

function Requests() {
  const looking = useLooking()
  const q = useRequests()
  const accept = useRequestAccept()
  const cancel = useRequestCancel()
  return (
    <>
      <PageTitle
        subtitle="Work you queue for an agent to pick up. Not the same as Asks, which are asks to people."
        action={
          <LinkButton
            variant="contained"
            size="small"
            to="."
            search={(p) => ({ ...p, request: true })}
          >
            New agent work
          </LinkButton>
        }
      >
        Agent work
      </PageTitle>
      {accept.isError && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {errorText(accept.error)}
        </Alert>
      )}
      {cancel.isError && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {errorText(cancel.error)}
        </Alert>
      )}
      <DataState q={q}>
        {(items) =>
          items.length === 0 ? (
            <Empty>No agent work. Agent work is what an agent picks up from the queue.</Empty>
          ) : (
            <List disablePadding>
              {items.map((r) => (
                <ListItem key={r.id} disableGutters divider>
                  <ListItemText
                    primary={
                      <DocLink id={r.id}>
                        {stripIds(withNames(r.title, r.links, r.linkNames)) || 'Agent work'}
                      </DocLink>
                    }
                    secondary={[
                      `queued ${humanTime(r.queuedAt)}`,
                      r.claimedBy && `taken by ${r.claimedBy}`,
                      (r.links ?? []).length > 0 &&
                        `about ${joinNames(nameList(r.links, r.linkNames))}`,
                    ]
                      .filter(Boolean)
                      .join(' - ')}
                    slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
                  />
                  <StateChip state={r.status} label={STATUS_WORDS[r.status]} />
                  <AgentLooking id={r.id} />
                  {!r.investigates && (
                    <RowMenu
                      label={`More actions for ${r.title || r.id}`}
                      always
                      items={[findContextItem(r.id, looking.has(r.id))]}
                    />
                  )}
                  <StateChip
                    state={r.status}
                    label={r.resolution ? r.resolution.replace(/_/g, ' ') : STATUS_WORDS[r.status]}
                  />
                  {(r.status === 'done' || r.status === 'failed') && <Respond id={r.id} />}
                  {r.status === 'proposed' && (
                    <>
                      <Button
                        size="small"
                        variant="contained"
                        disabled={accept.isPending || cancel.isPending}
                        aria-label={`Accept agent work: ${r.title || r.id}`}
                        onClick={() => accept.mutate({ id: r.id })}
                        sx={{ ml: 1 }}
                      >
                        Accept
                      </Button>
                      <Button
                        size="small"
                        color="error"
                        disabled={accept.isPending || cancel.isPending}
                        aria-label={`Decline agent work: ${r.title || r.id}`}
                        onClick={() => cancel.mutate({ id: r.id })}
                      >
                        Decline
                      </Button>
                    </>
                  )}
                </ListItem>
              ))}
            </List>
          )
        }
      </DataState>
    </>
  )
}
