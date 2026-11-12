import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

// Goals: the list is one request (list, type goal, with a summary per row), by due date.

const GoalListDoc = graphql(/* GraphQL */ `
  query GoalList($status: String) {
    para {
      v1 {
        list(type: goal, status: $status) {
          rows {
            id
            title
            status
            goal {
              project
              projectTitle
              due
              openTasks
              next
              nextTitle
            }
          }
        }
      }
    }
  }
`)

export type GoalStatus = 'active' | 'paused' | 'parked' | 'all'

// useGoals lists the goals of a status with their summaries, in one request.
export function useGoals(status: GoalStatus) {
  return useQuery({
    queryKey: ['goals', status],
    placeholderData: keepPreviousData,
    queryFn: async () =>
      need(
        (
          await gqlClient.request(GoalListDoc, {
            status: status === 'all' ? undefined : status,
          })
        ).para.v1.list,
        'goals',
      ).rows,
  })
}
