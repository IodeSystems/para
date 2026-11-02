import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import type { Para_RfxTargetStateInputMedium, Para_RfxTargetStateInputState } from '@/gql/graphql'

const RfxListDoc = graphql(/* GraphQL */ `
  query RfxList {
    para {
      v1 {
        rfxList {
          items {
            id
            title
            direction
            status
            route
            routeName
            nextNudge
            waiting
            targetNames
            dependsOn
            dependsOnNames
            targets {
              to
              state
              medium
            }
          }
        }
      }
    }
  }
`)

const RfxShowDoc = graphql(/* GraphQL */ `
  query RfxShow($id: String!) {
    para {
      v1 {
        rfxShow(id: $id) {
          draft
          dependedOnBy
          dependedOnByNames
          route
          routeName
          targetNames
          targets {
            to
            state
            medium
            sent
            value
          }
          chain {
            id
            title
            type
            status
            depth
          }
          doc {
            id
            title
            body
            meta {
              direction
              status
              route
              nextNudge
              targets {
                to
                state
                medium
                sent
                ref
                value
              }
            }
          }
        }
      }
    }
  }
`)

const RfxReplyDoc = graphql(/* GraphQL */ `
  mutation RfxReply($body: para_RFXReplyInputInput!) {
    para {
      v1 {
        rfxReply(body: $body) {
          id
          from
          state
          constraint
          warnings
        }
      }
    }
  }
`)

export function useRfxList() {
  return useQuery({
    queryKey: ['rfx'],
    queryFn: async () => {
      const items = need((await gqlClient.request(RfxListDoc)).para.v1.rfxList, 'requests').items
      // Open requests first, in the server's order.
      return [...items].sort((a, b) => Number(a.status !== 'open') - Number(b.status !== 'open'))
    },
  })
}

export function useRfx(id: string) {
  return useQuery({
    queryKey: ['rfx', id],
    queryFn: async () =>
      need((await gqlClient.request(RfxShowDoc, { id })).para.v1.rfxShow, 'request'),
  })
}

export type ReplyInput = {
  id: string
  from: string
  outcome: 'answered' | 'partial' | 'no'
  text: string
  why?: string
}

export function useRfxReply() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: ReplyInput) =>
      need(
        (await gqlClient.request(RfxReplyDoc, { body: { ...b, why: b.why || undefined } })).para.v1
          .rfxReply,
        'reply',
      ),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

const TargetStateDoc = graphql(/* GraphQL */ `
  mutation RfxTargetState($body: para_RFXTargetStateInputInput!) {
    para {
      v1 {
        rfxTargetState(body: $body) {
          id
          targets {
            to
            state
            medium
          }
        }
      }
    }
  }
`)

export function useRfxTargetState() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: {
      id: string
      to: string
      medium: Para_RfxTargetStateInputMedium
      state: Para_RfxTargetStateInputState
    }) =>
      need((await gqlClient.request(TargetStateDoc, { body: b })).para.v1.rfxTargetState, 'result'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}
