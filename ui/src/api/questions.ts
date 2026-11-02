import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import type { Para_QuestionAskInputVia, Para_TaskToQuestionInputKind } from '@/gql/graphql'

export const QuestionFields = graphql(/* GraphQL */ `
  fragment QuestionFields on para_QuestionView {
    id
    title
    kind
    status
    scope
    scopeType
    options
    recommended
    blocking
    blocks
    choice
    answered
    answeredBy
    rfx
  }
`)

const QuestionListDoc = graphql(/* GraphQL */ `
  query QuestionList($project: String, $area: String, $task: String, $root: Boolean) {
    para {
      v1 {
        questionList(project: $project, area: $area, task: $task, root: $root) {
          questions {
            ...QuestionFields
          }
        }
      }
    }
  }
`)

const QuestionShowDoc = graphql(/* GraphQL */ `
  query QuestionShow($id: String!) {
    para {
      v1 {
        questionShow(id: $id) {
          body
          question {
            ...QuestionFields
          }
        }
      }
    }
  }
`)

const AnswerDoc = graphql(/* GraphQL */ `
  mutation QuestionAnswer($body: para_QuestionAnswerInputInput!) {
    para {
      v1 {
        questionAnswer(body: $body) {
          id
          status
          choice
          answeredBy
          answered
          unblocked
        }
      }
    }
  }
`)

const DropDoc = graphql(/* GraphQL */ `
  mutation QuestionDrop($body: para_QuestionDropInputInput!) {
    para {
      v1 {
        questionDrop(body: $body) {
          id
          status
          unblocked
        }
      }
    }
  }
`)

const AskDoc = graphql(/* GraphQL */ `
  mutation QuestionAsk($body: para_QuestionAskInputInput!) {
    para {
      v1 {
        questionAsk(body: $body) {
          id
          rfx
          warnings
        }
      }
    }
  }
`)

const ToQuestionDoc = graphql(/* GraphQL */ `
  mutation TaskToQuestion($body: para_TaskToQuestionInputInput!) {
    para {
      v1 {
        taskToQuestion(body: $body) {
          question
          task
          relinked
        }
      }
    }
  }
`)

export type QuestionScope = {
  project?: string
  area?: string
  task?: string
  root?: boolean
}

// useQuestions lists open questions in a scope, ordered by the server by what they block.
export function useQuestions(scope: QuestionScope = {}) {
  return useQuery({
    queryKey: ['questions', scope],
    queryFn: async () =>
      need((await gqlClient.request(QuestionListDoc, scope)).para.v1.questionList, 'questions')
        .questions,
  })
}

export function useQuestion(id: string) {
  return useQuery({
    queryKey: ['question', id],
    queryFn: async () =>
      need((await gqlClient.request(QuestionShowDoc, { id })).para.v1.questionShow, 'question'),
  })
}

export type AnswerInput = {
  id: string
  choice?: string
  text?: string
  why?: string
  by?: string
  source?: string
}

// An answer unblocks tasks and requests anywhere, so every query refetches.
export function useAnswer() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: AnswerInput) =>
      need((await gqlClient.request(AnswerDoc, { body: b })).para.v1.questionAnswer, 'answer'),
    onSuccess: () => qc.invalidateQueries(),
  })
}

export function useDropQuestion() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: { id: string; reason?: string }) =>
      need((await gqlClient.request(DropDoc, { body: b })).para.v1.questionDrop, 'result'),
    onSuccess: () => qc.invalidateQueries(),
  })
}

export function useAskQuestion() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: {
      id: string
      to: string
      route: string
      via: Para_QuestionAskInputVia
    }) => need((await gqlClient.request(AskDoc, { body: b })).para.v1.questionAsk, 'result'),
    onSuccess: () => qc.invalidateQueries(),
  })
}

export function useTaskToQuestion() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: {
      id: string
      kind?: Para_TaskToQuestionInputKind
      options?: string[]
      recommended?: string
    }) =>
      need((await gqlClient.request(ToQuestionDoc, { body: b })).para.v1.taskToQuestion, 'result'),
    onSuccess: () => qc.invalidateQueries(),
  })
}
