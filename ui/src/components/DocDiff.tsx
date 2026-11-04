import type { ReactNode } from 'react'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import MenuItem from '@mui/material/MenuItem'
import Typography from '@mui/material/Typography'
import { useDocDiff } from '@/api/docs'
import { DataState } from '@/components/DataState'
import { DiffBlock } from '@/components/stack/parts'
import { diffLines } from '@/lib/docs'

// DocDiff shows what changed in a tracked document between two versions: removed lines red,
// added green, unchanged runs folded. from 0 and to 0 are the server's defaults (the last
// version seen, and the latest).
export function DocDiff({
  id,
  from = 0,
  to = 0,
  unit = 'line',
}: {
  id: string
  from?: number
  to?: number
  unit?: string
}) {
  const q = useDocDiff(id, from, to, unit)
  return (
    <DataState q={q}>
      {(d) => (
        <>
          <Typography variant="caption" color="text.secondary" component="div" sx={{ mb: 0.5 }}>
            {d.from === 0 ? 'Whole text' : `Version ${d.from}`} to version {d.to}:{' '}
            {d.same ? 'no change' : `${d.added} added, ${d.removed} removed`}
          </Typography>
          {!d.same && (
            <DiffBlock lines={diffLines(d.parts)} label={`Changes from ${d.from} to ${d.to}`} />
          )}
        </>
      )}
    </DataState>
  )
}

// VersionPick is a select of versions, with "nothing" as 0 when allowed.
export function VersionPick({
  label,
  value,
  latest,
  zero,
  onChange,
}: {
  label: string
  value: number
  latest: number
  zero?: string
  onChange: (v: number) => void
}) {
  const opts = Array.from({ length: latest }, (_, i) => i + 1)
  return (
    <TextField
      select
      size="small"
      label={label}
      value={value}
      onChange={(e) => onChange(Number(e.target.value))}
      sx={{ minWidth: 120 }}
    >
      {zero !== undefined && <MenuItem value={0}>{zero}</MenuItem>}
      {opts.map((v) => (
        <MenuItem key={v} value={v}>
          Version {v}
        </MenuItem>
      ))}
    </TextField>
  )
}

export function PickRow({ children }: { children: ReactNode }) {
  return (
    <Stack direction="row" spacing={1} useFlexGap sx={{ mb: 1, flexWrap: 'wrap' }}>
      {children}
    </Stack>
  )
}
