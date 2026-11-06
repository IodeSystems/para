import MenuItem from '@mui/material/MenuItem'
import TextField from '@mui/material/TextField'
import { useSetPriority, type Priority } from '@/api/priority'
import { priorityHelp } from '@/lib/priority'

// PrioritySelect shows and sets how much a person, a group, or a source matters. value is the
// document's own priority (null: unset). inherited, for a person, is what they take from their
// groups when unset.
export function PrioritySelect({
  id,
  value,
  inherited,
}: {
  id: string
  value?: string | null
  inherited?: string | null
}) {
  const set = useSetPriority()
  const current: Priority = (value as Priority | null | undefined) || 'unset'
  return (
    <TextField
      select
      size="small"
      label="Priority"
      value={current}
      disabled={set.isPending}
      error={set.isError}
      helperText={set.isError ? 'The server refused the change.' : priorityHelp(current, inherited)}
      onChange={(e) => set.mutate({ id, priority: e.target.value as Priority })}
      sx={{ minWidth: 160, mb: 2 }}
      slotProps={{ htmlInput: { 'aria-label': 'Priority' } }}
    >
      <MenuItem value="unset">Not set</MenuItem>
      <MenuItem value="high">High</MenuItem>
      <MenuItem value="normal">Normal</MenuItem>
      <MenuItem value="low">Low</MenuItem>
    </TextField>
  )
}
