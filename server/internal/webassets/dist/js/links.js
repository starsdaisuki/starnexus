const StarLinks = (() => {
  let map = null
  let layers = []

  function init(leafletMap) {
    map = leafletMap
  }

  function render(links, nodes) {
    layers.forEach(layer => map.removeLayer(layer))
    layers = []

    links.forEach(link => {
      const source = nodes.find(node => node.id === link.source_node_id)
      const target = nodes.find(node => node.id === link.target_node_id)
      if (!source || !target) return

      let targetLng = target.longitude
      const diff = source.longitude - targetLng
      if (diff > 180) targetLng += 360
      if (diff < -180) targetLng -= 360

      const line = L.polyline(
        [[source.latitude, source.longitude], [target.latitude, targetLng]],
        {
          color: colorByLink(link),
          weight: link.status === 'bad' ? 1 : 2.2,
          opacity: link.status === 'bad' ? 0.35 : 0.72,
          dashArray: link.status === 'bad' ? '4, 10' : '8, 10',
        }
      ).addTo(map)

      line.bindTooltip(buildTooltip(link, source, target), { sticky: true })
      layers.push(line)
    })
  }

  function buildTooltip(link, source, target) {
    return `
      <div>
        <strong>${source.name}</strong> → <strong>${target.name}</strong><br>
        <span>Latency ${formatLatency(link.latency_ms)} • Loss ${formatLoss(link.packet_loss)}</span>
      </div>
    `
  }

  function colorByLink(link) {
    if (link.status === 'bad' || link.latency_ms < 0 || link.packet_loss >= 100) return '#ff6d64'
    if (link.status === 'degraded' || link.latency_ms > 120 || link.packet_loss > 2) return '#ffba4a'
    return '#69e3ff'
  }

  function formatLatency(value) {
    if (value == null || value < 0) return 'N/A'
    if (value < 1) return '<1ms'
    return `${Math.round(value)}ms`
  }

  function formatLoss(value) {
    if (value == null) return '--'
    return `${value.toFixed(1)}%`
  }

  return { init, render }
})()
