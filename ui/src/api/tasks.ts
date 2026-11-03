import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import type { Para_TaskResolveInputOutcome } from '@/gql/graphql'

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
            outcome
            assignee
            project
            projectName
            parent
            parentName
            items
          }
          updates
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

const TaskUpdateDoc = graphql(/* GraphQL */ `
  mutation TaskUpdate($body: para_TaskUpdateInputInput!) {
    para {
      v1 {
        taskUpdate(body: $body) {
          id
          saved
          request
          appended
          note
        }
      }
    }
  }
`)

// useTaskUpdate adds free text to a task and queues agent work to sort it.
export function useTaskUpdate() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: { id: string; text: string }) =>
      need((await gqlClient.request(TaskUpdateDoc, { body: b })).para.v1.taskUpdate, 'result'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

const TaskResolveDoc = graphql(/* GraphQL */ `
  mutation TaskResolve($body: para_TaskResolveInputInput!) {
    para {
      v1 {
        taskResolve(body: $body) {
          id
          status
          outcome
          followUps
          moved
          released
          next
          warnings
        }
      }
    }
  }
`)

// useTaskResolve closes a task with an outcome: done, partial, dropped, duplicate, or superseded.
export function useTaskResolve() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: {
      id: string
      outcome: Para_TaskResolveInputOutcome
      note?: string
      link?: string
      followUps?: string[]
      doneWhen?: string
    }) =>
      need((await gqlClient.request(TaskResolveDoc, { body: b })).para.v1.taskResolve, 'result'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

const TaskStartDoc = graphql(/* GraphQL */ `
  mutation TaskStart($body: para_TaskStartInputInput!) {
    para {
      v1 {
        taskStart(body: $body) {
          id
          status
        }
      }
    }
  }
`)

export function useTaskStart() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: { id: string }) =>
      need((await gqlClient.request(TaskStartDoc, { body: b })).para.v1.taskStart, 'result'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

const TaskBlockDoc = graphql(/* GraphQL */ `
  mutation TaskBlock($body: para_TaskBlockInputInput!) {
    para {
      v1 {
        taskBlock(body: $body) {
          id
          status
        }
      }
    }
  }
`)

export function useTaskBlock() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: { id: string; reason: string }) =>
      need((await gqlClient.request(TaskBlockDoc, { body: b })).para.v1.taskBlock, 'result'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

const TaskAssignDoc = graphql(/* GraphQL */ `
  mutation TaskAssign($body: para_TaskAssignInputInput!) {
    para {
      v1 {
        taskAssign(body: $body) {
          id
          assignee
        }
      }
    }
  }
`)

export function useTaskAssign() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: { id: string; assignee: string }) =>
      need((await gqlClient.request(TaskAssignDoc, { body: b })).para.v1.taskAssign, 'result'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

const TaskDelegateDoc = graphql(/* GraphQL */ `
  mutation TaskDelegate($body: para_TaskDelegateInputInput!) {
    para {
      v1 {
        taskDelegate(body: $body) {
          id
          request
          status
          assignee
          appended
          note
        }
      }
    }
  }
`)

// useTaskDelegate gives a task to an agent: queues agent work linked to it, or adds the hint to
// the work that is open.
export function useTaskDelegate() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: { id: string; agent?: string; hint?: string }) =>
      need((await gqlClient.request(TaskDelegateDoc, { body: b })).para.v1.taskDelegate, 'result'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}
