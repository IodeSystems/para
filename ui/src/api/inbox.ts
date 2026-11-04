import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

const InboxListDoc = graphql(/* GraphQL */ `
  query Inbox {
    para {
      v1 {
        inboxList {
          items {
            id
            title
            created
          }
        }
      }
    }
  }
`)

const InboxShowDoc = graphql(/* GraphQL */ `
  query InboxShow($id: String!) {
    para {
      v1 {
        inboxShow(id: $id) {
          doc {
            id
            title
            body
          }
        }
      }
    }
  }
`)

export function useInbox() {
  return useQuery({
    queryKey: ['inbox'],
    queryFn: async () =>
      need((await gqlClient.request(InboxListDoc)).para.v1.inboxList, 'inbox').items,
  })
}

export function useInboxItem(id: string) {
  return useQuery({
    queryKey: ['inbox', id],
    enabled: id !== '',
    queryFn: async () =>
      need((await gqlClient.request(InboxShowDoc, { id })).para.v1.inboxShow, 'item').doc,
  })
}
