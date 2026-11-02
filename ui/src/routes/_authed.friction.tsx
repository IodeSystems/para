import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'
import Box from '@mui/material/Box'
import ToggleButton from '@mui/material/ToggleButton'
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup'
import Typography from '@mui/material/Typography'
import TextField from '@mui/material/TextField'
import Stack from '@mui/material/Stack'
import { useFriction } from '@/api/friction'
import { Empty, PageTitle, Section } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { RfxLink } from '@/components/DocLink'

export const Route = createFileRoute('/_authed/friction')({ component: Friction })

const days = (v: number | null | undefined) => (v == null ? 'none yet' : `${v} days`)

function Friction() {
  const [by, setBy] = useState('reason')
  const [since, setSince] = useState('')
  const valid = since === '' || /^\d{4}-\d{2}-\d{2}$/.test(since)
  const q = useFriction(by, valid ? since : '')
  return (
    <>
      <PageTitle subtitle="What slows asks down, by reason, domain, or policy. Never by person.">
        Friction
      </PageTitle>
      <Stack direction="row" spacing={2} sx={{ mb: 2, alignItems: 'center', flexWrap: 'wrap' }}>
        <ToggleButtonGroup
          exclusive
          size="small"
          value={by}
          aria-label="Group by"
          onChange={(_, v: string | null) => v && setBy(v)}
        >
          <ToggleButton value="reason">Reason</ToggleButton>
          <ToggleButton value="domain">Domain</ToggleButton>
          <ToggleButton value="policy">Policy</ToggleButton>
        </ToggleButtonGroup>
        <TextField
          size="small"
          label="Since (YYYY-MM-DD)"
          value={since}
          error={!valid}
          onChange={(e) => setSince(e.target.value)}
        />
      </Stack>
      <DataState q={q}>
        {(r) => (
          <>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
              {r.asks} asks, {r.withFriction} with a stall or redirect.
            </Typography>
            {r.rows.length === 0 && <Empty>Nothing to report.</Empty>}
            {r.rows.map((x) => (
              <Section key={x.key} title={x.name || x.label} count={x.asks.length}>
                <Typography variant="body2">
                  First answer {days(x.firstAnswerDays)}, resolution {days(x.resolutionDays)},{' '}
                  {x.hopsPerAsk} hops per ask, {x.redirects} redirects, {x.stalls} stalls,{' '}
                  {x.stalledDays} stalled days.
                </Typography>
                <Typography variant="body2">
                  {x.policyConflicts} policy conflicts, {x.exceptionsHit} exceptions,{' '}
                  {x.playbookCandidates} repeat asks without a playbook.
                </Typography>
                <Box sx={{ mt: 1 }}>
                  {x.asks.map((id, i) => (
                    <Box key={id}>
                      <RfxLink id={id}>{x.askNames?.[i] || id}</RfxLink>
                    </Box>
                  ))}
                </Box>
              </Section>
            ))}
            {r.repeats.length > 0 && (
              <Section title="Asked more than once, no playbook" count={r.repeats.length}>
                {r.repeats.map((x) => (
                  <Typography key={x.asks.join()} variant="body2">
                    {x.title} ({x.asks.length} times)
                  </Typography>
                ))}
              </Section>
            )}
          </>
        )}
      </DataState>
    </>
  )
}
