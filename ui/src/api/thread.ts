import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

const ThreadDoc = graphql(/* GraphQL */ `
  query ThreadOf($id: String!) {
    para {
      v1 {
        thread(id: $id) {
          waitingNoAgent
          queued
          claimed
          turns {
            kind
            at
            request
            status
            title
            body
            ownerWords
          }
        }
      }
    }
  }
`)

// useThreadOf reads the agent turns on an item (thread, read only).
export function useThreadOf(id: string) {
  return useQuery({
    queryKey: ['thread', id],
    queryFn: async () =>
      need((await gqlClient.request(ThreadDoc, { id })).para.v1.thread, 'thread'),
  })
}
