import { describe, expect, it } from 'vitest'
import {
  attachmentKind,
  attachmentSrc,
  canSend,
  draftHints,
  composerKey,
  composerPrompt,
  notable,
  openQuestion,
  answerState,
  fileLabel,
  isAttachmentPath,
  notesWithEntry,
  sendKeyHint,
  isMacPlatform,
} from './composer'

describe('composer keys', () => {
  it('sends on Ctrl+Enter or Cmd+Enter; plain Enter and Shift-Enter are a new line', () => {
    expect(composerKey({ key: 'Enter', ctrlKey: true })).toBe('send')
    expect(composerKey({ key: 'Enter', metaKey: true })).toBe('send')
    expect(composerKey({ key: 'Enter', shiftKey: false })).toBeNull()
    expect(composerKey({ key: 'Enter', shiftKey: true })).toBeNull()
    expect(composerKey({ key: 'Enter', ctrlKey: true, isComposing: true })).toBeNull()
    expect(composerKey({ key: 'a', ctrlKey: true })).toBeNull()
  })
  it('names the key that sends', () => {
    expect(sendKeyHint(false)).toBe('Ctrl+Enter sends, Enter adds a line')
    expect(sendKeyHint(true)).toMatch(/^Cmd\+Enter sends/)
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

describe('composer on managed records', () => {
  it('asks a prompt and keeps notes', () => {
    for (const type of ['person', 'group', 'tracked_doc', 'area']) {
      expect(composerPrompt({ type }, new Date())).toBeTruthy()
      expect(notable({ type })).toBe(true)
    }
  })
  it('leaves other types as they were', () => {
    expect(composerPrompt({ type: 'rfx', status: 'open' }, new Date())).toBeNull()
    expect(notable({ type: 'receipt' })).toBe(false)
  })
})

describe('composer on an open question', () => {
  it('answers with words or a choice', () => {
    expect(openQuestion({ type: 'question', status: 'open' })).toBe(true)
    expect(openQuestion({ type: 'question', status: 'answered' })).toBe(false)
    expect(answerState('', '')).toEqual({ answer: false, decline: false })
    expect(answerState('  ', '')).toEqual({ answer: false, decline: false })
    expect(answerState('', 'EU')).toEqual({ answer: true, decline: false })
    expect(answerState('US, per legal', '')).toEqual({ answer: true, decline: true })
  })
})

describe('draftHints', () => {
  it('is empty with an empty box', () => {
    expect(draftHints('  ', 0)).toEqual({ note: null, look: null, reason: null })
  })
  it('names the words, and the words alone are a reason', () => {
    const h = draftHints('called Dev', 0)
    expect(h.note).toBe('Uses what you typed: added to the notes as a dated entry')
    expect(h.look).toBe('Uses what you typed: added to the notes and sent to the agent')
    expect(h.reason).toBe('Uses what you typed as the reason')
  })
  it('names the files; files alone give no reason', () => {
    expect(draftHints('', 2)).toEqual({
      note: 'Uses the 2 files: added to the notes as a dated entry',
      look: 'Uses the 2 files: added to the notes and sent to the agent',
      reason: null,
    })
    expect(draftHints('x', 1).note).toBe(
      'Uses what you typed and the file: added to the notes as a dated entry',
    )
  })
})

describe('isMacPlatform', () => {
  it('Cmd on Apple platforms, Ctrl on Linux and Windows', () => {
    expect(isMacPlatform('MacIntel')).toBe(true)
    expect(isMacPlatform('iPhone')).toBe(true)
    expect(isMacPlatform('Linux x86_64')).toBe(false)
    expect(isMacPlatform('Win32')).toBe(false)
    expect(isMacPlatform('', 'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36')).toBe(false)
    expect(isMacPlatform('', 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)')).toBe(true)
    expect(sendKeyHint(isMacPlatform('Linux x86_64'))).toMatch(/^Ctrl\+Enter/)
  })
})
