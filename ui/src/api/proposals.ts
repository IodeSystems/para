import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

// Proposals are read here and decided in the chat: the web has no decide call.

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
            change
            resultName
            op
            input
            subtype
            people
            channel
            subject
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

// useOpenProposals lists the proposals that wait for the owner in the chat.
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
