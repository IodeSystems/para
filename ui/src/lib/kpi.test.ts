import { describe, expect, it } from 'vitest'
import { isKPIRow, kpiRowParts, kpiSlug, readingInput } from '@/lib/kpi'

describe('kpi', () => {
  it('knows a KPI row and its parts', () => {
    expect(isKPIRow({ id: 'kpi:project-shop/weekly-signups', kind: 'kpi' })).toBe(true)
    expect(isKPIRow({ id: 'task-a', kind: 'task' })).toBe(false)
    expect(kpiRowParts('kpi:project-shop/weekly-signups')).toEqual({
      project: 'project-shop',
      slug: 'weekly-signups',
    })
    expect(kpiRowParts('task-a')).toBeNull()
    expect(kpiSlug(' Weekly Signups (EU) ')).toBe('weekly-signups-eu')
  })
  it('checks a reading', () => {
    expect(readingInput({ name: 'Signups', value: '42', source: ' owner ', date: '' })).toEqual({
      name: 'Signups',
      value: 42,
      source: 'owner',
    })
    expect(
      readingInput({ name: 'Signups', value: '4.5', source: 'https://x.example', date: '2026-11-10' }),
    ).toEqual({ name: 'Signups', value: 4.5, source: 'https://x.example', date: '2026-11-10' })
    expect(readingInput({ name: '', value: '1', source: 'owner', date: '' })).toBe('Pick a KPI.')
    expect(readingInput({ name: 'S', value: 'ten', source: 'owner', date: '' })).toBe(
      'The value is a number.',
    )
    expect(readingInput({ name: 'S', value: '1', source: ' ', date: '' })).toMatch(/where you read/)
    expect(readingInput({ name: 'S', value: '1', source: 'owner', date: '11/10' })).toMatch(/YYYY/)
  })
})
