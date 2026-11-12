import { createFileRoute, redirect } from '@tanstack/react-router'

// /receipts is the old address of the Log page; it keeps old bookmarks and links.
export const Route = createFileRoute('/_authed/receipts')({
  beforeLoad: () => {
    throw redirect({ to: '/log', replace: true })
  },
})
