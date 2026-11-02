import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

const SearchDoc = graphql(/* GraphQL */ `
  query Search($query: String!) {
    para {
      v1 {
        search(query: $query, limit: 30) {
          hits {
            id
            title
            kind
            passage
            unverified
            sourceUrl
          }
        }
      }
    }
  }
`)

export function useSearch(query: string) {
  return useQuery({
    queryKey: ['search', query],
    enabled: query.trim() !== '',
    queryFn: async () =>
      need((await gqlClient.request(SearchDoc, { query })).para.v1.search, 'hits').hits,
  })
}
