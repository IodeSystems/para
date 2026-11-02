import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import type { Para_InboxTriageInputKind, Para_InboxTriageInputTo } from '@/gql/graphql'

const InboxListDoc = graphql(/* GraphQL */ `
  query Inbox {
    para {
      v1 {
        inboxList {
          items {
            id
            title
            created
          }
        }
      }
    }
  }
`)

const InboxShowDoc = graphql(/* GraphQL */ `
  query InboxShow($id: String!) {
    para {
      v1 {
        inboxShow(id: $id) {
          doc {
            id
            title
            body
          }
        }
      }
    }
  }
`)

const InboxAddDoc = graphql(/* GraphQL */ `
  mutation InboxAdd($body: para_InboxAddInputInput!) {
    para {
      v1 {
        inboxAdd(body: $body) {
          id
        }
      }
    }
  }
`)

const TriageDoc = graphql(/* GraphQL */ `
  mutation InboxTriage($body: para_InboxTriageInputInput!) {
    para {
      v1 {
        inboxTriage(body: $body) {
          id
          to
          warnings
        }
      }
    }
  }
`)

export function useInbox() {
  return useQuery({
    queryKey: ['inbox'],
    queryFn: async () =>
      need((await gqlClient.request(InboxListDoc)).para.v1.inboxList, 'inbox').items,
  })
}

export function useInboxItem(id: string) {
  return useQuery({
    queryKey: ['inbox', id],
    enabled: id !== '',
    queryFn: async () =>
      need((await gqlClient.request(InboxShowDoc, { id })).para.v1.inboxShow, 'item').doc,
  })
}

// useCapture adds a line to the inbox. It works from every screen.
export function useCapture() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (text: string) =>
      need((await gqlClient.request(InboxAddDoc, { body: { text } })).para.v1.inboxAdd, 'capture'),
    onSuccess: () => qc.invalidateQueries(),
  })
}

export type TriageInput = {
  id: string
  to: Para_InboxTriageInputTo
  project?: string
  owner?: string
  person?: string
  route?: string
  // question: exactly one scope, and optional kind and options
  area?: string
  task?: string
  root?: boolean
  kind?: Para_InboxTriageInputKind
  options?: string[]
  recommended?: string
}

export function useTriage() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: TriageInput) =>
      need(
        (
          await gqlClient.request(TriageDoc, {
            body: {
              id: b.id,
              to: b.to,
              project: b.project || undefined,
              owner: b.owner || undefined,
              person: b.person || undefined,
              route: b.route || undefined,
              area: b.area || undefined,
              task: b.task || undefined,
              root: b.root || undefined,
              kind: b.kind,
              options: b.options?.length ? b.options : undefined,
              recommended: b.recommended || undefined,
            },
          })
        ).para.v1.inboxTriage,
        'result',
      ),
    onSuccess: () => qc.invalidateQueries(),
  })
}
