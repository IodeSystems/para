import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

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

const QuestionReviewDoc = graphql(/* GraphQL */ `
  mutation QuestionReview($body: para_QuestionReviewInputInput!) {
    para {
      v1 {
        questionReview(body: $body) {
          id
          status
          review
          request
          undo
        }
      }
    }
  }
`)

export type QuestionReviewBody = {
  id: string
  decision: 'answer' | 'decline' | 'skip' | 'ask'
  choices?: string[]
  addedOptions?: string[]
  editedOptions?: Record<string, string>
  ownWords?: string
  note?: string
  refs?: string[]
}

// useQuestionReview is the owner's action on an open vault question from the stack: answer,
// decline, skip, or ask an agent. A decline and an ask need a note.
export function useQuestionReview() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: QuestionReviewBody) =>
      need(
        (await gqlClient.request(QuestionReviewDoc, { body })).para.v1.questionReview,
        'question review',
      ),
    onSettled: () => qc.invalidateQueries(),
  })
}
