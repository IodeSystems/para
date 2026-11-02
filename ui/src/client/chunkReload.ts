// Reload once when a lazily loaded chunk fails (ERR_NETWORK_CHANGED on Wi-Fi to cellular).
//
// A cancelled module fetch is remembered as failed in the page's module map, so retrying the
// import() in the same page fails again: only a reload recovers. Vite dispatches
// `vite:preloadError` on window for every failed dynamic import.
//
// The loop guard lives in ONE place, public/boot.js (window.__paraReloadOnce), because boot.js
// must run before any module and cannot import this file: once per build + path per 60 s in
// sessionStorage; with no storage, never from a page that was itself loaded by a reload.
// No boot.js (it failed to load) means no auto-reload; the error screen's Reload button still works.

declare global {
  interface Window {
    __paraReloadOnce?: () => boolean
  }
}

// Messages Chromium, Firefox, and Safari use for a failed dynamic import, plus Vite's CSS error.
const CHUNK_ERROR =
  /^(Failed to fetch dynamically imported module|error loading dynamically imported module|Importing a module script failed|Unable to preload CSS)/

export function isChunkLoadError(err: unknown): boolean {
  return err instanceof Error && CHUNK_ERROR.test(err.message)
}

// requestChunkReload: true when a reload was started (now or earlier on this page).
export function requestChunkReload(): boolean {
  return typeof window !== 'undefined' && (window.__paraReloadOnce?.() ?? false)
}

export function installChunkReload(): void {
  window.addEventListener('vite:preloadError', () => {
    // Not preventDefault: the caller still gets the error, so a refused reload (the guard) ends
    // on the error screen with its Reload button.
    requestChunkReload()
  })
}
