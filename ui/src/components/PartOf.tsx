import { useState } from 'react'
import Link from '@mui/material/Link'
import useMediaQuery from '@mui/material/useMediaQuery'
import type { Theme } from '@mui/material/styles'
import { TextLink } from '@/components/links'
import { partLabel, partLink, partShown, type PartRef } from '@/lib/partof'

// PartOfLinks is what an item is part of, as links: its parent task, the obligation it waits on
// or serves, its goal, its tags. On a phone a row shows the nearest one and "+N"; a tap shows the
// rest (partShown). full shows all of them on every screen, as the item page does. omit leaves
// out the page the list is on.
export function PartOfLinks({
  parts,
  omit,
  full = false,
}: {
  parts?: readonly PartRef[] | null
  omit?: string
  full?: boolean
}) {
  const phone = useMediaQuery((t: Theme) => t.breakpoints.down('sm'))
  const [open, setOpen] = useState(false)
  const { shown, more } = partShown(parts, phone && !full && !open, omit)
  if (shown.length === 0) return null
  return (
    <span data-part-of>
      {shown.map((p, i) => (
        <span key={`${p.kind}-${p.id}`} data-part={p.kind}>
          {i > 0 && ', '}
          <TextLink {...partLink(p)} underline="hover" onClick={(e) => e.stopPropagation()}>
            {partLabel(p)}
          </TextLink>
        </span>
      ))}
      {more > 0 && (
        <>
          {' '}
          <Link
            component="button"
            type="button"
            underline="hover"
            variant="inherit"
            aria-label={`Show ${more} more`}
            data-part-more={more}
            onClick={(e) => {
              e.stopPropagation()
              setOpen(true)
            }}
          >
            +{more}
          </Link>
        </>
      )}
    </span>
  )
}

// PartOfLine is the "Part of" line under a row's title; nothing when the item belongs to nothing.
export function PartOfLine({ parts, omit }: { parts?: readonly PartRef[] | null; omit?: string }) {
  if (partShown(parts, false, omit).shown.length === 0) return null
  return (
    <span style={{ display: 'block' }}>
      Part of <PartOfLinks parts={parts} omit={omit} />
    </span>
  )
}
