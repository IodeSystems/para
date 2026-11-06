import { readNotes, saveNotes } from '@/api/notes'
import { localDate } from '@/api/today'
import { closeAny, investigate } from '@/api/verbs'
import { agentUnsend } from '@/api/composer'
import { revertReceipt } from '@/api/receipts'
import { uploadAttachment } from '@/client/upload'
import { closeBody, type DoneKind } from '@/lib/agenda'
import { notesWithEntry } from '@/lib/composer'

// Done and Investigate with the owner's words: the words and files go into the item's notes as a
// dated entry (as Save as notes does), then the item is closed or sent to investigate. Undo takes
// both back: the close is reverted or the agent work cancelled, then the entry comes out, only
// while the notes did not change since. No words and no files: no note.

type Note = { before: string; hash: string } | null

async function addNote(id: string, text: string, files: File[]): Promise<Note> {
  if (!text.trim() && files.length === 0) return null
  const atts = []
  for (const f of files) atts.push(await uploadAttachment(id, f))
  const before = await readNotes(id)
  const md = notesWithEntry(
    before.md,
    localDate(),
    text,
    atts.map((a) => a.embed),
  )
  const saved = await saveNotes(id, before.hash, md)
  return { before: before.md, hash: saved.hash }
}

// takeBack undoes the act, then takes the note out. It checks the notes first, so a refused undo
// changes nothing.
function takeBack(id: string, note: Note, act: () => Promise<void>) {
  return async () => {
    const now = note ? await readNotes(id) : null
    if (note && now && now.hash !== note.hash)
      throw new Error('the notes changed since; edit them by hand')
    await act()
    if (note && now) await saveNotes(id, now.hash, note.before)
  }
}

export type Noted = { noted: boolean; undo?: () => Promise<void> }

// doneWithNote closes an item after its note. reason is a drop's or a resolve's words, which close
// keeps too. undo is missing when the close left no receipt.
export async function doneWithNote(p: {
  id: string
  done: DoneKind
  text: string
  files: File[]
  reason?: string
}): Promise<Noted> {
  const note = await addNote(p.id, p.text, p.files)
  const r = await closeAny(closeBody(p.done, p.id, p.done === 'meeting' ? '' : (p.reason ?? '')))
  const receipt = r.receipt
  return {
    noted: !!note,
    undo: receipt ? takeBack(p.id, note, () => revertReceipt(receipt).then(() => {})) : undefined,
  }
}

// investigateWithNote sends an item to investigate after its note, with the words as the hint.
// When the item already had open agent work the hint went into it, and there is no undo.
export async function investigateWithNote(p: {
  id: string
  text: string
  files: File[]
}): Promise<Noted & { appended: boolean; request: string }> {
  const note = await addNote(p.id, p.text, p.files)
  const r = await investigate({ id: p.id, hint: p.text.trim() || undefined })
  return {
    noted: !!note,
    appended: r.appended,
    request: r.request,
    undo: r.appended ? undefined : takeBack(p.id, note, () => agentUnsend(r.request)),
  }
}
