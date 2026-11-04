import { describe, expect, it } from 'vitest'
import {
  answerEmpty,
  answerInput,
  blockIds,
  blockInfo,
  itemKind,
  linkParts,
  parseBlockInfo,
  sectionId,
  stringMap,
} from '@/lib/dossier'
import { lineDiff } from '@/lib/diff'

const fence = '```'

describe('dossier blocks', () => {
  it('reads the info string of an item block', () => {
    expect(parseBlockInfo('yaml para-question id=q1')).toEqual({ block: 'question', id: 'q1' })
    expect(parseBlockInfo('yaml para-doc-edit id=fix-2')).toEqual({
      block: 'doc-edit',
      id: 'fix-2',
    })
    expect(parseBlockInfo('yaml para-change id=c1')).toEqual({ block: 'change', id: 'c1' })
    expect(parseBlockInfo('yaml para-dossier')).toEqual({ block: 'dossier', id: '' })
  })
  it('gives null for other code and for a bad id', () => {
    expect(parseBlockInfo('yaml')).toBeNull()
    expect(parseBlockInfo('yaml para-receipt')).toBeNull()
    expect(parseBlockInfo('json para-question id=q1')).toBeNull()
    expect(parseBlockInfo('yaml para-question id=Q1')).toBeNull()
    expect(parseBlockInfo('yaml para-question id=1q')).toBeNull()
    expect(parseBlockInfo(`yaml para-question id=${'a'.repeat(41)}`)).toBeNull()
    expect(parseBlockInfo('yaml para-question id=q1 extra')).toBeNull()
  })
  it('joins the language and the meta the markdown parser splits', () => {
    expect(blockInfo('language-yaml', 'para-message id=m1')).toBe('yaml para-message id=m1')
    expect(blockInfo('language-yaml', undefined)).toBe('yaml')
    expect(blockInfo(undefined, undefined)).toBe('')
    expect(parseBlockInfo(blockInfo('language-yaml', 'para-message id=m1'))?.id).toBe('m1')
  })
  it('maps a block word to an item kind', () => {
    expect(itemKind('change')).toBe('para')
    expect(itemKind('doc-edit')).toBe('doc_edit')
    expect(itemKind('question')).toBe('question')
  })
  it('lists the item ids in a body, and skips blocks inside other code', () => {
    const md = [
      `${fence}yaml para-dossier`,
      'tldr: x',
      fence,
      '## Findings',
      `${fence}yaml para-question id=q1`,
      'title: Which?',
      fence,
      '````md',
      `${fence}yaml para-message id=inside`,
      fence,
      '````',
      `~~~yaml para-message id=m1`,
      'body: Hi',
      '~~~',
    ].join('\n')
    expect(blockIds(md)).toEqual(['q1', 'm1'])
  })
})

describe('section ids', () => {
  // The same cases as model.SectionID: lower case, runs of other characters become one dash.
  it('slugs a heading as para does', () => {
    expect(sectionId('Findings')).toBe('findings')
    expect(sectionId('What we found, and why')).toBe('what-we-found-and-why')
    expect(sectionId('  Cost (USD) / month  ')).toBe('cost-usd-month')
    expect(sectionId('Café 2026')).toBe('caf-2026')
    expect(sectionId('--x--')).toBe('x')
    expect(sectionId('The `foo` op')).toBe('the-foo-op')
  })
})

describe('answers', () => {
  it('says when an answer says nothing', () => {
    expect(answerEmpty(null)).toBe(true)
    expect(answerEmpty({ decision: null, choices: [], comment: '  ' })).toBe(true)
    expect(answerEmpty({ editedOptions: {} })).toBe(true)
    expect(answerEmpty({ decision: 'approve' })).toBe(false)
    expect(answerEmpty({ comment: 'why?' })).toBe(false)
    expect(answerEmpty({ editedOptions: { A: 'A2' } })).toBe(false)
  })
  it('drops empty keys, via, and stale from what it saves', () => {
    expect(answerInput({ decision: 'approve', choices: [], via: 'file', stale: true })).toEqual({
      decision: 'approve',
    })
    expect(answerInput({ choices: ['B'], ownWords: '' })).toEqual({ choices: ['B'] })
    expect(answerInput({ decision: null })).toBeNull()
  })
  it('reads a JSON map of strings', () => {
    expect(stringMap({ a: 'x', b: 2 })).toEqual({ a: 'x' })
    expect(stringMap(null)).toEqual({})
    expect(stringMap(['x'])).toEqual({})
  })
})

describe('evidence links', () => {
  it('finds web addresses and leaves a full stop out', () => {
    expect(linkParts('see https://acme.example/q.')).toEqual([
      { text: 'see ' },
      { text: 'https://acme.example/q', url: 'https://acme.example/q' },
      { text: '.' },
    ])
    expect(linkParts('note body')).toEqual([{ text: 'note body' }])
  })
})

describe('line diff', () => {
  it('marks added and removed lines', () => {
    expect(lineDiff('a\nb\nc', 'a\nB\nc')).toEqual(['  a', '- b', '+ B', '  c'])
    expect(lineDiff('', 'x')).toEqual(['+ x'])
    expect(lineDiff('x', '')).toEqual(['- x'])
  })
})
