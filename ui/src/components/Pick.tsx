import MenuItem from '@mui/material/MenuItem'
import TextField from '@mui/material/TextField'
import { nameOr } from '@/lib/names'

export type Option = { id: string; title: string }

// Pick chooses one document by name. The value is the id.
export function Pick({
  label,
  value,
  onChange,
  options,
  none,
  required,
}: {
  label: string
  value: string
  onChange: (v: string) => void
  options: Option[]
  none?: string
  required?: boolean
}) {
  return (
    <TextField
      select
      label={label}
      value={value}
      required={required}
      onChange={(e) => onChange(e.target.value)}
    >
      {none !== undefined && <MenuItem value="">{none}</MenuItem>}
      {options.map((o) => (
        <MenuItem key={o.id} value={o.id}>
          {o.title || nameOr('', o.id)}
        </MenuItem>
      ))}
      {options.length === 0 && none === undefined && (
        <MenuItem value="" disabled>
          Nothing to pick yet
        </MenuItem>
      )}
    </TextField>
  )
}
