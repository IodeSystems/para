import Typography from '@mui/material/Typography'
import { useIoProposalsCount } from '@/api/output'
import { DataState } from '@/components/DataState'

// ProposalsTab is a placeholder: the proposals review comes in a later node, which replaces this file.
export function ProposalsTab() {
  const q = useIoProposalsCount()
  return (
    <DataState q={q}>
      {(d) => (
        <Typography color="text.secondary">
          Coming next. {d.count} proposal{d.count === 1 ? '' : 's'}
          {d.needsYou > 0 ? `, ${d.needsYou} need you` : ''}.
        </Typography>
      )}
    </DataState>
  )
}
