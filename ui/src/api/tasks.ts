import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

const TaskShowDoc = graphql(/* GraphQL */ `
  query TaskShow($id: String!) {
    para {
      v1 {
        taskShow(id: $id) {
          task {
            id
            title
            status
            doneWhen
            doUrl
            doCommand
            steps
            outcome
            assignee
            project
            projectName
            parent
            parentName
            items
          }
          updates
          rules {
            id
            title
            via
          }
          questions {
            id
            title
            kind
          }
          parts {
            actionItems {
              id
              title
              status
              due
            }
            commitments {
              id
              title
              status
              due
              owner
            }
            answered {
              id
              title
              choice
            }
            policy {
              id
              title
              kind
            }
            followUps {
              id
              title
              status
              due
            }
          }
        }
      }
    }
  }
`)

export function useTask(id: string) {
  return useQuery({
    queryKey: ['task', id],
    queryFn: async () =>
      need((await gqlClient.request(TaskShowDoc, { id })).para.v1.taskShow, 'task').task,
  })
}

// useTaskParts is the task page: its sections, computed from links, and the update text.
export function useTaskParts(id: string) {
  return useQuery({
    queryKey: ['task', id, 'parts'],
    queryFn: async () =>
      need((await gqlClient.request(TaskShowDoc, { id })).para.v1.taskShow, 'task'),
  })
}
