/**
 * connections.js — Live connection visualization with CDN aggregation
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
  let colorIdx = 0

  // Cloudflare IPv4 CIDR ranges (from cloudflare.com/ips-v4)
  const CF_CIDRS = [
    [ipToNum('173.245.48.0'), 20],
    [ipToNum('103.21.244.0'), 22],
    [ipToNum('103.22.200.0'), 22],
    [ipToNum('103.31.4.0'), 22],
    [ipToNum('141.101.64.0'), 18],
    [ipToNum('108.162.192.0'), 18],
    [ipToNum('190.93.240.0'), 20],
    [ipToNum('188.114.96.0'), 20],
    [ipToNum('197.234.240.0'), 22],
    [ipToNum('198.41.128.0'), 17],
    [ipToNum('162.158.0.0'), 15],
    [ipToNum('104.16.0.0'), 13],
    [ipToNum('104.24.0.0'), 14],
    [ipToNum('172.64.0.0'), 13],
    [ipToNum('131.0.72.0'), 22],
  ]

  function init(leafletMap) {
    map = leafletMap
    layerGroup = L.layerGroup().addTo(map)
  }

  function setNodes(nodes) { nodesCache = nodes }

  function render(connData) {
    layerGroup.clearLayers()
    if (!visible || !connData) return

    // Flatten and group
    const groups = {} // key → { conns: [], nodeId, node }

    Object.keys(connData).forEach(nodeId => {
      const node = nodesCache.find(n => n.id === nodeId)
      if (!node) return

      const conns = connData[nodeId] || []
      conns.forEach(conn => {
        if (!conn.src_lat && !conn.src_lng) return

        // Group key: CDN → "/16-nodeId", direct → "ip-nodeId"
        const isCF = isCloudflare(conn.src_ip)
        const gKey = isCF
          ? subnet16(conn.src_ip) + '-' + nodeId
          : conn.src_ip + '-' + nodeId

        if (!groups[gKey]) {
          groups[gKey] = { conns: [], nodeId, node, isCF }
        }
        groups[gKey].conns.push(conn)
      })
    })

    // Render each group as one line
    const srcDots = {}

    Object.values(groups).forEach(grp => {
      const { conns, node, isCF } = grp
      const first = conns[0]

      // Aggregate stats
      let totalRate = 0, totalBytes = 0
      conns.forEach(c => {
        totalRate += (c.rate || 0)
        totalBytes += (c.total_bytes || 0)
      })

      // Classify: probe/scan vs real traffic
      const isProbe = totalRate === 0 && totalBytes < 5000
      const color = isProbe ? '#ff4466' : getColor(first.protocol)
      const w = isProbe ? 1 : calcWeight(totalRate)

      // Use first connection's geo (CDN IPs in same /16 geolocate the same)
      const srcLat = first.src_lat
      const srcLng = first.src_lng
      const city = first.src_city || '?'
      const country = first.src_country || '?'

      let tgtLng = node.longitude
      const d = srcLng - tgtLng
      if (d > 180) tgtLng += 360
      else if (d < -180) tgtLng -= 360

      const pts = [[srcLat, srcLng], [node.latitude, tgtLng]]

      // Visible line
      const line = L.polyline(pts, {
        color, weight: w, opacity: 0.6,
        dashArray: '4, 8', interactive: false,
      })
      layerGroup.addLayer(line)
      setTimeout(() => {
        const el = line.getElement()
        if (el) el.classList.add('conn-flow')
      }, 0)

      // Build display strings
      const subnetStr = isCF ? subnet16(first.src_ip) + '.x.x' : first.src_ip
      const rateStr = fmtRate(totalRate)
      const totalStr = fmtBytes(totalBytes)

      // Hover tooltip
      const proto = first.protocol || '?'
      const typeLabel = isProbe ? ' <span style="color:#ff4466">[probe/scan]</span>' : ''
      let tipHtml = '<div class="link-tooltip">'
      tipHtml += '<b>' + subnetStr + '</b> | ' + city + ', ' + country + '<br>'
      tipHtml += proto + ' | <b>' + rateStr + '</b> | ' + totalStr + typeLabel + '<br>'

      if (isCF && conns.length > 1) {
        tipHtml += '<br><span style="color:rgba(255,255,255,0.4)">Active IPs:</span><br>'
        const sorted = conns.slice().sort((a, b) => (b.rate || 0) - (a.rate || 0))
        sorted.forEach((c, i) => {
          const prefix = i < sorted.length - 1 ? '\u251c ' : '\u2514 '
          tipHtml += '<span style="color:rgba(255,255,255,0.5)">' + prefix + '</span>'
          tipHtml += c.src_ip + ' | ' + (c.protocol || '?') + ' | ' + fmtRate(c.rate || 0) + ' | ' + fmtBytes(c.total_bytes || 0) + '<br>'
        })
      }
      tipHtml += '</div>'

      // Wide hitbox for hover
      const hitbox = L.polyline(pts, {
        weight: Math.max(15, w + 10), opacity: 0, interactive: true,
      })
      hitbox.bindTooltip(tipHtml, { sticky: true })
      layerGroup.addLayer(hitbox)

      // Permanent label only for >= 1 MB/s
      if (totalRate >= 1048576) {
        const midLat = (srcLat + node.latitude) / 2
        const midLng = (srcLng + tgtLng) / 2
        layerGroup.addLayer(L.marker([midLat, midLng], {
          icon: L.divIcon({
            className: 'conn-tip-wrap',
            html: '<span class="conn-tip-text">' + city + ' | ' + rateStr + ' | ' + totalStr + '</span>',
            iconSize: [150, 16], iconAnchor: [75, 8],
          }),
          interactive: false,
        }))
      }

      // Source dot (one per group, red for probes, cyan for real traffic)
      const dotKey = isCF ? subnet16(first.src_ip) + '-' + city : first.src_ip
      if (!srcDots[dotKey]) {
        const dotColor = isProbe ? '#ff4466' : '#00ccff'
        layerGroup.addLayer(L.circleMarker([srcLat, srcLng], {
          radius: 3, color: dotColor, fillColor: dotColor,
          fillOpacity: 0.8, weight: 0, interactive: false,
        }))
        srcDots[dotKey] = true
      }
    })
  }

  // --- Cloudflare detection ---

  function ipToNum(ip) {
    const p = ip.split('.')
    return ((p[0] << 24) | (p[1] << 16) | (p[2] << 8) | p[3]) >>> 0
  }

  function isCloudflare(ip) {
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

  // --- Formatting ---

  function fmtRate(bps) {
    if (!bps || bps < 1) return '0 B/s'
    if (bps < 1024) return Math.round(bps) + ' B/s'
    if (bps < 1048576) return (bps / 1024).toFixed(1) + ' KB/s'
    return (bps / 1048576).toFixed(1) + ' MB/s'
  }

  function fmtBytes(b) {
    if (!b || b < 1) return '0 B'
    if (b < 1024) return b + ' B'
    if (b < 1048576) return (b / 1024).toFixed(1) + ' KB'
    if (b < 1073741824) return (b / 1048576).toFixed(1) + ' MB'
    return (b / 1073741824).toFixed(1) + ' GB'
  }

  function calcWeight(bps) {
    if (bps <= 0) return 1
    if (bps < 1024) return 1
    if (bps < 10240) return 2 + bps / 10240
    if (bps < 102400) return 4 + 2 * bps / 102400
    if (bps < 1048576) return 7 + 3 * bps / 1048576
    return 12
  }

  function getColor(protocol) {
    if (!portColorMap[protocol]) {
      portColorMap[protocol] = COLORS[colorIdx % COLORS.length]
      colorIdx++
    }
    return portColorMap[protocol]
  }

  function toggle() {
    visible = !visible
    if (!visible) layerGroup.clearLayers()
    return visible
  }

  function isVisible() { return visible }

  return { init, setNodes, render, toggle, isVisible }
})()
