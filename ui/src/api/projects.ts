import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import type { ReadingInput } from '@/lib/kpi'

// Projects: ongoing tracked things. The list is one request (list, type project, with a summary per
// row); a project page is its bundle (show). A goal or a former goal id on the project route shows as
// a goal.

const ProjectShowDoc = graphql(/* GraphQL */ `
  query ProjectShow($id: String!) {
    para {
      v1 {
        show(id: $id) {
          detail {
            __typename
            ... on para_GoalShowOutput {
              goal {
                id
              }
            }
            ... on para_ProjectShowOutput {
              body
              lastActivity
              stateAgeDays
              kpiAlerts
              kpis
              crons
              more
              project {
                id
                title
                status
                owner
                area
                areaName
                cadence
                lastReviewed
                stateLine
                stateDate
                links {
                  kind
                  label
                  target
                }
              }
              goals {
                id
                title
                status
                doneWhen
                due
                next
                nextTitle
                moreTasks
                tasks {
                  ...BundleItemFields
                }
              }
              waitsOn {
                ...BundleItemFields
              }
              icebox {
                ...BundleItemFields
              }
              direct {
                ...BundleItemFields
              }
              history {
                at
                id
                kind
                summary
              }
            }
          }
        }
      }
    }
  }

  fragment BundleItemFields on para_BundleItem {
    id
    type
    title
    status
    due
    via
    viaTitle
    note
    hiddenTasks
  }
`)

// One request for the Projects view: list {type: project} gives each row its summary, stalest state
// line first. status '' is every status (state all); the server's default is active.
const ProjectListDoc = graphql(/* GraphQL */ `
  query ProjectList($status: String, $state: String) {
    para {
      v1 {
        list(type: project, status: $status, state: $state) {
          rows {
            id
            title
            status
            project {
              stateLine
              stateDate
              stateAgeDays
              openGoals
              waitsOn
              firstWait
              nextCron
              kpiAlerts
              lastActivity
            }
          }
        }
      }
    }
  }
`)

// The owner's inline edit of a project's state line: edit with fields {state_line} only, nothing
// else (owner 2026-11-11; scoped in scripts/check-graphql.mjs). The server sets state_date.
const ProjectStateLineSaveDoc = graphql(/* GraphQL */ `
  mutation ProjectStateLineSave($body: para_EditInputInput!) {
    para {
      v1 {
        edit(body: $body) {
          op
          receipt
        }
      }
    }
  }
`)

// The KPIs of a project with their full names, for the reading form (kpi_list).
const ProjectKPIListDoc = graphql(/* GraphQL */ `
  query ProjectKPIList($id: String!) {
    para {
      v1 {
        kpiList(id: $id) {
          kpis {
            kpi {
              name
              unit
              owner
            }
          }
        }
      }
    }
  }
`)

// The owner's inline KPI reading: kpi_reading_add with {id, name, value, source, date?} only, nothing
// else (owner 2026-11-12; scoped in scripts/check-graphql.mjs). A reading always names its source.
const ProjectKPIReadingSaveDoc = graphql(/* GraphQL */ `
  mutation ProjectKPIReadingSave($body: para_KPIReadingAddInputInput!) {
    para {
      v1 {
        kpiReadingAdd(body: $body) {
          id
          name
          date
          line
        }
      }
    }
  }
`)

async function fetchProject(id: string) {
  const show = need((await gqlClient.request(ProjectShowDoc, { id })).para.v1.show, 'project')
  return need(show.detail, 'project')
}

// useProject is the bundle of a project, or the goal the id names (the page redirects to it).
export function useProject(id: string) {
  return useQuery({ queryKey: ['project', id], queryFn: () => fetchProject(id) })
}

export type ProjectStatus = 'active' | 'paused' | 'retired' | 'all'

// useProjects lists the projects of a status with their summaries, in one request.
export function useProjects(status: ProjectStatus) {
  return useQuery({
    queryKey: ['projects', status],
    // The status toggle stays on screen while the next status loads.
    placeholderData: keepPreviousData,
    queryFn: async () =>
      need(
        (
          await gqlClient.request(ProjectListDoc, {
            status: status === 'all' ? undefined : status,
            state: status === 'all' ? 'all' : undefined,
          })
        ).para.v1.list,
        'projects',
      ).rows,
  })
}

// useSaveStateLine sets a project's state line (the owner's edit).
export function useSaveStateLine(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (line: string) =>
      need(
        (
          await gqlClient.request(ProjectStateLineSaveDoc, {
            body: { id, fields: { state_line: line } },
          })
        ).para.v1.edit,
        'state line',
      ),
    onSettled: () => qc.invalidateQueries(),
  })
}

// useProjectKPIs lists a project's KPIs (names, units, owners) when enabled: the reading form.
export function useProjectKPIs(id: string, enabled: boolean) {
  return useQuery({
    queryKey: ['project-kpis', id],
    enabled,
    queryFn: async () =>
      need((await gqlClient.request(ProjectKPIListDoc, { id })).para.v1.kpiList, 'kpis').kpis.map(
        (v) => v.kpi,
      ),
  })
}

// useSaveKPIReading adds the owner's reading of a project's KPI (kpi_reading_add).
export function useSaveKPIReading(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async ({ name, value, source, date }: ReadingInput) =>
      need(
        (
          await gqlClient.request(ProjectKPIReadingSaveDoc, {
            body: { id, name, value, source, date },
          })
        ).para.v1.kpiReadingAdd,
        'KPI reading',
      ),
    onSettled: () => qc.invalidateQueries(),
  })
}
