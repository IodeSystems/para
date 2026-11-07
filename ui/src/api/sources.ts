import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

// Sources: the places work comes in and goes out (mailboxes, calendars, chats, trackers,
// folders). Agents reach them with their own tools and declare which ones they reach; the web
// app only reads.

const SourceListDoc = graphql(/* GraphQL */ `
  query SourceList {
    para {
      v1 {
        sourceList {
          reachDays
          sources {
            id
            title
            system
            vendor
            priority
            status
            via
            server
            cadence
            lastScan
            reachedBy
            declared {
              actor
              at
              fresh
            }
            due
            overdue
            request
            lastSweep
            lastFailed
          }
        }
        ioStats {
          sources {
            id
            lastFailure
            lastError
            openReplies
            overdue
            held
            windows {
              days
              sweeps
              failed
              in
              out
              replyHours
              replied
            }
          }
        }
      }
    }
  }
`)

const SourceShowDoc = graphql(/* GraphQL */ `
  query SourceShow($id: String!) {
    para {
      v1 {
        sourceShow(id: $id) {
          source {
            id
            title
            system
            vendor
            priority
            status
            via
            server
            cadence
            lastScan
            reachedBy
            declared {
              actor
              at
              fresh
            }
            due
            overdue
            request
          }
          tools
          fetch {
            tool
            args
            command
            query
            how
          }
          scanIn {
            tool
            args
            command
            query
            how
          }
          scanOut {
            tool
            args
            command
            query
            how
          }
          state {
            tool
            args
            command
            query
            how
          }
          body
          cites {
            id
            title
            type
            ref
          }
        }
      }
    }
  }
`)

export function useSourceList() {
  return useQuery({
    queryKey: ['sources'],
    queryFn: async () => {
      const v1 = (await gqlClient.request(SourceListDoc)).para.v1
      const list = need(v1.sourceList, 'sources')
      // The numbers of each source (io_stats), by id; a source with none has no entry.
      const stats = new Map((v1.ioStats?.sources ?? []).map((s) => [s.id, s]))
      return { ...list, stats }
    },
  })
}

export function useSourceShow(id: string) {
  return useQuery({
    queryKey: ['sources', 'show', id],
    enabled: id !== '',
    queryFn: async () =>
      need((await gqlClient.request(SourceShowDoc, { id })).para.v1.sourceShow, 'source'),
  })
}

export type SourceRow = NonNullable<ReturnType<typeof useSourceList>['data']>['sources'][number]
