import { Fragment } from 'react'
import { TextLink } from '@/components/links'

// ConceptLinks links the concept areas an item draws on, separated by commas.
export function ConceptLinks({ ids }: { ids: string[] }) {
  return (
    <>
      {ids.map((c, i) => (
        <Fragment key={c}>
          {i > 0 && ', '}
          <TextLink to="/areas/$id" params={{ id: c }} underline="hover">
            {c}
          </TextLink>
        </Fragment>
      ))}
    </>
  )
}
