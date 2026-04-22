const StarApp = (() => {
  const API_BASE = '/api'
  const DASHBOARD_INTERVAL = 30000
  const CONNECTION_INTERVAL = 5000
  const DETAIL_INTERVAL = 30000
  const UPDATE_TICK = 1000

  const state = {
    dashboard: null,
    health: null,
    detail: null,
    selectedNodeId: null,
    dashboardTimer: null,
    healthTimer: null,
    detailTimer: null,
    connectionTimer: null,
    tickTimer: null,
    lastUpdateTime: null,
    hasError: false,
    searchText: '',
    detailHours: 24,
  }

  async function init() {
    const map = StarMap.init('map')
    StarNodes.init(map, selectNode)
    StarLinks.init(map)
    StarConns.init(map)

    bindUI()

    await fetchDashboard()
    await fetchHealth()
    await fetchConnections()

    state.dashboardTimer = setInterval(fetchDashboard, DASHBOARD_INTERVAL)
    state.healthTimer = setInterval(fetchHealth, DASHBOARD_INTERVAL)
    state.detailTimer = setInterval(fetchNodeDetails, DETAIL_INTERVAL)
    state.connectionTimer = setInterval(fetchConnections, CONNECTION_INTERVAL)
    state.tickTimer = setInterval(updateLastUpdateDisplay, UPDATE_TICK)
  }

  function bindUI() {
    StarMap.onFullscreenChange(updateMapFullscreenButton)

    document.getElementById('btn-refresh').addEventListener('click', async () => {
      await fetchDashboard()
      await fetchHealth()
      await fetchConnections()
    })

    document.getElementById('btn-toggle-conns').addEventListener('click', async event => {
      const visible = StarConns.toggle()
      event.currentTarget.classList.toggle('active', visible)
      if (visible) {
        await fetchConnections()
      }
    })

    document.getElementById('node-search').addEventListener('input', event => {
      state.searchText = event.target.value.trim().toLowerCase()
      renderDashboard()
    })

    document.getElementById('detail-hours').addEventListener('change', async event => {
      state.detailHours = Number(event.target.value) || 24
      await fetchNodeDetails()
    })

    document.getElementById('btn-map-fullscreen').addEventListener('click', () => {
      StarMap.toggleFullscreen()
      updateMapFullscreenButton()
    })

    updateMapFullscreenButton()
  }

  async function fetchDashboard() {
    try {
      const response = await fetch(`${API_BASE}/dashboard`)
      if (!response.ok) {
        throw new Error(`dashboard request failed with ${response.status}`)
      }

      state.dashboard = await response.json()
      state.lastUpdateTime = Date.now()
      clearError()

      if (!state.selectedNodeId) {
        state.selectedNodeId = chooseDefaultNode(state.dashboard.nodes || [], state.dashboard.scores || [])
      }

      renderDashboard()
      if (state.selectedNodeId) {
        await fetchNodeDetails()
      }
    } catch (error) {
      console.error('Dashboard fetch failed', error)
      showError()
    }
  }

  async function fetchHealth() {
    try {
      const response = await fetch(`${API_BASE}/health`)
      if (!response.ok && response.status !== 503) {
        throw new Error(`health request failed with ${response.status}`)
      }
      state.health = await response.json()
      renderControlPlane(state.health)
    } catch (error) {
      console.error('Health fetch failed', error)
      renderControlPlane(null)
    }
  }

  async function fetchNodeDetails() {
    if (!state.selectedNodeId) return

    try {
      const response = await fetch(`${API_BASE}/nodes/${state.selectedNodeId}/details?hours=${state.detailHours}`)
      if (!response.ok) {
        throw new Error(`node detail request failed with ${response.status}`)
      }
      state.detail = await response.json()
      renderNodeDetails()
    } catch (error) {
      console.error('Node detail fetch failed', error)
    }
  }

  async function fetchConnections() {
    if (!StarConns.isVisible()) return

    try {
      const response = await fetch(`${API_BASE}/connections`)
      if (!response.ok) return
      StarConns.render(await response.json())
    } catch (error) {
      console.error('Live connection fetch failed', error)
    }
  }

  async function selectNode(nodeId) {
    state.selectedNodeId = nodeId
    const selectedNode = (state.dashboard?.nodes || []).find(node => node.id === nodeId)
    if (selectedNode) {
      StarMap.focusNode(selectedNode)
    }
    renderDashboard()
    await fetchNodeDetails()
  }

  function renderDashboard() {
    if (!state.dashboard) return

    const nodes = state.dashboard.nodes || []
    const scores = scoreMap(state.dashboard.scores || [])
    const filteredNodes = sortNodes(nodes, scores).filter(matchesSearch)

    renderSummary(nodes, state.dashboard.status || {}, state.dashboard.links || [], state.dashboard.hot_sources || [], scores)
    renderIncidents(state.dashboard.incidents || [])
    renderControlPlane(state.health)
    renderEvents(state.dashboard.events || [])
    renderFleetRadar(state.dashboard.fleet_analytics || {})
    renderGroundTruth(state.dashboard.ground_truth || null)
    renderReliability(state.dashboard.reliability_analytics || null)
    renderNodeTable(filteredNodes, scores)
    renderLinksList(state.dashboard.links || [], nodes)
    renderSources(state.dashboard.hot_sources || [])

    StarMap.fitToNodes(nodes)
    StarNodes.render(filteredNodes, state.selectedNodeId)
    StarLinks.render(state.dashboard.links || [], nodes)
    StarConns.setNodes(nodes)

    const selectedNode = nodes.find(node => node.id === state.selectedNodeId)
    document.getElementById('status-headline').textContent = buildHeadline(state.dashboard.status || {}, selectedNode)
    document.getElementById('system-health-badge').className = `health-badge ${healthClass(state.dashboard.status || {})}`
    document.getElementById('system-health-badge').textContent = healthLabel(state.dashboard.status || {})
  }

  function renderSummary(nodes, status, links, hotSources, scores) {
    document.getElementById('summary-total').textContent = `${status.total || nodes.length || 0} nodes`
    document.getElementById('summary-online').textContent = `${status.online || 0} online`
    document.getElementById('summary-degraded').textContent = `${status.degraded || 0} degraded`
    document.getElementById('summary-offline').textContent = `${status.offline || 0} offline`

    const bestNode = [...nodes]
      .map(node => ({ node, score: scores[node.id]?.composite_score ?? -1 }))
      .sort((a, b) => b.score - a.score)[0]
    document.getElementById('summary-best-node').textContent = bestNode?.node?.name || '--'
    document.getElementById('summary-best-node-detail').textContent = bestNode ? `${bestNode.score.toFixed(0)}/100 composite` : 'Waiting for scores'

    const weakestLink = [...links].sort(compareLinks)[0]
    document.getElementById('summary-worst-link').textContent = weakestLink ? `${weakestLink.source_node_id} → ${weakestLink.target_node_id}` : '--'
    document.getElementById('summary-worst-link-detail').textContent = weakestLink ? `${formatLatency(weakestLink.latency_ms)} • ${formatPercent(weakestLink.packet_loss)}` : 'Waiting for probes'

    const hotSource = hotSources[0]
    document.getElementById('summary-hot-source').textContent = hotSource ? hotSource.source_ip : '--'
    document.getElementById('summary-hot-source-detail').textContent = hotSource ? `${hotSource.node_name || hotSource.node_id || '--'} • ${formatRate(hotSource.peak_rate_bps)}` : 'Waiting for connection samples'
  }

  function renderEvents(events) {
    const root = document.getElementById('events-feed')
    root.innerHTML = ''

    if (!events.length) {
      root.innerHTML = emptyListItem('No recent events recorded.')
      return
    }

    events.forEach(event => {
      root.insertAdjacentHTML('beforeend', `
        <article class="event-item ${event.severity || 'info'}">
          <div class="event-topline">
            <span>${escapeHtml(event.node_name || event.node_id || 'system')} • ${escapeHtml(event.type || 'event')}</span>
            <span>${relativeTime(event.created_at)}</span>
          </div>
          <div class="event-title">${escapeHtml(event.title || 'Untitled event')}</div>
          <div class="event-body">${escapeHtml(event.body || 'No event details available.')}</div>
        </article>
      `)
    })
  }

  function renderIncidents(incidents) {
    const root = document.getElementById('incidents-list')
    root.innerHTML = ''

    if (!incidents.length) {
      root.innerHTML = emptyListItem('No active incidents. Recovered issues remain available through node detail and /api/incidents?status=recent.')
      return
    }

    incidents.forEach(incident => {
      root.insertAdjacentHTML('beforeend', incidentMarkup(incident))
      const article = root.lastElementChild
      if (article && incident.node_id) {
        article.addEventListener('click', () => selectNode(incident.node_id))
      }
    })
  }

  function renderControlPlane(health) {
    const badge = document.getElementById('health-status-badge')
    const componentsRoot = document.getElementById('health-components')
    if (!badge || !componentsRoot) return

    componentsRoot.innerHTML = ''
    if (!health) {
      badge.className = 'health-badge offline'
      badge.textContent = 'Unknown'
      document.getElementById('health-version').textContent = '--'
      document.getElementById('health-build').textContent = 'health endpoint unavailable'
      document.getElementById('health-db').textContent = '--'
      document.getElementById('health-db-detail').textContent = '--'
      componentsRoot.innerHTML = emptyListItem('Control-plane health is unavailable.')
      return
    }

    const status = health.status || 'unknown'
    badge.className = `health-badge ${status === 'ok' ? 'online' : status === 'degraded' ? 'degraded' : 'offline'}`
    badge.textContent = status
    document.getElementById('health-version').textContent = `${health.version?.commit || 'unknown'}`
    document.getElementById('health-build').textContent = `${health.version?.component || 'server'} • ${health.version?.build_time || 'build time unknown'} • uptime ${formatDuration(health.uptime_seconds)}`
    document.getElementById('health-db').textContent = health.database?.quick_check || '--'
    document.getElementById('health-db-detail').textContent = `migration ${health.database?.latest_migration ?? '--'} • ${health.database?.node_count ?? 0} nodes • ${health.database?.incident_count ?? 0} incidents`

    ;(health.components || []).forEach(component => {
      componentsRoot.insertAdjacentHTML('beforeend', stackItem(
        `${escapeHtml(component.name)} • ${component.ok ? 'ok' : 'attention'}`,
        `${escapeHtml(component.status || 'unknown')}${component.detail ? ` • ${escapeHtml(component.detail)}` : ''}`,
        `${escapeHtml(component.path || 'not configured')}`
      ))
    })
    if (!(health.components || []).length) {
      componentsRoot.innerHTML = emptyListItem('No component checks returned.')
    }
  }

  function renderFleetRadar(fleetAnalytics) {
    const root = document.getElementById('radar-list')
    const summary = document.getElementById('radar-summary')
    root.innerHTML = ''

    if (!fleetAnalytics || !(fleetAnalytics.node_insights || []).length) {
      summary.textContent = 'Not enough historical samples to rank node risk yet.'
      root.innerHTML = emptyListItem('No fleet radar entries available.')
      return
    }

    summary.textContent = fleetAnalytics.summary || '24h fleet radar is available.'

    fleetAnalytics.node_insights.slice(0, 6).forEach(insight => {
      const article = document.createElement('article')
      article.className = 'stack-item'
      article.innerHTML = `
        <div class="stack-topline">
          <span>${escapeHtml(insight.node_name || insight.node_id || 'Unknown node')}</span>
          <span class="radar-risk ${escapeHtml(insight.risk_level || 'unknown')}">${escapeHtml(insight.risk_level || 'unknown')}</span>
        </div>
        <div class="stack-body">${escapeHtml(insight.summary || 'No summary available.')}</div>
        <div class="stack-topline">
          <span>${Number(insight.coverage_percent || 0).toFixed(0)}% coverage • ${insight.signal_count || 0} signal(s)</span>
          <span>${insight.composite_score != null ? `${Number(insight.composite_score).toFixed(0)}/100 score` : 'score unavailable'}</span>
        </div>
      `
      article.addEventListener('click', () => selectNode(insight.node_id))
      root.appendChild(article)
    })
  }

  function renderGroundTruth(groundTruth) {
    const root = document.getElementById('experiment-list')
    root.innerHTML = ''

    if (!groundTruth || !groundTruth.experiment_count) {
      document.getElementById('experiment-detection-rate').textContent = '--'
      document.getElementById('experiment-detection-delay').textContent = 'delay --'
      document.getElementById('experiment-recovery-rate').textContent = '--'
      document.getElementById('experiment-recovery-delay').textContent = 'delay --'
      document.getElementById('experiment-false-positive').textContent = '--'
      root.innerHTML = emptyListItem('No labelled experiments in the active dashboard window.')
      return
    }

    document.getElementById('experiment-detection-rate').textContent = `${Number(groundTruth.detection_rate_percent || 0).toFixed(0)}%`
    document.getElementById('experiment-detection-delay').textContent = `delay ${formatDuration(groundTruth.mean_detection_delay_seconds)} • status/anomaly ${groundTruth.status_detection_count || 0}/${groundTruth.anomaly_detection_count || 0}`
    document.getElementById('experiment-recovery-rate').textContent = `${Number(groundTruth.recovery_rate_percent || 0).toFixed(0)}%`
    document.getElementById('experiment-recovery-delay').textContent = `delay ${formatDuration(groundTruth.mean_recovery_delay_seconds)}`
    document.getElementById('experiment-false-positive').textContent = `${groundTruth.false_positive_event_count || 0}`
    document.getElementById('experiment-false-positive').nextElementSibling.textContent = `${Number(groundTruth.false_positive_events_per_node_hour || 0).toFixed(2)}/node-hour • status/anomaly ${groundTruth.false_positive_status_count || 0}/${groundTruth.false_positive_anomaly_count || 0}`

    ;(groundTruth.experiments || []).slice(0, 6).forEach(experiment => {
      const article = document.createElement('article')
      article.className = 'stack-item'
      article.innerHTML = `
        <div class="stack-topline">
          <span>${escapeHtml(experiment.node_id)} • ${escapeHtml(experiment.injection_type || 'experiment')}</span>
          <span class="radar-risk ${experiment.detected ? 'stable' : 'critical'}">${experiment.detected ? 'detected' : 'missed'}</span>
        </div>
        <div class="stack-title">${escapeHtml(experiment.experiment_id || 'experiment')}</div>
        <div class="stack-body">
          ${escapeHtml(experiment.expected_metric || 'metric')} peak ${formatMetricPeak(experiment)} • detection ${formatDuration(experiment.detection_delay_seconds)} via ${escapeHtml(experiment.detection_type || 'unknown')} • recovery ${experiment.recovered ? formatDuration(experiment.recovery_delay_seconds) : 'missed'}
        </div>
        <div class="stack-topline">
          <span>${absoluteTime(experiment.started_at)} → ${absoluteTime(experiment.ended_at)}</span>
          <span>${experiment.recovered ? 'recovered' : 'not recovered'}</span>
        </div>
      `
      article.addEventListener('click', () => selectNode(experiment.node_id))
      root.appendChild(article)
    })
  }

  function renderReliability(reliability) {
    const root = document.getElementById('reliability-list')
    const summary = document.getElementById('reliability-summary')
    root.innerHTML = ''

    if (!reliability || !(reliability.nodes || []).length) {
      document.getElementById('reliability-score').textContent = '--'
      document.getElementById('reliability-coverage').textContent = '--'
      document.getElementById('reliability-incidents').textContent = '--'
      document.getElementById('reliability-signals').textContent = '--'
      summary.textContent = 'No reliability analytics available yet.'
      root.innerHTML = emptyListItem('Collect more telemetry to build the reliability ledger.')
      return
    }

    document.getElementById('reliability-score').textContent = `${Number(reliability.fleet_operational_score || 0).toFixed(0)}/100`
    document.getElementById('reliability-coverage').textContent = `${Number(reliability.fleet_data_coverage_percent || 0).toFixed(0)}%`
    document.getElementById('reliability-incidents').textContent = `${reliability.incident_count || 0}`
    document.getElementById('reliability-signals').textContent = `${reliability.signal_event_count || 0}`
    summary.textContent = reliability.summary || '24h reliability ledger is available.'

    reliability.nodes.slice(0, 6).forEach(node => {
      const signals = (node.signals || []).slice(0, 3).map(escapeHtml).join(' • ')
      const article = document.createElement('article')
      article.className = 'stack-item'
      article.innerHTML = `
        <div class="stack-topline">
          <span>${escapeHtml(node.node_name || node.node_id || 'Unknown node')}</span>
          <span class="quality-pill ${escapeHtml(node.data_quality || 'weak')}">${escapeHtml(node.data_quality || 'weak')}</span>
        </div>
        <div class="stack-title">${Number(node.operational_score || 0).toFixed(0)}/100 operational score • ${escapeHtml(node.status || 'unknown')}</div>
        <div class="stack-body">${escapeHtml(node.recommendation || 'No recommendation available.')}</div>
        <div class="stack-topline">
          <span>${Number(node.availability_percent || 0).toFixed(0)}% availability proxy • ${Number(node.data_coverage_percent || 0).toFixed(0)}% coverage</span>
          <span>${node.incident_count || 0} incident(s) • ${node.signal_event_count || 0} signal(s)</span>
        </div>
        <div class="stack-footnote">${signals || 'no signals'}</div>
      `
      article.addEventListener('click', () => selectNode(node.node_id))
      root.appendChild(article)
    })
  }

  function renderNodeTable(nodes, scores) {
    const body = document.getElementById('nodes-table-body')
    body.innerHTML = ''

    if (!nodes.length) {
      body.innerHTML = `<tr><td colspan="7">No nodes match the current search.</td></tr>`
      return
    }

    nodes.forEach(node => {
      const metrics = node.metrics || {}
      const score = scores[node.id]
      const links = (state.dashboard.links || []).filter(link => link.source_node_id === node.id || link.target_node_id === node.id)
      const worstLink = [...links].sort(compareLinks)[0]

      const row = document.createElement('tr')
      if (node.id === state.selectedNodeId) row.classList.add('active')
      row.innerHTML = `
        <td>
          <div class="node-name-cell">
            <strong>${escapeHtml(node.name)}</strong>
            <span class="node-provider">${escapeHtml(node.provider || 'Unknown')} • ${escapeHtml(node.ip_address || 'IP unavailable')}</span>
          </div>
        </td>
        <td><span class="status-pill ${node.status || 'unknown'}">${node.status || 'unknown'}</span></td>
        <td>${metricBar(metrics.cpu_percent)}</td>
        <td>${metricBar(metrics.memory_percent)}</td>
        <td>${formatBandwidth(metrics.bandwidth_down)}</td>
        <td>${worstLink ? `${formatLatency(worstLink.latency_ms)} / ${formatPercent(worstLink.packet_loss)}` : '--'}</td>
        <td>${score ? `${score.composite_score.toFixed(0)}/100` : '--'}</td>
      `
      row.addEventListener('click', () => selectNode(node.id))
      body.appendChild(row)
    })
  }

  function renderLinksList(links, nodes) {
    const root = document.getElementById('links-list')
    root.innerHTML = ''

    const ranked = [...links].sort(compareLinks)
    if (!ranked.length) {
      root.innerHTML = emptyListItem('No probe links available yet.')
      return
    }

    ranked.slice(0, 8).forEach(link => {
      const source = nodes.find(node => node.id === link.source_node_id)
      const target = nodes.find(node => node.id === link.target_node_id)
      root.insertAdjacentHTML('beforeend', stackItem(
        `${escapeHtml(source?.name || link.source_node_id)} → ${escapeHtml(target?.name || link.target_node_id)}`,
        `${formatLatency(link.latency_ms)} latency • ${formatPercent(link.packet_loss)} loss`,
        `${escapeHtml(link.status || 'unknown')} • updated ${relativeTime(link.updated_at)}`
      ))
    })
  }

  function renderSources(sources) {
    const root = document.getElementById('sources-list')
    root.innerHTML = ''

    if (!sources.length) {
      root.innerHTML = emptyListItem('No ingress sources have been sampled yet.')
      return
    }

    sources.forEach(source => {
      root.insertAdjacentHTML('beforeend', stackItem(
        `${escapeHtml(source.source_ip)} • ${escapeHtml(source.node_name || source.node_id || 'Unknown node')}`,
        `${escapeHtml(source.protocol || 'unknown')} • ${escapeHtml(source.source_city || '?')}${source.source_country ? `, ${escapeHtml(source.source_country)}` : ''}`,
        `${formatRate(source.peak_rate_bps)} peak • ${formatBytes(source.latest_total_bytes)} total • ${relativeTime(source.last_seen)}`
      ))
    })
  }

  function renderNodeDetails() {
    if (!state.detail || !state.detail.node || state.detail.node.id !== state.selectedNodeId) return

    const {
      node,
      score,
      history,
      events,
      links,
      metrics,
      incidents,
      live_connections: liveConnections,
      recent_connections: recentConnections,
      analytics,
    } = state.detail
    const statsRoot = document.getElementById('detail-stats')
    statsRoot.innerHTML = ''

    const detailEmpty = document.getElementById('detail-empty')
    const detailContent = document.getElementById('detail-content')
    detailEmpty.classList.add('hidden')
    detailContent.classList.remove('hidden')

    document.getElementById('detail-name').textContent = node.name
    document.getElementById('detail-provider').textContent = node.provider || 'Unknown provider'
    document.getElementById('detail-meta').textContent = `${node.id} • ${node.ip_address || 'IP unavailable'} • ${formatCoordinates(node.latitude, node.longitude)} • ${describeLocationSource(node.location_source)} • last seen ${relativeTime(node.last_seen)}`
    document.getElementById('detail-subtitle').textContent = `${state.detail.metrics_window_hours}h trend window with persisted event and connection context.`

    const statusBadge = document.getElementById('detail-status-badge')
    statusBadge.className = `health-badge ${node.status || 'unknown'}`
    statusBadge.textContent = node.status || 'unknown'
    document.getElementById('detail-score').textContent = score ? `Score ${score.composite_score.toFixed(0)}/100` : 'Score unavailable'
    renderAnalyticsSummary(analytics)

    const metricsSnapshot = node.metrics || {}
    const tiles = [
      ['CPU', formatPercent(metricsSnapshot.cpu_percent)],
      ['Memory', formatPercent(metricsSnapshot.memory_percent)],
      ['Disk', formatPercent(metricsSnapshot.disk_percent)],
      ['Load', metricsSnapshot.load_avg != null ? metricsSnapshot.load_avg.toFixed(2) : '--'],
      ['Bandwidth Up', formatBandwidth(metricsSnapshot.bandwidth_up)],
      ['Bandwidth Down', formatBandwidth(metricsSnapshot.bandwidth_down)],
      ['TCP Connections', metricsSnapshot.connections != null ? metricsSnapshot.connections : '--'],
      ['Uptime', formatUptime(metricsSnapshot.uptime_seconds)],
      ['CPU P95', analytics ? formatPercent(analytics.cpu?.p95) : '--'],
      ['Memory P95', analytics ? formatPercent(analytics.memory?.p95) : '--'],
      ['CPU Robust Z', analytics?.cpu ? formatSigned(analytics.cpu.robust_z) : '--'],
      ['Conn Slope/hr', analytics?.connections ? formatSigned(analytics.connections.slope_per_hour) : '--'],
    ]
    tiles.forEach(([label, value]) => {
      statsRoot.insertAdjacentHTML('beforeend', `
        <article class="stat-tile">
          <span>${label}</span>
          <strong>${value}</strong>
        </article>
      `)
    })

    renderChart('cpu', metrics, point => point.cpu_percent, '#69e3ff', formatPercent(metricsSnapshot.cpu_percent))
    renderChart('memory', metrics, point => point.memory_percent, '#ffba4a', formatPercent(metricsSnapshot.memory_percent))
    renderChart('bandwidth', metrics, point => point.bandwidth_down, '#87ff86', formatBandwidth(metricsSnapshot.bandwidth_down))
    renderChart('connections', metrics, point => point.connections, '#8cb0ff', metricsSnapshot.connections != null ? `${metricsSnapshot.connections}` : '--')

    renderCompactList('detail-live-connections', (liveConnections || []).slice(0, 8), conn => stackItem(
      `${escapeHtml(conn.src_ip)} • ${escapeHtml(conn.protocol || 'unknown')}`,
      `${escapeHtml(conn.src_city || '?')}${conn.src_country ? `, ${escapeHtml(conn.src_country)}` : ''}`,
      `${formatRate(conn.rate || 0)} • ${formatBytes(conn.total_bytes || 0)}`
    ), 'No live connections in the current snapshot.')

    renderCompactList('detail-recent-connections', recentConnections || [], conn => stackItem(
      `${escapeHtml(conn.source_ip)} • ${escapeHtml(conn.protocol || 'unknown')}`,
      `${escapeHtml(conn.source_city || '?')}${conn.source_country ? `, ${escapeHtml(conn.source_country)}` : ''}`,
      `${formatRate(conn.peak_rate_bps)} peak • ${formatBytes(conn.latest_total_bytes)} total • ${relativeTime(conn.last_seen)}`
    ), 'No persisted source summary available yet.')

    renderCompactList('detail-links', links || [], link => stackItem(
      `${escapeHtml(link.source_node_id)} → ${escapeHtml(link.target_node_id)}`,
      `${formatLatency(link.latency_ms)} latency • ${formatPercent(link.packet_loss)} loss`,
      `${escapeHtml(link.status || 'unknown')} • updated ${relativeTime(link.updated_at)}`
    ), 'No related links available.')

    renderCompactList('detail-events', events || [], event => stackItem(
      `${escapeHtml(event.title || 'Untitled event')}`,
      `${escapeHtml(event.body || 'No event details available.')}`,
      `${escapeHtml(event.type || 'event')} • ${relativeTime(event.created_at)}`
    ), 'No node-specific events recorded.')

    renderCompactList('detail-incidents', incidents || [], incident => incidentMarkup(incident), 'No incidents recorded for this node.')

    renderCompactList('detail-history', (history || []).slice(0, 10), item => stackItem(
      `${escapeHtml(item.old_status || 'unknown')} → ${escapeHtml(item.new_status || 'unknown')}`,
      `${escapeHtml(item.reason || 'No reason recorded.')}`,
      relativeTime(item.created_at)
    ), 'No status transitions recorded.')
  }

  function renderChart(name, points, accessor, color, summaryValue) {
    document.getElementById(`chart-${name}-value`).textContent = summaryValue
    const root = document.getElementById(`chart-${name}`)
    root.innerHTML = sparkline(points || [], accessor, color)
  }

  function renderAnalyticsSummary(analytics) {
    const summary = document.getElementById('detail-analytics-summary')
    const risk = document.getElementById('detail-analytics-risk')
    const highlights = document.getElementById('detail-analytics-highlights')

    if (!analytics) {
      summary.textContent = 'No statistical summary available.'
      risk.className = 'analytics-risk'
      risk.textContent = 'risk unknown'
      highlights.innerHTML = '<div class="analytics-highlight">No statistical highlights available.</div>'
      return
    }

    summary.textContent = analytics.summary || 'No summary available.'
    risk.className = `analytics-risk ${analytics.risk_level || ''}`.trim()
    risk.textContent = `risk ${analytics.risk_level || 'unknown'}`
    highlights.innerHTML = ''
    ;(analytics.highlights || []).forEach(item => {
      highlights.insertAdjacentHTML('beforeend', `<div class="analytics-highlight">${escapeHtml(item)}</div>`)
    })
  }

  function updateMapFullscreenButton() {
    const button = document.getElementById('btn-map-fullscreen')
    if (!button) return
    const active = StarMap.isFullscreen()
    button.classList.toggle('active', active)
    button.textContent = active ? 'Exit Fullscreen' : 'Fullscreen'
  }

  function renderCompactList(elementId, items, renderItem, emptyMessage) {
    const root = document.getElementById(elementId)
    root.innerHTML = ''
    if (!items.length) {
      root.innerHTML = emptyListItem(emptyMessage)
      return
    }
    items.forEach(item => {
      root.insertAdjacentHTML('beforeend', renderItem(item))
    })
  }

  function matchesSearch(node) {
    if (!state.searchText) return true
    const haystack = [node.name, node.provider, node.id, node.status].join(' ').toLowerCase()
    return haystack.includes(state.searchText)
  }

  function sortNodes(nodes, scores) {
    return [...nodes].sort((a, b) => {
      const severityDelta = statusRank(a.status) - statusRank(b.status)
      if (severityDelta !== 0) return severityDelta
      const scoreDelta = (scores[b.id]?.composite_score ?? -1) - (scores[a.id]?.composite_score ?? -1)
      if (scoreDelta !== 0) return scoreDelta
      return a.name.localeCompare(b.name)
    })
  }

  function chooseDefaultNode(nodes, scores) {
    const ranked = sortNodes(nodes, scoreMap(scores))
    return ranked[0]?.id || null
  }

  function compareLinks(a, b) {
    const scoreA = (a.status === 'bad' ? 100000 : a.status === 'degraded' ? 50000 : 0) + (a.packet_loss || 0) * 100 + (a.latency_ms > 0 ? a.latency_ms : 1000)
    const scoreB = (b.status === 'bad' ? 100000 : b.status === 'degraded' ? 50000 : 0) + (b.packet_loss || 0) * 100 + (b.latency_ms > 0 ? b.latency_ms : 1000)
    return scoreB - scoreA
  }

  function scoreMap(scores) {
    return Object.fromEntries(scores.map(score => [score.node_id, score]))
  }

  function updateLastUpdateDisplay() {
    const root = document.getElementById('last-update')
    if (!state.lastUpdateTime) {
      root.textContent = 'Last sync --'
      return
    }
    root.textContent = `Last sync ${Math.floor((Date.now() - state.lastUpdateTime) / 1000)}s ago`
  }

  function buildHeadline(status, selectedNode) {
    const total = status.total || 0
    if (selectedNode) {
      return `${total} monitored nodes. Focused on ${selectedNode.name} with ${selectedNode.status || 'unknown'} status and ${selectedNode.metrics?.connections ?? 0} current TCP sockets.`
    }
    return `${total} monitored nodes. ${status.online || 0} online, ${status.degraded || 0} degraded, ${status.offline || 0} offline.`
  }

  function healthClass(status) {
    if ((status.offline || 0) > 0) return 'offline'
    if ((status.degraded || 0) > 0) return 'degraded'
    if ((status.online || 0) > 0) return 'online'
    return 'unknown'
  }

  function healthLabel(status) {
    if ((status.offline || 0) > 0) return 'Attention'
    if ((status.degraded || 0) > 0) return 'Degraded'
    if ((status.online || 0) > 0) return 'Healthy'
    return 'Unknown'
  }

  function showError() {
    if (state.hasError) return
    state.hasError = true
    document.getElementById('error-banner').classList.remove('hidden')
  }

  function clearError() {
    if (!state.hasError) return
    state.hasError = false
    document.getElementById('error-banner').classList.add('hidden')
  }

  function metricBar(value) {
    if (value == null) return '--'
    const safe = Math.max(0, Math.min(100, value))
    return `
      <span class="metric-inline">
        ${safe.toFixed(0)}%
        <span class="metric-bar"><span style="width:${safe}%"></span></span>
      </span>
    `
  }

  function sparkline(points, accessor, color) {
    if (!points.length) return `<div class="event-body">No samples in the selected window.</div>`

    const width = 260
    const height = 92
    const padding = 10
    const values = points.map(point => Number(accessor(point) || 0))
    const min = Math.min(...values)
    const max = Math.max(...values)
    const range = max - min || 1
    const stepX = (width - padding * 2) / Math.max(1, points.length - 1)
    const path = values.map((value, index) => {
      const x = padding + stepX * index
      const y = height - padding - ((value - min) / range) * (height - padding * 2)
      return `${index === 0 ? 'M' : 'L'}${x.toFixed(2)},${y.toFixed(2)}`
    }).join(' ')

    return `
      <svg viewBox="0 0 ${width} ${height}" preserveAspectRatio="none" role="img" aria-label="Metric sparkline">
        <defs>
          <linearGradient id="fill-${color.replace('#', '')}" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stop-color="${color}" stop-opacity="0.4"></stop>
            <stop offset="100%" stop-color="${color}" stop-opacity="0.02"></stop>
          </linearGradient>
        </defs>
        <path d="${path}" fill="none" stroke="${color}" stroke-width="2.4" stroke-linecap="round"></path>
      </svg>
    `
  }

  function stackItem(title, body, meta) {
    return `
      <article class="stack-item">
        <div class="stack-title">${title}</div>
        <div class="stack-body">${body}</div>
        <div class="stack-topline">
          <span>${meta}</span>
        </div>
      </article>
    `
  }

  function incidentMarkup(incident) {
    const node = incident.node_name || incident.node_id || 'system'
    const status = incident.status || 'open'
    const statusDetail = status === 'suppressed' && incident.suppress_until
      ? `suppressed until ${absoluteDateTime(incident.suppress_until)}`
      : status
    return `
      <article class="stack-item incident-card ${escapeHtml(incident.severity || 'info')} ${escapeHtml(status)}">
        <div class="stack-topline">
          <span>#${incident.id} • ${escapeHtml(node)} • ${escapeHtml(incident.type || 'incident')}</span>
          <span class="incident-state ${escapeHtml(status)}">${escapeHtml(statusDetail)}</span>
        </div>
        <div class="stack-title">${escapeHtml(incident.title || 'Untitled incident')}</div>
        <div class="stack-body">${escapeHtml(incident.body || 'No incident details available.')}</div>
        <div class="stack-topline">
          <span>${escapeHtml(incident.severity || 'info')} • ${incident.event_count || 1} event(s)</span>
          <span>${relativeTime(incident.last_seen)} • first ${relativeTime(incident.first_seen)}</span>
        </div>
      </article>
    `
  }

  function emptyListItem(message) {
    return `<article class="stack-item"><div class="stack-body">${escapeHtml(message)}</div></article>`
  }

  function statusRank(status) {
    switch (status) {
      case 'offline':
        return 0
      case 'degraded':
        return 1
      case 'online':
        return 2
      default:
        return 3
    }
  }

  function formatLatency(value) {
    if (value == null || value < 0) return 'N/A'
    if (value < 1) return '<1ms'
    return `${Math.round(value)}ms`
  }

  function formatPercent(value) {
    if (value == null) return '--'
    return `${value.toFixed(1)}%`
  }

  function formatBandwidth(value) {
    if (value == null) return '--'
    if (value >= 1024) return `${(value / 1024).toFixed(1)} MB/s`
    return `${value.toFixed(1)} KB/s`
  }

  function formatCoordinates(latitude, longitude) {
    if (latitude == null || longitude == null) return 'coords unavailable'
    return `${Number(latitude).toFixed(2)}, ${Number(longitude).toFixed(2)}`
  }

  function formatRate(value) {
    if (value == null) return '--'
    if (value < 1024) return `${Math.round(value)} B/s`
    if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB/s`
    return `${(value / 1024 / 1024).toFixed(1)} MB/s`
  }

  function formatBytes(value) {
    if (value == null) return '--'
    if (value < 1024) return `${value} B`
    if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`
    if (value < 1024 * 1024 * 1024) return `${(value / 1024 / 1024).toFixed(1)} MB`
    return `${(value / 1024 / 1024 / 1024).toFixed(1)} GB`
  }

  function formatUptime(seconds) {
    if (!seconds) return '--'
    const days = Math.floor(seconds / 86400)
    const hours = Math.floor((seconds % 86400) / 3600)
    if (days > 0) return `${days}d ${hours}h`
    const minutes = Math.floor((seconds % 3600) / 60)
    return `${hours}h ${minutes}m`
  }

  function formatSigned(value) {
    if (value == null) return '--'
    const sign = value > 0 ? '+' : ''
    return `${sign}${value.toFixed(2)}`
  }

  function formatDuration(seconds) {
    if (seconds == null || seconds === '') return '--'
    const value = Number(seconds)
    if (!Number.isFinite(value)) return '--'
    if (value < 60) return `${Math.round(value)}s`
    const minutes = Math.floor(value / 60)
    const rest = Math.round(value % 60)
    return rest ? `${minutes}m ${rest}s` : `${minutes}m`
  }

  function formatMetricPeak(experiment) {
    const value = Number(experiment.peak_metric_value || 0)
    const metric = experiment.expected_metric || ''
    if (metric.includes('percent') || metric === 'cpu' || metric === 'memory') {
      return `${value.toFixed(1)}%`
    }
    return value.toFixed(1)
  }

  function absoluteTime(timestamp) {
    if (!timestamp) return '--'
    return new Date(timestamp * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  }

  function absoluteDateTime(timestamp) {
    if (!timestamp) return '--'
    return new Date(timestamp * 1000).toLocaleString([], { month: 'short', day: '2-digit', hour: '2-digit', minute: '2-digit' })
  }

  function describeLocationSource(source) {
    switch (source) {
      case 'manual':
        return 'manual map position'
      case 'manual_override':
        return 'server-enforced map position'
      case 'geoip':
        return 'GeoIP-estimated map position'
      default:
        return 'location precision unknown'
    }
  }

  function relativeTime(timestamp) {
    if (!timestamp) return '--'
    const diff = Math.max(0, Math.floor(Date.now() / 1000) - timestamp)
    if (diff < 60) return `${diff}s ago`
    if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
    if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`
    return `${Math.floor(diff / 86400)}d ago`
  }

  function escapeHtml(value) {
    if (value == null) return ''
    const div = document.createElement('div')
    div.textContent = String(value)
    return div.innerHTML
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init)
  } else {
    init()
  }
})()
