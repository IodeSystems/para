import { useState } from 'react'
import FormControlLabel from '@mui/material/FormControlLabel'
import Switch from '@mui/material/Switch'
import Typography from '@mui/material/Typography'
import { notifyEnabled, notifySupported, setNotifyEnabled } from '@/lib/notify'

// Opt-in per vault, off by default. A notice says a kind and a count, never document text, and only
// shows while a tab or the installed app is open.
export function NotifySetting({ vault }: { vault: string }) {
  const [on, setOn] = useState(() => notifyEnabled(vault))
  const [note, setNote] = useState('')
  if (!notifySupported()) {
    return (
      <Typography variant="caption" color="text.secondary">
        This browser has no notifications.
      </Typography>
    )
  }
  const change = async (want: boolean) => {
    if (want && Notification.permission !== 'granted') {
      const p = await Notification.requestPermission()
      if (p !== 'granted') {
        setNote('The browser did not allow notifications.')
        return
      }
    }
    setNote('')
    setNotifyEnabled(vault, want)
    setOn(want)
  }
  return (
    <>
      <FormControlLabel
        control={<Switch checked={on} onChange={(_, v) => void change(v)} />}
        label={`Notify me in this browser (vault ${vault})`}
      />
      <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
        Proposal filed, answer to check, agent work that needs you, incident. A notice names the
        kind and the count only. It shows while a tab or the installed app is open.
      </Typography>
      {note && (
        <Typography variant="caption" color="error" sx={{ display: 'block' }}>
          {note}
        </Typography>
      )}
    </>
  )
}
