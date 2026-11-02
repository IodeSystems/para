import { createFileRoute } from '@tanstack/react-router'
import { LinkButton } from '@/components/links'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import { useRequests } from '@/api/requests'
import { Empty, PageTitle, StateChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { ago } from '@/lib/events'
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

function Requests() {
  const q = useRequests()
  return (
    <>
      <PageTitle
        subtitle="Work you queue for an agent to pick up. Not the same as RFX, which are asks to people."
        action={
          <LinkButton
            variant="contained"
            size="small"
            to="."
            search={(p) => ({ ...p, request: true })}
          >
            New request
          </LinkButton>
        }
      >
        Requests
      </PageTitle>
      <DataState q={q}>
        {(items) =>
          items.length === 0 ? (
            <Empty>No request. A request is an ask an agent picks up from the queue.</Empty>
          ) : (
            <List disablePadding>
              {items.map((r) => (
                <ListItem key={r.id} disableGutters divider>
                  <ListItemText
                    primary={
                      <DocLink id={r.id}>
                        {stripIds(withNames(r.title, r.links, r.linkNames)) || 'Request'}
                      </DocLink>
                    }
                    secondary={[
                      `queued ${ago(r.queuedAt)}`,
                      r.claimedBy && `taken by ${r.claimedBy}`,
                      (r.links ?? []).length > 0 &&
                        `about ${joinNames(nameList(r.links, r.linkNames))}`,
                    ]
                      .filter(Boolean)
                      .join(' - ')}
                    slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
                  />
                  <StateChip state={r.status} label={STATUS_WORDS[r.status]} />
                </ListItem>
              ))}
            </List>
          )
        }
      </DataState>
    </>
  )
}
