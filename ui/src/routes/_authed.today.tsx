import { createFileRoute, redirect } from '@tanstack/react-router'

// Today is the Agenda now. Old links and bookmarks land there, keeping their search.
export const Route = createFileRoute('/_authed/today')({
  beforeLoad: () => {
    throw redirect({ to: '/agenda', search: true, replace: true })
  },
})
