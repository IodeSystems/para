import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
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
        ioShow(id: $id) {
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

export function useIoItem(id: string) {
  return useQuery({
    queryKey: ['io', 'item', id],
    enabled: id !== '',
    queryFn: async () =>
      need((await gqlClient.request(IoShowDoc, { id })).para.v1.ioShow, 'io item'),
  })
}

const IOApproveDoc = graphql(/* GraphQL */ `
  mutation IOApprove($body: para_IOApproveInputInput!) {
    para {
      v1 {
        ioApprove(body: $body) {
          id
          state
          approvedHash
        }
      }
    }
  }
`)

// useIOApprove is Approve on an outgoing draft: the owner approves its exact medium, to, subject,
// and text (io_approve, the owner's path), and it is ready for an agent to send. People only.
export function useIOApprove() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: string) =>
      need((await gqlClient.request(IOApproveDoc, { body: { id } })).para.v1.ioApprove, 'approve'),
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
