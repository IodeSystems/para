import { createFileRoute } from '@tanstack/react-router'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import Tab from '@mui/material/Tab'
import Tabs from '@mui/material/Tabs'
import { usePolicy } from '@/api/policy'
import { useAreas } from '@/api/areas'
import { useWork } from '@/api/work'
import { nameOr } from '@/lib/names'
import { Details, Empty, PageTitle, StateChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'

const TABS = ['policies', 'rules', 'exceptions', 'constraints'] as const
type PolicyTab = (typeof TABS)[number]

export const Route = createFileRoute('/_authed/policy')({
  validateSearch: (s: Record<string, unknown>): { tab?: PolicyTab } => ({
    tab: TABS.find((t) => t === s.tab && t !== 'policies'),
  }),
  component: Policy,
})

function Policy() {
  const { tab = 'policies' } = Route.useSearch()
  const navigate = Route.useNavigate()
  const q = usePolicy()
  const work = useWork()
  const areas = useAreas()
  // title is the name of a document an exception or constraint points at.
  const known = new Map<string, string>()
  for (const p of q.data?.policies ?? []) known.set(p.id, p.title)
  for (const p of work.data?.projects ?? []) known.set(p.id, p.title)
  for (const a of areas.data ?? []) known.set(a.id, a.title)
  const title = (id: string) => nameOr(known.get(id), id)
  return (
    <>
      <PageTitle>Policy</PageTitle>
      <DataState q={q}>
        {(d) => (
          <>
            <Tabs
              value={tab}
              variant="scrollable"
              scrollButtons="auto"
              onChange={(_e, v: PolicyTab) =>
                void navigate({
                  search: (p) => ({ ...p, tab: v === 'policies' ? undefined : v }),
                  replace: true,
                })
              }
              sx={{ mb: 2 }}
            >
              <Tab value="policies" label={`Policies (${d.policies.length})`} />
              <Tab value="rules" label={`Rules (${d.rules.length})`} />
              <Tab value="exceptions" label={`Exceptions (${d.exceptions.length})`} />
              <Tab value="constraints" label={`Constraints (${d.constraints.length})`} />
            </Tabs>
            {tab === 'policies' &&
              (d.policies.length === 0 ? (
                <Empty>No policies.</Empty>
              ) : (
                <>
                  {d.policies.some((p) => !p.verified) && (
                    <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
                      Unverified means no person has checked the summary against its source. Open
                      it, compare, then confirm it.
                    </Typography>
                  )}
                  <List disablePadding>
                    {d.policies.map((p) => (
                      <ListItem key={p.id} disableGutters divider sx={{ display: 'block' }}>
                        <Stack direction="row" spacing={1} sx={{ alignItems: 'flex-start' }}>
                          <ListItemText
                            primary={<DocLink id={p.id}>{p.title || nameOr('', p.id)}</DocLink>}
                            secondary={`${p.rules} ${p.rules === 1 ? 'rule' : 'rules'}`}
                          />
                          {!p.verified && (
                            <Typography
                              variant="caption"
                              color="text.secondary"
                              sx={{ fontStyle: 'italic', pt: 0.5 }}
                            >
                              unverified
                            </Typography>
                          )}
                        </Stack>
                        <Details>
                          Source: {p.sourceStatus}. How closely the summary follows it: {p.fidelity}
                          .{p.url ? ` From ${p.url}.` : ''}
                        </Details>
                      </ListItem>
                    ))}
                  </List>
                </>
              ))}
            {tab === 'rules' &&
              (d.rules.length === 0 ? (
                <Empty>No rules.</Empty>
              ) : (
                <List disablePadding>
                  {d.rules.map((r) => (
                    <ListItem key={`${r.policy}-${r.id}`} disableGutters divider>
                      <ListItemText
                        primary={r.text}
                        secondary={
                          <DocLink id={r.policy}>
                            {`${r.id} in ${nameOr(r.policyName, r.policy)}`}
                          </DocLink>
                        }
                        slotProps={{ primary: { sx: { overflowWrap: 'anywhere' } } }}
                      />
                    </ListItem>
                  ))}
                </List>
              ))}
            {tab === 'exceptions' &&
              (d.exceptions.length === 0 ? (
                <Empty>No exceptions.</Empty>
              ) : (
                <List disablePadding>
                  {d.exceptions.map((x) => (
                    <ListItem key={x.id} disableGutters divider>
                      <ListItemText
                        primary={<DocLink id={x.id}>{x.title || x.id}</DocLink>}
                        secondary={[
                          x.kind.replace(/_/g, ' '),
                          x.policy && title(x.policy),
                          x.rule,
                          x.reviewBy && `review by ${x.reviewBy}`,
                        ]
                          .filter(Boolean)
                          .join(' - ')}
                      />
                      <StateChip state={x.status} />
                    </ListItem>
                  ))}
                </List>
              ))}
            {tab === 'constraints' &&
              (d.constraints.length === 0 ? (
                <Empty>No constraints.</Empty>
              ) : (
                <List disablePadding>
                  {d.constraints.map((c) => (
                    <ListItem key={c.id} disableGutters divider>
                      <ListItemText
                        primary={<DocLink id={c.id}>{c.says || c.id}</DocLink>}
                        secondary={[
                          `why: ${c.why}`,
                          `by ${c.by}`,
                          c.appliesIn.length > 0 &&
                            `applies in ${c.appliesIn.map(title).join(', ')}`,
                          c.notIn.length > 0 && `not in ${c.notIn.map(title).join(', ')}`,
                          c.reviewBy && `review by ${c.reviewBy}`,
                        ]
                          .filter(Boolean)
                          .join(' - ')}
                        slotProps={{ primary: { sx: { overflowWrap: 'anywhere' } } }}
                      />
                    </ListItem>
                  ))}
                </List>
              ))}
          </>
        )}
      </DataState>
    </>
  )
}
