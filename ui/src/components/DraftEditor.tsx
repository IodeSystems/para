import { useState } from 'react'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import FormControlLabel from '@mui/material/FormControlLabel'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import { useIOEdit } from '@/api/io'
import { errorText } from '@/client/gqlClient'
import { draftEditBody } from '@/lib/draft'

// DraftEditor is the owner's edit of an outgoing draft's text. Save keeps the text; an approved
// draft becomes a draft again. "and approve" (off) makes the same click approve the text it saves.
// approved says the draft is approved now. onSaved runs after a save and says whether it approved.
export function DraftEditor({
  id,
  text,
  approved,
  onSaved,
  onCancel,
}: {
  id: string
  text: string
  approved: boolean
  onSaved: (approvedNow: boolean) => void
  onCancel: () => void
}) {
  const edit = useIOEdit()
  const [draft, setDraft] = useState(text)
  const [approve, setApprove] = useState(false)
  const body = draftEditBody(id, text, draft, approve)
  const save = () => {
    if (!body) return
    edit.mutate(body, { onSuccess: () => onSaved(approve) })
  }
  return (
    <Stack spacing={1} data-draft-editor={id} sx={{ my: 1 }}>
      <TextField
        multiline
        fullWidth
        minRows={4}
        size="small"
        label="Message text"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        slotProps={{ htmlInput: { 'data-draft-text': '' } }}
        helperText={
          approved && draft !== text && !approve
            ? 'Saving this takes the approval back: approve the new text again.'
            : undefined
        }
      />
      <Stack
        direction="row"
        spacing={0.5}
        useFlexGap
        sx={{ flexWrap: 'wrap', alignItems: 'center' }}
      >
        <Button
          size="small"
          variant="contained"
          data-act="draft-save"
          disabled={edit.isPending || !body}
          onClick={save}
        >
          Save
        </Button>
        <FormControlLabel
          sx={{ mr: 1 }}
          control={
            <Checkbox
              size="small"
              checked={approve}
              onChange={(e) => setApprove(e.target.checked)}
              slotProps={{ input: { 'data-draft-approve': '' } as object }}
            />
          }
          label="and approve"
        />
        <Button size="small" data-act="draft-cancel" disabled={edit.isPending} onClick={onCancel}>
          Cancel
        </Button>
      </Stack>
      {edit.isError && <Alert severity="error">{errorText(edit.error)}</Alert>}
    </Stack>
  )
}
