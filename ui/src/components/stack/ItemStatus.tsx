import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Link from '@mui/material/Link'
import Typography from '@mui/material/Typography'
import { StatusChip } from '@/components/Bits'
import { DocLink } from '@/components/DocLink'
import { isOutward } from '@/lib/proposal'
import { humanTime } from '@/lib/when'
import { Row, type Item } from '@/components/stack/parts'

// ItemStatus says what became of a decided item: who decided, who has it now, where the result
// is, and why it failed.
export function ItemStatus({ it }: { it: Item }) {
  const outward = isOutward(it.kind)
  const dec = it.decided
  return (
    <Box aria-label="Status">
      {dec && (
        <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
          {dec.decision.replace(/_/g, ' ')} by {dec.by}, {humanTime(dec.at)}
          {dec.surface ? ` (${dec.surface})` : ''}{' '}
          {dec.selfReported && <StatusChip tone="wait" label="self-reported" />}
        </Typography>
      )}
      {dec?.ownerSaid && (
        <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
          Owner said: &ldquo;{dec.ownerSaid}&rdquo;
        </Typography>
      )}
      {it.claimedBy && (
        <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
          {it.claimedBy} has it
          {it.leaseUntil ? `, until ${humanTime(it.leaseUntil)}` : ''}.
        </Typography>
      )}
      {it.reason && it.state !== 'failed' && <Row label="Reason" text={it.reason} />}
      {it.state === 'approved' && outward && !it.execution && (
        <Alert severity="warning" sx={{ my: 1 }}>
          Approved. An agent has not done it yet.
        </Alert>
      )}
      {it.state === 'approved' && !outward && !it.result && (
        <Alert severity="info" sx={{ my: 1 }}>
          {it.waits ? `Approved. Runs after ${it.after}.` : 'Approved.'}
        </Alert>
      )}
      {it.state === 'failed' || it.state === 'halted' ? (
        <Alert severity="error" sx={{ my: 1 }}>
          {it.state === 'halted' ? 'Stopped' : 'Failed'}
          {it.reason || it.result ? `: ${it.reason || it.result}` : '.'}
        </Alert>
      ) : (
        it.state === 'done' &&
        it.result && (
          <Alert severity="success" sx={{ my: 1 }}>
            {it.resultName ? (
              <>
                Done. {outward ? '' : 'Made '}
                <DocLink id={it.result}>{it.resultName}</DocLink>.
              </>
            ) : (
              <>Done. {it.result}</>
            )}
          </Alert>
        )
      )}
      {it.execution && (
        <Box aria-label="Execution">
          <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
            {it.execution.result} by {it.execution.by}, {humanTime(it.execution.at)}{' '}
            {it.execution.selfReported ? (
              <StatusChip tone="wait" label="self-reported" />
            ) : (
              <StatusChip tone="good" label="recorded by a hook" />
            )}
          </Typography>
          {it.execution.url && (
            <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
              <Link href={it.execution.url} target="_blank" rel="noreferrer noopener">
                {it.execution.url}
              </Link>
            </Typography>
          )}
          <Row label="Where" text={it.execution.locator} />
          <Row label="Receipt" text={it.execution.receipt} />
        </Box>
      )}
    </Box>
  )
}
