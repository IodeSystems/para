import { createFileRoute, redirect } from '@tanstack/react-router'

// A meeting is an obligation now. Old links and bookmarks land on its obligation page.
export const Route = createFileRoute('/_authed/meetings/$id')({
  beforeLoad: ({ params }) => {
    throw redirect({ to: '/obligations/$id', params: { id: params.id }, replace: true })
  },
})
