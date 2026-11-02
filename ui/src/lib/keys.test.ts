import { describe, expect, it } from 'vitest'
import { actionFor, TABS } from '@/lib/keys'

const press = (key: string, extra: Partial<Parameters<typeof actionFor>[0]> = {}) =>
  actionFor({ key, ctrlKey: false, metaKey: false, altKey: false, target: null, ...extra })

describe('shortcuts', () => {
  it('digits switch tabs', () => {
    for (const t of TABS) expect(press(t.key)).toEqual({ kind: 'tab', to: t.to })
    expect(press('0')).toBeNull()
    expect(press('9')).toBeNull()
  })
  it('/ searches, c captures, ? shows help', () => {
    expect(press('/')).toEqual({ kind: 'search' })
    expect(press('c')).toEqual({ kind: 'capture' })
    expect(press('?')).toEqual({ kind: 'help' })
  })
  it('does nothing while typing in a field', () => {
    expect(press('c', { target: { tagName: 'INPUT' } })).toBeNull()
    expect(press('1', { target: { tagName: 'textarea' } })).toBeNull()
    expect(press('/', { target: { isContentEditable: true } })).toBeNull()
  })
  it('does nothing with a modifier', () => {
    expect(press('c', { ctrlKey: true })).toBeNull()
    expect(press('1', { metaKey: true })).toBeNull()
  })
})
