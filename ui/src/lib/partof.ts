// Part of: what an item belongs to, as the server sends it, nearest first: its parent task, the
// obligations and questions it waits on or serves, its goal, then its tags. Each is a link to that
// item's page; a tag links to the tag page.

export type PartRef = { kind: string; via: string; id: string; title: string }

// partShown is what a row shows. On a phone only the nearest one shows, with "+N" for the rest;
// a wider screen shows all of them. omit drops the page the list is on (a goal page's own tasks
// need not say the goal). The owner's default; keep the rule here so it can change.
export function partShown(
  parts: readonly PartRef[] | null | undefined,
  phone: boolean,
  omit?: string,
): { shown: PartRef[]; more: number } {
  const all = (parts ?? []).filter((p) => p.id !== omit)
  if (!phone || all.length <= 1) return { shown: all, more: 0 }
  return { shown: all.slice(0, 1), more: all.length - 1 }
}

// partLink is the route of one Part of row.
export function partLink(
  p: PartRef,
):
  | { to: '/tags/$tag'; params: { tag: string } }
  | { to: '/projects/$id'; params: { id: string } }
  | { to: '/obligations/$id'; params: { id: string } }
  | { to: '/doc/$id'; params: { id: string } } {
  switch (p.kind) {
    case 'tag':
      return { to: '/tags/$tag', params: { tag: p.id } }
    case 'goal':
      return { to: '/projects/$id', params: { id: p.id } }
    case 'obligation':
      return { to: '/obligations/$id', params: { id: p.id } }
    default:
      return { to: '/doc/$id', params: { id: p.id } }
  }
}

// partLabel is the words of one row: a tag shows as #tag.
export function partLabel(p: PartRef): string {
  return p.kind === 'tag' ? `#${p.title || p.id}` : p.title || p.id
}
