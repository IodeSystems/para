import { createFileRoute } from '@tanstack/react-router'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import { useActivity } from '@/api/receipts'
import { Empty, PageTitle, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { humanTime } from '@/lib/when'

export const Route = createFileRoute('/_authed/activity')({ component: Activity })

// Activity shows what each agent did. Anything no hook recorded is marked self-reported.
function Activity() {
  const q = useActivity()
  return (
    <>
      <PageTitle>Agent activity</PageTitle>
      <DataState q={q}>
        {(items) =>
          items.length === 0 ? (
            <Empty>No agent has written here yet.</Empty>
          ) : (
            <List dense disablePadding>
              {items.map((a) => (
                <ListItem key={a.receipt} disableGutters divider>
                  <ListItemText
                    primary={
                      <>
                        {a.actor}: {a.op.replace(/_/g, ' ')}{' '}
                        {a.doc === 'vault' ? (
                          'the vault'
                        ) : (
                          <DocLink id={a.doc}>{a.docName || a.doc}</DocLink>
                        )}{' '}
                        {a.selfReported && <StatusChip tone="wait" label="self-reported" />}
                      </>
                    }
                    secondary={[humanTime(a.ts), a.why].filter(Boolean).join(' - ')}
                  />
                </ListItem>
              ))}
            </List>
          )
        }
      </DataState>
    </>
  )
}
