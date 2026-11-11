import { createFileRoute, redirect } from '@tanstack/react-router'

// /io was the one page for messages in and out. Input and Output replaced it; /io/$id stays the
// item page. Old links land on the screen that took over.
export const Route = createFileRoute('/_authed/io/')({
  validateSearch: (
    s: Record<string, unknown>,
  ): { view?: 'digest' | 'outbox'; medium?: string; forMe?: boolean } => ({
    view: s.view === 'outbox' || s.view === 'digest' ? s.view : undefined,
    medium: typeof s.medium === 'string' && s.medium ? s.medium : undefined,
    forMe: s.forMe === true || s.forMe === 'true' ? true : undefined,
  }),
  beforeLoad: ({ search }) => {
    if (search.view === 'outbox') {
      // Sent for you is the Sent tab; the medium filter still means something there.
      throw redirect(
        search.forMe
          ? { to: '/output', search: { tab: 'sent', medium: search.medium }, replace: true }
          : { to: '/output', search: { tab: undefined }, replace: true },
      )
    }
    throw redirect({
      to: '/input',
      search: { tab: search.view === 'digest' ? 'rest' : undefined },
      replace: true,
    })
  },
})
