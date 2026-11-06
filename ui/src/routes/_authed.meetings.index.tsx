import { createFileRoute, redirect } from '@tanstack/react-router'

// Meetings are obligations now. Old links and bookmarks land on the obligations page.
export const Route = createFileRoute('/_authed/meetings/')({
  beforeLoad: () => {
    throw redirect({ to: '/obligations', replace: true })
  },
})
