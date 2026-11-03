import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { Section } from '@/components/Bits'

type Reading = { date: string; value: string; source: string }
type Brief = {
  why?: { text: string } | null
  confidence?: { percent: number; basis?: string | null } | null
  alternatives?: Array<{ name: string }> | null
  unknowns?: Array<{ text: string; link?: string | null }> | null
  signals?: Array<{
    what: string
    expected?: string | null
    everyDays?: number | null
    readings?: Reading[] | null
  }> | null
  pivot?: { condition: string; date?: string | null; action: string } | null
  extensions?: string[] | null
  ecosystem?: { keyHolder?: boolean | null; replaces?: string[] | null } | null
  rank?: number | null
  validation?: string | null
}

function lastReading(r: Reading[] | null | undefined): Reading | undefined {
  return (r ?? []).reduce<Reading | undefined>(
    (best, x) => (best === undefined || x.date >= best.date ? x : best),
    undefined,
  )
}

// GoalBrief shows the brief of a goal in a few lines. A goal without one shows nothing.
export function GoalBrief({ brief }: { brief: Brief | null | undefined }) {
  if (!brief) return null
  const lines: Array<[string, string]> = []
  if (brief.validation) lines.push(['State', brief.validation])
  if (brief.rank) lines.push(['Rank', String(brief.rank)])
  if (brief.why) lines.push(['Why', brief.why.text])
  if (brief.confidence) {
    lines.push([
      'Confidence',
      `${brief.confidence.percent}%${brief.confidence.basis ? ` - ${brief.confidence.basis}` : ''}`,
    ])
  }
  if (brief.alternatives?.length) {
    lines.push(['Alternatives', brief.alternatives.map((a) => a.name).join(', ')])
  }
  for (const u of brief.unknowns ?? []) lines.push(['Unknown', u.text])
  for (const s of brief.signals ?? []) {
    const last = lastReading(s.readings)
    lines.push([
      'Signal',
      `${s.what}: expected ${s.expected || 'not stated'}; ${
        last ? `read ${last.value} on ${last.date}` : 'not read yet'
      }`,
    ])
  }
  if (brief.pivot) {
    lines.push([
      'Pivot',
      `${brief.pivot.action}${brief.pivot.date ? ` by ${brief.pivot.date}` : ''} if ${brief.pivot.condition}`,
    ])
  }
  if (brief.extensions?.length) lines.push(['Extensions', brief.extensions.join('; ')])
  if (brief.ecosystem?.keyHolder) lines.push(['Key holder', 'this goal makes the owner one'])
  if (lines.length === 0) return null
  return (
    <Section title="Brief">
      <Stack spacing={0.5}>
        {lines.map(([k, v], i) => (
          <Typography key={`${k}-${i}`} variant="body2" sx={{ overflowWrap: 'anywhere' }}>
            <strong>{k}:</strong> {v}
          </Typography>
        ))}
      </Stack>
    </Section>
  )
}
