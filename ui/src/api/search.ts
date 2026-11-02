import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

const SearchDoc = graphql(/* GraphQL */ `
  query Search(
    $query: String!
    $type: String
    $status: String
    $area: String
    $project: String
    $since: String
  ) {
    para {
      v1 {
        search(
          query: $query
          limit: 30
          type: $type
          status: $status
          area: $area
          project: $project
          since: $since
        ) {
          degraded
          hits {
            id
            title
            kind
            passage
            snippet
            marks {
              start
              end
            }
            unverified
            sourceUrl
            match
            via
            caveat
          }
        }
      }
    }
  }
`)

export type SearchFilters = {
  type?: string
  status?: string
  area?: string
  project?: string
  since?: string
}

export function useSearch(query: string, filters: SearchFilters = {}) {
  return useQuery({
    queryKey: ['search', query, filters],
    enabled: query.trim() !== '',
    // The last results stay on screen while the next ones load, so typing does not flash.
    placeholderData: keepPreviousData,
    queryFn: async () => {
      const r = need(
        (await gqlClient.request(SearchDoc, { query, ...filters })).para.v1.search,
        'hits',
      )
      return { hits: r.hits, degraded: r.degraded ?? '' }
    },
  })
}
