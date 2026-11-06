import { createFileRoute } from '@tanstack/react-router'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { useSourceShow } from '@/api/sources'
import { Empty, PageTitle, Section, StateChip, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { Markdown } from '@/components/Markdown'
import { hitKindLabel } from '@/lib/hitkind'
import { cadenceWords, kindWords, reachWords, viaWords } from '@/lib/sources'
import { PrioritySelect } from '@/components/PrioritySelect'
import { exactTime, humanTime } from '@/lib/when'

export const Route = createFileRoute('/_authed/sources/$id')({ component: SourcePage })

type Step = {
  tool?: string | null
  args?: string | null
  command?: string | null
  query?: string | null
  how?: string | null
}

// StepRow shows one thing an agent does on the source, as written in the source document.
function StepRow({ name, step }: { name: string; step?: Step | null }) {
  if (!step) return null
  const what = [step.tool, step.args, step.command].filter(Boolean).join(' ')
  return (
    <ListItem disableGutters divider data-step={name}>
      <ListItemText
        primary={name}
        secondary={
          <>
            {what && (
              <Typography
                component="code"
                variant="body2"
                sx={{ display: 'block', overflowWrap: 'anywhere' }}
              >
                {what}
              </Typography>
            )}
            {step.query && (
              <Typography
                component="span"
                variant="body2"
                sx={{ display: 'block', overflowWrap: 'anywhere' }}
              >
                Query: {step.query}
              </Typography>
            )}
            {step.how && (
              <Typography component="span" variant="body2" sx={{ display: 'block' }}>
                {step.how}
              </Typography>
            )}
          </>
        }
      />
    </ListItem>
  )
}

// SourcePage is one source: how an agent reaches it, who declared it, when it was swept and is
// due, the steps, and the items that cite it. Read only.
function SourcePage() {
  const { id } = Route.useParams()
  const q = useSourceShow(id)
  return (
    <DataState q={q}>
      {(d) => {
        const s = d.source
        return (
          <>
            <PageTitle
              action={
                <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                  <StatusChip tone="quiet" label={kindWords(s)} />
                  {s.overdue && <StatusChip tone="attn" label="Overdue" />}
                  {s.status !== 'active' && <StateChip state={s.status} />}
                </Stack>
              }
              subtitle={viaWords(s)}
            >
              {s.title || s.id}
            </PageTitle>
            <PrioritySelect id={s.id} value={s.priority} />
            <Section title="Sweeps">
              <Typography variant="body2">
                Cadence: {cadenceWords(s)}.{' '}
                {s.lastScan ? (
                  <span title={exactTime(s.lastScan)}>Last swept {humanTime(s.lastScan)}.</span>
                ) : (
                  'Never swept.'
                )}{' '}
                {s.due && `Next due ${s.due}.`}
              </Typography>
              <Typography variant="body2" data-reach>
                {reachWords(s)}.
              </Typography>
              {s.request && (
                <Typography variant="body2">
                  A sweep is waiting: <DocLink id={s.request} />
                </Typography>
              )}
            </Section>
            <Section title="Who reaches it" count={s.declared.length}>
              {s.declared.length === 0 ? (
                <Empty>
                  No agent on this machine declared it. Agents call agent_sources at session start.
                </Empty>
              ) : (
                <List disablePadding aria-label="Declarations">
                  {s.declared.map((x) => (
                    <ListItem key={x.actor} disableGutters divider data-actor={x.actor}>
                      <ListItemText
                        primary={x.actor}
                        secondary={`declared ${humanTime(x.at)}${x.fresh ? '' : ', too long ago to claim sweeps'}`}
                      />
                    </ListItem>
                  ))}
                </List>
              )}
            </Section>
            <Section title="Steps">
              {d.tools.length > 0 && (
                <Typography variant="body2" sx={{ mb: 1 }}>
                  Tools: {d.tools.join(', ')}
                </Typography>
              )}
              <List disablePadding aria-label="Steps">
                <StepRow name="Scan in" step={d.scanIn} />
                <StepRow name="Scan out" step={d.scanOut} />
                <StepRow name="Fetch one" step={d.fetch} />
                <StepRow name="Status" step={d.state} />
              </List>
            </Section>
            <Section title="Items from this source" count={d.cites.length} hideEmpty>
              <List disablePadding aria-label="Items from this source">
                {d.cites.map((c) => (
                  <ListItem key={c.id} disableGutters divider>
                    <ListItemText
                      primary={<DocLink id={c.id}>{c.title || c.id}</DocLink>}
                      secondary={hitKindLabel(c.type)}
                    />
                  </ListItem>
                ))}
              </List>
            </Section>
            {d.body && <Markdown source={d.body} />}
          </>
        )
      }}
    </DataState>
  )
}
