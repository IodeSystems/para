import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { Section } from '@/components/Bits'
import { DocLink } from '@/components/DocLink'

// RulesInEffect lists the conventions and constraints that apply to an item, with the scope
// each one came through. Read only.
export function RulesInEffect({ rules }: { rules: { id: string; title: string; via: string }[] }) {
  return (
    <Section title="Rules in effect" count={rules.length} hideEmpty>
      <Stack spacing={0.5}>
        {rules.map((r) => (
          <Typography key={r.id} variant="body2" sx={{ overflowWrap: 'anywhere' }}>
            <DocLink id={r.id}>{r.title || r.id}</DocLink> (via <DocLink id={r.via} />)
          </Typography>
        ))}
      </Stack>
    </Section>
  )
}
