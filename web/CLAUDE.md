# StarNexus Status Page - Claude Code 开发指令

## 项目简介

StarNexus（星枢）状态页——VPS 节点监控系统的 Web 地图可视化原型。暗色世界地图上显示所有节点状态，发光呼吸动画，昼夜分界线实时移动，点击查看详情。

当前阶段使用假数据，部署在 Cloudflare Pages（前端 + Functions API + D1 数据库），完全免费。

## 参考文档（开工前必读）

项目根目录下有两份规划文档：

- `starnexus-plan.md` — 完整系统架构（四模块总览、功能清单、技术选型）
- `starnexus-status-page-plan.md` — 本次开发的详细规划（D1 表结构、API 设计、前端设计要求）

**两份文档有重叠，以 `starnexus-status-page-plan.md` 为准。** 但注意以下内容被本文件覆盖：

- 项目结构 → 用本文件的（Pages Functions 统一架构，不是 Worker + Pages 分离）
- 路由方案 → 用 Hono（不是原生 fetch handler）
- 假数据时间戳 → API 层动态计算（不是写死的 `unixepoch()`）

---

## 技术决策（已定，不要更改）

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 部署架构 | Cloudflare Pages Functions（统一部署） | 同域零 CORS，一条命令部署 |
| API 路由 | Hono | Cloudflare 官方推荐，代码干净 |
| 数据库 | Cloudflare D1 (SQLite) | 免费，通过 Functions 访问 |
| 前端框架 | 无（纯 HTML + JS + CSS） | 不需要 React/Vue |
| 前端依赖 | CDN 引入 | 不需要 npm 管前端 |
| 地图库 | Leaflet 1.9.4 + CartoDB Dark Matter 底图 | 开源免费 |
| 昼夜线 | Leaflet.Terminator | 确认与 Leaflet 1.9.4 兼容后使用 |

---

## 项目结构

```
starnexus-web/
├── functions/                    # Cloudflare Pages Functions（后端 API）
│   └── api/
│       └── [[route]].ts          # Hono catch-all，处理所有 /api/* 请求
├── public/                       # 静态前端文件（Pages 自动托管）
│   ├── index.html
│   ├── css/
│   │   └── style.css
│   ├── js/
│   │   ├── app.js                # 主入口：数据拉取、轮询刷新、初始化协调
│   │   ├── map.js                # Leaflet 初始化、暗色底图、昼夜线
│   │   ├── nodes.js              # 节点标记：自定义圆形、发光动画、详情弹窗
│   │   └── links.js              # 连线渲染：颜色渐变、虚线动画、tooltip
│   └── assets/                   # 静态资源（如有）
├── schema.sql                    # D1 建表语句
├── seed-data.sql                 # 假数据填充
├── wrangler.toml                 # Pages 配置 + D1 绑定
├── package.json
├── tsconfig.json
├── CLAUDE.md                     # 本文件
├── starnexus-plan.md             # 参考文档
├── starnexus-status-page-plan.md # 参考文档
└── README.md
```

---

## wrangler.toml

```toml
name = "starnexus-web"
pages_build_output_dir = "public"
compatibility_date = "2024-12-01"

[[d1_databases]]
binding = "DB"
database_name = "starnexus-db"
database_id = "<TODO:部署时手动填入>"

[vars]
API_TOKEN = "<TODO:本地开发用，生产环境用 wrangler secret>"
```

---

## package.json 依赖

```json
{
  "name": "starnexus-web",
  "private": true,
  "scripts": {
    "dev": "wrangler pages dev public --d1=DB",
    "db:init": "wrangler d1 execute starnexus-db --local --file=schema.sql",
    "db:seed": "wrangler d1 execute starnexus-db --local --file=seed-data.sql",
    "db:reset": "npm run db:init && npm run db:seed",
    "deploy": "wrangler pages deploy public"
  },
  "devDependencies": {
    "wrangler": "^3",
    "@cloudflare/workers-types": "^4",
    "hono": "^4",
    "typescript": "^5"
  }
}
```

---

## tsconfig.json

```json
{
  "compilerOptions": {
    "target": "ESNext",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "strict": true,
    "types": ["@cloudflare/workers-types"]
  },
  "include": ["functions/**/*.ts"]
}
```

---

## API 实现 (functions/api/[[route]].ts)

使用 Hono + `hono/cloudflare-pages`：

```typescript
import { Hono } from 'hono'
import { handle } from 'hono/cloudflare-pages'

type Bindings = {
    DB: D1Database
    API_TOKEN: string
}

const app = new Hono<{ Bindings: Bindings }>().basePath('/api')

// 公开接口注册在这里...
// 管理接口注册在这里...

export const onRequest = handle(app)
```

### 公开接口（无需认证）

| 路由 | 说明 |
|------|------|
| `GET /api/nodes` | 所有节点 + 最新指标（JOIN nodes 和 node_metrics） |
| `GET /api/nodes/:id` | 单个节点详情 |
| `GET /api/links` | 所有链路信息 |
| `GET /api/status` | 总览统计：online/degraded/offline/unknown 各几个 |
| `GET /api/history/:id` | 某节点的状态变更历史 |

### 管理接口（Bearer Token 认证）

| 路由 | 说明 |
|------|------|
| `POST /api/report` | Agent 上报数据 |
| `POST /api/nodes` | 添加新节点 |
| `PUT /api/nodes/:id` | 更新节点信息 |
| `DELETE /api/nodes/:id` | 删除节点 |

管理接口需要中间件检查 `Authorization: Bearer <token>`，token 与 `env.API_TOKEN` 比对。

### 假数据时间戳动态化（重要！）

`/api/nodes` 返回数据时，**不要直接返回数据库中的 `last_seen` 和 `uptime_seconds`**，而是动态计算：

```
对于 online 节点：
  last_seen = 当前 UNIX 时间戳 - 随机(5~30)秒
  uptime_seconds = 保持数据库原值（大数字，看起来合理）

对于 degraded 节点：
  last_seen = 当前 UNIX 时间戳 - 随机(10~60)秒

对于 offline 节点：
  last_seen = 当前 UNIX 时间戳 - 随机(3600~86400)秒（1小时~1天前）
  uptime_seconds = 0

node_metrics.updated_at 同理，跟 last_seen 保持一致。
```

这样无论什么时候打开 demo，数据看起来都是活的。

### 响应格式

参照 `starnexus-status-page-plan.md` 中的 "响应格式示例" 部分。`/api/nodes` 要把 metrics 嵌套在每个 node 对象里返回，不要平铺。

### API 实现要点

- SQL 全部参数化查询（`env.DB.prepare(...).bind(...)`）
- 管理接口的 token 验证用 Hono middleware
- 不需要 CORS 处理（Pages Functions 与前端同域）

---

## D1 数据库

### 建表和假数据

直接使用 `starnexus-status-page-plan.md` 中的 SQL。将建表语句放在 `schema.sql`，假数据放在 `seed-data.sql`。

### 本地初始化

```bash
pnpm install
pnpm run db:reset   # 建表 + 填假数据
pnpm run dev         # 启动本地开发服务器
```

---

## 前端设计规范

### 所有视觉细节严格参照 `starnexus-status-page-plan.md` 的 "前端设计要求" 部分

以下是关键约束的重申和补充：

### CDN 资源

```html
<!-- Leaflet 1.9.4 -->
<link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css" />
<script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js"></script>

<!-- Leaflet.Terminator（昼夜线）-->
<!-- 注意：如果 @1.0.0 与 Leaflet 1.9.4 有兼容性问题，尝试不带版本号引入 -->
<script src="https://unpkg.com/@joergdietrich/leaflet.terminator/L.Terminator.js"></script>

<!-- JetBrains Mono -->
<link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@300;400;500;700&display=swap" rel="stylesheet">
```

### 配色（不要改）

| 用途 | 颜色 |
|------|------|
| 页面背景 | `#0a0a1a` |
| online 节点 | `#00ff88` + 发光 + 2s 呼吸动画 |
| degraded 节点 | `#ffaa00` + 3s 慢脉冲 |
| offline 节点 | `#ff4444`，暗淡，无动画 |
| unknown 节点 | `#666666`，无动画 |
| 链路 <50ms | `#00ff88` |
| 链路 50-150ms | `#ffaa00` |
| 链路 >150ms | `#ff4444` |

### 地图配置

- 底图：`https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png`
- 初始视图：`center: [20, 140], zoom: 2`（太平洋为中心，同时看到东亚和北美）
- 昼夜线：Leaflet.Terminator，半透明 `rgba(0, 0, 40, 0.3)` 覆盖夜晚，每 60 秒更新
- Leaflet 所有默认样式（popup、tooltip、zoom 控件、attribution）全部覆盖为暗色主题

### 节点标记

- 使用 `L.divIcon` 自定义 CSS 圆形，**不用默认图钉**
- 圆形直径 12-16px
- 发光效果用 CSS `box-shadow` + `@keyframes` 动画
- hover 显示暗色 tooltip（节点名称）
- 点击弹出自定义暗色 popup，内容：
  - 节点名称 + 服务商 + 状态 badge
  - CPU / 内存 / 磁盘：暗色底 + 彩色填充的进度条
  - 带宽（格式化为 KB/s 或 MB/s）
  - 负载、连接数
  - 在线时长（友好格式："30天 12小时"）
  - 最后上报（相对时间："3分钟前"）

### 连线

- `L.polyline`，颜色根据延迟阈值
- 正常 2px 实线，bad 状态 1px 虚线（`dashArray: '8, 8'`）
- hover 显示 tooltip："延迟: 45.2ms | 丢包: 0.1%"

### 顶部状态栏

- `position: fixed`，半透明暗色 + `backdrop-filter: blur(10px)`
- 左侧："StarNexus 星枢"
- 右侧：`● 4 Online  ● 1 Degraded  ● 1 Offline`
- 最右侧：刷新按钮 + "最后更新: xx秒前"

### 数据流

- 页面加载时调用 `/api/nodes`、`/api/links`、`/api/status`
- 每 30 秒轮询刷新
- 昼夜线每 60 秒更新
- "最后更新" 秒数实时递增

### 前端 API 调用

因为 Functions 和前端同域，直接用相对路径：

```javascript
const API_BASE = '/api'
```

### 错误处理

API 请求失败时：
- 不要白屏
- 状态栏显示"⚠ 连接中断"或类似提示
- 地图保持显示，节点标记保留最后已知状态
- 下次轮询如果恢复，自动清除错误提示

---

## 开发顺序

1. 初始化项目骨架（目录结构、package.json、wrangler.toml、tsconfig.json）
2. 创建 schema.sql + seed-data.sql
3. 实现 `functions/api/[[route]].ts`（所有 API 路由）
4. 创建 `public/index.html`（引入 CDN 资源、基础 HTML 结构）
5. 实现 `public/css/style.css`（暗色主题全套样式、动画、Leaflet 样式覆盖）
6. 实现 `public/js/map.js`（Leaflet 初始化 + 底图 + 昼夜线）
7. 实现 `public/js/nodes.js`（节点渲染 + 动画 + 弹窗）
8. 实现 `public/js/links.js`（连线渲染 + tooltip）
9. 实现 `public/js/app.js`（数据拉取 + 渲染协调 + 轮询 + 错误处理）
10. 顶部状态栏
11. 整体测试和调整
12. README.md

---

## 验证清单

开发完成后，用以下步骤验证：

```bash
pnpm run db:reset
pnpm run dev
# 浏览器打开 http://localhost:8788
```

检查：
- [ ] 地图正常显示暗色底图
- [ ] 6 个节点标记出现在正确位置
- [ ] online 节点有绿色发光呼吸动画
- [ ] degraded 节点有黄色慢脉冲
- [ ] offline 节点暗淡无动画
- [ ] 昼夜线可见且位置合理
- [ ] 点击节点弹出详情卡片，数据正确
- [ ] 进度条颜色根据使用率变化
- [ ] 节点间连线颜色正确
- [ ] hover 连线显示延迟和丢包
- [ ] 顶部状态栏统计数字正确
- [ ] "最后更新" 秒数在递增
- [ ] 30 秒后数据自动刷新
- [ ] 手动断开网络后不白屏
- [ ] `/api/nodes` 返回的 last_seen 是近期时间（不是几周前）

---

## 禁止事项

- 不要引入 React/Vue/任何前端框架
- 不要用 npm 管前端依赖
- 不要改配色方案或动画参数
- 不要把 Pages Functions 改回独立 Worker
- 不要用 itty-router（用 Hono）
- 不要在 seed-data.sql 里改节点数据（那些是模拟 Hoshino 真实的 VPS 节点）
- 不要 over-engineer，这是假数据展示原型
