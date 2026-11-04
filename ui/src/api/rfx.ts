import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

const RfxListDoc = graphql(/* GraphQL */ `
  query RfxList {
    para {
      v1 {
        rfxList {
          items {
            id
            title
            direction
            status
            route
            routeName
            nextNudge
            waiting
            targetNames
            dependsOn
            dependsOnNames
            targets {
              to
              state
              medium
            }
          }
        }
      }
    }
  }
`)

const RfxShowDoc = graphql(/* GraphQL */ `
  query RfxShow($id: String!) {
    para {
      v1 {
        rfxShow(id: $id) {
          draft
          dependedOnBy
          dependedOnByNames
          route
          routeName
          targetNames
          targets {
            to
            state
            medium
            sent
            value
          }
          chain {
            id
            title
            type
            status
            depth
          }
          doc {
            id
            title
            body
            meta {
              direction
              status
              route
              nextNudge
              targets {
                to
                state
                medium
                sent
                ref
                value
              }
            }
          }
        }
      }
    }
  }
`)

export function useRfxList() {
  return useQuery({
    queryKey: ['rfx'],
    queryFn: async () => {
      const items = need((await gqlClient.request(RfxListDoc)).para.v1.rfxList, 'requests').items
      // Open requests first, in the server's order.
      return [...items].sort((a, b) => Number(a.status !== 'open') - Number(b.status !== 'open'))
    },
  })
}

export function useRfx(id: string) {
  return useQuery({
    queryKey: ['rfx', id],
    queryFn: async () =>
      need((await gqlClient.request(RfxShowDoc, { id })).para.v1.rfxShow, 'request'),
  })
}
