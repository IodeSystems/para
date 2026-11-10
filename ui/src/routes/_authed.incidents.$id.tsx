import { createFileRoute, redirect } from '@tanstack/react-router'

// An incident is a task of kind incident now (vault format 10). Old links and bookmarks land on its
// document page, which shows the severity, the timeline, and the follow-ups.
export const Route = createFileRoute('/_authed/incidents/$id')({
  beforeLoad: ({ params }) => {
    throw redirect({ to: '/docs/$id', params: { id: params.id }, replace: true })
  },
})
