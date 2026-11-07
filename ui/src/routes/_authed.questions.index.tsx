import { createFileRoute, redirect } from '@tanstack/react-router'

// Questions is a filter on the Agenda now. Old links and bookmarks land there.
export const Route = createFileRoute('/_authed/questions/')({
  beforeLoad: () => {
    throw redirect({ to: '/agenda', search: { show: 'questions' }, replace: true })
  },
})
