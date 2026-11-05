import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

const RequestListDoc = graphql(/* GraphQL */ `
  query Requests {
    para {
      v1 {
        requestList {
          items {
            id
            title
            status
            runner
            queuedAt
            claimedBy
            leaseUntil
            linkNames
            links
            investigates
            delegates
            resolution
            turn
            reply
            kind
            requiresSource
            claimedAt
            activeAt
            stale
          }
        }
      }
    }
  }
`)

const ChangesDoc = graphql(/* GraphQL */ `
  query RequestChanges($id: String!) {
    para {
      v1 {
        requestChanges(id: $id) {
          items {
            doc
            docName
            op
            receipt
          }
        }
      }
    }
  }
`)

export function useRequestChanges(id: string, enabled: boolean) {
  return useQuery({
    queryKey: ['requestChanges', id],
    enabled,
    queryFn: async () =>
      need((await gqlClient.request(ChangesDoc, { id })).para.v1.requestChanges, 'changes').items,
  })
}

export function useRequests() {
  return useQuery({
    queryKey: ['requests'],
    queryFn: async () =>
      need((await gqlClient.request(RequestListDoc)).para.v1.requestList, 'requests').items,
  })
}

// useLooking is the ids of the items that open agent work looks into. It reads the request list
// once; every row reads the same cached query, so lists do not slow down.
export function useLooking(): Set<string> {
  const q = useRequests()
  const out = new Set<string>()
  for (const r of q.data ?? []) if (r.investigates) for (const l of r.links ?? []) out.add(l)
  return out
}

// useReplies maps an item id to the finished Ask-agent work that replied about it: the work's id
// and its resolution note. Derived from the request list; an item with open work has no reply.
export function useReplies(): Map<string, { request: string; reply: string }> {
  const q = useRequests()
  const open = useLooking()
  const out = new Map<string, { request: string; reply: string }>()
  for (const r of q.data ?? []) {
    if (!r.reply) continue
    for (const l of r.links ?? [])
      if (!open.has(l) && !out.has(l)) out.set(l, { request: r.id, reply: r.reply })
  }
  return out
}

// useWorking is the ids of the tasks that open delegated agent work serves.
export function useWorking(): Set<string> {
  const q = useRequests()
  const out = new Set<string>()
  for (const r of q.data ?? []) if (r.delegates) for (const l of r.links ?? []) out.add(l)
  return out
}
