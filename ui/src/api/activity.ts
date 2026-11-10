import { useInfiniteQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

// Activity is the change log of the vault, newest first: what changed, who changed it, the item,
// and why when an agent write has a receipt. The server joins the events of every install with the
// receipts; it stores nothing new. A read: it needs no write scope.

const ActivityDoc = graphql(/* GraphQL */ `
  query Activity(
    $since: String
    $until: String
    $actor: String
    $types: [String!]
    $doc: String
    $limit: Int
    $cursor: String
  ) {
    para {
      v1 {
        activity(
          since: $since
          until: $until
          actor: $actor
          types: $types
          doc: $doc
          limit: $limit
          cursor: $cursor
        ) {
          more
          nextCursor
          items {
            at
            first
            count
            who
            actor
            install
            remote
            kind
            summary
            from
            to
            doc
            docType
            docName
            receipt
            receipts
            why
            sources
            lines
          }
        }
      }
    }
  }
`)

export type ActivityFilter = {
  since?: string
  until?: string
  actor?: string
  types?: string[]
  doc?: string
}

export type ActivityItem = NonNullable<
  ReturnType<typeof useActivity>['data']
>['pages'][number]['items'][number]

const PAGE = 40

// useActivity reads the change log a page at a time; fetchNextPage reads older rows.
export function useActivity(filter: ActivityFilter) {
  return useInfiniteQuery({
    queryKey: ['activity', filter],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) =>
      need(
        (await gqlClient.request(ActivityDoc, { ...filter, limit: PAGE, cursor: pageParam })).para
          .v1.activity,
        'activity',
      ),
    getNextPageParam: (last) => (last.more ? (last.nextCursor ?? undefined) : undefined),
  })
}
