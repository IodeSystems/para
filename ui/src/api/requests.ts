import { useEffect, useRef } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useRouteContext } from '@tanstack/react-router'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import { canDecide } from '@/client/session'
import { resultToCheck } from '@/lib/sent'

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
            seen
            needsYou
            reason
          }
        }
      }
    }
  }
`)

// AskedDoc is the Asked of agents view: every request (list, type request, state all), each with
// its kind, item, the owner's words, who has it, how it ended, stale, seen, and receipts.
const AskedDoc = graphql(/* GraphQL */ `
  query AskedOfAgents {
    para {
      v1 {
        list(type: request, state: "all") {
          count
          rows {
            id
            status
            life
            needsYou
            reason
            request {
              request
              title
              kind
              item
              itemName
              note
              status
              claimedBy
              claimedAt
              resolution
              summary
              queuedAt
              activeAt
              stale
              seen
              results
              receipts
              undoable
            }
          }
        }
      }
    }
  }
`)

const RequestSeenDoc = graphql(/* GraphQL */ `
  mutation RequestSeen($body: para_RequestSeenInputInput!) {
    para {
      v1 {
        requestSeen(body: $body) {
          seen
          skipped
        }
      }
    }
  }
`)

// useAsked lists every request for the Asked of agents view, from the generic list.
export function useAsked() {
  return useQuery({
    queryKey: ['asked'],
    queryFn: async () => need((await gqlClient.request(AskedDoc)).para.v1.list, 'asked').rows,
  })
}

// requestSeen marks the results of finished agent work seen by the owner. People only.
export async function requestSeen(ids: string[]) {
  return need(
    (await gqlClient.request(RequestSeenDoc, { body: { ids } })).para.v1.requestSeen,
    'request seen',
  )
}

// useSeenOnOpen marks seen the finished agent work about an item (or the work itself) whose result
// the owner has not seen, when the owner opens its page. A session that cannot decide sends
// nothing.
export function useSeenOnOpen(id: string) {
  const { session } = useRouteContext({ from: '/_authed' })
  const decide = canDecide(session)
  const q = useRequests()
  const qc = useQueryClient()
  const sent = useRef(new Set<string>())
  useEffect(() => {
    if (!decide) return
    const ids = (q.data ?? [])
      .filter((r) => (r.id === id || (r.links ?? []).includes(id)) && resultToCheck(r))
      .map((r) => r.id)
      .filter((r) => !sent.current.has(r))
    if (ids.length === 0) return
    for (const r of ids) sent.current.add(r)
    void requestSeen(ids).then(
      () => qc.invalidateQueries(),
      () => undefined,
    )
  }, [decide, id, q.data, qc])
}

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
