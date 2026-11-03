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
    scopeName
    scopeType
    options
    recommended
    context
    optionDetails {
      name
      pros {
        text
        source
      }
      cons {
        text
        source
      }
      cost {
        text
        source
      }
      benefit {
        text
        source
      }
      confidence
      confidenceBasis
      recommendation
      examples {
        kind
        text
        source
      }
    }
    blocking
    blocks
    choice
    answered
    answeredBy
    rfx
    dependsOn
    waitingOn
    hasProposal
    proposedAnswer
    proposedChoice
    proposedBy
    proposedWhy
    proposed
    proposedSources {
      ref
      quote
    }
    rejectedReason
    rejectedBy
    deferred
    deferUntil
    deferredReason
    waitsOn
    waitsOnNames
    readyWhy
    agentLooking
  }
`)

const QuestionListDoc = graphql(/* GraphQL */ `
  query QuestionList(
    $project: String
    $area: String
    $task: String
    $root: Boolean
    $deferred: Boolean
  ) {
    para {
      v1 {
        questionList(
          project: $project
          area: $area
          task: $task
          root: $root
          deferred: $deferred
        ) {
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

const AcceptDoc = graphql(/* GraphQL */ `
  mutation QuestionAccept($body: para_QuestionAcceptInputInput!) {
    para {
      v1 {
        questionAccept(body: $body) {
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

const RejectDoc = graphql(/* GraphQL */ `
  mutation QuestionReject($body: para_QuestionRejectInputInput!) {
    para {
      v1 {
        questionReject(body: $body) {
          id
          status
        }
      }
    }
  }
`)

const EditDoc = graphql(/* GraphQL */ `
  mutation QuestionEdit($body: para_QuestionEditInputInput!) {
    para {
      v1 {
        questionEdit(body: $body) {
          id
          title
          options
          recommended
        }
      }
    }
  }
`)

const DependsAddDoc = graphql(/* GraphQL */ `
  mutation QuestionDependsAdd($body: para_QuestionDependsInputInput!) {
    para {
      v1 {
        questionDependsAdd(body: $body) {
          id
          dependsOn
        }
      }
    }
  }
`)

const DependsRemoveDoc = graphql(/* GraphQL */ `
  mutation QuestionDependsRemove($body: para_QuestionDependsInputInput!) {
    para {
      v1 {
        questionDependsRemove(body: $body) {
          id
          dependsOn
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

const DeferDoc = graphql(/* GraphQL */ `
  mutation Defer($body: para_DeferInputInput!) {
    para {
      v1 {
        defer(body: $body) {
          changed
          skipped
          until
          after
          created
          similar {
            id
            title
          }
        }
      }
    }
  }
`)

const UndeferDoc = graphql(/* GraphQL */ `
  mutation Undefer($body: para_UndeferInputInput!) {
    para {
      v1 {
        undefer(body: $body) {
          changed
          skipped
        }
      }
    }
  }
`)

const InvestigateDoc = graphql(/* GraphQL */ `
  mutation Investigate($body: para_InvestigateInputInput!) {
    para {
      v1 {
        investigate(body: $body) {
          id
          request
          status
          runner
          appended
        }
      }
    }
  }
`)

const CutDoc = graphql(/* GraphQL */ `
  mutation DependsCut($body: para_DependsCutInputInput!) {
    para {
      v1 {
        dependsCut(body: $body) {
          id
          dependsOn
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
  // deferred includes the questions put off with Later; each is marked.
  deferred?: boolean
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
  // sources are optional for a person: a URL, a document id, or words such as a meeting.
  sources?: { ref: string; quote?: string }[]
}

// An answer unblocks tasks and requests anywhere, so every query refetches.
export function useAnswer() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: AnswerInput) =>
      need((await gqlClient.request(AnswerDoc, { body: b })).para.v1.questionAnswer, 'answer'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

export function useDropQuestion() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: { id: string; reason?: string }) =>
      need((await gqlClient.request(DropDoc, { body: b })).para.v1.questionDrop, 'result'),
    onSuccess: () => void qc.invalidateQueries(),
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
    onSuccess: () => void qc.invalidateQueries(),
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
    onSuccess: () => void qc.invalidateQueries(),
  })
}

export type AcceptInput = { id: string; text?: string; choice?: string; why?: string }

// useAcceptAnswer turns an agent's proposed answer into the answer. Dependents unblock, so every
// query refetches.
export function useAcceptAnswer() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: AcceptInput) =>
      need((await gqlClient.request(AcceptDoc, { body: b })).para.v1.questionAccept, 'answer'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

// useRejectAnswer sends a proposed answer back with a reason the agent can read.
export function useRejectAnswer() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: { id: string; reason: string }) =>
      need((await gqlClient.request(RejectDoc, { body: b })).para.v1.questionReject, 'result'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

export type QuestionEditInput = {
  id: string
  title?: string
  options?: string[]
  clearOptions?: boolean
  recommended?: string
  clearRecommended?: boolean
}

export function useEditQuestion() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: QuestionEditInput) =>
      need((await gqlClient.request(EditDoc, { body: b })).para.v1.questionEdit, 'result'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

export function useQuestionDepends() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: { id: string; dependsOn: string[]; remove?: boolean }) => {
      const body = { id: b.id, dependsOn: b.dependsOn }
      return b.remove
        ? need(
            (await gqlClient.request(DependsRemoveDoc, { body })).para.v1.questionDependsRemove,
            'result',
          )
        : need(
            (await gqlClient.request(DependsAddDoc, { body })).para.v1.questionDependsAdd,
            'result',
          )
    },
    onSuccess: () => void qc.invalidateQueries(),
  })
}

// useDefer puts questions, tasks, or goals off (Later). They keep blocking what depends on them.
export function useDefer() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: {
      ids: string[]
      until?: string
      reason?: string
      after?: string
      when?: string
      place?: string
      new?: boolean
    }) => need((await gqlClient.request(DeferDoc, { body: b })).para.v1.defer, 'result'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

export function useUndefer() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (ids: string[]) =>
      need((await gqlClient.request(UndeferDoc, { body: { ids } })).para.v1.undefer, 'result'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

// useInvestigate queues agent work that finds context for an item, or adds the hint to the open
// work. The item stays as it is.
export function useInvestigate() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: { id: string; hint: string }) =>
      need((await gqlClient.request(InvestigateDoc, { body: b })).para.v1.investigate, 'result'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

// useCut removes one dependency so the work can go ahead, with the reason recorded on it.
export function useCut() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: { id: string; dep: string; reason: string }) =>
      need((await gqlClient.request(CutDoc, { body: b })).para.v1.dependsCut, 'result'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}
