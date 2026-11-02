import type { ReactNode } from 'react'
import { useNavigate } from '@tanstack/react-router'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import { NO_OVERLAYS } from '@/lib/overlays'

// Every overlay is a search param on the current route (WEB-10). Closing drops the params in
// place with replace, so Back never re-opens one.
export function useClose() {
  const navigate = useNavigate()
  return () =>
    void navigate({ to: '.', search: (prev) => ({ ...prev, ...NO_OVERLAYS }), replace: true })
}

export function Frame({
  title,
  children,
  actions,
  onSubmit,
}: {
  title: string
  children: ReactNode
  actions: ReactNode
  onSubmit?: () => void
}) {
  const close = useClose()
  return (
    <Dialog open onClose={close} fullWidth maxWidth="sm" aria-labelledby="overlay-title">
      <form
        onSubmit={(e) => {
          e.preventDefault()
          onSubmit?.()
        }}
      >
        <DialogTitle id="overlay-title">{title}</DialogTitle>
        <DialogContent>{children}</DialogContent>
        <DialogActions>{actions}</DialogActions>
      </form>
    </Dialog>
  )
}
