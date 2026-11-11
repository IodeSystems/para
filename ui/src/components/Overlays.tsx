import { useNavigate, useRouteContext, useRouterState, useSearch } from '@tanstack/react-router'
import Box from '@mui/material/Box'
import Drawer from '@mui/material/Drawer'
import List from '@mui/material/List'
import ListItemButton from '@mui/material/ListItemButton'
import ListItemText from '@mui/material/ListItemText'
import Divider from '@mui/material/Divider'
import Button from '@mui/material/Button'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import useMediaQuery from '@mui/material/useMediaQuery'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { useClose } from '@/components/OverlayFrame'
import { EXTRA_SCREENS, NEXT_KEYS, TABS } from '@/lib/keys'
import { UI_COMMIT } from '@/buildStamp'
import { Details, StatusChip } from '@/components/Bits'
import { NotifySetting } from '@/components/NotifySetting'

// The app's overlays: help and the screen list. Nothing here writes.
export function Overlays() {
  const s = useSearch({ strict: false })
  return (
    <>
      {s.menu && <NavDrawer />}
      {s.help && <HelpDialog />}
    </>
  )
}

function HelpDialog() {
  const close = useClose()
  const { session } = useRouteContext({ from: '/_authed' })
  // A touch screen has no keyboard to show: the section is left out.
  const touch = useMediaQuery('(hover: none), (pointer: coarse), (max-width: 599px)')
  return (
    <Dialog open onClose={close} fullWidth maxWidth="xs" aria-labelledby="help-title">
      <DialogTitle id="help-title">{touch ? 'Help' : 'Keyboard'}</DialogTitle>
      <DialogContent>
        <Typography variant="body2" sx={{ mb: 2 }}>
          Agents write through the chat and leave receipts you can check or undo. For other changes,
          use the chat with your agent.
        </Typography>
        {!touch && (
          <>
            <Stack component="dl" spacing={0.5} sx={{ m: 0 }}>
              {TABS.map((t) => (
                <Row key={t.key} k={t.key} v={t.label} />
              ))}
              <Row k="/" v="Search or go to (also Ctrl K)" />
              <Row k="t" v="Capture: save what happened and send it to your agent" />
              <Row k="?" v="This help" />
              <Row k="Esc" v="Close a dialog" />
            </Stack>
            <Typography variant="subtitle2" component="h3" sx={{ mt: 1.5, mb: 0.5 }}>
              On Next
            </Typography>
            <Stack component="dl" spacing={0.5} sx={{ m: 0 }}>
              {NEXT_KEYS.map((k) => (
                <Row key={k.key} k={k.key} v={k.words} />
              ))}
            </Stack>
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
              Keys do nothing while you type in a field.
            </Typography>
            <Divider sx={{ my: 2 }} />
          </>
        )}
        <Typography variant="subtitle2" component="h3" sx={{ mb: 0.5 }}>
          Colors
        </Typography>
        <Stack direction="row" useFlexGap sx={{ flexWrap: 'wrap', gap: 0.5 }}>
          <StatusChip tone="good" label="finished" />
          <StatusChip tone="wait" label="in progress or with someone" />
          <StatusChip tone="attn" label="needs a look" />
          <StatusChip tone="bad" label="blocked or late" />
          <StatusChip tone="quiet" label="plain state" />
        </Stack>
        <Divider sx={{ my: 2 }} />
        <Typography variant="subtitle2" component="h3" sx={{ mb: 0.5 }}>
          Notifications
        </Typography>
        <NotifySetting vault={session.vault} />
        <Divider sx={{ my: 2 }} />
        <Details summary="About">
          Vault {session.vault}. Signed in with key {session.key}. Build {UI_COMMIT}.
        </Details>
      </DialogContent>
      <DialogActions>
        <Button onClick={close}>Close</Button>
      </DialogActions>
    </Dialog>
  )
}

// NavDrawer lists every screen when the tab bar is folded away. It is a URL overlay like the rest.
function NavDrawer() {
  const close = useClose()
  const path = useRouterState({ select: (st) => st.location.pathname })
  const navigate = useNavigate()
  return (
    <Drawer open onClose={close} anchor="left" aria-label="Screens">
      <Box sx={{ width: 280 }} role="navigation">
        <List>
          {[...TABS, ...EXTRA_SCREENS].map((t) => (
            <ListItemButton
              key={t.to}
              component="a"
              href={t.to}
              selected={path === t.to || path.startsWith(`${t.to}/`)}
              onClick={(e) => {
                // Let the router do it: a plain navigation here would reload the app.
                e.preventDefault()
                void navigate({ href: t.to })
              }}
            >
              <ListItemText primary={t.label} secondary={t.hint} />
              {t.key && (
                <Typography
                  component="kbd"
                  variant="caption"
                  color="text.secondary"
                  sx={{ fontFamily: 'ui-monospace, monospace' }}
                >
                  {t.key}
                </Typography>
              )}
            </ListItemButton>
          ))}
        </List>
      </Box>
    </Drawer>
  )
}

function Row({ k, v }: { k: string; v: string }) {
  return (
    <Stack direction="row" spacing={2} sx={{ alignItems: 'baseline' }}>
      <Typography component="dt" sx={{ fontFamily: 'ui-monospace, monospace', width: 40 }}>
        {k}
      </Typography>
      <Typography component="dd" sx={{ m: 0 }}>
        {v}
      </Typography>
    </Stack>
  )
}
