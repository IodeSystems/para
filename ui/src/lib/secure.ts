// plainHttp says whether the page is not a secure context. Service workers, PWA install, and
// navigator.clipboard need one. http://localhost and http://<vault>.localhost count as secure;
// a LAN address on plain HTTP does not.
export function plainHttp(): boolean {
  return typeof window !== 'undefined' && !window.isSecureContext
}
