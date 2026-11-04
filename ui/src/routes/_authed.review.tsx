import { createFileRoute, redirect } from '@tanstack/react-router'

// Review is Recent agent writes now. Old links and bookmarks land there.
export const Route = createFileRoute('/_authed/review')({
  beforeLoad: () => {
    throw redirect({ to: '/writes', replace: true })
  },
})
