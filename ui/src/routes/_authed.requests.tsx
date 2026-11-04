import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Box from '@mui/material/Box'
import { AgentLooking } from '@/components/FindContext'
import Stack from '@mui/material/Stack'
import { useRequestChanges, useRequests } from '@/api/requests'
import { Empty, PageTitle, StateChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
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

// Changes lists what finished agent work changed, from its receipts. It loads when opened.
function Changes({ id }: { id: string }) {
  const [open, setOpen] = useState(false)
  const changes = useRequestChanges(id, open)
  return (
    <Box
      component="details"
      onToggle={(e) => setOpen(e.currentTarget.open)}
      sx={{ ml: 1, fontSize: '0.8125rem' }}
    >
      <Box component="summary" sx={{ cursor: 'pointer' }}>
        What it changed
      </Box>
      <Stack spacing={0.5}>
        {(changes.data ?? []).map((c) => (
          <DocLink key={c.receipt} id={c.doc}>
            {c.docName || c.doc}
          </DocLink>
        ))}
        {changes.data?.length === 0 && 'Nothing.'}
      </Stack>
    </Box>
  )
}

function Requests() {
  const q = useRequests()
  return (
    <>
      <PageTitle subtitle="Work queued for an agent. Not the same as Asks, which are asks to people. Queue, accept, or answer agent work in the chat.">
        Agent work
      </PageTitle>
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
                  <AgentLooking id={r.id} />
                  <StateChip
                    state={r.status}
                    label={r.resolution ? r.resolution.replace(/_/g, ' ') : STATUS_WORDS[r.status]}
                  />
                  {(r.status === 'done' || r.status === 'failed') && <Changes id={r.id} />}
                </ListItem>
              ))}
            </List>
          )
        }
      </DataState>
    </>
  )
}
