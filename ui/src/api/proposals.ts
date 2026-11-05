import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

// Proposals (dossiers) and the review stack. A session with review:decide drafts answers, submits
// them, and approves or rejects items across dossiers. Every other change goes through the chat.

const ProposalListDoc = graphql(/* GraphQL */ `
  query ProposalList {
    para {
      v1 {
        proposalList {
          items {
            id
            title
            status
            revision
            itemCount
            pending
            proposedBy
            created
            sourceName
            error
          }
        }
      }
    }
  }
`)

const ProposalShowDoc = graphql(/* GraphQL */ `
  query ProposalShow($id: String!) {
    para {
      v1 {
        proposalShow(id: $id) {
          rev
          tldr
          context
          untrusted
          dossier
          markdown
          sections {
            id
            title
          }
          response {
            state
            via
            submittedAt
            overall
            sections
          }
          replies {
            section
            turn {
              id
              kind
              text
              by
            }
          }
          reviewCommand
          reviewUrl
          supersededBy
          proposal {
            id
            title
            status
            revision
            itemCount
            pending
            proposedBy
            created
            sourceDoc
            sourcePath
            sourceName
            error
          }
          source {
            id
            title
            body
            error
          }
          items {
            id
            kind
            summary
            stack
            deferUntil
            section
            rev
            multi
            options
            recommended
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
            range
            old
            new
            from
            to
            comments
            claimedBy
            leaseUntil
            answer {
              decision
              input
              subject
              body
              new
              choices
              addedOptions
              editedOptions
              ownWords
              comments {
                id
                to
                note
                due
              }
              comment
              people
              refs
              via
              stale
            }
            bodyFile {
              path
              doc
              title
              text
            }
            change
            resultName
            op
            input
            subtype
            people
            channel
            subject
            io
            target
            what
            previous
            how
            impact
            title
            body
            diff
            fields
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
            reversible
            reversibleHow
            after
            bundle
            choice
            group
            waits
            filled
            execution {
              result
              url
              locator
              by
              at
              selfReported
              receipt
            }
            state
            edited
            reason
            result
            heldBy
            risk
            for
            notDuplicate
            decided {
              decision
              by
              at
              ownerSaid
              selfReported
              surface
            }
            hash
            approvedHash
            actions
            thread {
              id
              kind
              quote
              text
              by
              revision
              state
              answer
            }
            preview {
              error
              changes {
                doc
                title
                type
                new
                lines
              }
              downstream {
                doc
                title
                effect
              }
            }
            evidence {
              locator
              quote
              via {
                source
                ref
              }
              verify
            }
          }
          history {
            id
            revision
            status
            created
          }
        }
      }
    }
  }
`)

const ProposalDiffDoc = graphql(/* GraphQL */ `
  query ProposalDiff($id: String!) {
    para {
      v1 {
        proposalDiff(id: $id) {
          id
          previous
          revision
          items {
            item
            change
            fields {
              field
              lines
            }
          }
        }
      }
    }
  }
`)

const DraftDoc = graphql(/* GraphQL */ `
  mutation ProposalDraft($body: para_ProposalDraftInputInput!) {
    para {
      v1 {
        proposalDraft(body: $body) {
          id
          rev
          revision
          drafted
          total
        }
      }
    }
  }
`)

const SubmitDoc = graphql(/* GraphQL */ `
  mutation ProposalSubmit($body: para_ProposalSubmitInputInput!) {
    para {
      v1 {
        proposalSubmit(body: $body) {
          id
          status
          failed
          requests
          left {
            item
            why
          }
          items {
            item
            state
            reason
            undo
            receipts
          }
        }
      }
    }
  }
`)

const StackDoc = graphql(/* GraphQL */ `
  query ProposalStack(
    $state: String
    $kind: String
    $group: String
    $proposal: String
    $actor: String
    $tag: String
    $needsYou: Boolean
    $batch: String
    $cursor: String
    $limit: Int
  ) {
    para {
      v1 {
        proposalStack(
          state: $state
          kind: $kind
          group: $group
          proposal: $proposal
          actor: $actor
          tag: $tag
          needs_you: $needsYou
          batch: $batch
          cursor: $cursor
          limit: $limit
        ) {
          counts {
            state
            count
          }
          groups {
            proposal
            group
            done
            total
            proposed
          }
          rows {
            proposal
            title
            item
            kind
            state
            stack
            group
            bundle
            actor
            summary
            drafted
            claimedBy
            url
            receipt
            result
            reason
            question
            refs
            request
            proposed
            deferUntil
            skipBatch
            tags
          }
          total
          needsYou
          requests {
            request
            title
            kind
            item
            itemName
            note
            status
            claimedBy
            resolution
            summary
            queuedAt
            activeAt
            results
            needsYou
          }
          nextCursor
        }
      }
    }
  }
`)

const StackDecideDoc = graphql(/* GraphQL */ `
  mutation StackDecide($body: para_StackDecideInputInput!) {
    para {
      v1 {
        stackDecide(body: $body) {
          proposals {
            id
            items {
              item
              state
              undo
              receipts
            }
          }
          requests
        }
      }
    }
  }
`)

const StackUndoDoc = graphql(/* GraphQL */ `
  mutation StackUndo($body: para_StackUndoInputInput!) {
    para {
      v1 {
        stackUndo(body: $body) {
          items {
            item
            state
          }
          question
          undone
        }
      }
    }
  }
`)

const DoneDoc = graphql(/* GraphQL */ `
  mutation ProposalDone($body: para_ProposalDoneInputInput!) {
    para {
      v1 {
        proposalDone(body: $body) {
          id
          item
          state
          receipt
          io {
            id
            state
            became
          }
        }
      }
    }
  }
`)

const UndoneDoc = graphql(/* GraphQL */ `
  query ProposalUndone {
    para {
      v1 {
        proposalUndone {
          items {
            proposal
            item
            title
            summary
            target
          }
        }
      }
    }
  }
`)

export function useProposals() {
  return useQuery({
    queryKey: ['proposals'],
    queryFn: async () =>
      need((await gqlClient.request(ProposalListDoc)).para.v1.proposalList, 'proposals').items,
  })
}

// useOpenProposals lists the proposals that wait for the owner.
export function useOpenProposals() {
  const q = useProposals()
  return { ...q, open: (q.data ?? []).filter((p) => p.status === 'open' && !p.error) }
}

export function useProposal(id: string) {
  return useQuery({
    queryKey: ['proposal', id],
    queryFn: async () =>
      need((await gqlClient.request(ProposalShowDoc, { id })).para.v1.proposalShow, 'proposal'),
  })
}

export function useProposalDiff(id: string, enabled: boolean) {
  return useQuery({
    queryKey: ['proposal', id, 'diff'],
    enabled,
    queryFn: async () =>
      need((await gqlClient.request(ProposalDiffDoc, { id })).para.v1.proposalDiff, 'diff'),
  })
}

// useUndone lists outward items that were approved and not reported done.
export function useUndone() {
  return useQuery({
    queryKey: ['proposals', 'undone'],
    queryFn: async () =>
      need((await gqlClient.request(UndoneDoc)).para.v1.proposalUndone, 'undone').items,
  })
}

export type DraftBody = Parameters<typeof draftCall>[0]

function draftCall(body: {
  id: string
  revision: number
  items?: { item: string; answer: Record<string, unknown> | null }[]
  sections?: { section: string; comment: string }[]
  overall?: string
}) {
  return gqlClient.request(DraftDoc, { body })
}

// useProposalDraft saves answers as a draft. The server refuses it when the revision is old.
export function useProposalDraft() {
  return useMutation({
    mutationFn: async (body: DraftBody) =>
      need((await draftCall(body)).para.v1.proposalDraft, 'draft'),
  })
}

// useProposalSubmit turns the drafted answers into decisions.
export function useProposalSubmit() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: { id: string; revision: number }) =>
      need((await gqlClient.request(SubmitDoc, { body })).para.v1.proposalSubmit, 'submit'),
    onSettled: () => qc.invalidateQueries(),
  })
}

export type StackFilter = {
  state?: string
  kind?: string
  group?: string
  proposal?: string
  actor?: string
  tag?: string
}

const STACK_PAGE = 200

// useStack reads the review stack a page at a time. batch is this visit to the stack: the
// to-decide view leaves out what the visit skipped.
export function useStack(f: StackFilter, batch?: string) {
  return useInfiniteQuery({
    queryKey: ['stack', f, batch],
    initialPageParam: '',
    queryFn: async ({ pageParam }) =>
      need(
        (
          await gqlClient.request(StackDoc, {
            ...f,
            // With no state the stack shows what waits on the owner.
            needsYou: f.state ? undefined : true,
            batch: f.state ? undefined : batch,
            cursor: pageParam || undefined,
            limit: STACK_PAGE,
          })
        ).para.v1.proposalStack,
        'stack',
      ),
    getNextPageParam: (last) => last.nextCursor || undefined,
  })
}

export type StackDecideBody = {
  items?: { proposal: string; item: string }[]
  group?: string
  proposal?: string
  decision: 'approve' | 'reject' | 'skip' | 'ask'
  comment?: string
  refs?: string[]
  // batch is the visit to the stack that skips the items (skip only).
  batch?: string
}

// useStackDecide approves, declines, skips, or sends back items across dossiers, or one group of
// one dossier. A decline and an ask need a comment.
export function useStackDecide() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: StackDecideBody) =>
      need((await gqlClient.request(StackDecideDoc, { body })).para.v1.stackDecide, 'decide'),
    onSettled: () => qc.invalidateQueries(),
  })
}

export type StackUndoBody = {
  items?: { proposal: string; item: string }[]
  question?: string
  requests?: string[]
}

// useStackUndo takes back the owner's decision on items of the stack, or on a vault question that
// was skipped or sent to an agent. The server decides how: a decision only recorded is reset, a
// vault change that ran is undone from its receipts first.
export function useStackUndo() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: StackUndoBody) =>
      need((await gqlClient.request(StackUndoDoc, { body })).para.v1.stackUndo, 'undo'),
    onSettled: () => qc.invalidateQueries(),
  })
}

export type DoneBody = {
  id: string
  item: string
  url?: string
  locator?: string
  text?: string
  note?: string
}

// useProposalDone marks an outward item done by the owner: no agent carries it out. It takes
// effect at once; it is not a draft.
export function useProposalDone() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: DoneBody) =>
      need((await gqlClient.request(DoneDoc, { body })).para.v1.proposalDone, 'done'),
    onSettled: () => qc.invalidateQueries(),
  })
}

const StackLaterDoc = graphql(/* GraphQL */ `
  mutation StackLater($body: para_StackLaterInputInput!) {
    para {
      v1 {
        stackLater(body: $body) {
          changed
          skipped
          until
        }
      }
    }
  }
`)

const StackReturnDoc = graphql(/* GraphQL */ `
  mutation StackReturn($body: para_StackReturnInputInput!) {
    para {
      v1 {
        stackReturn(body: $body) {
          changed
          skipped
        }
      }
    }
  }
`)

export type LaterBody = {
  items?: { proposal: string; item: string }[]
  questions?: string[]
  until?: string
}

// useStackLater puts stack items off until a date (stack_later); stackReturn brings them back, as
// the toast's Undo.
export function useStackLater() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (body: LaterBody) =>
      need((await gqlClient.request(StackLaterDoc, { body })).para.v1.stackLater, 'later'),
    onSettled: () => qc.invalidateQueries(),
  })
}

export async function stackReturn(body: Omit<LaterBody, 'until'>) {
  return need((await gqlClient.request(StackReturnDoc, { body })).para.v1.stackReturn, 'return')
}

const TagListDoc = graphql(/* GraphQL */ `
  query TagList {
    para {
      v1 {
        tagList {
          tags {
            tag
            open
            stack
            later
          }
        }
      }
    }
  }
`)

// useTags lists the topics in use: the stack's saved views.
export function useTags() {
  return useQuery({
    queryKey: ['tags'],
    queryFn: async () =>
      need((await gqlClient.request(TagListDoc, {})).para.v1.tagList, 'tags').tags,
  })
}

const SentListDoc = graphql(/* GraphQL */ `
  query SentList {
    para {
      v1 {
        sentList {
          open
          stale
          items {
            request
            title
            kind
            item
            itemName
            note
            status
            claimedBy
            claimedAt
            resolution
            summary
            queuedAt
            activeAt
            stale
            results
            receipts
            needsYou
            undoable
          }
        }
      }
    }
  }
`)

// useSent lists the agent work the owner asked for, newest first: Send to agent, and what the
// owner asked an agent in the chat (owner-ask). open and stale count the work not finished.
export function useSent() {
  return useQuery({
    queryKey: ['sent'],
    queryFn: async () => need((await gqlClient.request(SentListDoc, {})).para.v1.sentList, 'sent'),
  })
}
