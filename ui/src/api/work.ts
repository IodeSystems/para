import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

const WorkDoc = graphql(/* GraphQL */ `
  query Work {
    para {
      v1 {
        goalList(all: true) {
          goals {
            id
            title
            status
            owner
          }
        }
        taskList(deferred: true) {
          tasks {
            id
            deferred
            deferUntil
            laterText
            waitsOn
            waitsOnNames
            readyWhy
            title
            status
            goal
            goalName
            partOf {
              kind
              via
              id
              title
            }
            area
            areaName
            due
            recur
            assignee
            assigneeName
            dependsOn
            dependsOnNames
            blockedBy
            blockers {
              id
              type
              state
              title
            }
            doneWhen
            outcome
            created
            updated
          }
        }
      }
    }
  }
`)

const GraphDoc = graphql(/* GraphQL */ `
  query WorkGraph($goal: String) {
    para {
      v1 {
        graph(goal: $goal) {
          order
          cycles
          edges {
            from
            to
          }
          nodes {
            id
            title
            type
            status
            state
            kind
            goal
            goalName
            scope
            scopeName
            blockedBy
            waitingOn
          }
        }
      }
    }
  }
`)

export function useWork() {
  return useQuery({
    queryKey: ['work'],
    queryFn: async () => {
      const v = (await gqlClient.request(WorkDoc)).para.v1
      return {
        goals: need(v.goalList, 'goals').goals,
        tasks: need(v.taskList, 'tasks').tasks,
      }
    },
  })
}

export function useGraph(goal: string | undefined) {
  return useQuery({
    queryKey: ['graph', goal ?? ''],
    queryFn: async () =>
      need((await gqlClient.request(GraphDoc, { goal })).para.v1.graph, 'graph'),
  })
}
