import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { Section } from '@/components/Bits'

type Risk = {
  text: string
  likelihood?: string | null
  impact?: string | null
  owner?: string | null
  mitigation?: string | null
  source?: string | null
}

type Assumption = {
  text: string
  source?: string | null
  checked?: string | null
  by?: string | null
  status?: string | null
  fact?: string | null
  fromBody?: boolean | null
  stale?: boolean | null
}

function Line({ children }: { children: React.ReactNode }) {
  return (
    <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
      {children}
    </Typography>
  )
}

// Risks lists the risks of a goal's brief. Read only.
export function Risks({ risks }: { risks: Risk[] | null | undefined }) {
  const list = risks ?? []
  return (
    <Section title="Risks" count={list.length} hideEmpty>
      <Stack spacing={0.5}>
        {list.map((r) => (
          <Line key={r.text}>
            <strong>{r.text}</strong>
            {` - likelihood ${r.likelihood || 'not stated'}, impact ${r.impact || 'not stated'}`}
            {`; owner ${r.owner || 'none'}`}
            {`; mitigation ${r.mitigation || 'none'}`}
            {r.source ? ` (source: ${r.source})` : ''}
          </Line>
        ))}
      </Stack>
    </Section>
  )
}

function checkedText(a: Assumption): string {
  if (!a.checked) return 'never checked'
  const by = a.by ? ` by ${a.by}` : ''
  const stale = a.stale ? ', due for a recheck' : ''
  return `${a.status || 'checked'} on ${a.checked}${by}${stale}`
}

// Assumptions lists what a goal or a task takes as true, with the last check. Read only.
export function Assumptions({ list }: { list: Assumption[] | null | undefined }) {
  const items = list ?? []
  return (
    <Section title="Assumptions" count={items.length} hideEmpty>
      <Stack spacing={0.5}>
        {items.map((a) => (
          <Line key={a.text}>
            {a.text}
            {` - ${checkedText(a)}`}
            {a.fact ? `; now: ${a.fact}` : ''}
            {a.source ? ` (source: ${a.source})` : ''}
          </Line>
        ))}
      </Stack>
    </Section>
  )
}
