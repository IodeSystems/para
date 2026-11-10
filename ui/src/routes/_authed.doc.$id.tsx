import { createFileRoute, redirect } from '@tanstack/react-router'

// /docs/$id is the old address of the document page; it keeps old bookmarks and server-made links.
export const Route = createFileRoute('/_authed/doc/$id')({
  beforeLoad: ({ params }) => {
    throw redirect({ to: '/docs/$id', params: { id: params.id }, replace: true })
  },
})
