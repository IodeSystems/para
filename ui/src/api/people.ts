import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

const PeopleDoc = graphql(/* GraphQL */ `
  query People {
    para {
      v1 {
        orgPersonList {
          items {
            id
            title
          }
        }
      }
    }
  }
`)

const WhoDoc = graphql(/* GraphQL */ `
  query Who($query: String!) {
    para {
      v1 {
        who(query: $query) {
          query
          candidates {
            id
            title
          }
          person {
            id
            name
            org
            aliases
            notes
            priority
            effectivePriority
            effectiveFrom
            role {
              id
              title
            }
            team {
              id
              title
            }
            groups {
              id
              title
            }
            contacts {
              method
              value
              label
              preferred
            }
            openRfx {
              id
              title
            }
            interactions {
              id
              title
              date
              medium
            }
          }
        }
      }
    }
  }
`)

export function usePeople() {
  return useQuery({
    queryKey: ['people'],
    queryFn: async () =>
      need((await gqlClient.request(PeopleDoc)).para.v1.orgPersonList, 'people').items,
  })
}

export function useWho(query: string) {
  return useQuery({
    queryKey: ['who', query],
    enabled: query.trim() !== '',
    queryFn: async () => need((await gqlClient.request(WhoDoc, { query })).para.v1.who, 'answer'),
  })
}
