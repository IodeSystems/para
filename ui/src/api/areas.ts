import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import type { Para_IncidentStatusInputStatus } from '@/gql/graphql'

export const TaskRowFields = graphql(/* GraphQL */ `
  fragment TaskRowFields on para_TaskView {
    id
    title
    status
    project
    area
    due
    recur
    assignee
    dependsOn
    blockedBy
  }
`)

export const IncidentRowFields = graphql(/* GraphQL */ `
  fragment IncidentRowFields on para_IncidentView {
    id
    title
    severity
    status
    started
    ended
    area
  }
`)

const AreaListDoc = graphql(/* GraphQL */ `
  query AreaList {
    para {
      v1 {
        areaList {
          areas {
            id
            title
            status
          }
        }
      }
    }
  }
`)

const AreaShowDoc = graphql(/* GraphQL */ `
  query AreaShow($id: String!) {
    para {
      v1 {
        areaShow(id: $id) {
          body
          area {
            id
            title
            status
            links {
              kind
              label
              target
            }
          }
          projects {
            id
            title
            status
            owner
          }
          tasks {
            ...TaskRowFields
          }
          recurring {
            ...TaskRowFields
          }
          questions {
            ...QuestionFields
          }
          incidents {
            ...IncidentRowFields
          }
          notes {
            id
            title
          }
        }
      }
    }
  }
`)

const ProjectShowDoc = graphql(/* GraphQL */ `
  query ProjectShow($id: String!) {
    para {
      v1 {
        projectShow(id: $id) {
          body
          project {
            id
            title
            status
            owner
            area
            links {
              kind
              label
              target
            }
          }
          tasks {
            ...TaskRowFields
          }
          questions {
            ...QuestionFields
          }
        }
      }
    }
  }
`)

const IncidentListDoc = graphql(/* GraphQL */ `
  query IncidentList {
    para {
      v1 {
        incidentList {
          incidents {
            ...IncidentRowFields
          }
        }
      }
    }
  }
`)

const IncidentShowDoc = graphql(/* GraphQL */ `
  query IncidentShow($id: String!) {
    para {
      v1 {
        incidentShow(id: $id) {
          body
          incident {
            ...IncidentRowFields
            links {
              kind
              label
              target
            }
          }
          followUps {
            ...TaskRowFields
          }
        }
      }
    }
  }
`)

const IncidentNoteDoc = graphql(/* GraphQL */ `
  mutation IncidentNote($body: para_IncidentNoteInputInput!) {
    para {
      v1 {
        incidentNote(body: $body) {
          id
          status
        }
      }
    }
  }
`)

const IncidentStatusDoc = graphql(/* GraphQL */ `
  mutation IncidentStatus($body: para_IncidentStatusInputInput!) {
    para {
      v1 {
        incidentStatus(body: $body) {
          id
          status
        }
      }
    }
  }
`)

const WaitingDoc = graphql(/* GraphQL */ `
  query Waiting($today: String) {
    para {
      v1 {
        waiting(today: $today) {
          count
          today
          rfx {
            id
            title
            route
            nextNudge
            due
            stalled
            targets {
              to
              state
              medium
            }
          }
          tasks {
            ...TaskRowFields
          }
        }
      }
    }
  }
`)

export function useAreas() {
  return useQuery({
    queryKey: ['areas'],
    queryFn: async () =>
      need((await gqlClient.request(AreaListDoc)).para.v1.areaList, 'areas').areas,
  })
}

export function useArea(id: string) {
  return useQuery({
    queryKey: ['area', id],
    queryFn: async () =>
      need((await gqlClient.request(AreaShowDoc, { id })).para.v1.areaShow, 'area'),
  })
}

export function useProject(id: string) {
  return useQuery({
    queryKey: ['project', id],
    queryFn: async () =>
      need((await gqlClient.request(ProjectShowDoc, { id })).para.v1.projectShow, 'project'),
  })
}

export function useIncidents() {
  return useQuery({
    queryKey: ['incidents'],
    queryFn: async () =>
      need((await gqlClient.request(IncidentListDoc)).para.v1.incidentList, 'incidents').incidents,
  })
}

export function useIncident(id: string) {
  return useQuery({
    queryKey: ['incident', id],
    queryFn: async () =>
      need((await gqlClient.request(IncidentShowDoc, { id })).para.v1.incidentShow, 'incident'),
  })
}

export function useIncidentNote() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: { id: string; text: string }) =>
      need((await gqlClient.request(IncidentNoteDoc, { body: b })).para.v1.incidentNote, 'result'),
    onSuccess: () => qc.invalidateQueries(),
  })
}

export function useIncidentStatus() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (b: { id: string; status: Para_IncidentStatusInputStatus }) =>
      need(
        (await gqlClient.request(IncidentStatusDoc, { body: b })).para.v1.incidentStatus,
        'result',
      ),
    onSuccess: () => qc.invalidateQueries(),
  })
}

export function useWaiting(today: string) {
  return useQuery({
    queryKey: ['waiting'],
    queryFn: async () =>
      need((await gqlClient.request(WaitingDoc, { today })).para.v1.waiting, 'waiting'),
  })
}
