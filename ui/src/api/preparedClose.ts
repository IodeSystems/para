import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

// A proposed close of a task (prepared_close): an agent found it done or no longer needed, with
// evidence and a confidence. The owner approves it in one call (prepared_close_approve), which
// leaves a receipt that receipt_revert undoes.

const PreparedCloseDoc = graphql(/* GraphQL */ `
  query PreparedClose($id: String!) {
    para {
      v1 {
        preparedClose(id: $id) {
          id
          proposed
          ready
          stale
          by
          at
          high
          check
          close {
            outcome
            reason
            confidence
            confidenceBasis
            evidence {
              quote
              locator
              verify
              via {
                source
                ref
              }
            }
          }
        }
      }
    }
  }
`)

export function usePreparedClose(id: string) {
  return useQuery({
    queryKey: ['preparedClose', id],
    queryFn: async () =>
      need(
        (await gqlClient.request(PreparedCloseDoc, { id })).para.v1.preparedClose,
        'proposed close',
      ),
  })
}

export type PreparedCloseData = NonNullable<ReturnType<typeof usePreparedClose>['data']>

const ApproveDoc = graphql(/* GraphQL */ `
  mutation PreparedCloseApprove($body: para_PreparedCloseInputInput!) {
    para {
      v1 {
        preparedCloseApprove(body: $body) {
          id
          outcome
          proposedBy
          op
          receipt
        }
      }
    }
  }
`)

// useApproveClose closes the task as the agent proposed it, as the owner.
export function useApproveClose() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: string) =>
      need(
        (await gqlClient.request(ApproveDoc, { body: { id } })).para.v1.preparedCloseApprove,
        'close',
      ),
    onSettled: () => qc.invalidateQueries(),
  })
}
