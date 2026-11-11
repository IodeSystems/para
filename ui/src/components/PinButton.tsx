import { useRouteContext } from '@tanstack/react-router'
import IconButton from '@mui/material/IconButton'
import Tooltip from '@mui/material/Tooltip'
import PushPinIcon from '@mui/icons-material/PushPin'
import PushPinOutlinedIcon from '@mui/icons-material/PushPinOutlined'
import { usePinned, usePinToggle } from '@/api/pin'
import { errorText } from '@/client/gqlClient'
import { canDecide } from '@/client/session'
import { isPinned } from '@/lib/pinned'
import { useToasts } from '@/components/ToastStack'

// PinButton toggles the pin of an item (id) or of a thread key that is no item (pinKey: #tag,
// channel:...). A pinned item shows a filled pin. Only the owner pins (pin and unpin are person
// ops), so a read-only key sees no button. Every item page title and the Next card mount it.
export function PinButton({
  id,
  pinKey,
  size = 'small',
}: {
  id?: string
  pinKey?: string
  size?: 'small' | 'medium'
}) {
  const { session } = useRouteContext({ from: '/_authed' })
  const decide = canDecide(session)
  const pinned = usePinned()
  const toggle = usePinToggle()
  const toasts = useToasts()
  if (!decide || (!id && !pinKey)) return null
  const target = id ? { id } : { key: pinKey }
  const on = isPinned(pinned.data ?? [], target)
  const label = on ? 'Unpin' : 'Pin to watch'
  return (
    <Tooltip title={label}>
      <span>
        <IconButton
          size={size}
          aria-label={label}
          aria-pressed={on}
          data-pin={id ?? pinKey}
          data-pinned={on ? 'yes' : 'no'}
          color={on ? 'primary' : 'default'}
          disabled={toggle.isPending || pinned.isPending}
          onClick={(e) => {
            e.preventDefault()
            e.stopPropagation()
            toggle.mutate(
              { target, pin: !on },
              {
                onError: (err) =>
                  toasts.push({
                    text: `Could not ${on ? 'unpin' : 'pin'}: ${errorText(err)}`,
                    tone: 'error',
                  }),
              },
            )
          }}
        >
          {on ? <PushPinIcon fontSize="inherit" /> : <PushPinOutlinedIcon fontSize="inherit" />}
        </IconButton>
      </span>
    </Tooltip>
  )
}
