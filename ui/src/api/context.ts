import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import type { CaptureCall } from '@/lib/context'

// Context is what an item is for: where it came from, who it is for, and why the owner does it.
// The server reads what the vault already links; it stores nothing.

const ContextDoc = graphql(/* GraphQL */ `
  query Context($id: String!) {
    para {
      v1 {
        context(id: $id) {
          id
          looking
          cameFrom {
            kind
            id
            title
            medium
            mediumWords
            from
            fromId
            when
            url
            text
          }
          for {
            role
            id
            name
          }
          reasons {
            kind
            ref
            title
            due
            status
            text
            via {
              how
              id
              title
            }
          }
        }
      }
    }
  }
`)

export type ItemContextData = NonNullable<ReturnType<typeof useItemContext>['data']>

export function useItemContext(id: string) {
  return useQuery({
    queryKey: ['context', id],
    queryFn: async () =>
      need((await gqlClient.request(ContextDoc, { id })).para.v1.context, 'context'),
  })
}

const InteractionAddDoc = graphql(/* GraphQL */ `
  mutation InteractionAdd($body: para_InteractionAddInputInput!) {
    para {
      v1 {
        interactionAdd(body: $body) {
          id
          warnings
        }
      }
    }
  }
`)

const IOAddDoc = graphql(/* GraphQL */ `
  mutation IOAdd($body: para_IOAddInputInput!) {
    para {
      v1 {
        ioAdd(body: $body) {
          id
          warnings
        }
      }
    }
  }
`)

// useCapture logs a conversation the owner had (interaction_add, or io_add for a name not in the
// vault) or something they carried over by hand (io_add medium swivel).
export function useCapture() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (c: CaptureCall): Promise<{ id: string; warnings: string[] }> => {
      if (c.op === 'interaction_add') {
        const r = await gqlClient.request(InteractionAddDoc, { body: c.body })
        return need(r.para.v1.interactionAdd, 'conversation')
      }
      const r = await gqlClient.request(IOAddDoc, { body: c.body })
      return need(r.para.v1.ioAdd, 'message')
    },
    onSettled: () => qc.invalidateQueries(),
  })
}
