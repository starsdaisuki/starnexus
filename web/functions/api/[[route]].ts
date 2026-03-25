import { Hono } from 'hono'
import { handle } from 'hono/cloudflare-pages'

type Bindings = {
  DB: D1Database
  API_TOKEN: string
}

const app = new Hono<{ Bindings: Bindings }>().basePath('/api')

// ============================================================
// Helper: dynamic fake-data timestamps
// ============================================================

function dynamicTimestamp(status: string): { last_seen: number; uptime_seconds?: number } {
  const now = Math.floor(Date.now() / 1000)
  switch (status) {
    case 'online':
      return { last_seen: now - Math.floor(Math.random() * 26 + 5) } // 5~30s ago
    case 'degraded':
      return { last_seen: now - Math.floor(Math.random() * 51 + 10) } // 10~60s ago
    case 'offline':
      return {
        last_seen: now - Math.floor(Math.random() * 82801 + 3600), // 1h~1d ago
        uptime_seconds: 0,
      }
    default:
      return { last_seen: now - 86400 }
  }
}

// ============================================================
// Auth middleware for admin endpoints
// ============================================================

app.use('/report', async (c, next) => {
  const auth = c.req.header('Authorization')
  if (!auth || auth !== `Bearer ${c.env.API_TOKEN}`) {
    return c.json({ error: 'Unauthorized' }, 401)
  }
  await next()
})

app.use('/nodes', async (c, next) => {
  if (c.req.method === 'POST' || c.req.method === 'PUT' || c.req.method === 'DELETE') {
    const auth = c.req.header('Authorization')
    if (!auth || auth !== `Bearer ${c.env.API_TOKEN}`) {
      return c.json({ error: 'Unauthorized' }, 401)
    }
  }
  await next()
})

app.use('/nodes/:id', async (c, next) => {
  if (c.req.method === 'PUT' || c.req.method === 'DELETE') {
    const auth = c.req.header('Authorization')
    if (!auth || auth !== `Bearer ${c.env.API_TOKEN}`) {
      return c.json({ error: 'Unauthorized' }, 401)
    }
  }
  await next()
})

// ============================================================
// Public endpoints
// ============================================================

// GET /api/nodes — all nodes + latest metrics
app.get('/nodes', async (c) => {
  const { results } = await c.env.DB.prepare(`
    SELECT
      n.id, n.name, n.provider, n.latitude, n.longitude, n.status, n.last_seen,
      m.cpu_percent, m.memory_percent, m.disk_percent,
      m.bandwidth_up, m.bandwidth_down, m.load_avg, m.connections,
      m.uptime_seconds, m.updated_at
    FROM nodes n
    LEFT JOIN node_metrics m ON n.id = m.node_id
  `).all()

  const nodes = (results || []).map((row: any) => {
    const ts = dynamicTimestamp(row.status)
    return {
      id: row.id,
      name: row.name,
      provider: row.provider,
      latitude: row.latitude,
      longitude: row.longitude,
      status: row.status,
      last_seen: ts.last_seen,
      metrics: {
        cpu_percent: row.cpu_percent,
        memory_percent: row.memory_percent,
        disk_percent: row.disk_percent,
        bandwidth_up: row.bandwidth_up,
        bandwidth_down: row.bandwidth_down,
        load_avg: row.load_avg,
        connections: row.connections,
        uptime_seconds: ts.uptime_seconds !== undefined ? ts.uptime_seconds : row.uptime_seconds,
        updated_at: ts.last_seen,
      },
    }
  })

  return c.json({ nodes })
})

// GET /api/nodes/:id — single node detail
app.get('/nodes/:id', async (c) => {
  const id = c.req.param('id')
  const row: any = await c.env.DB.prepare(`
    SELECT
      n.id, n.name, n.provider, n.latitude, n.longitude, n.status, n.last_seen,
      m.cpu_percent, m.memory_percent, m.disk_percent,
      m.bandwidth_up, m.bandwidth_down, m.load_avg, m.connections,
      m.uptime_seconds, m.updated_at
    FROM nodes n
    LEFT JOIN node_metrics m ON n.id = m.node_id
    WHERE n.id = ?
  `).bind(id).first()

  if (!row) {
    return c.json({ error: 'Node not found' }, 404)
  }

  const ts = dynamicTimestamp(row.status)
  return c.json({
    id: row.id,
    name: row.name,
    provider: row.provider,
    latitude: row.latitude,
    longitude: row.longitude,
    status: row.status,
    last_seen: ts.last_seen,
    metrics: {
      cpu_percent: row.cpu_percent,
      memory_percent: row.memory_percent,
      disk_percent: row.disk_percent,
      bandwidth_up: row.bandwidth_up,
      bandwidth_down: row.bandwidth_down,
      load_avg: row.load_avg,
      connections: row.connections,
      uptime_seconds: ts.uptime_seconds !== undefined ? ts.uptime_seconds : row.uptime_seconds,
      updated_at: ts.last_seen,
    },
  })
})

// GET /api/links — all link info
app.get('/links', async (c) => {
  const { results } = await c.env.DB.prepare(`
    SELECT id, source_node_id, target_node_id, latency_ms, packet_loss, status, updated_at
    FROM links
  `).all()

  return c.json({ links: results || [] })
})

// GET /api/status — overview stats
app.get('/status', async (c) => {
  const { results } = await c.env.DB.prepare(`
    SELECT status, COUNT(*) as count FROM nodes GROUP BY status
  `).all()

  const stats: Record<string, number> = { online: 0, degraded: 0, offline: 0, unknown: 0 }
  let total = 0
  for (const row of results || []) {
    const r = row as any
    stats[r.status] = r.count
    total += r.count
  }

  return c.json({ total, ...stats })
})

// GET /api/history/:id — status change history for a node
app.get('/history/:id', async (c) => {
  const id = c.req.param('id')
  const { results } = await c.env.DB.prepare(`
    SELECT id, node_id, old_status, new_status, reason, created_at
    FROM status_history
    WHERE node_id = ?
    ORDER BY created_at DESC
  `).bind(id).all()

  return c.json({ history: results || [] })
})

// ============================================================
// Admin endpoints
// ============================================================

// POST /api/report — agent data report
app.post('/report', async (c) => {
  const body = await c.req.json()
  const {
    node_id, cpu_percent, memory_percent, disk_percent,
    bandwidth_up, bandwidth_down, load_avg, connections, uptime_seconds, status,
  } = body

  if (!node_id) {
    return c.json({ error: 'node_id is required' }, 400)
  }

  const now = Math.floor(Date.now() / 1000)

  // Update node status
  if (status) {
    const old: any = await c.env.DB.prepare('SELECT status FROM nodes WHERE id = ?').bind(node_id).first()
    if (old && old.status !== status) {
      await c.env.DB.prepare(
        'INSERT INTO status_history (node_id, old_status, new_status) VALUES (?, ?, ?)'
      ).bind(node_id, old.status, status).run()
    }
    await c.env.DB.prepare(
      'UPDATE nodes SET status = ?, last_seen = ? WHERE id = ?'
    ).bind(status, now, node_id).run()
  } else {
    await c.env.DB.prepare(
      'UPDATE nodes SET last_seen = ? WHERE id = ?'
    ).bind(now, node_id).run()
  }

  // Update metrics
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
  `).bind(node_id, cpu_percent, memory_percent, disk_percent, bandwidth_up, bandwidth_down, load_avg, connections, uptime_seconds, now).run()

  return c.json({ ok: true })
})

// POST /api/nodes — add new node
app.post('/nodes', async (c) => {
  const body = await c.req.json()
  const { id, name, provider, latitude, longitude } = body

  if (!id || !name || latitude == null || longitude == null) {
    return c.json({ error: 'id, name, latitude, longitude are required' }, 400)
  }

  await c.env.DB.prepare(
    'INSERT INTO nodes (id, name, provider, latitude, longitude) VALUES (?, ?, ?, ?, ?)'
  ).bind(id, name, provider || null, latitude, longitude).run()

  return c.json({ ok: true }, 201)
})

// PUT /api/nodes/:id — update node
app.put('/nodes/:id', async (c) => {
  const id = c.req.param('id')
  const body = await c.req.json()
  const { name, provider, latitude, longitude, status } = body

  const fields: string[] = []
  const values: any[] = []

  if (name !== undefined) { fields.push('name = ?'); values.push(name) }
  if (provider !== undefined) { fields.push('provider = ?'); values.push(provider) }
  if (latitude !== undefined) { fields.push('latitude = ?'); values.push(latitude) }
  if (longitude !== undefined) { fields.push('longitude = ?'); values.push(longitude) }
  if (status !== undefined) { fields.push('status = ?'); values.push(status) }

  if (fields.length === 0) {
    return c.json({ error: 'No fields to update' }, 400)
  }

  values.push(id)
  await c.env.DB.prepare(
    `UPDATE nodes SET ${fields.join(', ')} WHERE id = ?`
  ).bind(...values).run()

  return c.json({ ok: true })
})

// DELETE /api/nodes/:id — delete node
app.delete('/nodes/:id', async (c) => {
  const id = c.req.param('id')

  await c.env.DB.prepare('DELETE FROM node_metrics WHERE node_id = ?').bind(id).run()
  await c.env.DB.prepare('DELETE FROM links WHERE source_node_id = ? OR target_node_id = ?').bind(id, id).run()
  await c.env.DB.prepare('DELETE FROM status_history WHERE node_id = ?').bind(id).run()
  await c.env.DB.prepare('DELETE FROM nodes WHERE id = ?').bind(id).run()

  return c.json({ ok: true })
})

export const onRequest = handle(app)
