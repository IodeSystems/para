import { describe, expect, it } from 'vitest'
import { badNotesLine, notesDirty, staleRefusal } from './notes'

describe('notes', () => {
  it('reads a stale refusal', () => {
    expect(
      staleRefusal(
        'notes_save: the notes of task-a changed since you read them (current hash sha256:ab): read them again',
      ),
    ).toBe(true)
    expect(staleRefusal('notes_save: task-a has no notes now: retry without hash')).toBe(true)
    expect(staleRefusal('notes_save: not found')).toBe(false)
  })
  it('finds a line that would end the section', () => {
    expect(badNotesLine('ok\n### fine')).toBeNull()
    expect(badNotesLine('ok\n## Split')).toBe('## Split')
  })
  it('compares drafts by text', () => {
    expect(notesDirty(' a \n', 'a')).toBe(false)
    expect(notesDirty('a', 'b')).toBe(true)
  })
})
