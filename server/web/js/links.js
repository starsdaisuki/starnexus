/**
 * links.js — Link rendering: color by latency, dashed bad links, tooltips
 */

const StarLinks = (() => {
  let polylines = []
  let map = null
  let nodesData = []

  function init(leafletMap) {
    map = leafletMap
  }

  function render(links, nodes) {
    // Clear old polylines
    polylines.forEach(p => map.removeLayer(p))
    polylines = []
    nodesData = nodes

    links.forEach(link => {
      const source = nodes.find(n => n.id === link.source_node_id)
      const target = nodes.find(n => n.id === link.target_node_id)
      if (!source || !target) return

      const color = getColorByLatency(link.latency_ms)
      const isBad = link.status === 'bad'

      // Fix antimeridian wrapping: if longitude difference > 180,
      // offset target longitude so the line takes the short path
      let tgtLng = target.longitude
      const lngDiff = source.longitude - tgtLng
      if (lngDiff > 180) tgtLng += 360
      else if (lngDiff < -180) tgtLng -= 360

      const polyline = L.polyline(
        [[source.latitude, source.longitude], [target.latitude, tgtLng]],
        {
          color: color,
          weight: isBad ? 1 : 2,
          opacity: isBad ? 0.5 : 0.7,
          dashArray: isBad ? '8, 8' : null,
        }
      ).addTo(map)

      // tooltip
      const tooltipContent = `
        <div class="link-tooltip">
          <span class="label">Latency:</span> <span class="value">${link.latency_ms.toFixed(1)}ms</span><br>
          <span class="label">Pkt Loss:</span> <span class="value">${link.packet_loss.toFixed(1)}%</span>
        </div>
      `
      polyline.bindTooltip(tooltipContent, { sticky: true })

      polylines.push(polyline)
    })
  }

  function getColorByLatency(ms) {
    if (ms < 50) return '#00ff88'
    if (ms <= 150) return '#ffaa00'
    return '#ff4444'
  }

  return { init, render }
})()
