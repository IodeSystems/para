import { createFileRoute, redirect } from '@tanstack/react-router'

// Dossiers are retired. A closed one is a read-only document: old links land on its document page.
export const Route = createFileRoute('/_authed/proposals/$id')({
  beforeLoad: ({ params }) => {
    throw redirect({ to: '/docs/$id', params: { id: params.id }, replace: true })
  },
})
