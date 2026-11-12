// Blocked by: the open items an item waits on, as the server labels them (type: output or input
// for an io item, task, Ask, question; state: unapproved, approved, waiting for reply, open).

export type BlockerLike = { id: string; type: string; state: string; title: string }

// blockerLink is the route of one blocker's page: an io item on /io, an Ask on /rfx, anything
// else on /docs.
export function blockerLink(
  b: BlockerLike,
):
  | { to: '/io/$id'; params: { id: string } }
  | { to: '/rfx/$id'; params: { id: string } }
  | { to: '/docs/$id'; params: { id: string } } {
  if (b.type === 'output' || b.type === 'input') return { to: '/io/$id', params: { id: b.id } }
  if (b.type === 'Ask') return { to: '/rfx/$id', params: { id: b.id } }
  return { to: '/docs/$id', params: { id: b.id } }
}

// blockerTone is the tone of the state chip: a blocker that waits on the owner (unapproved,
// changed after approval, asked of you, answer to check) asks for attention; the rest wait.
export function blockerTone(state: string): 'attn' | 'wait' {
  return /^(unapproved|changed after approval|asked of you|answer to check)$/.test(state)
    ? 'attn'
    : 'wait'
}
