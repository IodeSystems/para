import { createFileRoute } from '@tanstack/react-router'
import { LinkButton } from '@/components/links'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import { useRequestAccept, useRequestCancel, useRequests } from '@/api/requests'
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

function Requests() {
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
