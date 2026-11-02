import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

const WorkDoc = graphql(/* GraphQL */ `
  query Work {
    para {
      v1 {
        projectList(all: true) {
          projects {
            id
            title
            status
            owner
          }
        }
        taskList {
          tasks {
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
        }
      }
    }
  }
`)

const GraphDoc = graphql(/* GraphQL */ `
  query WorkGraph($project: String) {
    para {
      v1 {
        graph(project: $project) {
          order
          cycles
          edges {
            from
            to
          }
          nodes {
            id
            title
            type
            status
            state
            project
            blockedBy
            waitingOn
          }
        }
      }
    }
  }
`)

export function useWork() {
  return useQuery({
    queryKey: ['work'],
    queryFn: async () => {
      const v = (await gqlClient.request(WorkDoc)).para.v1
      return {
        projects: need(v.projectList, 'projects').projects,
        tasks: need(v.taskList, 'tasks').tasks,
      }
    },
  })
}

export function useGraph(project: string | undefined) {
  return useQuery({
    queryKey: ['graph', project ?? ''],
    queryFn: async () =>
      need((await gqlClient.request(GraphDoc, { project })).para.v1.graph, 'graph'),
  })
}
