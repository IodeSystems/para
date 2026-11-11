import Alert from '@mui/material/Alert'
import { plainHttp } from '@/lib/secure'
import { canDecide, type SessionView } from '@/client/session'
import { useUpdateStatus } from '@/api/update'

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

// UpdateBanner: a newer para release is available, or this para was just updated. It reads only;
// installing is the service's (at a quiet moment) or the owner's (para upgrade).
export function UpdateBanner() {
  const { data } = useUpdateStatus()
  if (!data) return null
  if (data.available) {
    const how =
      data.release && data.auto
        ? 'installs automatically at a quiet moment'
        : 'run para upgrade on the machine running para'
    return (
      <Alert severity="info" sx={{ borderRadius: 0 }} data-banner="update-available">
        para {data.available} available — {how}.
      </Alert>
    )
  }
  if (data.installedTo && data.installedTo === data.running && recent(data.installedAt, 7)) {
    return (
      <Alert severity="success" sx={{ borderRadius: 0 }} data-banner="updated">
        Updated to {data.installedTo}
        {data.installedFrom ? ` from ${data.installedFrom}` : ''} — see what changed: para changes.
      </Alert>
    )
  }
  return null
}

// recent reports whether an RFC 3339 time is within the last days.
function recent(at: string | null | undefined, days: number): boolean {
  if (!at) return false
  const t = Date.parse(at)
  return !Number.isNaN(t) && Date.now() - t < days * 24 * 60 * 60 * 1000
}
