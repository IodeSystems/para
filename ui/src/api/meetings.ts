import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import type { Para_HoldActionInput, Para_MeetingHoldInputInput } from '@/gql/graphql'

export const MeetingFields = graphql(/* GraphQL */ `
  fragment MeetingFields on para_MeetingView {
    id
    title
    status
    start
    end
    with
    withNames
    rfx
    rfxNames
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

const MeetingScheduleDoc = graphql(/* GraphQL */ `
  mutation MeetingSchedule($body: para_MeetingScheduleInputInput!) {
    para {
      v1 {
        meetingSchedule(body: $body) {
          id
          status
          start
          end
          warnings
        }
      }
    }
  }
`)

const MeetingCancelDoc = graphql(/* GraphQL */ `
  mutation MeetingCancel($body: para_MeetingCancelInputInput!) {
    para {
      v1 {
        meetingCancel(body: $body) {
          id
          status
        }
      }
    }
  }
`)

const MeetingHoldDoc = graphql(/* GraphQL */ `
  mutation MeetingHold($body: para_MeetingHoldInputInput!) {
    para {
      v1 {
        meetingHold(body: $body) {
          id
          status
          replied
          answered
          tasks
          warnings
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
            kind
            id
            title
            date
            time
            at
            until
            overdue
            detail
            project
            projectName
            who
            whoNames
          }
        }
      }
    }
  }
`)

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

export function useMeetingSchedule() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: { id: string; start: string; end: string }) =>
      need(
        (await gqlClient.request(MeetingScheduleDoc, { body: b })).para.v1.meetingSchedule,
        'result',
      ),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

export function useMeetingCancel() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: { id: string; reason?: string }) =>
      need(
        (
          await gqlClient.request(MeetingCancelDoc, {
            body: { ...b, reason: b.reason || undefined },
          })
        ).para.v1.meetingCancel,
        'result',
      ),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

export type HoldAction = Para_HoldActionInput
export type HoldInput = Para_MeetingHoldInputInput

export function useMeetingHold() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: HoldInput) =>
      need((await gqlClient.request(MeetingHoldDoc, { body: b })).para.v1.meetingHold, 'result'),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

export function useAgenda(days: number, zone: string) {
  return useQuery({
    queryKey: ['agenda', days, zone],
    queryFn: async () =>
      need((await gqlClient.request(AgendaDoc, { days, zone })).para.v1.agenda, 'agenda'),
  })
}

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
