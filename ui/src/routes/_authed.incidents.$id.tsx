import { createFileRoute } from '@tanstack/react-router'
import Stack from '@mui/material/Stack'
import { useAreas, useIncident } from '@/api/areas'
import { Empty, PageTitle, Section, StateChip, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { PointerLinks } from '@/components/PointerLinks'
import { LinkButton } from '@/components/links'
import { Markdown } from '@/components/Markdown'
import { dropHeading } from '@/lib/markdown'
import { nameOr } from '@/lib/names'
import { humanTime } from '@/lib/when'
import { toneOfSeverity } from '@/lib/status'
import { TaskRows } from '@/components/TaskRows'

export const Route = createFileRoute('/_authed/incidents/$id')({ component: IncidentPage })

function IncidentPage() {
  const { id } = Route.useParams()
  const q = useIncident(id)
  const areas = useAreas()
  return (
    <DataState q={q}>
      {(d) => {
        const i = d.incident
        return (
          <>
            <PageTitle>{i.title || id}</PageTitle>
            <Stack direction="row" spacing={1} useFlexGap sx={{ mb: 2, flexWrap: 'wrap' }}>
              <StatusChip tone={toneOfSeverity(i.severity)} label={i.severity} />
              <StateChip state={i.status} tone={i.status === 'open' ? 'attn' : undefined} />
              <StatusChip tone="quiet" label={`started ${humanTime(i.started)}`} />
              {i.ended && <StatusChip tone="quiet" label={`ended ${humanTime(i.ended)}`} />}
              {i.area && (
                <LinkButton size="small" to="/areas/$id" params={{ id: i.area }}>
                  area {nameOr(areas.data?.find((a) => a.id === i.area)?.title, i.area)}
                </LinkButton>
              )}
            </Stack>
            <Section title="Timeline">
              {d.body.trim() === '' ? (
                <Empty>No entry yet.</Empty>
              ) : (
                <Markdown source={dropHeading(d.body, 'Timeline')} />
              )}
            </Section>
            <PointerLinks links={i.links} />
            <Section title="Follow-ups" count={d.followUps.length} hideEmpty>
              <TaskRows tasks={d.followUps} />
            </Section>
          </>
        )
      }}
    </DataState>
  )
}
