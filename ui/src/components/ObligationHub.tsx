import Chip from '@mui/material/Chip'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { useItemContext } from '@/api/context'
import { useObligationParts, type ObligationPartsData } from '@/api/obligations'
import { Empty, Section, StatusChip } from '@/components/Bits'
import { DocLink } from '@/components/DocLink'
import { ChangeButtons } from '@/components/ObligationChange'
import { promiseWords } from '@/lib/obligations'

type Part = ObligationPartsData['before'][number]

// ObligationHub is the part of an obligation's page that holds what hangs on it: Before (the prep:
// open and done children, and what it depends on, with the backward date open prep falls due by),
// Documents (notes and tracked documents linked to it), Serves (the goal or area it leads to), and
// Came out of it (action items and follow-ups made after its date). Push out, Redirect, and Cancel
// sit on top while it is open; its changes and the notices they wrote are listed last.
export function ObligationHub({ id }: { id: string }) {
  const q = useObligationParts(id)
  const ctx = useItemContext(id)
  const p = q.data
  if (!p) return null
  const serves = (ctx.data?.reasons ?? []).filter((r) => r.kind === 'goal' || r.kind === 'upkeep')
  const promise = promiseWords(p.promise)
  return (
    <>
      <Stack
        direction="row"
        spacing={1}
        sx={{ mb: 1, alignItems: 'center', flexWrap: 'wrap' }}
        data-hub={id}
      >
        {promise && <StatusChip tone={p.promise === 'broken' ? 'bad' : 'good'} label={promise} />}
        {p.state === 'open' && (
          <ChangeButtons
            size="medium"
            x={{
              id,
              title: p.title,
              meeting: p.kind === 'meeting',
              parties: p.parties,
              calendar: p.calendar,
            }}
          />
        )}
      </Stack>
      <Section title="Before" count={p.before.length}>
        {p.before.length === 0 ? (
          <Empty>
            No prep linked: an item whose parent is this one, or that it depends on, shows here.
          </Empty>
        ) : (
          <Parts parts={p.before} data="before" />
        )}
      </Section>
      <Section title="Documents" count={p.documents.length} hideEmpty>
        <List dense disablePadding data-documents>
          {p.documents.map((d) => (
            <ListItem key={d.id} disableGutters>
              <ListItemText
                primary={<DocLink id={d.id}>{d.title || d.id}</DocLink>}
                secondary={d.kind}
              />
            </ListItem>
          ))}
        </List>
      </Section>
      <Section title="Serves" count={serves.length} hideEmpty>
        <List dense disablePadding data-serves>
          {serves.map((r) => (
            <ListItem key={`${r.kind}-${r.ref ?? r.title ?? ''}`} disableGutters>
              <ListItemText primary={r.ref ? <DocLink id={r.ref}>{r.text}</DocLink> : r.text} />
            </ListItem>
          ))}
        </List>
      </Section>
      <Section title="Came out of it" count={p.cameOut.length} hideEmpty>
        <Parts parts={p.cameOut} data="came-out" />
      </Section>
      <Section title="Changes" count={p.changes.length} hideEmpty>
        <Stack spacing={0.5} data-changes>
          {p.changes.map((c) => (
            <Typography key={c} variant="body2" sx={{ overflowWrap: 'anywhere' }}>
              {c}
            </Typography>
          ))}
          {p.notices.map((n) => (
            <Typography key={n.id} variant="body2" data-notice={n.id}>
              Notice by {n.medium}: <DocLink id={n.id}>{n.title || n.id}</DocLink> ({n.state})
            </Typography>
          ))}
        </Stack>
      </Section>
    </>
  )
}

function Parts({ parts, data }: { parts: readonly Part[]; data: string }) {
  return (
    <List dense disablePadding data-parts={data}>
      {parts.map((x) => (
        <ListItem key={x.id} disableGutters data-part={x.id}>
          <ListItemText
            primary={<DocLink id={x.id}>{x.title || x.id}</DocLink>}
            secondary={[
              x.kind,
              x.status,
              x.effort,
              x.back && `by ${x.back}`,
              x.relation === 'depends_on' && 'it waits on this',
            ]
              .filter(Boolean)
              .join(' - ')}
            sx={{ opacity: x.done ? 0.6 : 1 }}
          />
          {x.blocked && <Chip size="small" color="error" label="Blocked" />}
        </ListItem>
      ))}
    </List>
  )
}
