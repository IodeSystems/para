import { useState } from 'react'
import { useRouteContext } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import { useMerges, useNotes, useSaveNotes } from '@/api/notes'
import { canDecide } from '@/client/session'
import { errorText } from '@/client/gqlClient'
import { Empty, Section } from '@/components/Bits'
import { DocLink } from '@/components/DocLink'
import { Markdown } from '@/components/Markdown'
import { badNotesLine, notesDirty, staleRefusal } from '@/lib/notes'

// NotesEditor shows the ## Notes of any item. With review:decide the owner edits them: a save
// names the notes it was based on, and when they changed since, the server refuses it, the page
// shows the current notes, and the next save is based on them. The draft is kept.
export function NotesEditor({ id }: { id: string }) {
  const { session } = useRouteContext({ from: '/_authed' })
  const decide = canDecide(session)
  const q = useNotes(id)
  const save = useSaveNotes(id)
  const [draft, setDraft] = useState<string | null>(null)
  const [base, setBase] = useState<string | null>(null)
  const [stale, setStale] = useState<string | null>(null)
  if (q.isError) return null
  const notes = q.data
  const editing = draft !== null
  const bad = editing ? badNotesLine(draft) : null
  const start = () => {
    setDraft(notes?.md ?? '')
    setBase(notes?.hash ?? '')
    setStale(null)
  }
  const submit = async () => {
    if (draft === null) return
    try {
      await save.mutateAsync({ hash: base ?? '', md: draft })
      setDraft(null)
      setStale(null)
    } catch (err) {
      const msg = errorText(err)
      if (!staleRefusal(msg)) return
      const fresh = await q.refetch()
      setStale(fresh.data?.md ?? '')
      setBase(fresh.data?.hash ?? '')
    }
  }
  return (
    <Section
      title="Notes"
      action={
        decide && !editing ? (
          <Button size="small" variant="outlined" onClick={start} data-notes-act="edit">
            Edit notes
          </Button>
        ) : undefined
      }
    >
      <div data-notes={id}>
        {!editing && (notes?.md ? <Markdown source={notes.md} /> : <Empty>No notes yet.</Empty>)}
        {editing && (
          <Stack spacing={1}>
            {stale !== null && (
              <Alert severity="warning" data-notes-stale>
                The notes changed since you opened them. They now read:
                <Markdown source={stale || '(none)'} />
                Your text is below. Save again to replace them with it.
              </Alert>
            )}
            <TextField
              label="Notes (markdown)"
              multiline
              minRows={4}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              error={!!bad}
              helperText={bad ? `A line may not start with # or ##: ${bad}` : ' '}
            />
            {save.isError && !staleRefusal(errorText(save.error)) && (
              <Alert severity="error">{errorText(save.error)}</Alert>
            )}
            <Stack direction="row" spacing={1}>
              <Button
                size="small"
                variant="contained"
                onClick={() => void submit()}
                disabled={
                  !!bad || save.isPending || (stale === null && !notesDirty(draft, notes?.md ?? ''))
                }
                data-notes-act="save"
              >
                Save notes
              </Button>
              <Button size="small" onClick={() => setDraft(null)} data-notes-act="cancel">
                Cancel
              </Button>
            </Stack>
          </Stack>
        )}
      </div>
    </Section>
  )
}

// MergedFrom says what was merged into an item, and on a merged one what it was merged into.
export function MergedFrom({ id }: { id: string }) {
  const q = useMerges(id)
  const m = q.data
  if (!m) return null
  return (
    <>
      {m.duplicateOf && (
        <Alert severity="info" sx={{ mb: 2 }} data-merged-into={m.duplicateOf}>
          Merged into <DocLink id={m.duplicateOf} />. It is left out of lists and search.
        </Alert>
      )}
      {m.archived && !m.duplicateOf && (
        <Alert severity="info" sx={{ mb: 2 }} data-archived>
          Archived. It is left out of lists and search.
        </Alert>
      )}
      {m.mergedFrom.length > 0 && (
        <Section title="Merged from" count={m.mergedFrom.length}>
          <Stack spacing={0.5} data-merged-from>
            {m.mergedFrom.map((x) => (
              <DocLink key={x.id} id={x.id}>
                {x.title || x.id}
              </DocLink>
            ))}
          </Stack>
        </Section>
      )}
    </>
  )
}
