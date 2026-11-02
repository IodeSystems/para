// Build stamp, read from the <meta> tags vite writes into index.html (vite.config.ts
// buildStampMeta). It stays out of the JS so chunk hashes change only when code does.
// No document (vitest) or no tag gives 'dev'/'unknown'.
const meta = (name: string, fallback: string): string =>
  (typeof document !== 'undefined' &&
    document.querySelector<HTMLMetaElement>(`meta[name="${name}"]`)?.content) ||
  fallback

export const UI_VERSION: string = meta('ui-version', 'dev')
export const UI_COMMIT: string = meta('ui-commit', 'dev')
export const UI_BUILT: string = meta('ui-built', 'unknown')
