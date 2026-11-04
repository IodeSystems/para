import { useMutation, useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

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
          }
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
