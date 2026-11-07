import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import type { ChangeBody } from '@/lib/obligations'

const ObligationRowsDoc = graphql(/* GraphQL */ `
  query ObligationRows($state: String) {
    para {
      v1 {
        list(type: obligation, state: $state) {
          rows {
            id
            title
            status
            life
            needsYou
            reason
            obligation {
              kind
              schedule
              date
              start
              end
              recur
              reply
              agent
              owedBy
              parties
              promise
              outcome
            }
          }
        }
      }
    }
  }
`)

// useObligationRows lists obligations with their kind, schedule, date, and who they are owed to:
// open and put off, or all with closed ones.
export function useObligationRows(all: boolean) {
  return useQuery({
    queryKey: ['obligations', 'rows', all],
    queryFn: async () =>
      need(
        (await gqlClient.request(ObligationRowsDoc, { state: all ? 'all' : '' })).para.v1.list,
        'obligations',
      ).rows,
  })
}

export type ObligationListRow = NonNullable<ReturnType<typeof useObligationRows>['data']>[number]

export const ObligationPartFields = graphql(/* GraphQL */ `
  fragment ObligationPartFields on para_ObligationPart {
    id
    title
    type
    kind
    status
    done
    effort
    due
    back
    blocked
    relation
  }
`)

const TimelineDoc = graphql(/* GraphQL */ `
  query ObligationTimeline($days: Int) {
    para {
      v1 {
        obligationTimeline(days: $days) {
          from
          to
          score {
            days
            kept
            renegotiated
            broken
          }
          overdue {
            ...TimelinePointFields
          }
          days {
            date
            weekday
            hours
            points {
              ...TimelinePointFields
            }
          }
        }
      }
    }
  }
`)

export const TimelinePointFields = graphql(/* GraphQL */ `
  fragment TimelinePointFields on para_TimelinePoint {
    id
    title
    kind
    date
    start
    end
    overdue
    promise
    parties
    prepHours
    hoursLeft
    atRisk
    risk
    prep {
      ...ObligationPartFields
    }
  }
`)

// useObligationTimeline is the schedule of the coming days (obligation_timeline).
export function useObligationTimeline(days: number) {
  return useQuery({
    queryKey: ['obligations', 'timeline', days],
    queryFn: async () =>
      need((await gqlClient.request(TimelineDoc, { days })).para.v1.obligationTimeline, 'timeline'),
  })
}

const PartsDoc = graphql(/* GraphQL */ `
  query ObligationParts($id: String!) {
    para {
      v1 {
        obligationParts(id: $id) {
          id
          title
          kind
          state
          outcome
          date
          start
          end
          series
          promise
          calendar
          changes
          before {
            ...ObligationPartFields
          }
          cameOut {
            ...ObligationPartFields
          }
          documents {
            id
            title
            kind
          }
          parties {
            id
            name
            medium
          }
          notices {
            id
            title
            state
            medium
          }
        }
      }
    }
  }
`)

// useObligationParts is an obligation's hub: its prep, documents, what came out of it, and who a
// change tells.
export function useObligationParts(id: string) {
  return useQuery({
    queryKey: ['obligations', 'parts', id],
    queryFn: async () =>
      need((await gqlClient.request(PartsDoc, { id })).para.v1.obligationParts, 'obligation'),
  })
}

export type ObligationPartsData = NonNullable<ReturnType<typeof useObligationParts>['data']>

// ObligationChange is Push out, Redirect, or Cancel from the owner's page (obligation_change on
// the owner's session). It writes notices as drafts the owner approves; its receipt undoes it.
const ChangeDoc = graphql(/* GraphQL */ `
  mutation ObligationChange($body: para_ObligationChangeInputInput!) {
    para {
      v1 {
        obligationChange(body: $body) {
          id
          action
          state
          notices
          told
          calendar
          receipt
          warnings
        }
      }
    }
  }
`)

export async function obligationChange(body: ChangeBody) {
  return need(
    (await gqlClient.request(ChangeDoc, { body })).para.v1.obligationChange,
    'obligation change',
  )
}
