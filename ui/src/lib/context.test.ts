import { describe, expect, it } from 'vitest'
import {
  captureCall,
  captureReady,
  fileTaskCall,
  firstLine,
  forGroups,
  roleLabel,
  type Capture,
} from '@/lib/context'

const base: Capture = {
  kind: 'verbal',
  with: [],
  from: '',
  date: '2026-11-06',
  summary: 'Dana wants the slides by Friday',
  url: '',
}

describe('roleLabel', () => {
  it('names each role', () => {
    expect(roleLabel('requested_by')).toBe('Requested by')
    expect(roleLabel('owed_to')).toBe('Owed to')
    expect(roleLabel('other')).toBe('other')
  })
})

describe('forGroups', () => {
  it('groups by role in role order', () => {
    const g = forGroups([
      { role: 'owed_to', name: 'Bo' },
      { role: 'requested_by', name: 'VP eng' },
      { role: 'owed_to', name: 'Ada', id: 'person-ada' },
    ])
    expect(g.map((x) => [x.role, x.parties.map((p) => p.name)])).toEqual([
      ['requested_by', ['VP eng']],
      ['owed_to', ['Bo', 'Ada']],
    ])
  })
})

describe('captureCall', () => {
  it('logs a conversation with vault people as a logged contact', () => {
    const c = captureCall({ ...base, with: [{ id: 'person-dana', name: 'Dana' }] })
    expect(c).toEqual({
      op: 'interaction_add',
      body: {
        summary: 'Dana wants the slides by Friday',
        with: ['person-dana'],
        medium: 'verbal',
        direction: 'in',
        date: '2026-11-06',
        url: undefined,
      },
    })
  })
  it('takes a name not in the vault as an incoming item to sort', () => {
    const c = captureCall({ ...base, with: [{ name: ' Dana ' }] })
    expect(c.op).toBe('io_add')
    if (c.op === 'io_add') {
      expect(c.body.medium).toBe('verbal')
      expect(c.body.from).toBe('Dana')
      expect(c.body.at.startsWith('2026-11-0')).toBe(true)
    }
  })
  it('carries over by hand with the system as from', () => {
    const c = captureCall({
      ...base,
      kind: 'swivel',
      from: ' the finance tracker ',
      url: 'https://tracker.example.com/1',
    })
    expect(c).toMatchObject({
      op: 'io_add',
      body: {
        direction: 'in',
        medium: 'swivel',
        from: 'the finance tracker',
        ref: 'https://tracker.example.com/1',
      },
    })
  })
})

describe('captureReady', () => {
  it('needs a summary, a date, and who or where', () => {
    expect(captureReady(base)).toBe(false)
    expect(captureReady({ ...base, with: [{ name: 'Dana' }] })).toBe(true)
    expect(captureReady({ ...base, with: [{ name: 'Dana' }], summary: ' ' })).toBe(false)
    expect(captureReady({ ...base, with: [{ name: 'Dana' }], date: '' })).toBe(false)
    expect(captureReady({ ...base, kind: 'swivel', from: 'tracker' })).toBe(true)
    expect(captureReady({ ...base, kind: 'swivel' })).toBe(false)
  })
})

describe('make a task of it', () => {
  const task = { on: true, title: '', due: '', area: 'area-home' }
  const dana = { id: 'person-dana', name: 'Dana' }

  it('takes the first non-empty line of the summary as the title', () => {
    expect(firstLine('\n  Send slides \nby Friday')).toBe('Send slides')
  })

  it('keeps a conversation with vault people as an io item, so the task can say Came from', () => {
    expect(captureCall({ ...base, with: [dana], task }).op).toBe('io_add')
  })

  it('files into a task in the area, for the one person it was with', () => {
    expect(fileTaskCall({ ...base, with: [dana], task: { ...task, due: '2026-11-13' } })).toEqual({
      to: 'task',
      area: 'area-home',
      title: undefined,
      due: '2026-11-13',
      requestedBy: 'person-dana',
    })
  })

  it('sends a title only when it differs from the first line; a swivel item leaves requested_by to the op', () => {
    const c: Capture = {
      ...base,
      kind: 'swivel',
      from: 'tracker',
      task: { ...task, title: 'Slides for Dana' },
    }
    expect(fileTaskCall(c)).toMatchObject({ title: 'Slides for Dana', requestedBy: undefined })
    const same = { ...c, task: { ...task, title: 'Dana wants the slides by Friday' } }
    expect(fileTaskCall(same)?.title).toBeUndefined()
  })

  it('is no task unless asked, and needs an area', () => {
    expect(fileTaskCall(base)).toBeNull()
    expect(fileTaskCall({ ...base, task: { ...task, on: false } })).toBeNull()
    const c: Capture = { ...base, with: [dana], task: { ...task, area: '' } }
    expect(captureReady(c)).toBe(false)
    expect(captureReady({ ...c, task })).toBe(true)
    expect(captureReady({ ...c, task: { ...task, due: 'soon' } })).toBe(false)
  })
})
