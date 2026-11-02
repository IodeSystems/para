import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

const TaskDoneDoc = graphql(/* GraphQL */ `
  mutation TaskDone($body: para_TaskDoneInputInput!) {
    para {
      v1 {
        taskDone(body: $body) {
          id
          status
          next
          request
          doneWhen
          warnings
        }
      }
    }
  }
`)

// useTaskDone finishes a task. The note records what was done or decided.
export function useTaskDone() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: { id: string; note?: string }) =>
      need((await gqlClient.request(TaskDoneDoc, { body: b })).para.v1.taskDone, 'result'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

const TaskDueDoc = graphql(/* GraphQL */ `
  mutation TaskDue($body: para_TaskDueInputInput!) {
    para {
      v1 {
        taskDue(body: $body) {
          id
          due
        }
      }
    }
  }
`)

// useTaskDue sets a task's due date (an ISO date), or clears it.
export function useTaskDue() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: { id: string; date?: string; clear?: boolean }) =>
      need((await gqlClient.request(TaskDueDoc, { body: b })).para.v1.taskDue, 'result'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

const TaskEditDoc = graphql(/* GraphQL */ `
  mutation TaskEdit($body: para_TaskEditInputInput!) {
    para {
      v1 {
        taskEdit(body: $body) {
          id
          doneWhen
        }
      }
    }
  }
`)

// useTaskEdit sets how we know a task is done.
export function useTaskEdit() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: { id: string; doneWhen: string }) =>
      need((await gqlClient.request(TaskEditDoc, { body: b })).para.v1.taskEdit, 'result'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

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
            project
            projectName
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
