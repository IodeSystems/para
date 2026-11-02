import { createFileRoute } from '@tanstack/react-router'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import { useRfxList } from '@/api/rfx'
import { Empty, PageTitle, StateChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { RfxLink } from '@/components/DocLink'

export const Route = createFileRoute('/_authed/rfx/')({ component: RfxList })

function RfxList() {
  const q = useRfxList()
  return (
    <>
      <PageTitle>RFX</PageTitle>
      <DataState q={q}>
        {(items) =>
          items.length === 0 ? (
            <Empty>No open requests.</Empty>
          ) : (
            <List disablePadding>
              {items.map((x) => (
                <ListItem key={x.id} disableGutters divider alignItems="flex-start">
                  <ListItemText
                    primary={<RfxLink id={x.id}>{x.title || x.id}</RfxLink>}
                    secondary={
                      <>
                        {x.direction === 'in' ? 'asked of you' : 'you ask'}
                        {x.route ? ` - ${x.route}` : ''}
                        {x.nextNudge ? ` - nudge ${x.nextNudge}` : ''}
                      </>
                    }
                    slotProps={{ secondary: { component: 'div' } }}
                  />
                  <Stack
                    direction="row"
                    sx={{ maxWidth: '45%', flexWrap: 'wrap', gap: 0.5, justifyContent: 'flex-end' }}
                  >
                    {x.targets.map((t) => (
                      <StateChip
                        key={`${t.to}-${t.medium}`}
                        state={t.state}
                        label={`${t.to}: ${t.state.replace(/_/g, ' ')}`}
                      />
                    ))}
                    {x.status !== 'open' && <StateChip state={x.status} />}
                  </Stack>
                </ListItem>
              ))}
            </List>
          )
        }
      </DataState>
    </>
  )
}
