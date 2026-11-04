import { createFileRoute } from '@tanstack/react-router'
import FormControlLabel from '@mui/material/FormControlLabel'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Switch from '@mui/material/Switch'
import Tab from '@mui/material/Tab'
import Tabs from '@mui/material/Tabs'
import TextField from '@mui/material/TextField'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import { useIoList } from '@/api/io'
import { CHIP_ROW, Empty, PageTitle, Section, StateChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { TextLink } from '@/components/links'
import { ioParty } from '@/lib/io'
import { humanTime } from '@/lib/when'

type View = 'inbox' | 'outbox'

const MEDIUMS = [
  'email',
  'slack',
  'jira',
  'confluence',
  'github',
  'teams',
  'phone',
  'meeting',
  'other',
]

export const Route = createFileRoute('/_authed/io/')({
  validateSearch: (
    s: Record<string, unknown>,
  ): { view?: View; medium?: string; all?: boolean } => ({
    view: s.view === 'outbox' ? 'outbox' : undefined,
    medium: typeof s.medium === 'string' && MEDIUMS.includes(s.medium) ? s.medium : undefined,
    all: s.all === true || s.all === 'true' ? true : undefined,
  }),
  component: IoPage,
})

function IoPage() {
  const { view, medium, all } = Route.useSearch()
  const navigate = Route.useNavigate()
  const dir = view === 'outbox' ? 'out' : 'in'
  const q = useIoList(dir, all ?? false)
  const c = q.data?.counts
  return (
    <>
      <PageTitle subtitle="Messages not dealt with yet. Agents file what came in and send what is ready; you decide in dossiers.">
        IO
      </PageTitle>
      <Tabs
        value={view ?? 'inbox'}
        onChange={(_e, v: View) =>
          void navigate({
            search: (p) => ({ ...p, view: v === 'outbox' ? 'outbox' : undefined }),
            replace: true,
          })
        }
        sx={{ mb: 2 }}
      >
        <Tab value="inbox" label={c ? `Inbox (${c.unread + c.read})` : 'Inbox'} />
        <Tab value="outbox" label={c ? `Outbox (${c.draft + c.ready})` : 'Outbox'} />
      </Tabs>
      <Stack direction="row" spacing={2} sx={{ mb: 2, alignItems: 'center', flexWrap: 'wrap' }}>
        <TextField
          select
          size="small"
          label="Medium"
          value={medium ?? ''}
          onChange={(e) =>
            void navigate({
              search: (p) => ({ ...p, medium: e.target.value || undefined }),
              replace: true,
            })
          }
          sx={{ minWidth: 140 }}
        >
          <MenuItem value="">Any</MenuItem>
          {MEDIUMS.map((m) => (
            <MenuItem key={m} value={m}>
              {m}
            </MenuItem>
          ))}
        </TextField>
        <FormControlLabel
          control={
            <Switch
              checked={all ?? false}
              onChange={(e) =>
                void navigate({
                  search: (p) => ({ ...p, all: e.target.checked || undefined }),
                  replace: true,
                })
              }
            />
          }
          label={dir === 'in' ? 'Show filed and dropped' : 'Show sent and dropped'}
        />
      </Stack>
      <DataState q={q}>
        {(d) => {
          const items = d.items.filter((x) => !medium || x.medium === medium)
          if (dir === 'in') {
            return items.length === 0 ? (
              <Empty>Nothing in the inbox.</Empty>
            ) : (
              <IoRows items={items} />
            )
          }
          const groups = [
            { key: 'response', title: 'Expects a response (becomes an Ask when sent)' },
            { key: 'none', title: 'Expects no response (logged when sent)' },
          ]
          return items.length === 0 ? (
            <Empty>Nothing to send.</Empty>
          ) : (
            <>
              {groups.map((g) => {
                const list = items.filter((x) => x.expects === g.key)
                return (
                  <Section key={g.key} title={g.title} count={list.length} hideEmpty>
                    <IoRows items={list} />
                  </Section>
                )
              })}
            </>
          )
        }}
      </DataState>
    </>
  )
}

type Row = NonNullable<ReturnType<typeof useIoList>['data']>['items'][number]

function IoRows({ items }: { items: Row[] }) {
  return (
    <List disablePadding>
      {items.map((x) => {
        const who = x.direction === 'in' ? ioParty(x.from) : x.to.map(ioParty).join(', ')
        return (
          <ListItem key={x.id} disableGutters divider sx={CHIP_ROW}>
            <ListItemText
              primary={
                <TextLink to="/io/$id" params={{ id: x.id }} underline="hover">
                  {x.title || 'Untitled message'}
                </TextLink>
              }
              secondary={[
                who && (x.direction === 'in' ? `from ${who}` : `to ${who}`),
                x.medium,
                humanTime(x.at || x.created),
                x.legacy && 'old inbox capture: run para vault upgrade',
              ]
                .filter(Boolean)
                .join(' - ')}
              slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
            />
            <StateChip state={x.state} />
          </ListItem>
        )
      })}
    </List>
  )
}
