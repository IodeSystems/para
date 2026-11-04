import { useQuery } from '@tanstack/react-query'
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

export function useHealth() {
  return useQuery({
    queryKey: ['health'],
    queryFn: async () => need((await gqlClient.request(HealthDoc)).para.v1.health, 'health'),
  })
}
