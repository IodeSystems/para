import Alert from '@mui/material/Alert'
import { plainHttp } from '@/lib/secure'

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
