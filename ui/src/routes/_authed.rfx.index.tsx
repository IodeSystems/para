import { createFileRoute } from '@tanstack/react-router'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import { useRfxList } from '@/api/rfx'
import { Empty, PageTitle, StateChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { RfxLink } from '@/components/DocLink'
import { nameList, nameOr } from '@/lib/names'
import { dateWords } from '@/lib/when'
import { plainState } from '@/lib/status'

export const Route = createFileRoute('/_authed/rfx/')({ component: RfxList })

function RfxList() {
  const q = useRfxList()
  return (
    <>
      <PageTitle subtitle="Asks between you and other people, and what they said back.">
        RFX
      </PageTitle>
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
                    secondary={[
                      x.direction === 'in' ? 'asked of you' : 'you ask',
                      x.route && `via ${nameOr(x.routeName, x.route)}`,
                      x.nextNudge && `nudge ${dateWords(x.nextNudge)}`,
                      x.dependsOn.length > 0 &&
                        `after ${nameList(x.dependsOn, x.dependsOnNames).join(', ')}`,
                    ]
                      .filter(Boolean)
                      .join(' - ')}
                    slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
                  />
                  <Stack
                    direction="row"
                    sx={{ maxWidth: '45%', flexWrap: 'wrap', gap: 0.5, justifyContent: 'flex-end' }}
                  >
                    {x.targets.map((t, i) => (
                      <StateChip
                        key={`${t.to}-${t.medium}`}
                        state={t.state}
                        label={`${nameOr(x.targetNames?.[i], t.to)}, ${plainState(t.state)}`}
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
