const StarConns = (() => {
  let map = null
  let layerGroup = null
  let visible = true
  let nodesCache = []

  function init(leafletMap) {
    map = leafletMap
    layerGroup = L.layerGroup().addTo(map)
  }

  function setNodes(nodes) {
    nodesCache = nodes
  }

  function render(connData) {
    layerGroup.clearLayers()
    if (!visible || !connData) return

    Object.entries(connData).forEach(([nodeId, connections]) => {
      const node = nodesCache.find(item => item.id === nodeId)
      if (!node) return

      ;(connections || []).slice(0, 20).forEach(conn => {
        if (!conn.src_lat && !conn.src_lng) return

        let targetLng = node.longitude
        const diff = conn.src_lng - targetLng
        if (diff > 180) targetLng += 360
        if (diff < -180) targetLng -= 360

        const points = [[conn.src_lat, conn.src_lng], [node.latitude, targetLng]]
        const line = L.polyline(points, {
          color: connectionColor(conn.rate || 0),
          weight: connectionWeight(conn.rate || 0),
          opacity: 0.48,
          dashArray: '4, 8',
          interactive: false,
        })
        layerGroup.addLayer(line)

        const hitbox = L.polyline(points, {
          weight: Math.max(14, connectionWeight(conn.rate || 0) + 8),
          opacity: 0,
        })
        hitbox.bindTooltip(buildTooltip(conn, node), { sticky: true })
        layerGroup.addLayer(hitbox)

        layerGroup.addLayer(L.circleMarker([conn.src_lat, conn.src_lng], {
          radius: 2.8,
          color: connectionColor(conn.rate || 0),
          fillColor: connectionColor(conn.rate || 0),
          fillOpacity: 0.8,
          weight: 0,
          interactive: false,
        }))
      })
    })
  }

  function buildTooltip(conn, node) {
    return `
      <div>
        <strong>${conn.src_ip}</strong> → <strong>${node.name}</strong><br>
        <span>${conn.src_city || '?'}${conn.src_country ? `, ${conn.src_country}` : ''}</span><br>
        <span>${conn.protocol || 'unknown'} • ${formatRate(conn.rate || 0)} • ${formatBytes(conn.total_bytes || 0)}</span>
      </div>
    `
  }

  function connectionColor(rate) {
    if (rate < 1024) return '#ff6d64'
    if (rate < 1024 * 128) return '#ffba4a'
    return '#69e3ff'
  }

  function connectionWeight(rate) {
    if (rate < 1024) return 1
    if (rate < 1024 * 32) return 2
    if (rate < 1024 * 512) return 4
    return 7
  }

  function formatRate(rate) {
    if (rate < 1024) return `${Math.round(rate)} B/s`
    if (rate < 1024 * 1024) return `${(rate / 1024).toFixed(1)} KB/s`
    return `${(rate / 1024 / 1024).toFixed(1)} MB/s`
  }

  function formatBytes(bytes) {
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
    return `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB`
  }

  function toggle() {
    visible = !visible
    if (!visible) {
      layerGroup.clearLayers()
    }
    return visible
  }

  function isVisible() {
    return visible
  }

  return { init, setNodes, render, toggle, isVisible }
})()
