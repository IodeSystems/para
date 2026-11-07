import type { ReactNode } from 'react'
import Tooltip from '@mui/material/Tooltip'
import EditNoteIcon from '@mui/icons-material/EditNote'

// DraftHint shows that a button takes what the owner typed in the item's composer: put NoteIcon on
// the button, and the tooltip says what happens to the words (lib/composer draftHints). With no
// hint the button is as it is. The span keeps the tooltip working while the button is disabled.
export function DraftHint({ hint, children }: { hint: string | null; children: ReactNode }) {
  if (!hint) return children
  return (
    <Tooltip title={hint}>
      <span style={{ display: 'inline-flex' }}>{children}</span>
    </Tooltip>
  )
}

export function NoteIcon() {
  return <EditNoteIcon fontSize="small" />
}
