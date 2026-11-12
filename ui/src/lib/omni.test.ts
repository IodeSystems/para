import { describe, expect, it } from 'vitest'
import { entriesFor, groupOf, idOf, isOmniKey, nextIndex, pagesFor } from '@/lib/omni'

const h = (id: string, kind: string, title = id) => ({ id, kind, title })

describe('omni', () => {
  it('detects a pasted id', () => {
    expect(idOf(' task-sample-a ')).toBe('task-sample-a')
    expect(idOf('meeting-sync')).toBe('meeting-sync')
    expect(idOf('task-')).toBeNull()
    expect(idOf('write the task-plan')).toBeNull()
    expect(idOf('quota')).toBeNull()
  })
  it('matches page names', () => {
    expect(pagesFor('rec').map((p) => p.to)).toEqual(['/receipts', '/activity'])
    expect(pagesFor('activity').map((p) => p.to)).toEqual(['/activity'])
    expect(pagesFor('Today')[0]?.to).toBe('/agenda')
    expect(pagesFor('')).toEqual([])
    expect(pagesFor('inbox').map((p) => p.to)).toEqual(['/input'])
    expect(pagesFor('proposals').map((p) => p.to)).toEqual(['/output'])
    expect(pagesFor('io').some((p) => p.to === '/io')).toBe(false)
  })
  it('groups hits in order and caps each group', () => {
    const hits = [
      h('note-a', 'note'),
      h('person-a', 'person'),
      h('task-a', 'task'),
      h('task-a', 'task'),
      h('meeting-a', 'task'),
      h('goal-a', 'goal'),
      h('project-a', 'project'),
      ...[1, 2, 3, 4, 5].map((i) => h(`task-${i}`, 'task')),
    ]
    const e = entriesFor('x', hits)
    expect(e.map((x) => x.group).filter((g, i, a) => a.indexOf(g) === i)).toEqual([
      'Tasks',
      'Projects',
      'Goals',
      'Meetings',
      'People',
      'Docs',
    ])
    expect(e.filter((x) => x.group === 'Tasks')).toHaveLength(4)
    expect(groupOf(h('rfx-a', 'rfx'))).toBe('Asks')
    expect(groupOf(h('question-a', 'question'))).toBe('Questions')
  })
  it('puts a pasted id first', () => {
    const e = entriesFor('task-a', [h('task-a', 'task')])
    expect(e[0]).toMatchObject({ group: 'Open', id: 'task-a' })
    expect(e).toHaveLength(1)
  })
  it('moves the index', () => {
    expect(nextIndex(-1, 'down', 3)).toBe(0)
    expect(nextIndex(2, 'down', 3)).toBe(-1)
    expect(nextIndex(-1, 'up', 3)).toBe(2)
    expect(nextIndex(0, 'up', 3)).toBe(-1)
    expect(nextIndex(0, 'down', 0)).toBe(-1)
  })
  it('knows Ctrl/Cmd-K', () => {
    const k = { key: 'k', ctrlKey: false, metaKey: false, altKey: false }
    expect(isOmniKey({ ...k, ctrlKey: true })).toBe(true)
    expect(isOmniKey({ ...k, metaKey: true, key: 'K' })).toBe(true)
    expect(isOmniKey(k)).toBe(false)
  })
})
