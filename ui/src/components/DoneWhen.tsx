import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { LinkButton } from '@/components/links'

// DoneWhen shows how we know a task or a goal is done. Without one it asks, quietly, and offers
// the edit. `param` is the overlay that opens the edit dialog.
export function DoneWhen({
  value,
  param,
  id,
  prompt,
}: {
  value: string | null | undefined
  param: 'taskdw' | 'goaldw'
  id: string
  prompt: string
}) {
  const text = (value ?? '').trim()
  return (
    <Stack direction="row" spacing={1} sx={{ mb: 2, alignItems: 'baseline', flexWrap: 'wrap' }}>
      {text ? (
        <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
          Done when: {text}
        </Typography>
      ) : (
        <Typography variant="body2" color="text.secondary">
          {prompt}
        </Typography>
      )}
      <LinkButton
        size="small"
        to="."
        search={(p) => ({ ...p, [param]: id })}
        aria-label={text ? 'Edit done when' : 'Set done when'}
      >
        {text ? 'Edit' : 'Set'}
      </LinkButton>
    </Stack>
  )
}
