/**
 * links.js — Link rendering: latency labels, flowing animation, color by latency, tooltips
 */

const StarLinks = (() => {
  let layers = [] // each entry: { polyline, decorator, label }
  let map = null

  function init(leafletMap) {
    map = leafletMap
  }

  function render(links, nodes) {
    // Clear old layers
    layers.forEach(l => {
      map.removeLayer(l.polyline)
      if (l.label) map.removeLayer(l.label)
    })
    layers = []

    links.forEach(link => {
      const source = nodes.find(n => n.id === link.source_node_id)
      const target = nodes.find(n => n.id === link.target_node_id)
      if (!source || !target) return

      const color = getColorByLatency(link.latency_ms)
      const isBad = link.status === 'bad'

      // Fix antimeridian wrapping
      let tgtLng = target.longitude
      const lngDiff = source.longitude - tgtLng
      if (lngDiff > 180) tgtLng += 360
      else if (lngDiff < -180) tgtLng -= 360

      const latlngs = [[source.latitude, source.longitude], [target.latitude, tgtLng]]

      // Main line
      const polyline = L.polyline(latlngs, {
        color: color,
        weight: isBad ? 1 : 2.5,
        opacity: isBad ? 0.4 : 0.8,
        dashArray: isBad ? '6, 10' : '8, 12',
        className: isBad ? '' : 'link-flow',
      }).addTo(map)

      // Tooltip on hover
      const tooltipContent = `
        <div class="link-tooltip">
          <span class="label">Latency:</span> <span class="value">${formatLatency(link.latency_ms)}</span><br>
          <span class="label">Pkt Loss:</span> <span class="value">${link.packet_loss.toFixed(1)}%</span>
        </div>
      `
      polyline.bindTooltip(tooltipContent, { sticky: true })

      // Latency label at midpoint
      const midLat = (source.latitude + target.latitude) / 2
      const midLng = (source.longitude + tgtLng) / 2
      const labelText = formatLatency(link.latency_ms)

      const label = L.marker([midLat, midLng], {
        icon: L.divIcon({
          className: 'link-label',
          html: `<span class="link-label-text" style="color:${color}">${labelText}</span>`,
          iconSize: [60, 16],
          iconAnchor: [30, 8],
        }),
        interactive: false,
      }).addTo(map)

      layers.push({ polyline, label })
    })
  }

  function formatLatency(ms) {
    if (ms < 0) return 'N/A'
    if (ms < 1) return '<1ms'
    return Math.round(ms) + 'ms'
  }

  function getColorByLatency(ms) {
    if (ms < 0) return '#ff4444'
    if (ms < 50) return '#00ff88'
    if (ms <= 150) return '#ffaa00'
    return '#ff4444'
  }

  return { init, render }
})()
