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
    if (await ensureSession()) throw redirect({ href: search.redirect ?? '/today', replace: true })
  },
  component: Login,
})

function Login() {
  const { redirect, signedout } = Route.useSearch()
  const navigate = useNavigate()
  const [token, setToken] = useState('')
  const [error, setError] = useState(lastSignInError)
  const [busy, setBusy] = useState(false)

  const go = () => void navigate({ href: redirect ?? '/today', replace: true })

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await signIn(token)
      go()
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
              Sign in to para
            </Typography>
            {signedout && !error && <Alert severity="success">You are signed out.</Alert>}
            <Typography variant="body2" color="text.secondary">
              Open the sign-in link para gave you. To get one, run{' '}
              <Box
                component="code"
                sx={{
                  px: 0.75,
                  py: 0.25,
                  borderRadius: 0.5,
                  bgcolor: 'action.hover',
                  fontFamily: 'ui-monospace, monospace',
                  fontSize: '0.9em',
                  overflowWrap: 'anywhere',
                }}
              >
                para key create &lt;name&gt; --print-url
              </Box>{' '}
              on the machine running para. Or paste an access key below.
            </Typography>
            {error && <Alert severity="error">{error}</Alert>}
            <TextField
              label="Access key"
              type="password"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              autoComplete="off"
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
