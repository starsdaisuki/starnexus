const StarMap = (() => {
  let map = null
  let terminator = null
  let hasFit = false
  let mapPanel = null
  let fullscreenHandler = null
  let tileLayer = null
  let basemapKey = 'dark'
  const listeners = new Set()

  const BASEMAP_STORAGE_KEY = 'starnexus.basemap'

  // No API key required for any of these providers. The AMap (高德)
  // raster endpoints are the same tiles its own JS SDK loads and are
  // reachable from mainland China without a proxy, which the CARTO /
  // OSM CDNs are not always. AMap tiles use GCJ-02 coordinates, so
  // markers inside mainland China appear offset by a few hundred
  // meters; for VPS nodes (typically overseas) this is negligible.
  const BASEMAPS = {
    dark: {
      label: 'Dark · CARTO',
      url: 'https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png',
      options: { attribution: '&copy; OpenStreetMap &copy; CARTO', subdomains: 'abcd', maxZoom: 19 },
      theme: 'dark',
    },
    light: {
      label: 'Light · CARTO',
      url: 'https://{s}.basemaps.cartocdn.com/light_all/{z}/{x}/{y}{r}.png',
      options: { attribution: '&copy; OpenStreetMap &copy; CARTO', subdomains: 'abcd', maxZoom: 19 },
      theme: 'light',
    },
    osm: {
      label: 'OpenStreetMap',
      url: 'https://tile.openstreetmap.org/{z}/{x}/{y}.png',
      options: { attribution: '&copy; OpenStreetMap contributors', maxZoom: 19 },
      theme: 'light',
    },
    amap: {
      label: '高德 · Streets',
      url: 'https://webrd0{s}.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=8&x={x}&y={y}&z={z}',
      options: { attribution: '&copy; 高德地图 AutoNavi', subdomains: '1234', maxZoom: 18 },
      theme: 'light',
    },
    'amap-dark': {
      // AMap has no official dark raster tiles; a CSS invert filter on
      // the tile container fakes one well enough for a dashboard.
      label: '高德 · Dark',
      url: 'https://webrd0{s}.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=8&x={x}&y={y}&z={z}',
      options: { attribution: '&copy; 高德地图 AutoNavi', subdomains: '1234', maxZoom: 18, className: 'tiles-inverted' },
      theme: 'dark',
    },
    'amap-satellite': {
      label: '高德 · Satellite',
      url: 'https://webst0{s}.is.autonavi.com/appmaptile?style=6&x={x}&y={y}&z={z}',
      options: { attribution: '&copy; 高德地图 AutoNavi', subdomains: '1234', maxZoom: 18 },
      theme: 'dark',
    },
  }

  function loadSavedBasemap() {
    try {
      const saved = localStorage.getItem(BASEMAP_STORAGE_KEY)
      return BASEMAPS[saved] ? saved : 'dark'
    } catch (error) {
      return 'dark'
    }
  }

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

    setBasemap(loadSavedBasemap())

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

  function setBasemap(key) {
    if (!map || !BASEMAPS[key]) return
    const basemap = BASEMAPS[key]
    if (tileLayer) {
      map.removeLayer(tileLayer)
    }
    tileLayer = L.tileLayer(basemap.url, basemap.options).addTo(map)
    tileLayer.bringToBack()
    basemapKey = key
    map.getContainer().classList.toggle('basemap-light', basemap.theme === 'light')
    try {
      localStorage.setItem(BASEMAP_STORAGE_KEY, key)
    } catch (error) {
      // Private browsing or storage quota — the choice just won't persist.
    }
  }

  function getBasemap() {
    return basemapKey
  }

  function listBasemaps() {
    return Object.entries(BASEMAPS).map(([key, basemap]) => ({ key, label: basemap.label }))
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

  return { init, fitToNodes, getMap, focusNode, toggleFullscreen, isFullscreen, onFullscreenChange, setBasemap, getBasemap, listBasemaps }
})()
