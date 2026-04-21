import { Hono } from 'hono'
import { handle } from 'hono/cloudflare-pages'

type Bindings = {
  DB: D1Database
  API_TOKEN: string
}

type NodeRow = {
  id: string
  name: string
  provider?: string | null
  ip_address?: string | null
  latitude: number
  longitude: number
  location_source?: string | null
  status: string
  last_seen?: number | null
  cpu_percent?: number | null
  memory_percent?: number | null
  disk_percent?: number | null
  bandwidth_up?: number | null
  bandwidth_down?: number | null
  load_avg?: number | null
  connections?: number | null
  uptime_seconds?: number | null
  updated_at?: number | null
}

const app = new Hono<{ Bindings: Bindings }>().basePath('/api')

function dynamicTimestamp(status: string): { last_seen: number; uptime_seconds?: number } {
  const now = Math.floor(Date.now() / 1000)
  switch (status) {
    case 'online':
      return { last_seen: now - Math.floor(Math.random() * 24 + 6) }
    case 'degraded':
      return { last_seen: now - Math.floor(Math.random() * 52 + 12) }
    case 'offline':
      return { last_seen: now - Math.floor(Math.random() * 86400 + 3600), uptime_seconds: 0 }
    default:
      return { last_seen: now - 3600 }
  }
}

function rowToNode(row: NodeRow) {
  const ts = dynamicTimestamp(row.status)
  return {
    id: row.id,
    name: row.name,
    provider: row.provider ?? null,
    ip_address: row.ip_address ?? null,
    latitude: row.latitude,
    longitude: row.longitude,
    location_source: row.location_source ?? 'unknown',
    status: row.status,
    last_seen: ts.last_seen,
    metrics: {
      cpu_percent: row.cpu_percent ?? null,
      memory_percent: row.memory_percent ?? null,
      disk_percent: row.disk_percent ?? null,
      bandwidth_up: row.bandwidth_up ?? null,
      bandwidth_down: row.bandwidth_down ?? null,
      load_avg: row.load_avg ?? null,
      connections: row.connections ?? null,
      uptime_seconds: ts.uptime_seconds ?? row.uptime_seconds ?? null,
      updated_at: row.updated_at ?? ts.last_seen,
    },
  }
}

async function getNodes(db: D1Database) {
  const query = `
    SELECT
      n.id, n.name, n.provider, n.ip_address, n.latitude, n.longitude, n.status, n.last_seen,
      n.location_source,
      m.cpu_percent, m.memory_percent, m.disk_percent, m.bandwidth_up, m.bandwidth_down,
      m.load_avg, m.connections, m.uptime_seconds, m.updated_at
    FROM nodes n
    LEFT JOIN node_metrics m ON n.id = m.node_id
  `
  const { results } = await db.prepare(query).all<NodeRow>()
  return (results || []).map(rowToNode)
}

async function getStatus(db: D1Database) {
  const { results } = await db.prepare('SELECT status, COUNT(*) as count FROM nodes GROUP BY status').all<{ status: string; count: number }>()
  const counts: Record<string, number> = { online: 0, degraded: 0, offline: 0, unknown: 0 }
  let total = 0
  for (const row of results || []) {
    counts[row.status] = row.count
    total += row.count
  }
  return { total, ...counts }
}

async function getLinks(db: D1Database) {
  const { results } = await db.prepare(`
    SELECT id, source_node_id, target_node_id, latency_ms, packet_loss, status, updated_at
    FROM links
  `).all()
  return results || []
}

async function getScores(db: D1Database) {
  const { results } = await db.prepare(`
    SELECT node_id, availability, latency_score, stability, composite_score, updated_at
    FROM node_scores
    ORDER BY composite_score DESC
  `).all()
  return results || []
}

async function getEvents(db: D1Database, limit = 20, nodeId?: string) {
  const base = `
    SELECT e.id, e.node_id, n.name AS node_name, e.type, e.severity, e.title, e.body, e.metadata, e.created_at
    FROM events e
    LEFT JOIN nodes n ON n.id = e.node_id
  `
  if (nodeId) {
    const { results } = await db.prepare(`${base} WHERE e.node_id = ? ORDER BY e.created_at DESC LIMIT ?`).bind(nodeId, limit).all()
    return results || []
  }
  const { results } = await db.prepare(`${base} ORDER BY e.created_at DESC LIMIT ?`).bind(limit).all()
  return results || []
}

async function getHotSources(db: D1Database, limit = 8, nodeId?: string) {
  const now = Math.floor(Date.now() / 1000)
  const since = now - 86400
  const base = `
    SELECT
      cs.source_key,
      cs.source_ip,
      COALESCE(cs.source_country, '') AS source_country,
      COALESCE(cs.source_city, '') AS source_city,
      COALESCE(cs.protocol, '') AS protocol,
      COALESCE(cs.local_port, 0) AS local_port,
      MAX(cs.is_cloudflare) AS is_cloudflare,
      COUNT(*) AS sample_count,
      MAX(cs.rate_bps) AS peak_rate_bps,
      AVG(cs.rate_bps) AS avg_rate_bps,
      MAX(cs.total_bytes) AS latest_total_bytes,
      MAX(cs.sample_at) AS last_seen,
      n.id AS node_id,
      n.name AS node_name
    FROM connection_samples cs
    LEFT JOIN nodes n ON n.id = cs.node_id
    WHERE cs.sample_at >= ?
  `
  if (nodeId) {
    const { results } = await db.prepare(`${base} AND cs.node_id = ? GROUP BY cs.source_key, cs.source_ip, cs.source_country, cs.source_city, cs.protocol, cs.local_port, n.id, n.name ORDER BY MAX(cs.rate_bps) DESC LIMIT ?`)
      .bind(since, nodeId, limit).all()
    return results || []
  }
  const { results } = await db.prepare(`${base} GROUP BY cs.source_key, cs.source_ip, cs.source_country, cs.source_city, cs.protocol, cs.local_port, n.id, n.name ORDER BY MAX(cs.rate_bps) DESC LIMIT ?`)
    .bind(since, limit).all()
  return results || []
}

function buildFleetAnalytics(nodes: any[], scores: any[]) {
  const scoreMap = new Map((scores || []).map((score: any) => [score.node_id, score]))
  const nodeInsights = (nodes || []).map((node: any) => {
    const cpu = Number(node.metrics?.cpu_percent ?? 0)
    const memory = Number(node.metrics?.memory_percent ?? 0)
    const load = Number(node.metrics?.load_avg ?? 0)
    let riskLevel = 'stable'
    if (cpu >= 85 || memory >= 90) {
      riskLevel = 'critical'
    } else if (cpu >= 70 || memory >= 80 || load >= 1.5) {
      riskLevel = 'elevated'
    }

    const highlights = []
    if (cpu >= 70) highlights.push(`CPU is running at ${cpu.toFixed(1)}%.`)
    if (memory >= 80) highlights.push(`Memory is running at ${memory.toFixed(1)}%.`)
    if (!highlights.length) highlights.push('Recent metrics look stable in the demo dataset.')

    return {
      node_id: node.id,
      node_name: node.name,
      risk_level: riskLevel,
      composite_score: scoreMap.get(node.id)?.composite_score ?? null,
      coverage_percent: 100,
      signal_count: highlights.length,
      summary: `${node.name} is ${riskLevel} in the demo radar based on current CPU and memory pressure.`,
      highlights,
    }
  }).sort((a: any, b: any) => {
    const rank = (value: string) => value === 'critical' ? 0 : value === 'elevated' ? 1 : 2
    return rank(a.risk_level) - rank(b.risk_level)
  })

  return {
    window_hours: 24,
    critical: nodeInsights.filter((item: any) => item.risk_level === 'critical').length,
    elevated: nodeInsights.filter((item: any) => item.risk_level === 'elevated').length,
    stable: nodeInsights.filter((item: any) => item.risk_level === 'stable').length,
    summary: `24h radar across ${nodeInsights.length} demo nodes.`,
    node_insights: nodeInsights,
  }
}

function buildReliabilityAnalytics(nodes: any[], scores: any[], events: any[]) {
  const scoreMap = new Map((scores || []).map((score: any) => [score.node_id, score]))
  const eventsByNode = new Map<string, { incidents: number; critical: number; warning: number }>()
  let incidentCount = 0
  let criticalEventCount = 0
  let warningEventCount = 0

  for (const event of events || []) {
    if (event.severity !== 'critical' && event.severity !== 'warning') continue
    incidentCount++
    if (event.severity === 'critical') criticalEventCount++
    if (event.severity === 'warning') warningEventCount++

    const nodeId = event.node_id
    if (!nodeId) continue
    const counts = eventsByNode.get(nodeId) || { incidents: 0, critical: 0, warning: 0 }
    counts.incidents++
    if (event.severity === 'critical') counts.critical++
    if (event.severity === 'warning') counts.warning++
    eventsByNode.set(nodeId, counts)
  }

  const reliabilityNodes = (nodes || []).map((node: any) => {
    const score = scoreMap.get(node.id)
    const counts = eventsByNode.get(node.id) || { incidents: 0, critical: 0, warning: 0 }
    const availability = Number(score?.availability ?? (node.status === 'online' ? 100 : node.status === 'degraded' ? 72 : 0))
    const coverage = 100
    const stability = Number(score?.stability ?? 92)
    const eventHealth = Math.max(0, 100 - counts.critical * 18 - counts.warning * 8)
    const operationalScore = Math.max(0, Math.min(100, availability * 0.35 + coverage * 0.25 + stability * 0.25 + eventHealth * 0.15))
    const dataQuality = coverage >= 80 ? 'good' : coverage >= 50 ? 'partial' : 'weak'

    return {
      node_id: node.id,
      node_name: node.name,
      status: node.status,
      operational_score: operationalScore,
      availability_percent: availability,
      data_coverage_percent: coverage,
      last_seen_age_seconds: Math.max(0, Math.floor(Date.now() / 1000) - Number(node.last_seen || 0)),
      incident_count: counts.incidents,
      critical_event_count: counts.critical,
      warning_event_count: counts.warning,
      data_quality: dataQuality,
      recommendation: counts.critical > 0 ? 'Inspect recent critical events in the demo dataset.' : 'No immediate action in the demo dataset.',
      signals: counts.incidents > 0 ? [`${counts.incidents} incident(s) in window`] : ['healthy demo telemetry'],
    }
  }).sort((a: any, b: any) => a.operational_score - b.operational_score)

  const avg = (key: string) => reliabilityNodes.length
    ? reliabilityNodes.reduce((sum: number, node: any) => sum + Number(node[key] || 0), 0) / reliabilityNodes.length
    : 0

  return {
    window_hours: 24,
    fleet_operational_score: avg('operational_score'),
    fleet_availability_percent: avg('availability_percent'),
    fleet_data_coverage_percent: avg('data_coverage_percent'),
    incident_count: incidentCount,
    critical_event_count: criticalEventCount,
    warning_event_count: warningEventCount,
    summary: `24h demo reliability ledger: ${avg('operational_score').toFixed(0)}/100 fleet score across ${reliabilityNodes.length} nodes.`,
    nodes: reliabilityNodes,
  }
}

function buildDemoGroundTruth() {
  const now = Math.floor(Date.now() / 1000)
  return {
    experiment_count: 1,
    detected_count: 1,
    missed_count: 0,
    recovered_count: 1,
    mean_detection_delay_seconds: 27,
    mean_recovery_delay_seconds: 31,
    false_positive_event_count: 2,
    detection_rate_percent: 100,
    recovery_rate_percent: 100,
    experiments: [
      {
        experiment_id: 'demo-cpu-fault',
        node_id: 'sj-1',
        injection_type: 'cpu_stress',
        expected_metric: 'cpu_percent',
        started_at: now - 5400,
        ended_at: now - 5250,
        detected: true,
        first_detection_at: now - 5373,
        detection_delay_seconds: 27,
        recovered: true,
        first_recovery_at: now - 5219,
        recovery_delay_seconds: 31,
        peak_metric_value: 99.4,
        detection_titles: ['CPU outlier detected'],
      },
    ],
  }
}

app.use('/report', async (c, next) => {
  if (c.req.header('Authorization') !== `Bearer ${c.env.API_TOKEN}`) {
    return c.json({ error: 'Unauthorized' }, 401)
  }
  await next()
})

app.get('/dashboard', async c => {
  const [status, nodes, links, scores, events, hotSources] = await Promise.all([
    getStatus(c.env.DB),
    getNodes(c.env.DB),
    getLinks(c.env.DB),
    getScores(c.env.DB),
    getEvents(c.env.DB, 15),
    getHotSources(c.env.DB, 8),
  ])

  return c.json({
    generated_at: Math.floor(Date.now() / 1000),
    status,
    nodes,
    links,
    scores,
    events,
    hot_sources: hotSources,
    fleet_analytics: buildFleetAnalytics(nodes, scores),
    reliability_analytics: buildReliabilityAnalytics(nodes, scores, events),
    ground_truth: buildDemoGroundTruth(),
  })
})

app.get('/nodes', async c => c.json({ nodes: await getNodes(c.env.DB) }))

app.get('/nodes/:id', async c => {
  const nodes = await getNodes(c.env.DB)
  const node = nodes.find(item => item.id === c.req.param('id'))
  if (!node) return c.json({ error: 'Node not found' }, 404)
  return c.json(node)
})

app.get('/nodes/:id/details', async c => {
  const nodeId = c.req.param('id')
  const hours = Math.min(Math.max(Number(c.req.query('hours') || 24), 1), 168)
  const nodes = await getNodes(c.env.DB)
  const node = nodes.find(item => item.id === nodeId)
  if (!node) return c.json({ error: 'Node not found' }, 404)

  const now = Math.floor(Date.now() / 1000)
  const from = now - hours * 3600

  const [history, scores, events, links, metrics, recentConnections] = await Promise.all([
    c.env.DB.prepare(`
      SELECT id, node_id, old_status, new_status, reason, created_at
      FROM status_history
      WHERE node_id = ?
      ORDER BY created_at DESC
      LIMIT 20
    `).bind(nodeId).all(),
    getScores(c.env.DB),
    getEvents(c.env.DB, 12, nodeId),
    c.env.DB.prepare(`
      SELECT id, source_node_id, target_node_id, latency_ms, packet_loss, status, updated_at
      FROM links
      WHERE source_node_id = ? OR target_node_id = ?
    `).bind(nodeId, nodeId).all(),
    c.env.DB.prepare(`
      SELECT created_at AS timestamp, cpu_percent, memory_percent, disk_percent, bandwidth_up, bandwidth_down, load_avg, connections
      FROM metrics_raw
      WHERE node_id = ? AND created_at >= ?
      ORDER BY created_at
    `).bind(nodeId, from).all(),
    getHotSources(c.env.DB, 10, nodeId),
  ])

  const score = (scores || []).find((item: any) => item.node_id === nodeId) || null

  return c.json({
    generated_at: now,
    node,
    score,
    history: history.results || [],
    events,
    links: links.results || [],
    metrics_window_hours: hours,
    metrics: metrics.results || [],
    recent_connections: recentConnections,
    live_connections: [],
  })
})

app.get('/links', async c => c.json({ links: await getLinks(c.env.DB) }))
app.get('/status', async c => c.json(await getStatus(c.env.DB)))
app.get('/events', async c => c.json({ events: await getEvents(c.env.DB, Number(c.req.query('limit') || 20)) }))
app.get('/scores', async c => c.json({ scores: await getScores(c.env.DB) }))
app.get('/connections', async c => c.json({}))

app.get('/history/:id', async c => {
  const { results } = await c.env.DB.prepare(`
    SELECT id, node_id, old_status, new_status, reason, created_at
    FROM status_history
    WHERE node_id = ?
    ORDER BY created_at DESC
  `).bind(c.req.param('id')).all()
  return c.json({ history: results || [] })
})

app.post('/report', async c => {
  const body = await c.req.json()
  const nodeId = body.node_id
  if (!nodeId) return c.json({ error: 'node_id is required' }, 400)

  const now = Math.floor(Date.now() / 1000)
  await c.env.DB.prepare(`
    INSERT INTO node_metrics (node_id, cpu_percent, memory_percent, disk_percent, bandwidth_up, bandwidth_down, load_avg, connections, uptime_seconds, updated_at)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    ON CONFLICT(node_id) DO UPDATE SET
      cpu_percent = excluded.cpu_percent,
      memory_percent = excluded.memory_percent,
      disk_percent = excluded.disk_percent,
      bandwidth_up = excluded.bandwidth_up,
      bandwidth_down = excluded.bandwidth_down,
      load_avg = excluded.load_avg,
      connections = excluded.connections,
      uptime_seconds = excluded.uptime_seconds,
      updated_at = excluded.updated_at
  `).bind(
    nodeId,
    body.cpu_percent,
    body.memory_percent,
    body.disk_percent,
    body.bandwidth_up,
    body.bandwidth_down,
    body.load_avg,
    body.connections,
    body.uptime_seconds,
    now
  ).run()

  await c.env.DB.prepare('UPDATE nodes SET last_seen = ? WHERE id = ?').bind(now, nodeId).run()
  return c.json({ ok: true })
})

export const onRequest = handle(app)
