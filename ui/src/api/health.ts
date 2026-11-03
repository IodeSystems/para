import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

const HealthDoc = graphql(/* GraphQL */ `
  query Health {
    para {
      v1 {
        health {
          count
          dismissed
          groups {
            goal
            title
            due
            findings {
              kind
              label
              severity
              id
              title
              goal
              message
              fix
            }
          }
        }
      }
    }
  }
`)

const HealthDismissDoc = graphql(/* GraphQL */ `
  mutation HealthDismiss($body: para_HealthDismissInputInput!) {
    para {
      v1 {
        healthDismiss(body: $body) {
          id
          dismissed
        }
      }
    }
  }
`)

export function useHealth() {
  return useQuery({
    queryKey: ['health'],
    queryFn: async () => need((await gqlClient.request(HealthDoc)).para.v1.health, 'health'),
  })
}

export function useDismissHealth() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: { id: string; kind: string; reason: string }) =>
      need(
        (await gqlClient.request(HealthDismissDoc, { body: b })).para.v1.healthDismiss,
        'result',
      ),
    onSuccess: () => void qc.invalidateQueries(),
  })
}
