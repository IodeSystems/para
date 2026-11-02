import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

const RequestListDoc = graphql(/* GraphQL */ `
  query Requests {
    para {
      v1 {
        requestList {
          items {
            id
            title
            status
            runner
            queuedAt
            claimedBy
            leaseUntil
            linkNames
            links
          }
        }
      }
    }
  }
`)

const RequestAddDoc = graphql(/* GraphQL */ `
  mutation RequestAdd($body: para_RequestAddInputInput!) {
    para {
      v1 {
        requestAdd(body: $body) {
          id
        }
      }
    }
  }
`)

export function useRequests() {
  return useQuery({
    queryKey: ['requests'],
    queryFn: async () =>
      need((await gqlClient.request(RequestListDoc)).para.v1.requestList, 'requests').items,
  })
}

export function useRequestAdd() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (ask: string) =>
      need(
        (await gqlClient.request(RequestAddDoc, { body: { ask } })).para.v1.requestAdd,
        'request',
      ),
    onSuccess: () => void qc.invalidateQueries(),
  })
}
