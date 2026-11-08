import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import type { BulkBody, BulkResult } from '@/lib/bulk'

// bulk applies one verb to many items: each item runs the verb's own op, with its rules and
// receipt; dryRun says per item what would happen and writes nothing; undo with the batch takes
// the whole run back.
const BulkDoc = graphql(/* GraphQL */ `
  mutation Bulk($body: para_BulkInputInput!) {
    para {
      v1 {
        bulk(body: $body) {
          verb
          batch
          dryRun
          applied
          refused
          items {
            id
            title
            state
            op
            receipt
            error
          }
        }
      }
    }
  }
`)

export async function bulk(body: BulkBody): Promise<BulkResult> {
  return need((await gqlClient.request(BulkDoc, { body })).para.v1.bulk, 'bulk')
}
