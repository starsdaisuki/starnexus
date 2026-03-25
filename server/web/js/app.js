/**
 * app.js — Main entry: data fetching, polling, render coordination
 */

const StarApp = (() => {
  const API_BASE = '/api'
  const POLL_INTERVAL = 30000  // 30s for nodes/links/status
  const CONN_INTERVAL = 5000   // 5s for live connections
  const UPDATE_TICK = 1000     // 1s tick for "last update" display

  let lastUpdateTime = null
  let pollTimer = null
  let connTimer = null
  let tickTimer = null
  let hasError = false

  async function init() {
    // Init map
    const map = StarMap.init()
    StarNodes.init(map)
    StarLinks.init(map)
    StarConns.init(map)

    // Bind refresh button
    document.getElementById('btn-refresh').addEventListener('click', () => {
      const btn = document.getElementById('btn-refresh')
      btn.classList.add('spinning')
      setTimeout(() => btn.classList.remove('spinning'), 600)
      fetchAll()
    })

    // Bind connection toggle
    document.getElementById('btn-toggle-conns').addEventListener('click', () => {
      const btn = document.getElementById('btn-toggle-conns')
      const visible = StarConns.toggle()
      btn.classList.toggle('active', visible)
      if (visible) fetchConnections()
    })

    // Initial load
    await fetchAll()

    // Polling
    pollTimer = setInterval(fetchAll, POLL_INTERVAL)
    connTimer = setInterval(fetchConnections, CONN_INTERVAL)

    // Tick "last update" every second
    tickTimer = setInterval(updateLastUpdateDisplay, UPDATE_TICK)
  }

  async function fetchAll() {
    try {
      const [nodesRes, linksRes, statusRes] = await Promise.all([
        fetch(`${API_BASE}/nodes`),
        fetch(`${API_BASE}/links`),
        fetch(`${API_BASE}/status`),
      ])

      if (!nodesRes.ok || !linksRes.ok || !statusRes.ok) {
        throw new Error('API request failed')
      }

      const nodesData = await nodesRes.json()
      const linksData = await linksRes.json()
      const statusData = await statusRes.json()

      const nodes = nodesData.nodes || []

      // Render
      StarNodes.render(nodes)
      StarLinks.render(linksData.links || [], nodes)
      StarConns.setNodes(nodes)
      updateStatusBar(statusData)

      // Update timestamp
      lastUpdateTime = Date.now()
      clearError()
    } catch (e) {
      console.error('Data fetch failed:', e)
      showError()
    }
  }

  async function fetchConnections() {
    if (!StarConns.isVisible()) return

    try {
      const resp = await fetch(`${API_BASE}/connections`)
      if (!resp.ok) return
      const data = await resp.json()
      StarConns.render(data)
    } catch (e) {
      // Silent — connections are best-effort
    }
  }

  function updateStatusBar(status) {
    document.getElementById('count-online').textContent = status.online || 0
    document.getElementById('count-degraded').textContent = status.degraded || 0
    document.getElementById('count-offline').textContent = status.offline || 0
  }

  function updateLastUpdateDisplay() {
    const el = document.getElementById('last-update')
    if (!lastUpdateTime) {
      el.textContent = 'Last update: --'
      return
    }
    const seconds = Math.floor((Date.now() - lastUpdateTime) / 1000)
    el.textContent = `Last update: ${seconds}s ago`
  }

  function showError() {
    if (hasError) return
    hasError = true
    document.getElementById('error-banner').classList.remove('hidden')
  }

  function clearError() {
    if (!hasError) return
    hasError = false
    document.getElementById('error-banner').classList.add('hidden')
  }

  // DOM ready
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init)
  } else {
    init()
  }

  return { fetchAll }
})()
