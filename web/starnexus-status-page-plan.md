# StarNexus Status Page（星枢状态页）- 项目规划

## 这是什么

这是 StarNexus（星枢）分布式节点监控系统的 **Web 状态页原型**。StarNexus 完整系统包含四个模块（Agent、Server、Web、Bot），但我们先做最酷最直观的部分——Web 地图状态页，部署在 Cloudflare Workers + Pages + D1 上，完全免费。

当前阶段用假数据展示效果，未来 StarNexus 的 Agent/Server 模块完成后，只需把数据源从 D1 假数据切换为真实 API，前端不需要改动。

**最终效果**：打开网页，暗色世界地图上标着所有 VPS 节点，绿色闪烁表示在线，红色表示离线，节点之间有连线表示链路状态，昼夜分界线实时移动。点击节点可以看到 CPU、内存、延迟等详细信息。

---

## 技术栈

| 组件 | 技术 | 说明 |
|------|------|------|
| 后端 API | Cloudflare Workers | Serverless，免费额度 100K requests/天 |
| 数据库 | Cloudflare D1 (SQLite) | 免费 5GB 存储，通过 Workers 访问 |
| 前端 | Cloudflare Pages | 静态站点托管，HTML + JS |
| 地图 | Leaflet + 暗色底图 | 开源免费，插件丰富 |
| 昼夜线 | Leaflet.Terminator 插件 | 地图上实时显示昼夜分界线 |
| 图表 | Chart.js（可选，后续加） | 节点详情里的历史趋势图 |

### 项目结构

```
starnexus-web/
├── worker/                    # Cloudflare Worker（后端 API）
│   ├── src/
│   │   └── index.ts           # Worker 入口，所有 API 路由
│   ├── schema.sql             # D1 数据库建表语句
│   └── wrangler.toml          # Worker 配置（绑定 D1）
├── frontend/                  # 前端静态页面
│   ├── index.html             # 主页面
│   ├── css/
│   │   └── style.css          # 自定义样式（暗色主题、发光效果等）
│   ├── js/
│   │   ├── app.js             # 主入口，初始化地图和数据
│   │   ├── map.js             # 地图相关（Leaflet 初始化、图层管理）
│   │   ├── nodes.js           # 节点渲染（标记、弹窗、发光效果）
│   │   └── links.js           # 节点间连线渲染
│   └── assets/                # 静态资源（如自定义图标）
├── seed-data.sql              # 假数据填充脚本
├── PLAN.md                    # 本规划文件
└── README.md
```

---

## D1 数据库设计

### 表结构

```sql
-- 节点基本信息
CREATE TABLE nodes (
    id TEXT PRIMARY KEY,           -- 节点 ID，如 "tokyo-1"
    name TEXT NOT NULL,            -- 显示名称，如 "东京 DMIT"
    provider TEXT,                 -- 服务商，如 "DMIT", "LisaHost"
    latitude REAL NOT NULL,        -- 纬度
    longitude REAL NOT NULL,       -- 经度
    status TEXT DEFAULT 'unknown', -- online / offline / degraded / unknown
    last_seen INTEGER,             -- 最后一次上报的 UNIX 时间戳
    created_at INTEGER DEFAULT (unixepoch())
);

-- 节点实时指标快照（最新一条数据，供地图展示）
CREATE TABLE node_metrics (
    node_id TEXT PRIMARY KEY REFERENCES nodes(id),
    cpu_percent REAL,
    memory_percent REAL,
    disk_percent REAL,
    bandwidth_up REAL,             -- 上行速率 KB/s
    bandwidth_down REAL,           -- 下行速率 KB/s
    load_avg REAL,                 -- 系统负载
    connections INTEGER,           -- 当前连接数
    uptime_seconds INTEGER,        -- 系统运行时长
    updated_at INTEGER DEFAULT (unixepoch())
);

-- 节点间链路信息
CREATE TABLE links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_node_id TEXT NOT NULL REFERENCES nodes(id),
    target_node_id TEXT NOT NULL REFERENCES nodes(id),
    latency_ms REAL,               -- 延迟（毫秒）
    packet_loss REAL,              -- 丢包率 0-100
    status TEXT DEFAULT 'unknown', -- good / degraded / bad / unknown
    updated_at INTEGER DEFAULT (unixepoch()),
    UNIQUE(source_node_id, target_node_id)
);

-- 状态变更历史（用于告警记录和历史回看）
CREATE TABLE status_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id TEXT NOT NULL REFERENCES nodes(id),
    old_status TEXT,
    new_status TEXT NOT NULL,
    reason TEXT,                    -- 如 "疑似被墙", "服务进程挂了"
    created_at INTEGER DEFAULT (unixepoch())
);
```

### 假数据（模拟 Hoshino 的真实 VPS 节点）

```sql
-- 节点数据
INSERT INTO nodes (id, name, provider, latitude, longitude, status, last_seen) VALUES
('tokyo-dmit', '东京 DMIT', 'DMIT', 35.6762, 139.6503, 'online', unixepoch()),
('osaka-dmit', '大阪 DMIT', 'DMIT', 34.6937, 135.5023, 'online', unixepoch()),
('hk-dmit', '香港 DMIT', 'DMIT', 22.3193, 114.1694, 'online', unixepoch()),
('la-dmit', '洛杉矶 DMIT', 'DMIT', 34.0522, -118.2437, 'online', unixepoch()),
('sj-lisahost', '圣何塞 LisaHost', 'LisaHost', 37.3382, -121.8863, 'degraded', unixepoch()),
('sg-node', '新加坡节点', 'Other', 1.3521, 103.8198, 'offline', unixepoch());

-- 指标数据
INSERT INTO node_metrics (node_id, cpu_percent, memory_percent, disk_percent, bandwidth_up, bandwidth_down, load_avg, connections, uptime_seconds) VALUES
('tokyo-dmit', 12.5, 45.2, 33.0, 150.3, 2048.7, 0.35, 128, 2592000),
('osaka-dmit', 8.1, 38.7, 28.5, 80.1, 1200.4, 0.22, 76, 1728000),
('hk-dmit', 23.4, 62.1, 45.0, 220.5, 3500.2, 0.78, 256, 864000),
('la-dmit', 15.7, 51.3, 40.2, 180.9, 2800.6, 0.45, 192, 3456000),
('sj-lisahost', 78.3, 85.6, 72.1, 50.2, 500.1, 2.35, 512, 432000),
('sg-node', 0, 0, 0, 0, 0, 0, 0, 0);

-- 链路数据
INSERT INTO links (source_node_id, target_node_id, latency_ms, packet_loss, status) VALUES
('tokyo-dmit', 'osaka-dmit', 8.5, 0.0, 'good'),
('tokyo-dmit', 'hk-dmit', 45.2, 0.1, 'good'),
('tokyo-dmit', 'la-dmit', 120.8, 0.5, 'good'),
('tokyo-dmit', 'sj-lisahost', 135.4, 2.3, 'degraded'),
('hk-dmit', 'sg-node', 999.0, 100.0, 'bad'),
('la-dmit', 'sj-lisahost', 12.3, 0.0, 'good');
```

---

## Worker API 设计

所有 API 以 `/api/` 为前缀。

### 公开接口（无需认证）

```
GET  /api/nodes              → 获取所有节点 + 最新指标（地图展示用）
GET  /api/nodes/:id          → 获取单个节点详情
GET  /api/links              → 获取所有链路信息
GET  /api/status             → 总览：在线/离线/异常各几个
GET  /api/history/:id        → 某节点的状态变更历史
```

### 管理接口（需要 Bearer Token 认证，供未来 Agent 上报用）

```
POST /api/report             → Agent 上报数据（更新 node_metrics + 状态）
POST /api/nodes              → 添加新节点
PUT  /api/nodes/:id          → 更新节点信息
DELETE /api/nodes/:id        → 删除节点
```

### 响应格式示例

```json
// GET /api/nodes
{
  "nodes": [
    {
      "id": "tokyo-dmit",
      "name": "东京 DMIT",
      "provider": "DMIT",
      "latitude": 35.6762,
      "longitude": 139.6503,
      "status": "online",
      "last_seen": 1740700000,
      "metrics": {
        "cpu_percent": 12.5,
        "memory_percent": 45.2,
        "disk_percent": 33.0,
        "bandwidth_up": 150.3,
        "bandwidth_down": 2048.7,
        "load_avg": 0.35,
        "connections": 128,
        "uptime_seconds": 2592000
      }
    }
  ]
}

// GET /api/status
{
  "total": 6,
  "online": 4,
  "degraded": 1,
  "offline": 1,
  "unknown": 0
}
```

---

## 前端设计要求

### 整体风格

- **暗色主题**：深色背景（#0a0a1a 或类似深蓝黑色），科技感
- **配色方案**：
  - 在线节点：翠绿色 (#00ff88) 带发光效果
  - 异常节点：琥珀色 (#ffaa00) 带缓慢脉冲
  - 离线节点：红色 (#ff4444) 无发光
  - 未知节点：灰色 (#666666)
  - 链路连线：根据延迟从绿 → 黄 → 红渐变
- **字体**：等宽字体（如 JetBrains Mono、Fira Code）营造终端/运维感

### 地图

- 使用 Leaflet，底图选用 CartoDB Dark Matter 或 Stadia Dark 等暗色瓦片
- 地图底图 URL 示例：`https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png`
- 初始视图：大致以太平洋为中心（能同时看到东亚和北美），zoom level 约 2-3
- 启用 Leaflet.Terminator 插件显示昼夜分界线，半透明蓝色覆盖夜晚区域
- 昼夜线每分钟更新一次位置

### 节点标记

- 不用 Leaflet 默认图钉，改用**自定义 CSS 圆形标记**
- 在线节点：圆点 + CSS box-shadow 做发光/呼吸效果（CSS animation: 2s 周期明暗交替）
- 异常节点：圆点 + 较慢的脉冲效果
- 离线节点：暗淡圆点，无动画
- 鼠标悬停：显示节点名称 tooltip
- 点击节点：弹出详情卡片（Leaflet popup 自定义样式），显示：
  - 节点名称 + 服务商
  - 状态标签（绿/黄/红色 badge）
  - CPU / 内存 / 磁盘 使用率（进度条样式）
  - 带宽（上行/下行）
  - 系统负载
  - 在线时长（转为 "30天12小时" 这种友好格式）
  - 最后上报时间

### 节点间连线

- 使用 Leaflet Polyline 绘制
- 线条颜色根据延迟：< 50ms 绿色，50-150ms 黄色，> 150ms 红色
- 线条粗细：正常 2px，bad 状态 1px 虚线
- 鼠标悬停连线：显示延迟和丢包率
- bad 状态的连线可以加虚线动画效果

### 顶部状态栏

- 固定在地图上方，半透明暗色背景
- 左侧：项目名 "StarNexus 星枢"
- 右侧：状态统计 "● 4 Online  ● 1 Degraded  ● 1 Offline"
- 可以加一个小小的刷新按钮和"最后更新: xx秒前"

### 响应式

- 桌面端为主要目标
- 移动端能看就行，不需要完美适配

---

## 开发步骤

### Step 1：初始化项目 + D1 数据库

```bash
# 1. 创建项目目录
mkdir starnexus-web && cd starnexus-web

# 2. 初始化 Worker
cd worker
pnpm init
pnpm add wrangler --save-dev

# 3. 配置 wrangler.toml（需要绑定 D1）
# 4. 创建 D1 数据库
wrangler d1 create starnexus-db

# 5. 执行建表 SQL
wrangler d1 execute starnexus-db --local --file=schema.sql

# 6. 填入假数据
wrangler d1 execute starnexus-db --local --file=../seed-data.sql
```

wrangler.toml 示例：
```toml
name = "starnexus-api"
main = "src/index.ts"
compatibility_date = "2024-12-01"

[[d1_databases]]
binding = "DB"
database_name = "starnexus-db"
database_id = "<创建后填入>"
```

### Step 2：Worker API 开发

在 `worker/src/index.ts` 中实现所有 API 路由。Worker 代码很简洁，用标准 fetch handler：

```typescript
export interface Env {
  DB: D1Database;
  API_TOKEN: string; // 用于管理接口的认证
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);
    // 路由分发...
    // CORS headers...
  }
}
```

要点：
- 所有响应加 CORS headers（前端可能跨域访问）
- GET 接口不需要认证
- POST/PUT/DELETE 接口检查 `Authorization: Bearer <token>`
- SQL 用参数化查询（`env.DB.prepare(...).bind(...)`），防止注入

### Step 3：前端开发（重头戏）

1. 先搭骨架：index.html 引入 Leaflet CSS/JS、Terminator 插件、自定义 CSS/JS
2. 初始化暗色地图 + 昼夜线
3. 从 Worker API 拉节点数据
4. 渲染节点标记 + 发光效果
5. 渲染连线
6. 实现点击弹出详情卡片
7. 顶部状态栏

### Step 4：本地联调

```bash
# Worker 本地运行
cd worker && wrangler dev

# 前端用任意静态服务器
cd frontend && npx serve .
```

### Step 5：部署上线

```bash
# 部署 Worker
cd worker && wrangler deploy

# 部署前端到 Pages
# 方式一：wrangler pages deploy frontend/
# 方式二：连接 GitHub 仓库自动部署
```

---

## CDN 资源引用

前端引用的外部资源（从 CDN 加载，不需要 npm install）：

```html
<!-- Leaflet -->
<link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css" />
<script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js"></script>

<!-- Leaflet.Terminator（昼夜线） -->
<script src="https://unpkg.com/@joergdietrich/leaflet.terminator@1.0.0/L.Terminator.js"></script>

<!-- Chart.js（可选，后续详情图表用） -->
<script src="https://cdn.jsdelivr.net/npm/chart.js@4"></script>

<!-- JetBrains Mono 字体 -->
<link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@300;400;500;700&display=swap" rel="stylesheet">
```

---

## 注意事项

1. **D1 的限制**：
   - 免费版：5GB 存储，5M reads/天，100K writes/天
   - D1 目前不支持 Worker 之外的直连，所有数据操作必须通过 Worker
   - D1 不支持实时推送，前端需要轮询 API

2. **Worker 的限制**：
   - 免费版：100K requests/天，CPU 时间 10ms/request
   - 足够这个项目使用

3. **安全**：
   - 公开 GET 接口任何人都能访问（这是故意的，状态页就是给人看的）
   - 管理接口（POST/PUT/DELETE）必须验证 Token
   - Token 存在 Worker 的环境变量（Secrets）里，不要硬编码
   - 所有 SQL 使用参数化查询

4. **前端性能**：
   - 节点数量不多（<20），性能不是问题
   - 数据每 30 秒轮询刷新一次即可
   - 昼夜线每 60 秒更新一次

5. **将来扩展**：
   - 等 StarNexus Agent 开发完成后，Agent 通过 POST /api/report 上报真实数据
   - 前端完全不需要改，只是数据从假的变成真的
   - 可以后续加上 Chart.js 历史趋势图、WebSocket 实时推送等
