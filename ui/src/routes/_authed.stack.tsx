import { createFileRoute, redirect } from '@tanstack/react-router'

// The review stack is retired. Old links and bookmarks land on Next; view=sent was Asked of agents.
export const Route = createFileRoute('/_authed/stack')({
  validateSearch: (s: Record<string, unknown>): { view?: 'sent' } => ({
    view: s.view === 'sent' ? 'sent' : undefined,
  }),
  beforeLoad: ({ search }) => {
    if (search.view === 'sent') throw redirect({ to: '/requests', replace: true })
    throw redirect({ to: '/next', replace: true })
  },
})
