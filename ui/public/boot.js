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
