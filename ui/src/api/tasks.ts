import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { needDetail } from '@/client/need'

const TaskShowDoc = graphql(/* GraphQL */ `
  query TaskShow($id: String!) {
    para {
      v1 {
        show(id: $id) {
          detail {
            __typename
            ... on para_TaskShowOutput {
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
    }
  }
`)

// taskShow is the task detail of show, or null when the id is not a task (an incident or a
// document of another type): callers show nothing for it.
async function taskShow(id: string) {
  const show = (await gqlClient.request(TaskShowDoc, { id })).para.v1.show
  if (show?.detail?.__typename !== 'para_TaskShowOutput') return null
  return needDetail(show, 'para_TaskShowOutput', 'task')
}

export function useTask(id: string) {
  return useQuery({
    queryKey: ['task', id],
    queryFn: async () => (await taskShow(id))?.task ?? null,
  })
}

// useTaskParts is the task page: its sections, computed from links, and the update text.
export function useTaskParts(id: string) {
  return useQuery({
    queryKey: ['task', id, 'parts'],
    queryFn: () => taskShow(id),
  })
}
