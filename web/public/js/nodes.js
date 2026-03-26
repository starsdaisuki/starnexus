/**
 * nodes.js — Node markers: custom circles, glow animations, detail popups
 */

const StarNodes = (() => {
  let markers = []
  let map = null

  function init(leafletMap) {
    map = leafletMap
  }

  function render(nodes) {
    // Clear old markers
    markers.forEach(m => map.removeLayer(m))
    markers = []

    nodes.forEach(node => {
      const size = 14
      const icon = L.divIcon({
        className: '',
        html: `<div class="node-marker ${node.status}" style="width:${size}px;height:${size}px;"></div>`,
        iconSize: [size, size],
        iconAnchor: [size / 2, size / 2],
      })

      const marker = L.marker([node.latitude, node.longitude], { icon })
        .addTo(map)

      // hover tooltip
      marker.bindTooltip(node.name, {
        direction: 'top',
        offset: [0, -10],
      })

      // click popup
      marker.bindPopup(buildPopupContent(node), {
        maxWidth: 320,
        minWidth: 260,
        closeButton: true,
        className: '',
      })

      markers.push(marker)
    })
  }

  function buildPopupContent(node) {
    const m = node.metrics || {}
    const statusLabel = {
      online: 'Online',
      degraded: 'Degraded',
      offline: 'Offline',
      unknown: 'Unknown',
    }

    return `
      <div class="node-popup">
        <div class="node-popup-header">
          <div>
            <div class="node-popup-name">${escapeHtml(node.name)}</div>
            <div class="node-popup-provider">${escapeHtml(node.provider || 'Unknown')}${node.ip_address ? ' &middot; ' + escapeHtml(node.ip_address) : ''}</div>
          </div>
          <span class="status-badge ${node.status}">${statusLabel[node.status] || 'Unknown'}</span>
        </div>

        ${buildMetricBar('CPU', m.cpu_percent)}
        ${buildMetricBar('Memory', m.memory_percent)}
        ${buildMetricBar('Disk', m.disk_percent)}

        <hr class="node-popup-divider">

        <div class="metric-text-row">
          <span class="label">BW Up</span>
          <span class="value">${formatBandwidth(m.bandwidth_up)}</span>
        </div>
        <div class="metric-text-row">
          <span class="label">BW Down</span>
          <span class="value">${formatBandwidth(m.bandwidth_down)}</span>
        </div>
        <div class="metric-text-row">
          <span class="label">Load</span>
          <span class="value">${m.load_avg != null ? m.load_avg.toFixed(2) : '--'}</span>
        </div>
        <div class="metric-text-row">
          <span class="label">Connections</span>
          <span class="value">${m.connections != null ? m.connections : '--'}</span>
        </div>

        <hr class="node-popup-divider">

        <div class="metric-text-row">
          <span class="label">Uptime</span>
          <span class="value">${formatUptime(m.uptime_seconds)}</span>
        </div>
        <div class="metric-text-row">
          <span class="label">Last Seen</span>
          <span class="value">${formatRelativeTime(node.last_seen)}</span>
        </div>
      </div>
    `
  }

  function buildMetricBar(label, percent) {
    if (percent == null) return ''
    const p = Math.min(100, Math.max(0, percent))
    const barClass = p > 80 ? 'bar-red' : p > 60 ? 'bar-yellow' : 'bar-green'
    return `
      <div class="metric-row">
        <span class="metric-label">${label}</span>
        <div class="metric-bar-wrap">
          <div class="metric-bar ${barClass}" style="width:${p}%"></div>
        </div>
        <span class="metric-value">${p.toFixed(1)}%</span>
      </div>
    `
  }

  function formatBandwidth(kbps) {
    if (kbps == null || kbps === 0) return '--'
    if (kbps >= 1024) return (kbps / 1024).toFixed(1) + ' MB/s'
    return kbps.toFixed(1) + ' KB/s'
  }

  function formatUptime(seconds) {
    if (!seconds) return '--'
    const days = Math.floor(seconds / 86400)
    const hours = Math.floor((seconds % 86400) / 3600)
    if (days > 0) return `${days}d ${hours}h`
    const mins = Math.floor((seconds % 3600) / 60)
    return `${hours}h ${mins}m`
  }

  function formatRelativeTime(timestamp) {
    if (!timestamp) return '--'
    const diff = Math.floor(Date.now() / 1000) - timestamp
    if (diff < 60) return `${diff}s ago`
    if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
    if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`
    return `${Math.floor(diff / 86400)}d ago`
  }

  function escapeHtml(str) {
    if (!str) return ''
    const div = document.createElement('div')
    div.textContent = str
    return div.innerHTML
  }

  return { init, render }
})()
