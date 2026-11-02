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

const RequestCancelDoc = graphql(/* GraphQL */ `
  mutation RequestCancel($body: para_RequestCancelInputInput!) {
    para {
      v1 {
        requestCancel(body: $body) {
          id
          status
        }
      }
    }
  }
`)

export function useRequestCancel() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: { id: string }) =>
      need(
        (await gqlClient.request(RequestCancelDoc, { body: b })).para.v1.requestCancel,
        'result',
      ),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

const RequestAcceptDoc = graphql(/* GraphQL */ `
  mutation RequestAccept($body: para_RequestAcceptInputInput!) {
    para {
      v1 {
        requestAccept(body: $body) {
          id
          status
        }
      }
    }
  }
`)

// useRequestAccept moves a proposed agent request into the queue. Only a browser session may.
export function useRequestAccept() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: { id: string }) =>
      need(
        (await gqlClient.request(RequestAcceptDoc, { body: b })).para.v1.requestAccept,
        'result',
      ),
    onSuccess: () => void qc.invalidateQueries(),
  })
}
