import { useMutation, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

// PrioritySet is how much a person, a group, or a source matters to the owner (priority_set, a
// person op): an incoming message from a high one needs the owner. Unset clears it; a person
// then takes the highest priority of their groups.
const PrioritySetDoc = graphql(/* GraphQL */ `
  mutation PrioritySet($body: para_PrioritySetInputInput!) {
    para {
      v1 {
        prioritySet(body: $body) {
          id
          priority
          rev
        }
      }
    }
  }
`)

export type Priority = 'high' | 'normal' | 'low' | 'unset'

export function useSetPriority() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ id, priority }: { id: string; priority: Priority }) =>
      need(
        (
          await gqlClient.request(PrioritySetDoc, {
            body: { id, priority: priority === 'unset' ? undefined : priority },
          })
        ).para.v1.prioritySet,
        'priority',
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['who'] })
      void qc.invalidateQueries({ queryKey: ['sources'] })
      void qc.invalidateQueries({ queryKey: ['doc'] })
      void qc.invalidateQueries({ queryKey: ['io'] })
    },
  })
}
