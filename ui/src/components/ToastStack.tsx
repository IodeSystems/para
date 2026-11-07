import { createContext, useContext, useState, useSyncExternalStore, type ReactNode } from 'react'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Paper from '@mui/material/Paper'
import Typography from '@mui/material/Typography'
import { keyframes } from '@mui/material/styles'
import { errorText } from '@/client/gqlClient'
import { COLLAPSE_MS, FADE_MS, prefersReducedMotion, ToastStore, type Toast } from '@/lib/toasts'

// ToastStack shows what the owner just did, at the right edge (under the app bar on a phone), one line per
// action with Undo. Any page gets the store from useToasts(); the stack is mounted once, behind
// sign-in. See lib/toasts.ts for the lifecycle.

const Ctx = createContext<ToastStore | null>(null)

export function ToastProvider({ children }: { children: ReactNode }) {
  const [store] = useState(
    () => new ToastStore({ reduced: prefersReducedMotion, describe: errorText }),
  )
  return (
    <Ctx.Provider value={store}>
      {children}
      <ToastStack store={store} />
    </Ctx.Provider>
  )
}

// useToasts is the shared toast store: push a toast, or dismiss one.
export function useToasts(): ToastStore {
  const s = useContext(Ctx)
  if (!s) throw new Error('useToasts: no ToastProvider above')
  return s
}

const slideIn = keyframes`
  from { transform: translateX(24px); opacity: 0; }
  to { transform: none; opacity: 1; }
`

function ToastStack({ store }: { store: ToastStore }) {
  const toasts = useSyncExternalStore(store.subscribe, store.getSnapshot)
  return (
    <Box
      role="status"
      aria-live="polite"
      aria-relevant="additions text"
      data-toast-stack
      sx={(t) => ({
        position: 'fixed',
        zIndex: t.zIndex.snackbar,
        pointerEvents: 'none',
        display: 'flex',
        flexDirection: 'column',
        // On a phone the toasts sit under the app bar: the bottom holds the sticky composer and
        // the bottom bar, and a toast there would cover what the owner types.
        top: { xs: 'calc(64px + env(safe-area-inset-top))', sm: 72 },
        right: { xs: 0, sm: 16 },
        left: { xs: 0, sm: 'auto' },
        width: { xs: 'auto', sm: 360 },
        maxWidth: '100vw',
        px: { xs: 2, sm: 0 },
      })}
    >
      {toasts.map((t) => (
        <ToastRow key={t.id} t={t} store={store} />
      ))}
    </Box>
  )
}

function ToastRow({ t, store }: { t: Toast; store: ToastStore }) {
  const gone = t.phase !== 'show'
  return (
    <Box
      data-toast={t.tone}
      data-phase={t.phase}
      onMouseEnter={() => store.hold(t.id)}
      onMouseLeave={() => store.release(t.id)}
      onFocus={() => store.hold(t.id)}
      onBlur={() => store.release(t.id)}
      sx={{
        display: 'grid',
        gridTemplateRows: t.phase === 'collapse' ? '0fr' : '1fr',
        opacity: gone ? 0 : 1,
        pointerEvents: gone ? 'none' : 'auto',
        transition: `opacity ${FADE_MS}ms ease, grid-template-rows ${COLLAPSE_MS}ms ease`,
        animation: `${slideIn} 180ms ease-out`,
        '@media (prefers-reduced-motion: reduce)': { transition: 'none', animation: 'none' },
      }}
    >
      <Box sx={{ overflow: 'hidden', minHeight: 0 }}>
        <Paper
          elevation={6}
          sx={{
            mb: 1,
            px: 2,
            py: 1,
            display: 'flex',
            alignItems: 'center',
            gap: 1,
            borderLeft: 4,
            borderColor: t.tone === 'error' ? 'error.main' : 'primary.main',
          }}
        >
          <Box sx={{ flexGrow: 1, minWidth: 0 }}>
            <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
              {t.text}
            </Typography>
            {t.note && (
              <Typography variant="caption" color="text.secondary" component="div">
                {t.note}
              </Typography>
            )}
          </Box>
          {t.undo && (
            <Button
              size="small"
              data-act="undo"
              disabled={t.undoing || gone}
              onClick={() => void store.runUndo(t.id)}
            >
              Undo
            </Button>
          )}
        </Paper>
      </Box>
    </Box>
  )
}
