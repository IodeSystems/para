import { createFileRoute, redirect } from '@tanstack/react-router'
import { ensureSession } from '@/client/auth'
import { homeFor } from '@/lib/next'

// Home: the owner lands on Next, a read-only key on the agenda. No session goes through sign-in.
export const Route = createFileRoute('/')({
  beforeLoad: async () => {
    throw redirect({ to: homeFor(await ensureSession()), replace: true })
  },
})
