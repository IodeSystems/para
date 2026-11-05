import { describe, expect, it } from 'vitest'
import { agendaActions, openTarget } from './agenda'

describe('agendaActions', () => {
  it('gives each kind the actions that fit it', () => {
    expect(agendaActions('task')).toEqual({ done: 'task', later: true })
    expect(agendaActions('meeting')).toEqual({ done: 'meeting', later: false })
    expect(agendaActions('ask')).toEqual({ done: 'ask', later: false })
    expect(agendaActions('question')).toEqual({ done: null, later: true })
    expect(agendaActions('io')).toEqual({ done: null, later: true })
    expect(agendaActions('incident')).toEqual({ done: null, later: false })
    expect(agendaActions(undefined)).toEqual({ done: null, later: false })
  })
  it('opens each kind on its own page', () => {
    expect(openTarget('incident')).toBe('incident')
    expect(openTarget('ask')).toBe('rfx')
    expect(openTarget('io')).toBe('io')
    expect(openTarget('task')).toBe('doc')
  })
})
