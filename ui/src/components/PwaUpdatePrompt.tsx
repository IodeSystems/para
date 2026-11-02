import { useEffect, useRef, useState } from 'react'
import { useRegisterSW } from 'virtual:pwa-register/react'
import { Alert, Button, Snackbar } from '@mui/material'

// How soon after the page loads a waiting update counts as "they just got here". A SW
// commonly installs a few seconds after a fresh load (the browser served the cached shell,
// then fetched + installed the newer SW); prompting "reload?" seconds into a fresh visit is
// pointless churn — just refresh onto the new version. Past this window the user may be
// mid-task, so we ask instead of yanking the page out from under them.
const FRESH_RELOAD_WINDOW_MS = 10_000
const APP_LOADED_AT = Date.now()

// How often a long-open tab re-checks for a new deploy. Without this, a tab left open for
// hours never notices a shipped fix until the user reloads by hand — the core of issue #16.
// registration.update() re-fetches the SW; if the build changed, `needRefresh` fires and the
// auto/banner logic below runs. 30 min is frequent enough to reach users same-session,
// infrequent enough to be negligible network cost.
const UPDATE_CHECK_INTERVAL_MS = 30 * 60 * 1000

// A reload activates the new SW and re-downloads the shell, so any half-typed text the user
// had would be lost. We snapshot editable text fields to sessionStorage right before the
// reload and reapply them once the forms remount. BEST-EFFORT by design: plain <input>/
// <textarea> values restore; MUI Select / complex controlled widgets and fields that haven't
// mounted yet may not. The goal is not to lose the common case (a half-typed message / name),
// not to perfectly rehydrate arbitrary UI.
const FORM_RESTORE_KEY = 'pwa-form-restore'
const FORM_RESTORE_MAX_AGE_MS = 60_000
const RESTORE_RETRY_ATTEMPTS = 12
const RESTORE_RETRY_DELAY_MS = 150

type FormSnapshot = {
  path: string
  ts: number
  fields: Record<string, string>
}

type EditableField = HTMLInputElement | HTMLTextAreaElement

// Skip anything sensitive: passwords, hidden inputs, and autocomplete="off" fields whose
// name/id hints at credentials/card/OTP data. We'd rather drop a value than persist a secret
// to sessionStorage.
function isSensitive(el: EditableField): boolean {
  if (el instanceof HTMLInputElement) {
    if (el.type === 'password' || el.type === 'hidden') return true
  }
  const autocomplete = (el.getAttribute('autocomplete') || '').toLowerCase()
  if (autocomplete === 'off') {
    const hint = `${el.name} ${el.id}`.toLowerCase()
    if (/pass|card|\bcc\b|cvv|cvc|secret|token|otp|ssn|routing|account|pin/.test(hint)) return true
  }
  return false
}

// Enumerate editable fields with a key that reproduces across a reload: prefer `name`, then
// `id`, else a stable index within the field's form (or a free-field bucket). Snapshot (read)
// and restore (write) call the SAME collector so keys line up.
function collectFields(): { key: string; el: EditableField }[] {
  const out: { key: string; el: EditableField }[] = []
  Array.from(document.forms).forEach((form, fi) => {
    const formKey = form.name || form.id || `form${fi}`
    const els = form.querySelectorAll('input, textarea') as NodeListOf<EditableField>
    els.forEach((el, idx) => {
      out.push({ key: el.name || el.id || `${formKey}::${idx}`, el })
    })
  })
  const free = document.querySelectorAll('input, textarea') as NodeListOf<EditableField>
  free.forEach((el, idx) => {
    if (el.form) return // already captured under its form above
    out.push({ key: el.name || el.id || `__free::${idx}`, el })
  })
  return out
}

// Returns true if any editable, non-sensitive text field currently holds user input. Used to
// note (but never suppress) an in-progress-work auto-reload.
function hasUnsavedInput(): boolean {
  try {
    return collectFields().some(({ el }) => el.value && !isSensitive(el))
  } catch {
    return false
  }
}

// Serialize current field values into sessionStorage, keyed by path so a reload onto a
// different route doesn't reapply stale values. Never throws.
function snapshotForms(): void {
  try {
    const fields: Record<string, string> = {}
    for (const { key, el } of collectFields()) {
      if (!el.value) continue
      if (isSensitive(el)) continue
      fields[key] = el.value
    }
    if (Object.keys(fields).length === 0) return
    const snap: FormSnapshot = {
      path: location.pathname + location.search,
      ts: Date.now(),
      fields,
    }
    sessionStorage.setItem(FORM_RESTORE_KEY, JSON.stringify(snap))
  } catch {
    // swallow: best-effort — a snapshot failure must never block the update
  }
}

// Set a React-controlled input's value the way React notices: the framework tracks the value
// via a cached descriptor, so el.value = x is ignored on the next render. Call the native
// prototype setter, then dispatch a bubbling 'input' event so React / react-hook-form's
// onChange fires and their state updates.
function setValueReactively(el: EditableField, value: string): void {
  const proto =
    el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype
  const descriptor: { set?: (this: EditableField, v: string) => void } | undefined =
    Object.getOwnPropertyDescriptor(proto, 'value')
  const setter = descriptor?.set
  if (setter) setter.call(el, value)
  else el.value = value
  el.dispatchEvent(new Event('input', { bubbles: true }))
}

// On startup, reapply a fresh, same-route snapshot if one exists, then drop it. Forms may not
// have mounted yet, so retry a few times; stop once every captured field has been reapplied.
// Always deletes the key (even on mismatch/expiry) so a stale snapshot can't linger. Never
// throws.
function restoreForms(): void {
  try {
    const raw = sessionStorage.getItem(FORM_RESTORE_KEY)
    if (!raw) return
    let snap: FormSnapshot
    try {
      snap = JSON.parse(raw) as FormSnapshot
    } catch {
      sessionStorage.removeItem(FORM_RESTORE_KEY)
      return
    }
    const valid =
      snap &&
      typeof snap === 'object' &&
      typeof snap.path === 'string' &&
      typeof snap.ts === 'number' &&
      snap.fields &&
      typeof snap.fields === 'object'
    const samePath = valid && snap.path === location.pathname + location.search
    const recent = valid && Date.now() - snap.ts < FORM_RESTORE_MAX_AGE_MS
    if (!valid || !samePath || !recent) {
      sessionStorage.removeItem(FORM_RESTORE_KEY)
      return
    }

    const wanted = new Set(Object.keys(snap.fields))
    const applied = new Set<string>()
    let attempts = 0
    const tick = () => {
      attempts++
      try {
        for (const { key, el } of collectFields()) {
          if (applied.has(key)) continue
          if (!(key in snap.fields)) continue
          setValueReactively(el, snap.fields[key])
          applied.add(key)
        }
      } catch {
        // swallow: ignore a bad pass; a later retry may still succeed
      }
      const done = applied.size >= wanted.size
      if (done || attempts >= RESTORE_RETRY_ATTEMPTS) {
        sessionStorage.removeItem(FORM_RESTORE_KEY)
        return
      }
      setTimeout(tick, RESTORE_RETRY_DELAY_MS)
    }
    // Defer the first pass so React has a chance to mount the route's forms.
    setTimeout(tick, RESTORE_RETRY_DELAY_MS)
  } catch {
    // best-effort — never let restore break app startup
    try {
      sessionStorage.removeItem(FORM_RESTORE_KEY)
    } catch {
      // swallow: best-effort, failure is irrelevant
    }
  }
}

// Service-worker update + offline-ready UX . The SW precaches the app shell only: never vault content or API answers. registerType is `prompt`, so a
// newly-built version installs but WAITS — then EITHER we auto-refresh (if it landed within
// FRESH_RELOAD_WINDOW_MS of load) OR surface a visible "Reload" the user chooses (mid-task).
// A periodic registration.update() (UPDATE_CHECK_INTERVAL_MS) makes long-open tabs notice a
// deploy without a manual reload . Either way, before reloading we snapshot form
// fields (snapshotForms) and reapply them after (restoreForms) so in-progress text survives.
export function PwaUpdatePrompt() {
  const {
    needRefresh: [needRefresh, setNeedRefresh],
    updateServiceWorker,
  } = useRegisterSW({
    onRegisteredSW(_swScriptUrl, registration) {
      if (!registration) return
      setInterval(() => {
        // Rejects when the registration is mid-install or gone ("Failed to update a
        // ServiceWorker… invalid state"). A missed check is harmless (the next one runs in 30 min);
        // unhandled it lands in error_log every half hour from every open tab.
        registration
          .update()
          .catch((e: unknown) => console.debug('service worker update check failed', e))
      }, UPDATE_CHECK_INTERVAL_MS)
    },
  })
  const autoReloaded = useRef(false)
  const updateInitiated = useRef(false)
  const reloading = useRef(false)

  // Reload exactly once — guards the double-reload a controllerchange + fallback race could cause.
  const reloadOnce = () => {
    if (reloading.current) return
    reloading.current = true
    window.location.reload()
  }

  // Reapply any pre-reload snapshot once, after mount.
  useEffect(() => {
    restoreForms()
  }, [])

  // OWN the reload. The reported bug: clicking "Reload" did nothing, or re-served the
  // stale shell. Cause: updateServiceWorker(true) relies on vite-plugin-pwa's internal
  // controllerchange listener, which no-ops if there's no waiting worker and can reload BEFORE the
  // new SW controls. Instead we reload on controllerchange ourselves — that fires once the new
  // worker takes control, so the reload is served by the NEW worker (fresh assets). Gated on
  // updateInitiated so a fresh visitor's first-load clientsClaim controllerchange never reloads.
  useEffect(() => {
    if (!('serviceWorker' in navigator)) return
    const onControllerChange = () => {
      if (updateInitiated.current) reloadOnce()
    }
    navigator.serviceWorker.addEventListener('controllerchange', onControllerChange)
    return () => navigator.serviceWorker.removeEventListener('controllerchange', onControllerChange)
  }, [])

  // Snapshot in-progress input, then activate the new version. Used by both the auto path and the
  // "Reload" button so no reload ever drops the user's work. If a new SW is waiting, skip-waiting
  // it (pass false so vite doesn't also reload — we own that via controllerchange) with a long
  // safety-net reload; if nothing is waiting the page is already latest, so a plain reload is right
  // and the click never no-ops.
  const reloadWithSnapshot = async () => {
    updateInitiated.current = true
    snapshotForms()
    let hasWaiting = false
    try {
      const reg = await navigator.serviceWorker?.getRegistration()
      hasWaiting = !!reg?.waiting
    } catch {
      // swallow: ignore — fall through to a plain reload
    }
    if (hasWaiting) {
      void updateServiceWorker(false)
      // controllerchange normally fires within a second of skip-waiting; this only catches a
      // stuck activation, and is long enough that the new worker has controlled by then.
      setTimeout(reloadOnce, 6000)
    } else {
      reloadOnce()
    }
  }

  // True once the "just loaded" window has elapsed. State (not Date.now() in render) so render stays
  // pure; the timer flips it the moment the window ends.
  const [pastFreshWindow, setPastFreshWindow] = useState(
    () => Date.now() - APP_LOADED_AT >= FRESH_RELOAD_WINDOW_MS,
  )
  useEffect(() => {
    if (pastFreshWindow) return
    const t = window.setTimeout(
      () => setPastFreshWindow(true),
      Math.max(0, APP_LOADED_AT + FRESH_RELOAD_WINDOW_MS - Date.now()),
    )
    return () => window.clearTimeout(t)
  }, [pastFreshWindow])

  // The newest reloadWithSnapshot, for effects that must not re-run when its identity changes.
  const latestReload = useRef(reloadWithSnapshot)
  useEffect(() => {
    latestReload.current = reloadWithSnapshot
  })

  useEffect(() => {
    if (!needRefresh || autoReloaded.current) return
    if (Date.now() - APP_LOADED_AT < FRESH_RELOAD_WINDOW_MS) {
      // Just loaded → skip the prompt, activate the new SW and refresh straight away. If the
      // user already started typing in this window we don't suppress the update — snapshot +
      // restore preserves it across the reload (hasUnsavedInput is just the guard signal).
      autoReloaded.current = true
      void hasUnsavedInput() // touch for clarity; snapshotForms captures regardless
      void latestReload.current()
    }
  }, [needRefresh])

  // Mid-task update: ask before reloading so we don't drop the user's work.
  if (needRefresh && pastFreshWindow) {
    return (
      <Snackbar open anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}>
        <Alert
          severity="info"
          variant="filled"
          action={
            <>
              <Button color="inherit" size="small" onClick={() => void reloadWithSnapshot()}>
                Reload
              </Button>
              <Button color="inherit" size="small" onClick={() => setNeedRefresh(false)}>
                Later
              </Button>
            </>
          }
        >
          A new version of para is ready.
        </Alert>
      </Snackbar>
    )
  }

  // NO "works offline" toast. It would fire once, on the first visit — which for a member is the join
  // screen — and be a message about something the person did not do, on the screen where they are
  // asked to prove who they are. The app still works offline; it just does not announce it.
  return null
}
