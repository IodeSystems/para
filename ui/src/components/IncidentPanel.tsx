import Stack from '@mui/material/Stack'
import { useAreas, useIncident } from '@/api/areas'
import { Section, StateChip, StatusChip } from '@/components/Bits'
import { PointerLinks } from '@/components/PointerLinks'
import { LinkButton } from '@/components/links'
import { TaskRows } from '@/components/TaskRows'
import { nameOr } from '@/lib/names'
import { toneOfSeverity } from '@/lib/status'
import { humanTime } from '@/lib/when'

// IncidentPanel is what an incident has beyond its text: severity, status, start and end, area,
// links, and follow-up tasks. An incident is a task of kind incident since vault format 10; the
// document page shows it (with the timeline in the body), for either form.
export function IncidentPanel({ id }: { id: string }) {
  const q = useIncident(id)
  const areas = useAreas()
  if (!q.data) return null
  const i = q.data.incident
  return (
    <div data-incident={id}>
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
      <PointerLinks links={i.links} />
      <Section title="Follow-ups" count={q.data.followUps.length} hideEmpty>
        <TaskRows tasks={q.data.followUps} />
      </Section>
    </div>
  )
}
