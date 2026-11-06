import { createFileRoute } from '@tanstack/react-router'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Typography from '@mui/material/Typography'
import { EXPIRING_DAYS, useAreasPage } from '@/api/areas'
import { CHIP_ROW, Empty, PageTitle, Section, StateChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { TextLink } from '@/components/links'
import { nameOr } from '@/lib/names'

// Areas, with what was the Policy page: the areas that follow a source (a policy and its
// summary), and the exceptions and constraints in any area due for review. An exception or a
// convention lives inline in its area's page, under the section it is about.
export const Route = createFileRoute('/_authed/areas/')({ component: Areas })

function AreaLink({ id, title }: { id: string; title?: string | null }) {
  return (
    <TextLink to="/areas/$id" params={{ id }} underline="hover">
      {nameOr(title, id)}
    </TextLink>
  )
}

function Areas() {
  const q = useAreasPage()
  return (
    <>
      <PageTitle subtitle="Areas you keep up, the policies they follow, and their exceptions and conventions.">
        Areas
      </PageTitle>
      <DataState q={q}>
        {(d) => {
          const followed = new Set(d.followed.map((p) => p.id))
          return (
            <>
              <Section title="Due for review" count={d.expiring.length} hideEmpty>
                <List dense disablePadding data-expiring>
                  {d.expiring.map((a) => (
                    <ListItem key={a.id} disableGutters sx={CHIP_ROW}>
                      <ListItemText
                        primary={<AreaLink id={a.area} title={a.areaTitle} />}
                        secondary={[
                          a.kind,
                          a.text.split('\n')[0],
                          a.reviewBy &&
                            (a.daysLeft != null && a.daysLeft < 0
                              ? `review was due ${a.reviewBy}`
                              : `review by ${a.reviewBy}`),
                        ]
                          .filter(Boolean)
                          .join(' - ')}
                        slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
                      />
                    </ListItem>
                  ))}
                </List>
                <Typography variant="caption" color="text.secondary">
                  Exceptions and constraints whose review date is past or within {EXPIRING_DAYS}{' '}
                  days.
                </Typography>
              </Section>
              <Section title="Follow a source" count={d.followed.length} hideEmpty>
                <List dense disablePadding>
                  {d.followed.map((p) => (
                    <ListItem key={p.id} disableGutters sx={CHIP_ROW}>
                      <ListItemText
                        primary={<AreaLink id={p.id} title={p.title} />}
                        secondary={`${p.rules} ${p.rules === 1 ? 'rule' : 'rules'} - source ${p.sourceStatus}${p.verified ? '' : ' - summary not checked yet'}`}
                      />
                      {!p.verified && <StateChip state="unverified" />}
                    </ListItem>
                  ))}
                </List>
              </Section>
              <Section title="Areas" count={d.areas.length}>
                {d.areas.length === 0 ? (
                  <Empty>No areas.</Empty>
                ) : (
                  <List dense disablePadding>
                    {d.areas
                      .filter((a) => !followed.has(a.id))
                      .map((a) => (
                        <ListItem key={a.id} disableGutters sx={CHIP_ROW}>
                          <ListItemText
                            primary={<AreaLink id={a.id} title={a.title} />}
                            secondary={[
                              a.kind === 'concept' ? 'Concept' : 'Project',
                              a.cadence && `review ${a.cadence}`,
                              a.nextReview && `next ${a.nextReview}`,
                            ]
                              .filter(Boolean)
                              .join(' - ')}
                          />
                          <StateChip state={a.status} />
                        </ListItem>
                      ))}
                  </List>
                )}
              </Section>
            </>
          )
        }}
      </DataState>
    </>
  )
}
