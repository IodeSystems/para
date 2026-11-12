import Typography from '@mui/material/Typography'

// A state line older than this is stale (health: project_state_stale).
export const STALE_DAYS = 14

// ageWords is how old a state line is, from the server's day count (-1: no date).
export function ageWords(days: number): string {
  if (days < 0) return 'no date'
  if (days === 0) return 'today'
  if (days === 1) return '1 day ago'
  return `${days} days ago`
}

// nextCron is the earliest next run among the bundle's cron lines ("<id> <recur>: makes ...; next
// <date>; ..."), or '' for none.
export function nextCron(crons: readonly string[]): string {
  const dates = crons.flatMap((c) => {
    const m = /; next (\d{4}-\d{2}-\d{2})/.exec(c)
    return m?.[1] ? [m[1]] : []
  })
  return dates.sort()[0] ?? ''
}

// StateLine shows the state line and how old it is.
export function StateLine({ line, ageDays }: { line: string | null | undefined; ageDays: number }) {
  const text = (line ?? '').trim()
  const stale = text !== '' && ageDays > STALE_DAYS
  return (
    <Typography
      sx={{ fontSize: '1rem', overflowWrap: 'anywhere' }}
      color={text ? 'text.primary' : 'text.secondary'}
    >
      {text || 'No state line yet.'}
      {text !== '' && (
        <Typography
          component="span"
          color={stale ? 'warning.main' : 'text.secondary'}
          sx={{ fontSize: '1rem' }}
        >
          {' - '}
          {ageWords(ageDays)}
          {stale && ', stale'}
        </Typography>
      )}
    </Typography>
  )
}
