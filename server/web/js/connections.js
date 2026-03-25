/**
 * connections.js — Live connection visualization: animated lines + source markers
 */

const StarConns = (() => {
  let map = null
  let layerGroup = null
  let visible = true
  let nodesCache = []

  const COLORS = [
    '#00ccff', '#ff66cc', '#66ff66', '#ffcc00',
    '#ff6644', '#aa66ff', '#00ffaa', '#ff9900',
  ]
  const portColorMap = {}
  let colorIndex = 0

  function init(leafletMap) {
    map = leafletMap
    layerGroup = L.layerGroup().addTo(map)
  }

  function setNodes(nodes) {
    nodesCache = nodes
  }

  function render(connData) {
    // Nuclear cleanup: remove layer group AND scrub any orphaned DOM elements
    const oldLabels = document.querySelectorAll('.sn-conn-label')
    const oldSrcDots = document.querySelectorAll('.sn-conn-src')
    layerGroup.clearLayers()
    oldLabels.forEach(el => el.remove())
    oldSrcDots.forEach(el => el.remove())

    if (!visible || !connData) return

    // Debug: log raw API data
    let totalConns = 0
    let withRate = 0
    Object.values(connData).forEach(conns => {
      conns.forEach(c => {
        totalConns++
        if ((c.rate_up || 0) + (c.rate_down || 0) > 50) withRate++
      })
    })
    console.log('[StarConns] API data: ' + totalConns + ' connections, ' + withRate + ' with rate > 50 B/s')

    const srcMarkers = {}
    let labelCount = 0

    Object.keys(connData).forEach(nodeId => {
      const node = nodesCache.find(n => n.id === nodeId)
      if (!node) return

      const conns = connData[nodeId] || []
      conns.forEach(conn => {
        if (!conn.src_lat && !conn.src_lng) return

        const color = getPortColor(conn.protocol)
        const rateUp = conn.rate_up || 0
        const rateDown = conn.rate_down || 0
        const peakRate = Math.max(rateUp, rateDown)
        const totalRate = rateUp + rateDown
        const weight = rateToWeight(totalRate)

        let tgtLng = node.longitude
        const lngDiff = conn.src_lng - tgtLng
        if (lngDiff > 180) tgtLng += 360
        else if (lngDiff < -180) tgtLng -= 360

        const latlngs = [[conn.src_lat, conn.src_lng], [node.latitude, tgtLng]]

        // Invisible hitbox
        const hitbox = L.polyline(latlngs, {
          weight: Math.max(15, weight + 8),
          opacity: 0,
          interactive: true,
        })
        hitbox.bindTooltip(
          '<div class="link-tooltip">' +
          '<span class="label">IP:</span> <span class="value">' + esc(conn.src_ip) + '</span><br>' +
          '<span class="label">Location:</span> <span class="value">' + esc(conn.src_city || '?') + ', ' + esc(conn.src_country || '?') + '</span><br>' +
          '<span class="label">Protocol:</span> <span class="value">' + esc(conn.protocol) + '</span><br>' +
          '<span class="label">Traffic:</span> <span class="value">' +
            '\u2191' + mbps(rateUp) + ' \u2193' + mbps(rateDown) +
          '</span></div>',
          { sticky: true }
        )
        layerGroup.addLayer(hitbox)

        // Visible line
        const polyline = L.polyline(latlngs, {
          color: color,
          weight: weight,
          opacity: 0.6,
          dashArray: '4, 8',
          interactive: false,
        })
        layerGroup.addLayer(polyline)
        setTimeout(() => {
          const el = polyline.getElement()
          if (el) el.classList.add('conn-flow')
        }, 0)

        // Rate label — only for connections with actual measured rate
        if (peakRate > 50 && totalRate > 50) {
          const midLat = (conn.src_lat + node.latitude) / 2
          const midLng = (conn.src_lng + tgtLng) / 2
          layerGroup.addLayer(L.marker([midLat, midLng], {
            icon: L.divIcon({
              className: 'sn-conn-label',
              html: '<span style="color:' + color + ';font-family:JetBrains Mono,monospace;font-size:9px;font-weight:500;text-shadow:0 0 4px #000,0 0 8px #000;white-space:nowrap">' + mbps(peakRate) + '</span>',
              iconSize: [70, 14],
              iconAnchor: [35, 7],
            }),
            interactive: false,
          }))
          labelCount++
        }

        // Source marker
        if (!srcMarkers[conn.src_ip]) {
          const cityLabel = conn.src_city || conn.src_ip
          const tipText = conn.src_ip + ' (' + (conn.src_city || '?') + ', ' + (conn.src_country || '?') + ')'
          const marker = L.marker([conn.src_lat, conn.src_lng], {
            icon: L.divIcon({
              className: 'sn-conn-src',
              html: '<div class="conn-src-marker"></div><div class="conn-src-label">' + esc(cityLabel) + '</div>',
              iconSize: [6, 6],
              iconAnchor: [3, 3],
            }),
          })
          marker.bindTooltip(esc(tipText), { direction: 'top', offset: [0, -6] })
          layerGroup.addLayer(marker)
          srcMarkers[conn.src_ip] = true
        }
      })
    })

    console.log('[StarConns] render: removed ' + (oldLabels.length + oldSrcDots.length) + ' old DOM nodes, created ' + labelCount + ' labels')
  }

  // Always MB/s, one decimal place
  function mbps(bytesPerSec) {
    return (bytesPerSec / 1048576).toFixed(1) + ' MB/s'
  }

  function rateToWeight(bytesPerSec) {
    if (bytesPerSec <= 0) return 1
    if (bytesPerSec < 1024) return 1
    if (bytesPerSec < 10240) return 2 + (bytesPerSec / 10240)
    if (bytesPerSec < 102400) return 4 + 2 * (bytesPerSec / 102400)
    if (bytesPerSec < 1048576) return 7 + 3 * (bytesPerSec / 1048576)
    return 12
  }

  function getPortColor(protocol) {
    if (!portColorMap[protocol]) {
      portColorMap[protocol] = COLORS[colorIndex % COLORS.length]
      colorIndex++
    }
    return portColorMap[protocol]
  }

  function esc(str) {
    if (!str) return ''
    const d = document.createElement('div')
    d.textContent = str
    return d.innerHTML
  }

  function toggle() {
    visible = !visible
    if (!visible) {
      layerGroup.clearLayers()
      document.querySelectorAll('.sn-conn-label').forEach(el => el.remove())
      document.querySelectorAll('.sn-conn-src').forEach(el => el.remove())
    }
    return visible
  }

  function isVisible() {
    return visible
  }

  return { init, setNodes, render, toggle, isVisible }
})()
