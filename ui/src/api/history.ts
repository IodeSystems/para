import { useInfiniteQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

// History is the timeline of one item, newest first: what happened to it and what was said about
// it. The server joins events, receipts, the notes entries, the agent work, and the messages; it
// stores nothing new.

const HistoryDoc = graphql(/* GraphQL */ `
  query History($id: String!, $filter: para_historyFilter, $limit: Int, $before: String) {
    para {
      v1 {
        history(id: $id, filter: $filter, limit: $limit, before: $before) {
          id
          more
          nextBefore
          items {
            at
            dateOnly
            who
            actor
            kind
            summary
            text
            ref
            url
            undo
          }
        }
      }
    }
  }
`)

export type HistoryFilter = 'all' | 'conversation'

export type HistoryRow = NonNullable<
  ReturnType<typeof useHistory>['data']
>['pages'][number]['items'][number]

const PAGE = 30

// useHistory reads the timeline a page at a time; fetchNextPage reads older rows.
export function useHistory(id: string, filter: HistoryFilter) {
  return useInfiniteQuery({
    queryKey: ['history', id, filter],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) =>
      need(
        (await gqlClient.request(HistoryDoc, { id, filter, limit: PAGE, before: pageParam })).para
          .v1.history,
        'history',
      ),
    getNextPageParam: (last) => (last.more ? (last.nextBefore ?? undefined) : undefined),
  })
}
