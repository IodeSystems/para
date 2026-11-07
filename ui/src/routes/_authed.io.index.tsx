import { createFileRoute } from '@tanstack/react-router'
import FormControlLabel from '@mui/material/FormControlLabel'
import Link from '@mui/material/Link'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Switch from '@mui/material/Switch'
import Tab from '@mui/material/Tab'
import Tabs from '@mui/material/Tabs'
import Typography from '@mui/material/Typography'
import TextField from '@mui/material/TextField'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import { useIoList } from '@/api/io'
import { CHIP_ROW, Empty, PageTitle, Section, StateChip, StatusChip } from '@/components/Bits'
import { CaptureButton } from '@/components/CaptureButton'
import { DataState } from '@/components/DataState'
import { TextLink } from '@/components/links'
import { ioParty } from '@/lib/io'
import { voiceWords } from '@/components/Voice'
import { digestRows, splitInbox } from '@/lib/priority'
import { humanTime } from '@/lib/when'

type View = 'inbox' | 'digest' | 'outbox'

const MEDIUMS = [
  'email',
  'slack',
  'jira',
  'confluence',
  'github',
  'teams',
  'phone',
  'meeting',
  'verbal',
  'swivel',
  'other',
]

export const Route = createFileRoute('/_authed/io/')({
  validateSearch: (
    s: Record<string, unknown>,
  ): { view?: View; medium?: string; all?: boolean; forMe?: boolean } => ({
    view: s.view === 'outbox' || s.view === 'digest' ? s.view : undefined,
    medium: typeof s.medium === 'string' && MEDIUMS.includes(s.medium) ? s.medium : undefined,
    all: s.all === true || s.all === 'true' ? true : undefined,
    forMe: s.forMe === true || s.forMe === 'true' ? true : undefined,
  }),
  component: IoPage,
})

function IoPage() {
  const { view, medium, all, forMe: forMeSearch } = Route.useSearch()
  const navigate = Route.useNavigate()
  const dir = view === 'outbox' ? 'out' : 'in'
  // Sent for you: what agents sent in your stead, stamped, without your review.
  const forMe = dir === 'out' && (forMeSearch ?? false)
  const q = useIoList(dir, forMe || (all ?? false), forMe ? 'for_me' : undefined)
  const c = q.data?.counts
  return (
    <>
      <PageTitle
        subtitle="Messages not dealt with yet. Agents file what came in and send what is ready; you decide in dossiers."
        action={<CaptureButton />}
      >
        IO
      </PageTitle>
      <Tabs
        value={view ?? 'inbox'}
        onChange={(_e, v: View) =>
          void navigate({
            search: (p) => ({ ...p, view: v === 'inbox' ? undefined : v }),
            replace: true,
          })
        }
        sx={{ mb: 2 }}
      >
        <Tab value="inbox" label={c ? `Inbox (${c.unread + c.read})` : 'Inbox'} />
        <Tab value="digest" label="Digest" />
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
        {dir === 'out' && (
          <FormControlLabel
            control={
              <Switch
                checked={forMe}
                data-filter="sent-for-you"
                onChange={(e) =>
                  void navigate({
                    search: (p) => ({ ...p, forMe: e.target.checked || undefined }),
                    replace: true,
                  })
                }
              />
            }
            label="Sent for you"
          />
        )}
      </Stack>
      <DataState q={q}>
        {(d) => {
          const items = d.items.filter(
            (x) => (!medium || x.medium === medium) && (!forMe || x.state === 'sent'),
          )
          if (forMe) {
            return items.length === 0 ? (
              <Empty>No agent sent anything for you without your review.</Empty>
            ) : (
              <>
                <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
                  Messages agents sent for you, stamped, without your review, under a rule of your
                  .para/delegation.yaml. Each names the rule that allowed it.
                </Typography>
                <IoRows items={items} />
              </>
            )
          }
          if (view === 'digest') {
            const list = digestRows(items)
            return list.length === 0 ? (
              <Empty>Nothing in the digest.</Empty>
            ) : (
              <>
                <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
                  Messages that do not need you: no reply expected, and the sender and the source
                  are not high priority, or a triage rule sent them here. The day&apos;s journal
                  lists them under Inbox digest.
                </Typography>
                <IoRows items={list} />
              </>
            )
          }
          if (dir === 'in') {
            if (items.length === 0) return <Empty>Nothing in the inbox.</Empty>
            const { needsYou, rest } = splitInbox(items)
            return (
              <>
                <Section title="Needs you" count={needsYou.length} hideEmpty>
                  <IoRows items={needsYou} />
                </Section>
                <Section title="The rest" count={rest.length} hideEmpty>
                  <IoRows items={rest} />
                </Section>
              </>
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
                x.direction === 'in' && x.needsYouReason,
                x.voice === 'for_me' && voiceWords(x),
                x.legacy && 'old inbox capture: run para vault upgrade',
              ]
                .filter(Boolean)
                .join(' - ')}
              slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
            />
            {x.ref && (
              <Link href={x.ref} target="_blank" rel="noreferrer" variant="body2" sx={{ mr: 1 }}>
                link
              </Link>
            )}
            {x.unverifiable && <StatusChip tone="attn" label="no way to verify" />}
            <StateChip state={x.state} />
          </ListItem>
        )
      })}
    </List>
  )
}
