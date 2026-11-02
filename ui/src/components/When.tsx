import { Children, type ReactNode } from 'react'
import { exactTime, humanTime, splitTimes } from '@/lib/when'

// When shows a timestamp local and in words, with the exact time on hover.
export function When({ iso }: { iso: string }) {
  return (
    <time dateTime={iso} title={exactTime(iso)}>
      {humanTime(iso)}
    </time>
  )
}

// whenify turns the timestamps inside the text children of an element into <When>.
export function whenify(children: ReactNode): ReactNode {
  return Children.map(children, (c) => {
    if (typeof c !== 'string') return c
    const parts = splitTimes(c)
    if (!parts.some((p) => p.time)) return c
    return parts.map((p, i) => (p.time ? <When key={i} iso={p.text} /> : p.text))
  })
}
