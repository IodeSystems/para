import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import { recurringBody, type RecurringInput } from '@/lib/recurring'

export const MeetingFields = graphql(/* GraphQL */ `
  fragment MeetingFields on para_MeetingView {
    id
    title
    status
    start
    end
    with
    withNames
    attendees
    rfx
    rfxNames
    calendarUids {
      uid
      role
    }
  }
`)

const MeetingListDoc = graphql(/* GraphQL */ `
  query MeetingList($all: Boolean) {
    para {
      v1 {
        meetingList(all: $all) {
          meetings {
            ...MeetingFields
          }
        }
      }
    }
  }
`)

const MeetingShowDoc = graphql(/* GraphQL */ `
  query MeetingShow($id: String!) {
    para {
      v1 {
        meetingShow(id: $id) {
          body
          meeting {
            ...MeetingFields
          }
        }
      }
    }
  }
`)

const AgendaDoc = graphql(/* GraphQL */ `
  query Agenda($days: Int, $zone: String) {
    para {
      v1 {
        agenda(days: $days, zone: $zone) {
          from
          to
          zone
          items {
            ...AgendaItemFields
          }
          skipped {
            ...AgendaItemFields
          }
          optional {
            ...AgendaItemFields
          }
        }
      }
    }
  }
`)

export const AgendaItemFields = graphql(/* GraphQL */ `
  fragment AgendaItemFields on para_AgendaItem {
    kind
    id
    title
    date
    time
    at
    until
    overdue
    detail
    why
    project
    projectName
    who
    whoNames
    optional
    attend
    needsYou
  }
`)

export function useMeetings(all: boolean) {
  return useQuery({
    queryKey: ['meetings', all],
    queryFn: async () =>
      need((await gqlClient.request(MeetingListDoc, { all })).para.v1.meetingList, 'meetings')
        .meetings,
  })
}

export function useMeeting(id: string) {
  return useQuery({
    queryKey: ['meetings', 'show', id],
    queryFn: async () =>
      need((await gqlClient.request(MeetingShowDoc, { id })).para.v1.meetingShow, 'meeting'),
  })
}

// An obligation reads as a task of its kind (task_show): a meeting, or a commitment owed.
const ObligationShowDoc = graphql(/* GraphQL */ `
  query ObligationShow($id: String!) {
    para {
      v1 {
        taskShow(id: $id) {
          body
          task {
            id
            title
            kind
            status
            outcome
            due
            scheduled
            end
            with
            withNames
            assignee
            assigneeName
            doneWhen
            project
            projectName
            needsYou
            reason
          }
          produce {
            agent
            kind
            template
            templateName
            to
            medium
            builtin
            ready
            request
            requestStatus
            artifact {
              id
              type
              title
              state
              occurrence
              at
            }
          }
        }
      }
    }
  }
`)

export function useObligation(id: string) {
  return useQuery({
    queryKey: ['obligations', 'show', id],
    queryFn: async () =>
      need((await gqlClient.request(ObligationShowDoc, { id })).para.v1.taskShow, 'obligation'),
  })
}

const ObligationListDoc = graphql(/* GraphQL */ `
  query ObligationList($state: String) {
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
          }
        }
      }
    }
  }
`)

// useObligations lists obligations: open and put off, or all with closed ones.
export function useObligations(all: boolean) {
  return useQuery({
    queryKey: ['obligations', all],
    queryFn: async () =>
      need(
        (await gqlClient.request(ObligationListDoc, { state: all ? 'all' : '' })).para.v1.list,
        'obligations',
      ).rows,
  })
}

export function useAgenda(days: number, zone: string) {
  return useQuery({
    queryKey: ['agenda', days, zone],
    queryFn: async () =>
      need((await gqlClient.request(AgendaDoc, { days, zone })).para.v1.agenda, 'agenda'),
  })
}

// CalendarExportDoc is a vault:read op: it changes no document.
const CalendarExportDoc = graphql(/* GraphQL */ `
  mutation CalendarExport($body: para_CalendarExportInputInput!) {
    para {
      v1 {
        calendarExport(body: $body) {
          text
          ext
          events
          destination
          marking
        }
      }
    }
  }
`)

// useCalendarExport renders the .ics feed. The op moves nothing: it logs the egress for the
// transport and returns the text, and the browser saves it as a file.
export function useCalendarExport() {
  return useMutation({
    mutationFn: async (b: { days: number; zone: string }) =>
      need(
        (await gqlClient.request(CalendarExportDoc, { body: { ...b, to: 'stdout' } })).para.v1
          .calendarExport,
        'calendar',
      ),
  })
}

// MeetingRsvp is the owner's answer to whether they go to a meeting (meeting_rsvp, a person op):
// Attend and Skip on the agenda. attend unset clears the answer.
const MeetingRsvpDoc = graphql(/* GraphQL */ `
  mutation MeetingRsvp($body: para_MeetingAttendInputInput!) {
    para {
      v1 {
        meetingRsvp(body: $body) {
          id
          title
          now {
            attend
            optional
          }
          previous {
            attend
            optional
          }
        }
      }
    }
  }
`)

export type Attend = 'yes' | 'no' | 'maybe' | 'unset'

export async function meetingRsvp(id: string, attend: Attend) {
  return need(
    (await gqlClient.request(MeetingRsvpDoc, { body: { id, attend } })).para.v1.meetingRsvp,
    'attend',
  )
}

// ObligationAdd from the web: New recurring artifact makes an obligation an agent owes, with
// what it produces (obligation_add with produce; owner 2026-11-06: PU-REC).
const ObligationAddDoc = graphql(/* GraphQL */ `
  mutation ObligationAdd($body: para_ObligationAddInputInput!) {
    para {
      v1 {
        obligationAdd(body: $body) {
          id
          schedule
          warnings
        }
      }
    }
  }
`)

export function useAddRecurring() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (i: RecurringInput) =>
      need(
        (await gqlClient.request(ObligationAddDoc, { body: recurringBody(i) })).para.v1
          .obligationAdd,
        'obligation',
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['obligations'] })
    },
  })
}
