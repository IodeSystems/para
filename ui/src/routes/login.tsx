import { useState, type FormEvent } from 'react'
import { createFileRoute, redirect, useNavigate } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Paper from '@mui/material/Paper'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { HttpBanner } from '@/components/Banners'
import { ensureSession, lastSignInError, signIn } from '@/client/auth'
import { homeFor } from '@/lib/next'

// CODE is how a command reads inside a sentence.
const CODE = {
  px: 0.75,
  py: 0.25,
  borderRadius: 0.5,
  bgcolor: 'action.hover',
  fontFamily: 'ui-monospace, monospace',
  fontSize: '0.9em',
  overflowWrap: 'anywhere',
} as const

export const Route = createFileRoute('/login')({
  validateSearch: (s: Record<string, unknown>): { redirect?: string; signedout?: true } => ({
    signedout: s.signedout === true || s.signedout === 'true' ? true : undefined,
    redirect:
      typeof s.redirect === 'string' && s.redirect.startsWith('/') && !s.redirect.startsWith('//')
        ? s.redirect
        : undefined,
  }),
  // A valid session needs no sign-in.
  beforeLoad: async ({ search }) => {
    const s = await ensureSession()
    if (s) throw redirect({ href: search.redirect ?? homeFor(s), replace: true })
  },
  component: Login,
})

function Login() {
  const { redirect, signedout } = Route.useSearch()
  const navigate = useNavigate()
  const [token, setToken] = useState('')
  const [error, setError] = useState(lastSignInError)
  const [busy, setBusy] = useState(false)

  const origin = window.location.origin
  const command = `para key create <device> --url ${origin} --scope web`
  const go = (s: Parameters<typeof homeFor>[0]) =>
    void navigate({ href: redirect ?? homeFor(s), replace: true })

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      go(await signIn(token))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'sign-in failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <HttpBanner />
      <Box sx={{ display: 'grid', placeItems: 'center', minHeight: '80dvh', p: 2 }}>
        <Paper
          component="form"
          onSubmit={(e) => void submit(e)}
          sx={{ p: 3, width: '100%', maxWidth: 420 }}
        >
          <Stack spacing={2}>
            <Typography variant="h5" component="h1">
              Not signed in
            </Typography>
            {signedout && !error && <Alert severity="success">You are signed out.</Alert>}
            <Typography variant="body1">
              Run{' '}
              <Box component="code" sx={CODE}>
                para serve login
              </Box>{' '}
              on this machine. It signs this browser in to every vault for 30 days, and the sign-in
              renews while you use it.
            </Typography>
            <Typography variant="body2" color="text.secondary">
              Your sign-in is missing, or it ended or was revoked. For another device, run this on
              the machine that runs para, and save the key it prints in your password manager (it
              does not expire until you revoke it with{' '}
              <Box component="code" sx={CODE}>
                para key revoke &lt;device&gt;
              </Box>
              ):
            </Typography>
            <Box component="code" sx={{ ...CODE, display: 'block', p: 1 }} data-login-command>
              {command}
            </Box>
            <Typography variant="body2" color="text.secondary">
              Then paste or fill the key below. Here you review and decide what agents propose;
              your agent makes other changes in the chat.
            </Typography>
            {error && <Alert severity="error">{error}</Alert>}
            {/* The site name is the username a password manager saves the key under. */}
            <TextField
              label="Site"
              name="username"
              value={window.location.host}
              autoComplete="username"
              size="small"
              slotProps={{ htmlInput: { readOnly: true } }}
            />
            <TextField
              label="Access key"
              type="password"
              name="password"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              autoComplete="current-password"
              autoFocus
              required
              slotProps={{ htmlInput: { autoCapitalize: 'none', spellCheck: false } }}
            />
            <Button type="submit" variant="contained" disabled={busy || token.trim() === ''}>
              Sign in
            </Button>
          </Stack>
        </Paper>
      </Box>
    </>
  )
}
