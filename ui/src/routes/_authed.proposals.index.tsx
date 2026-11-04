import { createFileRoute } from '@tanstack/react-router'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import { useProposals } from '@/api/proposals'
import { CHIP_ROW, Empty, PageTitle, StateChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { TextLink } from '@/components/links'
import { DECIDE_HINT } from '@/lib/proposal'
import { humanTime } from '@/lib/when'

export const Route = createFileRoute('/_authed/proposals/')({ component: Proposals })

// Proposals lists what agents asked the owner to decide. Deciding happens in the chat.
function Proposals() {
  const q = useProposals()
  return (
    <>
      <PageTitle subtitle={DECIDE_HINT}>Proposals</PageTitle>
      <DataState q={q}>
        {(items) =>
          items.length === 0 ? (
            <Empty>No agent has filed a proposal.</Empty>
          ) : (
            <List dense disablePadding>
              {items.map((p) => (
                <ListItem key={p.id} disableGutters divider sx={CHIP_ROW}>
                  <ListItemText
                    primary={
                      <TextLink to="/proposals/$id" params={{ id: p.id }} underline="hover">
                        {p.title || p.id}
                      </TextLink>
                    }
                    secondary={`revision ${p.revision}, ${p.itemCount} items, ${p.pending} to decide, by ${p.proposedBy}, ${humanTime(p.created)}`}
                  />
                  <StateChip state={p.status} />
                </ListItem>
              ))}
            </List>
          )
        }
      </DataState>
    </>
  )
}
