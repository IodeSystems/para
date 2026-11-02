import { createFileRoute } from '@tanstack/react-router'
import { LinkButton } from '@/components/links'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import { useRequests } from '@/api/requests'
import { Empty, PageTitle, StateChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'

export const Route = createFileRoute('/_authed/requests')({ component: Requests })

function Requests() {
  const q = useRequests()
  return (
    <>
      <PageTitle
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
                    primary={<DocLink id={r.id}>{r.title || r.id}</DocLink>}
                    secondary={[
                      `runner ${r.runner}`,
                      r.claimedBy && `claimed by ${r.claimedBy}`,
                      r.leaseUntil && `lease until ${r.leaseUntil}`,
                    ]
                      .filter(Boolean)
                      .join(' - ')}
                  />
                  <StateChip state={r.status} />
                </ListItem>
              ))}
            </List>
          )
        }
      </DataState>
    </>
  )
}
