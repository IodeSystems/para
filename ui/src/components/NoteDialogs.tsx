import { useState } from 'react'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useDoc, useNoteEdit } from '@/api/doc'
import { errorText } from '@/client/gqlClient'
import { Frame, useClose } from '@/components/OverlayFrame'
import type { OverlaySearch } from '@/lib/overlays'

export function NoteDialogs({ s, onDone }: { s: OverlaySearch; onDone: (m: string) => void }) {
  return s.noteedit ? <NoteEditDialog id={s.noteedit} onDone={onDone} /> : null
}

function NoteEditDialog({ id, onDone }: { id: string; onDone: (m: string) => void }) {
  const q = useDoc(id)
  const close = useClose()
  if (!q.data) {
    return (
      <Frame title="Edit note" actions={<Button onClick={close}>Close</Button>}>
        <Typography>{q.isError ? errorText(q.error) : 'Loading'}</Typography>
      </Frame>
    )
  }
  return <NoteEditForm id={id} title={q.data.title} body={q.data.body} onDone={onDone} />
}

function NoteEditForm({
  id,
  title: was,
  body: wasBody,
  onDone,
}: {
  id: string
  title: string
  body: string
  onDone: (m: string) => void
}) {
  const close = useClose()
  const edit = useNoteEdit()
  const [title, setTitle] = useState(was)
  const [body, setBody] = useState(wasBody)
  const changed = title !== was || body !== wasBody
  return (
    <Frame
      title="Edit note"
      onSubmit={() =>
        edit.mutate(
          { id, title, body },
          {
            onSuccess: () => {
              onDone('Note saved')
              close()
            },
          },
        )
      }
      actions={
        <>
          <Button onClick={close}>Cancel</Button>
          <Button
            type="submit"
            variant="contained"
            disabled={edit.isPending || !changed || title.trim() === ''}
          >
            Save
          </Button>
        </>
      }
    >
      <Stack spacing={1.5} sx={{ pt: 1 }}>
        {edit.isError && <Alert severity="error">{errorText(edit.error)}</Alert>}
        <TextField
          label="Title"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          required
          fullWidth
        />
        <TextField
          label="Body"
          value={body}
          onChange={(e) => setBody(e.target.value)}
          multiline
          minRows={8}
          autoFocus
          fullWidth
        />
        <Typography variant="caption" color="text.secondary">
          Change the text yourself. Saved at once.
        </Typography>
      </Stack>
    </Frame>
  )
}
