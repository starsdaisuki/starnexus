/**
 * map.js — Leaflet init, dark basemap, NASA night overlay, terminator line
 */

const StarMap = (() => {
  let map = null
  let terminator = null

  function init() {
    map = L.map('map', {
      center: [20, 140],
      zoom: 2,
      minZoom: 2,
      maxZoom: 16,
      zoomControl: true,
      attributionControl: true,
    })

    // CartoDB Dark Matter basemap
    L.tileLayer('https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png', {
      attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OSM</a> &copy; <a href="https://carto.com/">CARTO</a>',
      subdomains: 'abcd',
      maxZoom: 19,
    }).addTo(map)

    // Day/night terminator line
    try {
      terminator = L.terminator({
        fillColor: 'rgba(0, 0, 40, 0.5)',
        fillOpacity: 1,
        stroke: false,
      }).addTo(map)
    } catch (e) {
      console.warn('Leaflet.Terminator failed to load:', e)
    }

    // Update terminator every 60s
    setInterval(updateTerminator, 60000)

    return map
  }

  function updateTerminator() {
    if (!terminator || !map) return
    try {
      const newTerminator = L.terminator({
        fillColor: 'rgba(0, 0, 40, 0.5)',
        fillOpacity: 1,
        stroke: false,
      })
      map.removeLayer(terminator)
      terminator = newTerminator.addTo(map)
    } catch (e) {
      console.warn('Terminator update failed:', e)
    }
  }

  function getMap() {
    return map
  }

  return { init, getMap, updateTerminator }
})()
