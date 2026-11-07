// Reload once when a same-origin script/style fails during boot (see index.html). src/client/chunkReload.ts
;(function (w, d, l) {
  var KEY = 'chunk-reload',
    WINDOW_MS = 60000,
    started = false
  function reloadOnce() {
    if (started) return true
    var m = d.querySelector('meta[name="ui-commit"]')
    var id = (m ? m.content : 'dev') + ' ' + l.pathname,
      now = Date.now(),
      s = null
    try {
      s = w.sessionStorage
      var prev = JSON.parse(s.getItem(KEY) || 'null')
      if (prev && prev.id === id && now - prev.at < WINDOW_MS) return false
      s.setItem(KEY, JSON.stringify({ id: id, at: now }))
    } catch (e) {
      s = null
    }
    if (!s) {
      try {
        var nav = w.performance.getEntriesByType('navigation')[0]
        if (nav && nav.type === 'reload') return false
      } catch (e) {}
    }
    started = true
    l.reload()
    return true
  }
  w.__paraReloadOnce = reloadOnce
  w.addEventListener(
    'error',
    function (e) {
      var t = e.target,
        url = t && (t.src || t.href)
      if (!url || !d.getElementById('boot')) return
      if (
        t.tagName === 'LINK' ? !/^(stylesheet|modulepreload)$/.test(t.rel) : t.tagName !== 'SCRIPT'
      )
        return
      try {
        if (new URL(url, l.href).origin !== l.origin) return
      } catch (e2) {
        return
      }
      reloadOnce()
    },
    true,
  )
})(window, document, location)

// Light or dark before the first paint. Focus, the theme of the app (src/theme.ts), is dark unless
// the owner switched to light. The themes behind ?theme= follow the system setting unless the owner
// switched (para.mode); then the stored choice is set here.
;(function (d) {
  var focus = true
  try {
    var t = new URLSearchParams(location.search).get('theme') || localStorage.getItem('para.theme')
    focus = !t || t === 'c'
    var m = localStorage.getItem('para.mode')
    if (m === 'light' || m === 'dark') d.documentElement.setAttribute('data-mui-color-scheme', m)
    else if (focus) d.documentElement.setAttribute('data-mui-color-scheme', 'dark')
  } catch (e) {
    if (focus) d.documentElement.setAttribute('data-mui-color-scheme', 'dark')
  }
})(document)
