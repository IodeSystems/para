import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

export const TaskRowFields = graphql(/* GraphQL */ `
  fragment TaskRowFields on para_TaskView {
    id
    title
    status
    project
    projectName
    area
    areaName
    due
    recur
    assignee
    assigneeName
    dependsOn
    dependsOnNames
    blockedBy
    doneWhen
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
          lastAligned
          area {
            id
            title
            status
            kind
            cadence
            lastReviewed
            nextReview
            concepts
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
          drawnOnBy {
            id
            type
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
          lastAligned
          project {
            id
            title
            status
            owner
            doneWhen
            area
            areaName
            concepts
            links {
              kind
              label
              target
            }
            brief {
              rank
              validation
              extensions
              why {
                text
              }
              confidence {
                percent
                basis
              }
              alternatives {
                name
              }
              unknowns {
                text
                link
              }
              risks {
                text
                likelihood
                impact
                owner
                mitigation
                source
              }
              signals {
                what
                expected
                everyDays
                readings {
                  date
                  value
                  source
                }
              }
              pivot {
                condition
                date
                action
              }
              ecosystem {
                keyHolder
                replaces
              }
            }
          }
          tasks {
            ...TaskRowFields
          }
          questions {
            ...QuestionFields
          }
          rules {
            id
            title
            via
          }
          assumptions {
            text
            source
            checked
            by
            status
            fact
            fromBody
            stale
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
            routeName
            nextNudge
            due
            stalled
            targetNames
            blockedOn
            blockedOnNames
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

export function useWaiting(today: string) {
  return useQuery({
    queryKey: ['waiting'],
    queryFn: async () =>
      need((await gqlClient.request(WaitingDoc, { today })).para.v1.waiting, 'waiting'),
  })
}
