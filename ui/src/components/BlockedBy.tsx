import Chip from '@mui/material/Chip'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { StatusChip } from '@/components/Bits'
import { TextLink } from '@/components/links'
import { blockerLink, blockerTone, type BlockerLike } from '@/lib/blockers'

// BlockedBy is the row that says what an item waits on before the owner can act: per open blocker
// an outlined chip with its type (output, input, task, Ask, question), a chip with its state
// (unapproved, waiting for reply, open), and its title as a link to its page. Nothing shows when
// nothing blocks the item.
export function BlockedBy({ blockers }: { blockers: readonly BlockerLike[] | null | undefined }) {
  if (!blockers?.length) return null
  return (
    <Stack
      direction="row"
      spacing={0.5}
      useFlexGap
      data-blocked-by
      sx={{ flexWrap: 'wrap', alignItems: 'center', my: 0.5 }}
    >
      <Typography variant="body2" color="text.secondary" component="span">
        Blocked by
      </Typography>
      {blockers.map((b) => (
        <Stack
          key={b.id}
          direction="row"
          spacing={0.5}
          useFlexGap
          data-blocker={b.id}
          sx={{ alignItems: 'center', flexWrap: 'wrap', minWidth: 0 }}
        >
          <Chip size="small" variant="outlined" label={b.type} />
          <StatusChip tone={blockerTone(b.state)} label={b.state} />
          <TextLink {...blockerLink(b)} underline="hover" sx={{ overflowWrap: 'anywhere' }}>
            {b.title}
          </TextLink>
        </Stack>
      ))}
    </Stack>
  )
}
