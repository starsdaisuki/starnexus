const StarMap = (() => {
  let map = null
  let terminator = null
  let hasFit = false
  let mapPanel = null
  let fullscreenHandler = null
  const listeners = new Set()

  function init(containerId = 'map') {
    mapPanel = document.getElementById('panel-map')
    map = L.map(containerId, {
      center: [24, 132],
      zoom: 2,
      minZoom: 2,
      maxZoom: 16,
      zoomControl: true,
      attributionControl: true,
    })

    L.tileLayer('https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png', {
      attribution: '&copy; OpenStreetMap &copy; CARTO',
      subdomains: 'abcd',
      maxZoom: 19,
    }).addTo(map)

    renderTerminator()
    setInterval(renderTerminator, 60000)
    fullscreenHandler = () => {
      if (!map || !mapPanel) return
      const active = isFullscreen()
      mapPanel.classList.toggle('is-fullscreen', active)
      setTimeout(() => map.invalidateSize(), 180)
      listeners.forEach(listener => listener(active))
    }
    document.addEventListener('fullscreenchange', fullscreenHandler)
    return map
  }

  function renderTerminator() {
    if (!map || !L.terminator) return
    if (terminator) {
      map.removeLayer(terminator)
    }
    terminator = L.terminator({
      fillColor: 'rgba(2, 7, 11, 0.42)',
      fillOpacity: 1,
      stroke: false,
    }).addTo(map)
  }

  function fitToNodes(nodes) {
    if (!map || hasFit || !nodes || nodes.length === 0) return
    const bounds = L.latLngBounds(nodes.map(node => [node.latitude, node.longitude]))
    if (bounds.isValid()) {
      map.fitBounds(bounds.pad(0.28), { animate: false })
      hasFit = true
    }
  }

  function getMap() {
    return map
  }

  function focusNode(node) {
    if (!map || !node) return
    const currentZoom = map.getZoom()
    map.flyTo([node.latitude, node.longitude], Math.max(currentZoom, 4), {
      animate: true,
      duration: 0.6,
    })
  }

  function toggleFullscreen() {
    if (!mapPanel || !document.fullscreenEnabled) return false

    if (document.fullscreenElement === mapPanel) {
      document.exitFullscreen()
      return false
    }

    mapPanel.requestFullscreen()
    return true
  }

  function isFullscreen() {
    return document.fullscreenElement === mapPanel
  }

  function onFullscreenChange(listener) {
    if (typeof listener === 'function') {
      listeners.add(listener)
    }
  }

  return { init, fitToNodes, getMap, focusNode, toggleFullscreen, isFullscreen, onFullscreenChange }
})()
