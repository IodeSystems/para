import { useQueries, useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

// Projects: ongoing tracked things. A list is the ids of the active projects (list, type project),
// then one bundle each (show). The bundle is the page's data and the row's: one query, one cache
// entry per project. A goal or a former goal id on the project route shows as a goal.

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
    note
    hiddenTasks
  }
`)

const ProjectIdsDoc = graphql(/* GraphQL */ `
  query ProjectIds {
    para {
      v1 {
        list(type: project, status: "active") {
          rows {
            id
          }
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

// useProjects lists the active projects with their bundles; ids that are not projects are left out.
export function useProjects() {
  const ids = useQuery({
    queryKey: ['project-ids'],
    queryFn: async () =>
      need((await gqlClient.request(ProjectIdsDoc)).para.v1.list, 'projects').rows.map((r) => r.id),
  })
  const bundles = useQueries({
    queries: (ids.data ?? []).map((id) => ({
      queryKey: ['project', id],
      queryFn: () => fetchProject(id),
    })),
  })
  const error = ids.error ?? bundles.find((b) => b.error)?.error ?? null
  const items = bundles.flatMap((b) =>
    b.data?.__typename === 'para_ProjectShowOutput' ? [b.data] : [],
  )
  const isPending = ids.isPending || bundles.some((b) => b.isPending)
  return {
    // data is set once every bundle is in; DataState shows the wait and the error.
    data: isPending || error ? undefined : items,
    error,
    isPending,
    refetch: () => {
      void ids.refetch()
      bundles.forEach((b) => void b.refetch())
    },
  }
}
