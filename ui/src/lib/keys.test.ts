import { describe, expect, it } from 'vitest'
import { actionFor, EXTRA_SCREENS, TABS } from '@/lib/keys'

const press = (key: string, extra: Partial<Parameters<typeof actionFor>[0]> = {}) =>
  actionFor({ key, ctrlKey: false, metaKey: false, altKey: false, target: null, ...extra })

describe('shortcuts', () => {
  it('digits switch tabs', () => {
    for (const t of TABS.filter((x) => x.key !== ''))
      expect(press(t.key)).toEqual({ kind: 'tab', to: t.to })
    expect(press('')).toBeNull()
    expect(press('-')).toBeNull()
    expect(press('10')).toBeNull()
  })
  it('0 opens Obligations, and Meetings is a filter of it, not a screen', () => {
    expect(press('0')).toEqual({ kind: 'tab', to: '/obligations' })
    expect(TABS.some((t) => t.to === '/meetings')).toBe(false)
  })
  it('Input and Output are top tabs with no digit; Pinned and Next stay; /io is not a screen', () => {
    const tabs = TABS.map((t) => t.to)
    for (const to of ['/pinned', '/next', '/input', '/output']) expect(tabs).toContain(to)
    expect(TABS.find((t) => t.to === '/input')?.key).toBe('')
    expect(TABS.find((t) => t.to === '/output')?.key).toBe('')
    const all = [...tabs, ...EXTRA_SCREENS.map((t) => t.to)]
    expect(all).not.toContain('/io')
    expect(new Set(all).size).toBe(all.length)
  })
  it('t opens Capture, but not while typing', () => {
    expect(press('t')).toEqual({ kind: 'tell' })
    expect(press('t', { target: { tagName: 'TEXTAREA' } })).toBeNull()
    expect(press('t', { ctrlKey: true })).toBeNull()
  })
  it('/ searches, ? shows help, c opens nothing', () => {
    expect(press('/')).toEqual({ kind: 'search' })
    expect(press('c')).toBeNull()
    expect(press('?')).toEqual({ kind: 'help' })
  })
  it('does nothing while typing in a field', () => {
    expect(press('?', { target: { tagName: 'INPUT' } })).toBeNull()
    expect(press('1', { target: { tagName: 'textarea' } })).toBeNull()
    expect(press('/', { target: { isContentEditable: true } })).toBeNull()
  })
  it('does nothing with a modifier', () => {
    expect(press('?', { ctrlKey: true })).toBeNull()
    expect(press('1', { metaKey: true })).toBeNull()
  })
})
