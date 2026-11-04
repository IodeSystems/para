import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

const IoListDoc = graphql(/* GraphQL */ `
  query IoList($direction: para_ioListDirection, $all: Boolean) {
    para {
      v1 {
        ioList(direction: $direction, all: $all) {
          counts {
            unread
            read
            draft
            ready
          }
          items {
            id
            title
            direction
            medium
            state
            from
            to
            at
            subject
            expects
            became
            legacy
            created
          }
        }
      }
    }
  }
`)

const IoShowDoc = graphql(/* GraphQL */ `
  query IoShow($id: String!) {
    para {
      v1 {
        ioShow(id: $id) {
          doc {
            id
            title
            body
          }
          item {
            id
            title
            direction
            medium
            state
            from
            to
            at
            subject
            ref
            expects
            route
            became
            thread
            reason
            legacy
            created
          }
        }
      }
    }
  }
`)

export type IoDirection = 'in' | 'out'

// The open items of one direction (the inbox or the outbox), or every item with all, and the
// counts of every open item.
export function useIoList(direction?: IoDirection, all = false) {
  return useQuery({
    queryKey: ['io', direction ?? '', all],
    queryFn: async () =>
      need((await gqlClient.request(IoListDoc, { direction, all })).para.v1.ioList, 'io items'),
  })
}

export function useIoItem(id: string) {
  return useQuery({
    queryKey: ['io', 'item', id],
    enabled: id !== '',
    queryFn: async () =>
      need((await gqlClient.request(IoShowDoc, { id })).para.v1.ioShow, 'io item'),
  })
}
