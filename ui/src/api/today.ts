import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

export const TodayGroupFields = graphql(/* GraphQL */ `
  fragment TodayGroupFields on para_TodayGroup {
    count
    items {
      id
      title
      due
      detail
      blocks
      project
      projectName
      scope
      scopeName
      with
      kind
      why
      proposal
    }
  }
`)

const TodayDoc = graphql(/* GraphQL */ `
  query Today {
    para {
      v1 {
        uiToday {
          date
          today {
            vault
            error
            inbox
            unverified
            waiting
            answersToCheck
            syncConflicts
            lostUpdates
            incidents {
              ...TodayGroupFields
            }
            agenda {
              ...TodayGroupFields
            }
            otherOpen
            health
            deferred
            overdue {
              ...TodayGroupFields
            }
            dueSoon {
              ...TodayGroupFields
            }
            nudges {
              ...TodayGroupFields
            }
            due {
              ...TodayGroupFields
            }
            blocked {
              ...TodayGroupFields
            }
            expiring {
              ...TodayGroupFields
            }
          }
        }
        next {
          tasks {
            id
            title
            status
            project
            projectName
            due
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
      const v = (await gqlClient.request(TodayDoc)).para.v1
      return {
        today: need(v.uiToday, 'today').today,
        next: need(v.next, 'next tasks').tasks,
      }
    },
  })
}

// localDate is today's date in the browser's zone, as YYYY-MM-DD.
export function localDate(d = new Date()): string {
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}
