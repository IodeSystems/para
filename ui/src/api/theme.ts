import { useQuery } from '@tanstack/react-query'
import { graphql } from '@/gql'
import { gqlClient } from '@/client/gqlClient'
import { need } from '@/client/need'
import type { ThemeCloseBody } from '@/lib/theme'

// A theme is a goal, an obligation, a question, a parent task, or a #tag. theme_items lists the
// open items that hang on it (not those that only mention it), each with why and its Part of;
// theme_close closes the theme and applies the owner's drops and moves as one batch.

const ThemeItemsDoc = graphql(/* GraphQL */ `
  query ThemeItems($theme: String!) {
    para {
      v1 {
        themeItems(theme: $theme) {
          theme
          title
          kind
          open
          items {
            id
            title
            type
            kind
            life
            why
            whyText
            partOf {
              kind
              via
              id
              title
            }
          }
        }
      }
    }
  }
`)

export type ThemeItemsData = Awaited<ReturnType<typeof themeItems>>
export type ThemeItemRow = ThemeItemsData['items'][number]

export async function themeItems(theme: string) {
  return need((await gqlClient.request(ThemeItemsDoc, { theme })).para.v1.themeItems, 'theme')
}

export function useThemeItems(theme: string, enabled = true) {
  return useQuery({ queryKey: ['theme', theme], queryFn: () => themeItems(theme), enabled })
}

const ThemeCloseDoc = graphql(/* GraphQL */ `
  mutation ThemeClose($body: para_ThemeCloseInputInput!) {
    para {
      v1 {
        themeClose(body: $body) {
          theme
          batch
          closed
          applied
          refused
          left
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

export async function themeClose(body: ThemeCloseBody) {
  return need((await gqlClient.request(ThemeCloseDoc, { body })).para.v1.themeClose, 'theme close')
}
