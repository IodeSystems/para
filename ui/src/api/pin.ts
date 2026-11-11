import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

// Pinned items are the ones the owner watches (pinned, a read; pin and unpin, person ops). A pin
// is on the item's own file; a thread key that is no item (#tag, channel:...) has a small thread
// document. Owner approved the Pin and Unpin web writes 2026-11-10.
const PinnedDoc = graphql(/* GraphQL */ `
  query Pinned {
    para {
      v1 {
        pinned {
          items {
            id
            type
            key
            title
            pinned
            state
            life
            matters
            mattersName
            nextDate
            nextKind
            waitingOn
            waitingNames
            queued
            claimed
            last {
              at
              who
              kind
              summary
            }
            turns
            lastReply
            needsYou
            reason
            stale
          }
        }
      }
    }
  }
`)

export type PinnedCard = NonNullable<ReturnType<typeof usePinned>['data']>[number]

export function usePinned() {
  return useQuery({
    queryKey: ['pinned'],
    queryFn: async () => need((await gqlClient.request(PinnedDoc)).para.v1.pinned, 'pinned').items,
  })
}

const PinDoc = graphql(/* GraphQL */ `
  mutation Pin($body: para_PinInputInput!) {
    para {
      v1 {
        pin(body: $body) {
          id
          pinned
          changed
          rev
        }
      }
    }
  }
`)

const UnpinDoc = graphql(/* GraphQL */ `
  mutation Unpin($body: para_PinInputInput!) {
    para {
      v1 {
        unpin(body: $body) {
          id
          pinned
          changed
          rev
        }
      }
    }
  }
`)

// PinTarget names what to pin: an item id, or a thread key that is not an item (#tag, channel:...).
export type PinTarget = { id?: string; key?: string }

export function usePinToggle() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ target, pin }: { target: PinTarget; pin: boolean }) => {
      const body = { id: target.id, key: target.key }
      return pin
        ? need((await gqlClient.request(PinDoc, { body })).para.v1.pin, 'pin')
        : need((await gqlClient.request(UnpinDoc, { body })).para.v1.unpin, 'unpin')
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['pinned'] })
      void qc.invalidateQueries({ queryKey: ['doc'] })
      void qc.invalidateQueries({ queryKey: ['receipts'] })
    },
  })
}
