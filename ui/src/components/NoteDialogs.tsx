import { useState } from 'react'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import MenuItem from '@mui/material/MenuItem'
import { useDoc, useDocEdit, useDocFields, useNoteEdit } from '@/api/doc'
import { errorText } from '@/client/gqlClient'
import { Frame, useClose } from '@/components/OverlayFrame'
import type { OverlaySearch } from '@/lib/overlays'

export function NoteDialogs({ s, onDone }: { s: OverlaySearch; onDone: (m: string) => void }) {
  if (s.docedit) return <DocEditDialog id={s.docedit} onDone={onDone} />
  return s.noteedit ? <NoteEditDialog id={s.noteedit} onDone={onDone} /> : null
}

type EditField = {
  name: string
  kind: string
  enum?: string[] | null
  required?: boolean | null
  value: string
}

// fieldValue reads the text of a field back into the value the server takes.
function fieldValue(f: EditField, v: string): unknown {
  const t = v.trim()
  if (t === '') return null
  switch (f.kind) {
    case 'bool':
      return t === 'true'
    case 'count':
      return Number(t)
    case 'list':
    case 'links':
    case 'contacts':
    case 'pointers':
    case 'targets':
    case 'sources':
    case 'source':
      return JSON.parse(t)
    default:
      return t
  }
}

function DocEditDialog({ id, onDone }: { id: string; onDone: (m: string) => void }) {
  const q = useDoc(id)
  const fq = useDocFields(id)
  const close = useClose()
  if (!q.data || !fq.data) {
    return (
      <Frame title="Edit" actions={<Button onClick={close}>Close</Button>}>
        <Typography>
          {q.isError || fq.isError ? errorText(q.error ?? fq.error) : 'Loading'}
        </Typography>
      </Frame>
    )
  }
  return (
    <DocEditForm
      id={id}
      rev={q.data.rev ?? ''}
      title={q.data.title}
      body={q.data.body}
      fields={fq.data.fields}
      onDone={onDone}
    />
  )
}

function DocEditForm({
  id,
  rev,
  title: was,
  body: wasBody,
  fields,
  onDone,
}: {
  id: string
  rev: string
  title: string
  body: string
  fields: EditField[]
  onDone: (m: string) => void
}) {
  const close = useClose()
  const edit = useDocEdit()
  const [title, setTitle] = useState(was)
  const [body, setBody] = useState(wasBody)
  const [vals, setVals] = useState<Record<string, string>>(
    Object.fromEntries(fields.map((f) => [f.name, f.value])),
  )
  const [bad, setBad] = useState('')
  const changedFields = fields.filter((f) => vals[f.name] !== f.value)
  const changed = title !== was || body !== wasBody || changedFields.length > 0
  return (
    <Frame
      title="Edit"
      onSubmit={() => {
        const out: Record<string, unknown> = {}
        try {
          for (const f of changedFields) out[f.name] = fieldValue(f, vals[f.name] ?? '')
        } catch {
          setBad('A list or map field must be valid JSON.')
          return
        }
        setBad('')
        edit.mutate(
          { id, title, body, rev, fields: out },
          {
            onSuccess: () => {
              onDone('Saved')
              close()
            },
          },
        )
      }}
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
        {bad && <Alert severity="error">{bad}</Alert>}
        {edit.isError && <Alert severity="error">{errorText(edit.error)}</Alert>}
        <TextField
          label="Title"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          required
          fullWidth
        />
        {fields.map((f) => (
          <TextField
            key={f.name}
            label={f.name}
            select={!!f.enum?.length}
            value={vals[f.name] ?? ''}
            onChange={(e) => setVals({ ...vals, [f.name]: e.target.value })}
            multiline={!f.enum?.length && !['string', 'date', 'timestamp', 'link'].includes(f.kind)}
            helperText={f.required ? 'required' : undefined}
            fullWidth
          >
            {f.enum?.length
              ? [...(f.required ? [] : ['']), ...f.enum].map((v) => (
                  <MenuItem key={v} value={v}>
                    {v || 'none'}
                  </MenuItem>
                ))
              : undefined}
          </TextField>
        ))}
        <TextField
          label="Body"
          value={body}
          onChange={(e) => setBody(e.target.value)}
          multiline
          minRows={8}
          fullWidth
        />
        <Typography variant="caption" color="text.secondary">
          Change the text yourself. Saved at once. If the document changed since you opened it, the
          save is refused.
        </Typography>
      </Stack>
    </Frame>
  )
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
