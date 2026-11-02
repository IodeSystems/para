import { useEffect } from 'react'
import { Outlet, createFileRoute, redirect, useNavigate } from '@tanstack/react-router'
import { AppShell } from '@/components/AppShell'
import { Keyboard } from '@/components/Keyboard'
import { Overlays } from '@/components/Overlays'
import { dropSession, ensureSession, signingOut } from '@/client/auth'
import { UNAUTHORIZED_EVENT } from '@/client/gqlClient'
import { useLiveRefresh } from '@/client/live'
import { useNotifications } from '@/client/notify'

// Everything behind sign-in. No session sends the person to /login and back.
export const Route = createFileRoute('/_authed')({
  beforeLoad: async ({ location }) => {
    const session = await ensureSession()
    if (!session)
      throw redirect({
        to: '/login',
        search: { redirect: location.pathname + location.searchStr },
        replace: true,
      })
    return { session }
  },
  component: Authed,
})

function Authed() {
  const { session } = Route.useRouteContext()
  const navigate = useNavigate()
  useLiveRefresh()
  useNotifications(session.vault)
  // A 401 from any call means the session ended: sign in again, then return here.
  useEffect(() => {
    const onDead = () => {
      if (signingOut) return
      dropSession()
      void navigate({
        to: '/login',
        search: { redirect: window.location.pathname + window.location.search },
        replace: true,
      })
    }
    window.addEventListener(UNAUTHORIZED_EVENT, onDead)
    return () => window.removeEventListener(UNAUTHORIZED_EVENT, onDead)
  }, [navigate])
  return (
    <AppShell session={session}>
      <Outlet />
      <Keyboard />
      <Overlays />
    </AppShell>
  )
}
