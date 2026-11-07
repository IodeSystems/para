import { useMutation, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

const RetryDecideDoc = graphql(/* GraphQL */ `
  mutation RetryDecide($body: para_RetryDecideInputInput!) {
    para {
      v1 {
        retryDecide(body: $body) {
          id
          status
          applied
          doc
        }
      }
    }
  }
`)

export type RetryDecision = {
  request: string
  action: 'accept' | 'edit' | 'drop' | 'reject'
  input?: Record<string, unknown>
  notes?: string
  approve?: boolean
}

// useRetryDecide is the owner's decision on an agent's proposed fix of a refused write
// (retry_decide, a person op): accept, edit (save the input as the proposal; with approve, run it
// too), drop, or reject with notes.
export function useRetryDecide() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: RetryDecision) =>
      need((await gqlClient.request(RetryDecideDoc, { body })).para.v1.retryDecide, 'retry decide'),
    onSettled: () => qc.invalidateQueries(),
  })
}
