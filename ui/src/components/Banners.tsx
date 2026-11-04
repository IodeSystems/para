import Alert from '@mui/material/Alert'
import { plainHttp } from '@/lib/secure'
import { canDecide, type SessionView } from '@/client/session'

// Plain HTTP on a LAN address is a degraded mode: no service worker, no install, and no
// navigator.clipboard. Say so once, plainly.
export function HttpBanner() {
  if (!plainHttp()) return null
  return (
    <Alert severity="warning" sx={{ borderRadius: 0 }}>
      This page uses plain HTTP. Installing para as an app needs HTTPS, and offline loading and the
      clipboard button's fast path need it too. Copy still works through a fallback. Reach para over
      HTTPS (for example through hz) or at http://localhost to install it.
    </Alert>
  )
}

// ReadOnlyBanner: a key with vault:read only (an older login, or a key made for reading) shows
// every page but cannot answer or decide. Say how to get a key that can.
export function ReadOnlyBanner({ session }: { session: SessionView }) {
  if (canDecide(session)) return null
  return (
    <Alert severity="info" sx={{ borderRadius: 0 }} data-banner="read-only">
      This sign-in can read only. Review actions need a new login: run para serve login on the
      machine running para.
    </Alert>
  )
}
