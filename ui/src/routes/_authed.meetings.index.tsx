import { createFileRoute, redirect } from '@tanstack/react-router'

// Meetings are obligations now: a filter of the Obligations page. Old links and bookmarks land there.
export const Route = createFileRoute('/_authed/meetings/')({
  beforeLoad: () => {
    throw redirect({ to: '/obligations', search: { filter: 'meetings' }, replace: true })
  },
})
