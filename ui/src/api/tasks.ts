import { useMutation, useQueryClient } from '@tanstack/react-query'
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
