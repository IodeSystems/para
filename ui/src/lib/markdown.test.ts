import { describe, expect, it } from 'vitest'
import {
  docPath,
  dropHeading,
  graphToMermaid,
  isInternalHref,
  linkWikilinks,
  tidyLine,
  mermaidLabel,
  wikilinkTarget,
  withoutSection,
} from '@/lib/markdown'

describe('wikilinks', () => {
  it('reads target, alias, anchor, folder, and extension', () => {
    expect(wikilinkTarget('policy-x')).toEqual({ id: 'policy-x', label: 'policy-x' })
    expect(wikilinkTarget('policy-x|the policy')).toEqual({ id: 'policy-x', label: 'the policy' })
    expect(wikilinkTarget('policy-x#R4')).toEqual({ id: 'policy-x', label: 'policy-x#R4' })
    expect(wikilinkTarget('org/person-a.md')).toEqual({ id: 'person-a', label: 'org/person-a.md' })
  })

  it('rewrites to in-app links', () => {
    expect(linkWikilinks('see [[rfx-1]] and [[task-2|the task]].')).toBe(
      'see [rfx-1](/doc/rfx-1 "wiki") and [the task](/doc/task-2).',
    )
  })

  it('leaves code fences and inline code alone', () => {
    const md = 'a `[[x]]` b\n```\n[[y]]\n```\n[[z]]'
    expect(linkWikilinks(md)).toBe('a `[[x]]` b\n```\n[[y]]\n```\n[z](/doc/z "wiki")')
  })

  it('leaves what is not a link as it is', () => {
    expect(linkWikilinks('[[a|b]c]]')).toBe('[[a|b]c]]')
    expect(linkWikilinks('[[|]]')).toBe('[[|]]')
    expect(linkWikilinks('[[a|[b]]')).toBe('[b](/doc/a)') // brackets leave the label
  })

  it('shows a rule without its policy id or block id', () => {
    expect(tidyLine('- R1 policy-logging: Logs are kept 30 days ^R1')).toBe(
      '- **R1** Logs are kept 30 days',
    )
    expect(tidyLine('R2 policy-x.y: Two ^R2')).toBe('**R2** Two')
    expect(tidyLine('A line ^abc-1')).toBe('A line')
    expect(tidyLine('2 ^ 3 is eight')).toBe('2 ^ 3 is eight')
  })

  it('encodes ids in the path', () => {
    expect(docPath('a b/c')).toBe('/doc/a%20b%2Fc')
  })

  it('tells internal links from external', () => {
    expect(isInternalHref('/doc/x')).toBe(true)
    expect(isInternalHref('//evil.example')).toBe(false)
    expect(isInternalHref('https://x.example')).toBe(false)
    expect(isInternalHref(undefined)).toBe(false)
  })
})

describe('graphToMermaid', () => {
  const nodes = [
    { id: 'task-a', title: 'Do "A" <b>', type: 'task', status: 'done', state: 'done' },
    { id: 'task-b', title: 'B', type: 'task', status: 'open', state: 'blocked' },
  ]
  it('numbers nodes, escapes labels, and points from what must finish', () => {
    const out = graphToMermaid(nodes, [{ from: 'task-b', to: 'task-a' }])
    expect(out).toContain('n0["Do \'A\' b"]:::done')
    expect(out).toContain('n1["B"]:::blocked')
    expect(out).toContain('n0 --> n1')
    expect(out).not.toContain('task-a')
  })
  it('draws a question as a dashed hexagon and a request as a pill', () => {
    const out = graphToMermaid(
      [
        { id: 'q', title: 'Which region?', type: 'question', status: 'open', state: 'open' },
        { id: 'r', title: 'Ask Sam', type: 'rfx', status: 'open', state: 'waiting' },
      ],
      [],
    )
    expect(out).toContain('n0{{"Which region?"}}')
    expect(out).toContain('n1(["Ask Sam"]):::waiting')
    expect(out).toContain('class n0 question')
  })
  it('wraps a long label instead of cutting it', () => {
    const long = 'Write the quarterly platform capacity plan and share it with finance'
    const out = graphToMermaid(
      [{ id: 'a', title: long, type: 'task', status: 'todo', state: 'ready' }],
      [],
    )
    expect(out).toContain('<br/>')
    expect(out.replace(/<br\/>/g, ' ')).toContain(long)
  })
  it('drops an edge to an unknown node', () => {
    expect(graphToMermaid(nodes, [{ from: 'x', to: 'task-a' }])).not.toContain('-->')
  })
  it('keeps a label on one line', () => {
    expect(mermaidLabel('a\nb"c')).toBe("a b'c")
  })
})

describe('withoutSection', () => {
  it('drops one section and keeps the rest', () => {
    const md = 'intro\n\n## Draft\nhello\n\n## Notes\nkeep\n'
    expect(withoutSection(md, 'draft')).toBe('intro\n\n## Notes\nkeep')
  })
  it('keeps everything when the section is absent', () => {
    expect(withoutSection('a\n## B\nc', 'Draft')).toBe('a\n## B\nc')
  })
})

describe('dropHeading', () => {
  it('removes a heading of that name and keeps the rest', () => {
    expect(dropHeading('Lead.\n\n## Timeline\n\n- one', 'Timeline')).toBe('Lead.\n\n\n\n- one')
    expect(dropHeading('## Other\ntext', 'Timeline')).toBe('## Other\ntext')
  })
})
