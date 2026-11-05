import { describe, expect, it } from 'vitest'
import {
  attachmentKind,
  attachmentSrc,
  canSend,
  composerKey,
  composerPrompt,
  closable,
  fileLabel,
  isAttachmentPath,
  notesWithEntry,
} from './composer'

describe('composer keys', () => {
  it('sends on Enter, not on Shift-Enter or while composing', () => {
    expect(composerKey({ key: 'Enter', shiftKey: false })).toBe('send')
    expect(composerKey({ key: 'Enter', shiftKey: true })).toBeNull()
    expect(composerKey({ key: 'Enter', shiftKey: false, isComposing: true })).toBeNull()
    expect(composerKey({ key: 'a', shiftKey: false })).toBeNull()
  })
})

describe('send rules', () => {
  it('needs words or a file, and nothing in flight', () => {
    expect(canSend('', 0, false)).toBe(false)
    expect(canSend('  \n ', 0, false)).toBe(false)
    expect(canSend('Met Sam.', 0, false)).toBe(true)
    expect(canSend('', 1, false)).toBe(true)
    expect(canSend('Met Sam.', 1, true)).toBe(false)
  })
})

describe('notes entry', () => {
  it('adds a dated entry below the notes there', () => {
    expect(
      notesWithEntry('Old.\n', '2026-11-05', '## Decided\nShip.', ['![Image attachment a](x)']),
    ).toBe('Old.\n\n**2026-11-05**\n\n**Decided**\nShip.\n\n![Image attachment a](x)')
    expect(notesWithEntry('', '2026-11-05', 'Met.', [])).toBe('**2026-11-05**\n\nMet.')
    expect(notesWithEntry('', '2026-11-05', '', ['e'])).toBe('**2026-11-05**\n\ne')
  })
})

describe('what an item asks', () => {
  const now = new Date('2026-11-05T12:00:00Z')
  it('asks what came of a meeting that passed', () => {
    expect(
      composerPrompt({ type: 'meeting', status: 'scheduled', start: '2026-11-04T10:00:00Z' }, now),
    ).toMatch(/what came of it/)
    expect(
      composerPrompt({ type: 'meeting', status: 'held', start: '2026-11-04T10:00:00Z' }, now),
    ).toBeNull()
    expect(
      composerPrompt({ type: 'meeting', status: 'scheduled', start: '2026-11-06T10:00:00Z' }, now),
    ).toBeNull()
    expect(
      composerPrompt({ type: 'task', kind: 'call', status: 'todo', scheduled: '2026-11-04' }, now),
    ).toMatch(/what came of it/)
    expect(composerPrompt({ type: 'task', status: 'todo' }, now)).toBeNull()
  })
  it('asks nothing of a meeting the owner did not say they go to', () => {
    const m = { type: 'task', kind: 'meeting', status: 'todo', scheduled: '2026-11-04T10:00:00Z' }
    expect(composerPrompt(m, now)).toMatch(/what came of it/)
    expect(composerPrompt({ ...m, optional: true }, now)).toBeNull()
    expect(composerPrompt({ ...m, optional: true, attend: 'maybe' }, now)).toBeNull()
    expect(composerPrompt({ ...m, optional: true, attend: 'yes' }, now)).toMatch(/what came of it/)
    expect(composerPrompt({ ...m, attend: 'no' }, now)).toBeNull()
  })
  it('closes a task or a meeting directly, nothing else', () => {
    expect(closable({ type: 'task', status: 'doing' })).toBe(true)
    expect(closable({ type: 'task', kind: 'meeting', status: 'todo' })).toBe(true)
    expect(closable({ type: 'task', status: 'done' })).toBe(false)
    expect(closable({ type: 'meeting', status: 'scheduled' })).toBe(true)
    expect(closable({ type: 'question', status: 'open' })).toBe(false)
  })
})

describe('attachments', () => {
  it('knows a stored path and how to show it', () => {
    const p = 'attachments/task-a/20261105-140309-board.png'
    expect(isAttachmentPath(p)).toBe(true)
    expect(isAttachmentPath('attachments/task-a/../x.png')).toBe(false)
    expect(isAttachmentPath('https://example.com/x.png')).toBe(false)
    expect(attachmentSrc(p)).toBe(
      '/api/v1/attachment/file?path=attachments%2Ftask-a%2F20261105-140309-board.png',
    )
    expect(attachmentKind(p)).toBe('image')
    expect(attachmentKind('x.M4A')).toBe('audio')
    expect(attachmentKind('x.pdf')).toBe('other')
    expect(fileLabel({ name: '', size: 3 << 20 })).toBe('file (3.0 MB)')
  })
})
