const StarNodes = (() => {
  let map = null
  let markers = new Map()
  let onSelect = null

  function init(leafletMap, handleSelect) {
    map = leafletMap
    onSelect = handleSelect
  }

  function render(nodes, selectedNodeId) {
    const nextIds = new Set(nodes.map(node => node.id))

    for (const [nodeId, marker] of markers.entries()) {
      if (!nextIds.has(nodeId)) {
        map.removeLayer(marker)
        markers.delete(nodeId)
      }
    }

    nodes.forEach(node => {
      const existing = markers.get(node.id)
      if (existing) {
        existing.setLatLng([node.latitude, node.longitude])
        existing.setIcon(buildIcon(node, selectedNodeId))
        existing.setTooltipContent(buildTooltip(node))
        return
      }

      const marker = L.marker([node.latitude, node.longitude], {
        icon: buildIcon(node, selectedNodeId),
      }).addTo(map)

      marker.bindTooltip(buildTooltip(node), {
        direction: 'top',
        offset: [0, -12],
      })

      marker.on('click', () => {
        if (onSelect) onSelect(node.id)
      })

      markers.set(node.id, marker)
    })
  }

  function buildIcon(node, selectedNodeId) {
    const selectedClass = node.id === selectedNodeId ? ' selected' : ''
    const locationClass = isEstimated(node) ? ' estimated' : ''
    const size = node.id === selectedNodeId ? 18 : 14
    return L.divIcon({
      className: '',
      html: `<div class="node-marker ${node.status || 'unknown'}${selectedClass}${locationClass}" style="width:${size}px;height:${size}px;"></div>`,
      iconSize: [size, size],
      iconAnchor: [size / 2, size / 2],
    })
  }

  function buildTooltip(node) {
    const metrics = node.metrics || {}
    return `
      <div>
        <strong>${escapeHtml(node.name)}</strong><br>
        <span>${escapeHtml(node.provider || 'Unknown')}</span><br>
        <span>${node.status || 'unknown'} • CPU ${formatPercent(metrics.cpu_percent)} • Mem ${formatPercent(metrics.memory_percent)}</span><br>
        <span>${escapeHtml(describeLocationSource(node.location_source))}</span>
      </div>
    `
  }

  function isEstimated(node) {
    return node.location_source === 'geoip' || node.location_source === 'unknown' || !node.location_source
  }

  function describeLocationSource(source) {
    switch (source) {
      case 'manual':
        return 'Manual map position'
      case 'manual_override':
        return 'Server-enforced map position'
      case 'geoip':
        return 'GeoIP-estimated map position'
      default:
        return 'Location precision unknown'
    }
  }

  function escapeHtml(value) {
    if (!value) return ''
    const div = document.createElement('div')
    div.textContent = value
    return div.innerHTML
  }

  function formatPercent(value) {
    if (value == null) return '--'
    return `${value.toFixed(1)}%`
  }

  return { init, render }
})()
