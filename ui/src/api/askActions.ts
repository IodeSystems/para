import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'

// The owner's own writes on an Ask: a reply the owner heard outside para's sweep (rfx_reply) and
// a next move to another person (rfx_path_add). Neither files a receipt, so neither has an Undo.

const ReplyDoc = graphql(/* GraphQL */ `
  mutation RfxReply($body: para_RFXReplyInputInput!) {
    para {
      v1 {
        rfxReply(body: $body) {
          id
          from
          outcome
          state
          constraint
          warnings
        }
      }
    }
  }
`)

const PathAddDoc = graphql(/* GraphQL */ `
  mutation RfxPathAdd($body: para_RFXPathAddInputInput!) {
    para {
      v1 {
        rfxPathAdd(body: $body) {
          id
          held
        }
      }
    }
  }
`)

type ReplyVars = typeof ReplyDoc extends { __apiType?: (v: infer V) => unknown } ? V : never
type PathVars = typeof PathAddDoc extends { __apiType?: (v: infer V) => unknown } ? V : never

export type ReplyBody = Omit<ReplyVars['body'], 'outcome' | 'via'> & {
  outcome: string
  via: string
}

export async function rfxReply(b: ReplyBody) {
  const body = { ...b } as ReplyVars['body']
  return need((await gqlClient.request(ReplyDoc, { body })).para.v1.rfxReply, 'reply')
}

export type PathAddBody = Omit<PathVars['body'], 'medium' | 'reason'> & {
  medium: string
  reason: string
}

export async function rfxPathAdd(b: PathAddBody) {
  const body = { ...b } as PathVars['body']
  return need((await gqlClient.request(PathAddDoc, { body })).para.v1.rfxPathAdd, 'move')
}
