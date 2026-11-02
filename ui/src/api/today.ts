import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

const TodayDoc = graphql(/* GraphQL */ `
  query Today($today: String) {
    para {
      v1 {
        uiToday {
          date
          today {
            vault
            error
            inbox
            unverified
            syncConflicts
            lostUpdates
            due {
              count
              items {
                id
                title
              }
            }
            blocked {
              count
              items {
                id
                title
              }
            }
            nudges {
              count
              items {
                id
                title
              }
            }
            expiring {
              count
              items {
                id
                title
              }
            }
          }
        }
        next {
          tasks {
            id
            title
            status
            project
          }
        }
        rfxWaiting(today: $today) {
          items {
            id
            title
            route
            nextNudge
            due
            stalled
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

export function useToday() {
  return useQuery({
    queryKey: ['today'],
    queryFn: async () => {
      const v = (await gqlClient.request(TodayDoc, { today: localDate() })).para.v1
      return {
        today: need(v.uiToday, 'today').today,
        next: need(v.next, 'next tasks').tasks,
        waiting: need(v.rfxWaiting, 'waiting requests').items,
      }
    },
  })
}

// localDate is today's date in the browser's zone, as YYYY-MM-DD.
export function localDate(d = new Date()): string {
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}
