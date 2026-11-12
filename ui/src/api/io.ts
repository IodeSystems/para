import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need, needDetail } from '@/client/need'
import { closeAny } from '@/api/verbs'
import type { DraftEditBody } from '@/lib/draft'

const IoListDoc = graphql(/* GraphQL */ `
  query IoList($direction: para_ioListDirection, $all: Boolean, $voice: para_ioListVoice) {
    para {
      v1 {
        ioList(direction: $direction, all: $all, voice: $voice) {
          counts {
            unread
            read
            draft
            ready
          }
          items {
            id
            title
            direction
            medium
            state
            from
            to
            at
            subject
            ref
            unverifiable
            expects
            became
            legacy
            created
            needsYou
            needsYouReason
            digest
            made
            reviewed
            triageBy
            voice
            allowedBy
          }
        }
      }
    }
  }
`)

const IoShowDoc = graphql(/* GraphQL */ `
  query IoShow($id: String!) {
    para {
      v1 {
        show(id: $id) {
          detail {
            __typename
            ... on para_IOShowOutput {
              doc {
                id
                title
                body
              }
              item {
                id
                title
                direction
                medium
                state
                from
                to
                at
                subject
                ref
                verify
                unverifiable
                expects
                route
                became
                thread
                reason
                legacy
                created
                triage
                triageBy
                links
                voice
                agent
                stamp
                kind
                allowedBy
                draftHash
                importedFrom
                preparedBy
                preparedAt
                viaSource
                viaRef
                url
              }
            }
          }
        }
      }
    }
  }
`)

export type IoDirection = 'in' | 'out'
export type IoVoice = 'as_me' | 'for_me'

// The open items of one direction (the inbox or the outbox), or every item with all, and the
// counts of every open item. voice keeps outgoing items of that voice (for_me with all: what
// agents sent for the owner, stamped).
export function useIoList(direction?: IoDirection, all = false, voice?: IoVoice) {
  return useQuery({
    queryKey: ['io', direction ?? '', all, voice ?? ''],
    queryFn: async () =>
      need(
        (await gqlClient.request(IoListDoc, { direction, all, voice })).para.v1.ioList,
        'io items',
      ),
  })
}

const DelegationRunDoc = graphql(/* GraphQL */ `
  query DelegationRun($id: String!) {
    para {
      v1 {
        delegationRun(id: $id) {
          voice
          agent
          file
          rules
          rule
          stamp
          stamped
          misses {
            rule
            name
            why
          }
          next
        }
      }
    }
  }
`)

// useDelegationRun is the dry run of the owner's delegation rules on one outgoing draft: the
// rule that would let its agent send it for the owner, stamped, without review, or why none.
export function useDelegationRun(id: string, enabled: boolean) {
  return useQuery({
    queryKey: ['io', 'delegation', id],
    enabled: enabled && id !== '',
    queryFn: async () =>
      need((await gqlClient.request(DelegationRunDoc, { id })).para.v1.delegationRun, 'delegation'),
  })
}

const fetchIoItem = async (id: string) =>
  needDetail(
    (await gqlClient.request(IoShowDoc, { id })).para.v1.show,
    'para_IOShowOutput',
    'io item',
  )

export function useIoItem(id: string) {
  return useQuery({
    queryKey: ['io', 'item', id],
    enabled: id !== '',
    queryFn: () => fetchIoItem(id),
  })
}

// useIoItems reads several io items at once, with the same cache keys as useIoItem.
export function useIoItems(ids: string[]) {
  return useQueries({
    queries: ids.map((id) => ({
      queryKey: ['io', 'item', id],
      enabled: id !== '',
      queryFn: () => fetchIoItem(id),
    })),
  })
}

const IoMadeDoc = graphql(/* GraphQL */ `
  query IoMade($id: String!) {
    para {
      v1 {
        ioMade(id: $id) {
          id
          counts
          rows {
            doc
            title
            type
            actions
            at
            actor
            receipt
            why
          }
        }
      }
    }
  }
`)

const IoReviewDoc = graphql(/* GraphQL */ `
  mutation IoReview($body: para_IOReviewInputInput!) {
    para {
      v1 {
        ioReview(body: $body) {
          reviewed
          at
        }
      }
    }
  }
`)

// useIoMade is what an incoming item made in the vault: each document it created, edited, closed,
// or linked, with who did it and why.
export function useIoMade(id: string, enabled = true) {
  return useQuery({
    queryKey: ['io', 'made', id],
    enabled,
    queryFn: async () =>
      need((await gqlClient.request(IoMadeDoc, { id })).para.v1.ioMade, 'io made'),
  })
}

// ioReview marks io items reviewed by the owner (owner only). One receipt undoes it.
export async function ioReview(ids: string[]) {
  return need(
    (await gqlClient.request(IoReviewDoc, { body: { ids } })).para.v1.ioReview,
    'io review',
  )
}

const IOApproveDoc = graphql(/* GraphQL */ `
  mutation IOApprove($body: para_IOApproveInputInput!) {
    para {
      v1 {
        ioApprove(body: $body) {
          id
          state
          approvedHash
          superseded
          items {
            id
          }
        }
      }
    }
  }
`)

// approveIo is io_approve with a body: one draft, or items of {id, expectHash} (all or none).
export async function approveIo(body: {
  id?: string
  expectHash?: string
  items?: { id: string; expectHash: string }[]
}) {
  return need((await gqlClient.request(IOApproveDoc, { body })).para.v1.ioApprove, 'approve')
}

// useIOApprove is Approve on an outgoing draft: the owner approves its exact medium, to, subject,
// and text (io_approve, the owner's path), and it is ready for an agent to send. People only.
// voice for_me keeps the stamp of a for_me draft; left out (as_me) the stamp comes off.
export function useIOApprove() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (arg: string | { id: string; voice?: 'as_me' | 'for_me' }) => {
      const body = typeof arg === 'string' ? { id: arg } : arg
      return need((await gqlClient.request(IOApproveDoc, { body })).para.v1.ioApprove, 'approve')
    },
    onSettled: () => qc.invalidateQueries(),
  })
}

export type IODoneBody = { id: string; url?: string; locator?: string; text?: string }

// useIODone marks an outbox draft sent by the owner: it is filed as an agent's send would be
// (close with outcome sent, which runs io_done).
export function useIODone() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, ...fields }: IODoneBody) => {
      const r = await closeAny({ id, outcome: 'sent', fields })
      return { id, state: r.result.state as string, became: r.result.became as string | null }
    },
    onSettled: () => qc.invalidateQueries(),
  })
}

const IOEditDoc = graphql(/* GraphQL */ `
  mutation IOEdit($body: para_IOEditInputInput!) {
    para {
      v1 {
        ioEdit(body: $body) {
          id
          state
          warnings
        }
      }
    }
  }
`)

// useIOEdit saves the owner's edit of an outgoing draft's text (io_edit). With state ready the same
// call approves the text it saves.
export function useIOEdit() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: DraftEditBody) =>
      need((await gqlClient.request(IOEditDoc, { body })).para.v1.ioEdit, 'edit'),
    onSettled: () => qc.invalidateQueries(),
  })
}
