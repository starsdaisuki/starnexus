/**
 * connections.js — Live connection visualization.
 *
 * Cloudflare CDN connections are aggregated per /16 subnet into one
 * line with a tree-style per-IP breakdown in the tooltip (CDN edges
 * rotate the low octets constantly, so per-IP lines would flicker).
 * Real traffic is colored per protocol/port; red is reserved for
 * probe/scan noise (zero rate, tiny totals) so genuine connections
 * stay visually distinct even when momentarily idle.
 */

const StarConns = (() => {
  let map = null
  let layerGroup = null
  let visible = true
  let nodesCache = []

  const COLORS = [
    '#00ccff', '#ff66cc', '#66ff66', '#ffcc00',
    '#ff9900', '#aa66ff', '#00ffaa', '#8cb0ff',
  ]
  const portColorMap = {}
  let colorIdx = 0

  const PROBE_COLOR = '#ff5c6e'

  // Cloudflare IPv4 CIDR ranges (from cloudflare.com/ips-v4)
  const CF_CIDRS = [
    ['173.245.48.0', 20], ['103.21.244.0', 22], ['103.22.200.0', 22],
    ['103.31.4.0', 22], ['141.101.64.0', 18], ['108.162.192.0', 18],
    ['190.93.240.0', 20], ['188.114.96.0', 20], ['197.234.240.0', 22],
    ['198.41.128.0', 17], ['162.158.0.0', 15], ['104.16.0.0', 13],
    ['104.24.0.0', 14], ['172.64.0.0', 13], ['131.0.72.0', 22],
  ].map(([base, bits]) => [ipToNum(base), bits])

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

    // Group: CDN connections by /16 subnet per node, direct ones by IP.
    const groups = {}
    Object.entries(connData).forEach(([nodeId, connections]) => {
      const node = nodesCache.find(item => item.id === nodeId)
      if (!node) return
      ;(connections || []).forEach(conn => {
        if (!conn.src_lat && !conn.src_lng) return
        const isCF = isCloudflare(conn.src_ip)
        const key = (isCF ? subnet16(conn.src_ip) : conn.src_ip) + '|' + (conn.local_port || 0) + '|' + nodeId
        if (!groups[key]) {
          groups[key] = { conns: [], node, isCF }
        }
        groups[key].conns.push(conn)
      })
    })

    const srcDots = {}
    Object.values(groups).forEach(group => {
      const { conns, node, isCF } = group
      const first = conns[0]

      let totalRate = 0
      let totalBytes = 0
      conns.forEach(conn => {
        totalRate += conn.rate || 0
        totalBytes += conn.total_bytes || 0
      })

      // Probe/scan noise: never transferred anything meaningful.
      const isProbe = totalRate === 0 && totalBytes < 5000
      const color = isProbe ? PROBE_COLOR : protocolColor(first.protocol)
      const weight = isProbe ? 2 : rateWeight(totalRate)

      const srcLat = first.src_lat
      const srcLng = first.src_lng

      let targetLng = node.longitude
      const diff = srcLng - targetLng
      if (diff > 180) targetLng += 360
      if (diff < -180) targetLng -= 360
      const points = [[srcLat, srcLng], [node.latitude, targetLng]]

      // Real traffic gets the flowing-dash animation (cheap SVG
      // stroke-dashoffset, a handful of paths); probe noise stays
      // static so motion means "actual traffic".
      layerGroup.addLayer(L.polyline(points, {
        color,
        weight,
        opacity: isProbe ? 0.6 : 0.7,
        dashArray: '6, 10',
        className: isProbe ? '' : 'conn-flow',
        interactive: false,
      }))

      const hitbox = L.polyline(points, {
        weight: Math.max(15, weight + 10),
        opacity: 0,
      })
      hitbox.bindTooltip(buildTooltip(group, node, totalRate, totalBytes, isProbe), { sticky: true })
      layerGroup.addLayer(hitbox)

      // Permanent mid-line label for heavy flows only (>= 1 MB/s).
      if (totalRate >= 1048576) {
        const midLat = (srcLat + node.latitude) / 2
        const midLng = (srcLng + targetLng) / 2
        layerGroup.addLayer(L.marker([midLat, midLng], {
          icon: L.divIcon({
            className: 'conn-tip-wrap',
            html: `<span class="conn-tip-text">${escapeHtml(first.src_city || '?')} • ${formatRate(totalRate)} • ${formatBytes(totalBytes)}</span>`,
            iconSize: [170, 16],
            iconAnchor: [85, 8],
          }),
          interactive: false,
        }))
      }

      // One source dot per location; red for probes, cyan for traffic.
      const dotKey = (isCF ? subnet16(first.src_ip) : first.src_ip) + '|' + (first.src_city || '')
      if (!srcDots[dotKey]) {
        srcDots[dotKey] = true
        const dotColor = isProbe ? PROBE_COLOR : '#00ccff'
        layerGroup.addLayer(L.circleMarker([srcLat, srcLng], {
          radius: isProbe ? 2.6 : 3,
          color: dotColor,
          fillColor: dotColor,
          fillOpacity: 0.8,
          weight: 0,
          interactive: false,
        }))
      }
    })
  }

  function buildTooltip(group, node, totalRate, totalBytes, isProbe) {
    const { conns, isCF } = group
    const first = conns[0]
    const label = isCF ? `${escapeHtml(subnet16(first.src_ip))}.x.x (Cloudflare)` : escapeHtml(first.src_ip)
    const location = `${escapeHtml(first.src_city || '?')}${first.src_country ? `, ${escapeHtml(first.src_country)}` : ''}`
    const probeTag = isProbe ? ' <span class="conn-probe-tag">[probe/scan]</span>' : ''

    let html = `
      <div>
        <strong>${label}</strong> → <strong>${escapeHtml(node.name)}</strong><br>
        <span>${location}</span><br>
        <span>${escapeHtml(first.protocol || 'unknown')} • ${formatRate(totalRate)} • ${formatBytes(totalBytes)}${probeTag}</span>
    `
    if (isCF && conns.length > 1) {
      html += '<br><span class="conn-tree-dim">Active IPs:</span><br>'
      const sorted = conns.slice().sort((a, b) => (b.rate || 0) - (a.rate || 0))
      sorted.forEach((conn, index) => {
        const branch = index < sorted.length - 1 ? '├ ' : '└ '
        html += `<span class="conn-tree-dim">${branch}</span>${escapeHtml(conn.src_ip)} • ${formatRate(conn.rate || 0)} • ${formatBytes(conn.total_bytes || 0)}<br>`
      })
    }
    html += '</div>'
    return html
  }

  // --- Cloudflare detection ---

  function ipToNum(ip) {
    const parts = ip.split('.')
    return ((parts[0] << 24) | (parts[1] << 16) | (parts[2] << 8) | parts[3]) >>> 0
  }

  function isCloudflare(ip) {
    if (!ip || ip.indexOf('.') < 0) return false
    const num = ipToNum(ip)
    for (const [base, bits] of CF_CIDRS) {
      const mask = (~0 << (32 - bits)) >>> 0
      if ((num & mask) === (base & mask)) return true
    }
    return false
  }

  function subnet16(ip) {
    return ip.split('.').slice(0, 2).join('.')
  }

  // --- Styling helpers ---

  function protocolColor(protocol) {
    const key = protocol || 'unknown'
    if (!portColorMap[key]) {
      portColorMap[key] = COLORS[colorIdx % COLORS.length]
      colorIdx++
    }
    return portColorMap[key]
  }

  function rateWeight(rate) {
    if (rate < 1024) return 2.5
    if (rate < 1024 * 32) return 3.5
    if (rate < 1024 * 512) return 5
    if (rate < 1024 * 1024) return 7
    return 9
  }

  function formatRate(rate) {
    if (!rate || rate < 1) return '0 B/s'
    if (rate < 1024) return `${Math.round(rate)} B/s`
    if (rate < 1024 * 1024) return `${(rate / 1024).toFixed(1)} KB/s`
    return `${(rate / 1024 / 1024).toFixed(1)} MB/s`
  }

  function formatBytes(bytes) {
    if (!bytes || bytes < 1) return '0 B'
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
    return `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB`
  }

  function escapeHtml(value) {
    if (value == null) return ''
    const div = document.createElement('div')
    div.textContent = String(value)
    return div.innerHTML
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
