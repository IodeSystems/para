import { createFileRoute } from '@tanstack/react-router'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import { useSourceList } from '@/api/sources'
import { CHIP_ROW, Empty, PageTitle, StateChip, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { TextLink } from '@/components/links'
import { cadenceWords, kindWords, reachWords, viaWords } from '@/lib/sources'
import { humanTime } from '@/lib/when'

export const Route = createFileRoute('/_authed/sources/')({ component: SourcesPage })

// SourcesPage lists the places agents sweep: how each is reached, its cadence, the last sweep,
// and who reaches it. Read only: agents declare and record sweeps.
function SourcesPage() {
  const q = useSourceList()
  return (
    <>
      <PageTitle subtitle="Mailboxes, calendars, chats, trackers, and folders your agents sweep with their own tools. Para reaches none of them.">
        Sources
      </PageTitle>
      <DataState q={q}>
        {(d) =>
          d.sources.length === 0 ? (
            <Empty>No sources. An agent adds them with source_add; they are optional.</Empty>
          ) : (
            <List disablePadding aria-label="Sources">
              {d.sources.map((s) => (
                <ListItem key={s.id} disableGutters divider sx={CHIP_ROW} data-source={s.id}>
                  <ListItemText
                    primary={
                      <TextLink to="/sources/$id" params={{ id: s.id }} underline="hover">
                        {s.title || s.id}
                      </TextLink>
                    }
                    secondary={[
                      kindWords(s),
                      s.priority && `priority ${s.priority}`,
                      viaWords(s),
                      cadenceWords(s),
                      s.lastScan ? `swept ${humanTime(s.lastScan)}` : 'never swept',
                      s.lastSweep && `last sweep ${s.lastSweep}`,
                      reachWords(s),
                    ]
                      .filter(Boolean)
                      .join(' - ')}
                    slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
                  />
                  {s.lastFailed && <StatusChip tone="bad" label="Last sweep failed" />}
                  {s.overdue && <StatusChip tone="attn" label="Overdue" />}
                  {s.declared.every((x) => !x.fresh) && s.status === 'active' && (
                    <StatusChip tone="wait" label="No agent reaches it" />
                  )}
                  {s.request && <StatusChip tone="quiet" label="Sweep queued" />}
                  {s.status !== 'active' && <StateChip state={s.status} />}
                </ListItem>
              ))}
            </List>
          )
        }
      </DataState>
    </>
  )
}
