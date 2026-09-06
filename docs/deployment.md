# JuanNiang-Neo 部署与调试指南

本文档面向运维和开发者，覆盖部署模式、环境变量、构建流程、健康检查、日志排查和常见故障处理。

> 配置以**环境变量**为准，`config/config.yaml` 仅为参考文档（二进制不读它）。运行时模块配置（Provider/MCP/Prompt/Skill/ACL/ReplyStrategy/T2I/Sandbox/Webhook/CronJob）存 Postgres，通过 Web 面板热切换。

## 部署模式

| 模式 | 用途 | 命令 |
|------|------|------|
| **Dev** | Vite `:3000` 热更新 + Go `:8090` API | `make dev` |
| **本地裸跑** | 跑已构建 binary，单端口服务 `web/dist` | `make build && make run` |
| **Docker Compose** | postgres + redis + app 全栈 | `make docker-up` |
| **预构建镜像** | 直接 pull `ghcr.io/juanniangdev/juan` | 见 README |

## 环境变量

|.env 变量|默认|说明|
|----------|----|----|
| `WEB_DIR` | `web/dist` | 前端构建产物目录；容器内 `/app/web/dist`；空 → 引导提示页|
| `API_ADDR` | `:8090` | Hertz Web API + 仪表板监听地址|
| `JWT_SECRET` | `change-me-in-production` | JWT HMAC 密钥；**务必修改**|
| `OB_PORT` | `8081` | OneBot11 反向 WS 服务监听端口|
| `OB_TOKEN` | (空) | OneBot11 客户端访问令牌；空=不鉴权|
| `OB_ADMINS` | (空) | 逗号分隔的 admin QQ（env fallback；运行时实际从 DB `AdminQQNumbers` 读取）|
| `DB_HOST` | `postgres` (compose) / `localhost` | |
| `DB_PORT` | `5432` | |
| `DB_USER` | `postgres` | |
| `DB_PASSWORD` | `postgres` | |
| `DB_NAME` | `juan` | |
| `REDIS_ADDR` | `redis:6379` (compose) / `localhost:6379` | |
| `REDIS_PASSWORD` | `root` | |
| `REDIS_DB` | `0` | Redis 逻辑库索引|
| `REDIS_PREFIX` | `juan:` | ⚠ 未在 `.env.example` 但 `cache.NewCache` 实际读取|
| `LTM_RECALL_MODE` | `semantic` | 长期记忆对话召回模式：`semantic`=按消息语义召回（pg_trgm 倒排候选 + similarity 排序，空候选回退最近）；`recent`=旧行为（最近 N 条）|
| `IMG_DIR` | `data/imgs` | 图床图片存储目录（`imgstore`；元数据在 DB `image_assets`）|
| `T2I_BASE_URL` | (空/注释) | ⚠ 仅文档；运行时实际从 DB `t2i_configs` 读取|
| `SANDBOX_BASE_URL` | (空/注释) | ⚠ 同上，从 DB `sandbox_configs` 读取|
| `SANDBOX_API_KEY` | (空/注释) | ⚠ 同上|

> 注意：`T2I_BASE_URL` / `SANDBOX_API_KEY` 等是文档性 env，**真正生效**的配置在 DB（启动时 `loadT2IFromDB`/`loadSandboxFromDB` 读取并构建客户端）。首次启动 DB 无配置时自动 `InitConfig` 建默认行，前端可编辑。

## 开发环境配置（dev.yaml）

本地开发时可用 `dev.yaml` 配置基础设施连接端点，避免每次手动设置环境变量：

```bash
cp dev.yaml.example dev.yaml   # 复制并按需修改
make run                        # 自动读取 dev.yaml
make run-debug                  # 自动读取 dev.yaml + debug 模式
```

优先级：**环境变量 > dev.yaml > 内置默认值**。`dev.yaml` 不存在时程序正常启动（使用环境变量或内置默认值）。`make run` / `make run-debug` 通过 `-dev-config` 参数传入，二进制本身不硬编码该路径。

## 端口约定

| 端口 | 用途 |
|------|------|
| `8090` | Web API + 仪表板（前端 SPA 兜底同端口） |
| `8081` | OneBot11 反向 WebSocket 服务（QQ 机器人框架连接）|
| `8091` | Webhook HTTP 服务（独立端口，按 `WebhookConfig.Port`，默认关闭）|
| `3000` | Vite 开发服务器（仅 dev）|

## 构建流程

### 本地构建

```bash
# 全量: 先构建前端再产二进制, 产物 bin/juan-niang-neo + web/dist
make build

# 仅构建 Go 后端 (依赖 web/dist 已存在)
make build-go

# 仅前端
make web-install        # 首次安装依赖
make web-build          # typecheck + vite build

# 开发: Vite(:3000) + Go(:8090) 并行
make dev

# 仅跑后端 go run, 自动读取 dev.yaml, 前端走 web/dist
make run

# Debug 模式：自动读取 dev.yaml + pprof (:6060) + Debug 级别日志
make run-debug

# 综合检查 (go vet + 前端 typecheck)
make lint
```

二进制构建参数：`CGO_ENABLED=0 -trimpath -ldflags "-s -w"`，无 `//go:embed web/dist`（前端磁盘服务，便于只换前端不重编 Go）。

### Docker 构建（仓库自带）

`deployments/Dockerfile` 三阶段：

1. **`web-builder`** (`node:20-alpine`)：`npm ci || npm install` → `npm run build` → `dist/`
2. **`go-builder`** (`golang:1.25-alpine`)：`go mod download` → `go build -trimpath -ldflags "-s -w" -o /juan-niang-neo ./cmd/server/`
3. **runtime** (`alpine:latest`)：装 `ca-certificates tzdata wget`，以 root 运行，复制 binary 与 `web/dist`，`WEB_DIR=/app/web/dist` `TZ=Asia/Shanghai`，`EXPOSE 8081 8090`，`HEALTHCHECK` `wget -qO- http://127.0.0.1:8090/health`

另有 `Dockerfile.cn` 使用国内镜像加速。

### 运行时数据持久化（Docker）

容器内 `/app/data` 整体由 compose bind-mount 到仓库 `data/` 目录，跨升级/重建保留：

| 路径 | 内容 | 丢失影响 |
|------|------|----------|
| `data/pluggins/` | Lua 插件（含启动时自动写入的 SDK 与 system 插件） | 插件丢失 |
| `data/imgs/` | 图床图片 | 图片丢失 |
| `data/plugin_store.json` | 插件商店配置（镜像源选择 / 自定义镜像 / 仓库地址） | 商店配置重置为默认 |

> ⚠️ 首次启动前 `mkdir -p data && chmod 777 data`（当前镜像以 root 运行；改为非 root 用户后需按用户赋权）。

## 健康检查

- `GET /health` 二级域名/api 均可：`{"status":"ok"}`，无需鉴权
- Docker `HEALTHCHECK` 调用的就是它（30s 间隔）
- `GET /api/v1/overview` 含 `t2i_active`/`t2i_healthy`/`sandbox_active`/`sandbox_healthy`（需 token，前端仪表板调用）
- `GET /api/v1/t2i/health` / `GET /api/v1/sandbox/health` 实时探活

## Prometheus 监控

> 可观测性体系总览（指标/日志/Loki 统计/追踪 + 联合排障 + 告警）见 [observability.md](observability.md)。

### 指标端点

`GET /metrics`（与 `/health` 同级，**无需 JWT**），输出 Prometheus 文本格式指标（前缀 `juanniang_`）：

| 指标组 | 说明 |
|--------|------|
| `juanniang_events_total` / `juanniang_messages_total` | 事件与消息吞吐（按 post_type/message_type） |
| `juanniang_message_dedup_dropped_total` / `juanniang_message_blocked_total` / `juanniang_message_dropped_total` | 去重丢弃 / 黑名单拦截 / 噪音(irrelevant)-静默(silenced)丢弃 |
| `juanniang_chat_replies_total` / `juanniang_chat_stats_dropped_total` | Agent 回复发送量（全局趋势）/ 统计事件丢弃（Loki 通道积压，恒为 0 为正常） |
| `juanniang_agent_loops_total` / `_active` / `_duration_seconds` | Agent 循环完成结果（ok/error/timeout）、活跃数、耗时直方图 |
| `juanniang_agent_concurrency_in_use` / `_waits_total` / `_wait_seconds` | 全局并发槽占用与令牌等待 |
| `juanniang_llm_requests_total` / `_tokens_total` / `_latency_seconds` | LLM 调用、Token 消耗（按 agent/review 用途）、延迟 |
| `juanniang_groupmgr_violations_total` / `_detections_total` / `_rag_score` / `_llm_reviews_total` / `_spam_total` | 群管理处罚、判定流水（rag/keyword × punish/review/pass）、RAG 分数分布、审核结果、刷屏 |
| `juanniang_rag_search_latency_seconds` / `_errors_total` | RAG 检索延迟与降级失败 |
| `juanniang_plugins_loaded` / `juanniang_plugin_hook_errors_total` / `_duration_seconds` | 插件数、钩子错误（按 plugin+hook）、钩子耗时 |
| `juanniang_http_requests_total` / `_request_duration_seconds` | API 请求数/耗时（路由模板化 path） |
| `juanniang_inventory{resource=...}` | 业务库存（知识/记忆/会话/ChatArea/CronJob 等条目数，60s TTL 缓存） |
| `juanniang_external_health{service=...}` | rag/t2i/sandbox/redis 健康（未配置的服务不输出） |
| `go_*` / `process_*` | Go 运行时（goroutine/内存/GC）与进程指标（自带） |

> 设计约束：所有标签均为低基数（不使用群号/QQ 号等高基数标签，按群分析请用 Web 统计面板）；gauge 由 scrape 时实时读取（DB 数据经 TTL 缓存，scrape 不打库）。

### Prometheus 配置示例

```yaml
scrape_configs:
  - job_name: juanniang-neo
    scrape_interval: 15s
    static_configs:
      - targets: ['127.0.0.1:8090']
    metrics_path: /metrics
```

> ⚠️ `/metrics` 无鉴权，公网暴露时建议在部署层（防火墙/反代）限制来源 IP。

### Grafana 建议面板

1. **消息吞吐**：`sum(rate(juanniang_messages_total[5m]))` 与丢弃率（dedup/blocked/dropped 占比）
2. **Agent 健康**：`juanniang_agent_loops_active`、循环耗时 P95（`histogram_quantile(0.95, ...)`）、`outcome="error|timeout"` 速率
3. **LLM 成本**：`sum(rate(juanniang_llm_tokens_total[1h])) by (phase)` + 错误率
4. **群管理**：处罚速率、RAG 直罚比例（`detections_total{verdict="punish",path="rag"}` / 总量）、`juanniang_groupmgr_rag_score` 分布（调阈值依据）
5. **外部服务矩阵**：`juanniang_external_health`（0/1）
6. **运行时**：`go_goroutines` 趋势（goroutine 泄漏）、`go_memstats_heap_alloc_bytes`

## 链路追踪（Grafana Tempo）

机器人对**每条事件**生成一个 trace（根 span `process_event`），下游各阶段（群管理检测/RAG 核实/处罚、插件派发、参与窗口、Agent ReAct 循环、LLM 调用、工具执行、RAG 调用、审核闸门、回复发送）均为子 span——在 Grafana Tempo 里可查看单条事件处理的**全流程瀑布图**，直接定位最慢/失败的阶段。

### 环境变量

| 变量 | 默认 | 说明 |
|---|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | 空（禁用） | OTLP 上报地址（如 `http://tempo:4318`）；留空 = no-op 零开销 |
| `OTEL_SERVICE_NAME` | `juan-niang-neo` | 服务名（Tempo 按 `service.name` 过滤） |
| `OTEL_TRACE_SAMPLE_RATIO` | `1.0` | 采样率 0~1；热聊群量大可调低（如 `0.1`） |
| `OTEL_TRACE_CAPTURE_CONTENT` | `true` | 根 span 是否记录消息内容（截断 100 字符）；敏感环境设 `false` |

### 部署（docker compose）

`deployments/docker-compose.yaml` 已内置 Tempo 服务（`grafana/tempo:latest`，本地磁盘存储）：

```bash
docker compose up -d --build
# Tempo:  http://localhost:3200（Grafana 数据源）
# OTLP:   4318（机器人自动上报，无需额外配置）
```

Grafana（独立部署或复用现有实例）添加数据源：**Tempo → http://tempo:3200**（Docker 网络内）或 `http://localhost:3200`（宿主机）。

### 使用方式（Grafana Explore）

1. 数据源选 **Tempo**，按 `service.name=juan-niang-neo` + 时间范围搜索 trace
2. 按属性精确定位：`process_event.group_id="123456"` / `process_event.user_id` / `process_event.message_content`（内容为截断 100 字符，**精确匹配**，不支持模糊搜索）
3. 点击 trace → 瀑布图：`llm.call` 最长通常说明模型慢，`tool.execute` 长说明工具慢，`status=error` 的 span 直接显示失败原因
4. 排障习惯：找到一条消息 → 看 `agent.handle` 总耗时 → 逐段下钻各阶段耗时

> 提示：Tempo 的属性搜索是精确值匹配；按内容模糊检索请用 Web 面板日志页（Hub）或部署 Loki。

## 群消息/回复统计（Loki + Promtail）

用于统计**每个群的消息与对应回复**，与主日志 pipeline 完全隔离：

- 事件由 `internal/agent/stats` 以 **NDJSON 逐行**追加到独立文件（默认 `data/stats/chat-events.log`），**不走 slog / 主日志**（避免 ANSI 颜色、Web Hub 污染）
- 文件轮转由 lumberjack 负责（按大小 + 保留份数 + gzip 压缩）；Promtail 用 `__path__` 通配 + inode 跟随无缝衔接
- 埋点：
  - 群消息在事件循环 Phase 0（去重后）记录 `direction=msg`
  - Agent 回复在 `sendReply` 记录 `direction=reply, source=agent`（`reply_to` 携带触发消息原文）
  - 群管理处罚/刷屏/复读回复在 `replyGroup`/`replyGroupImage`/`checkCopySpam` 记录 `direction=reply, source=groupmgr`

### 启用

`dev.yaml` 的 `stats` 块或环境变量（`STATS_ENABLED` / `STATS_PATH` / `STATS_MAX_SIZE_MB` / `STATS_MAX_BACKUPS` / `STATS_MAX_AGE_DAYS` / `STATS_QUEUE_SIZE`）：

```yaml
stats:
  enabled: true
  path: data/stats/chat-events.log
  max_size_mb: 100
  max_backups: 10
  max_age_days: 7
  queue_size: 1024
```

### 部署（docker compose）

`deployments/docker-compose.yaml` 已内置 Loki + Promtail（与 Tempo 并存）：

```bash
docker compose up -d --build
# Loki:     http://localhost:3100（Grafana 数据源）
# Promtail: 自动采集 /app/data/stats/chat-events*.log（只读挂载 ../data）
# 机器人:   STATS_ENABLED=true 已默认开启，写入 /app/data/stats/chat-events.log
```

Grafana（独立部署或复用现有实例）添加数据源：**Loki → http://loki:3100**（Docker 网络内）或 `http://localhost:3100`（宿主机）。

### 采集与查询

Promtail 配置样例见 `deployments/promtail-chat-stats.yaml`（独立 job，与主日志采集共存）：

```logql
# 每群消息/回复数（按方向拆分）
sum by (group_id, direction) (count_over_time({job="juanniang"}[1h]))

# 每群按来源统计机器人输出（agent 回复 / groupmgr 处罚警告）
sum by (group_id, source) (count_over_time({job="juanniang", direction="reply"}[1h]))

# 某群最近消息与对应回复
{job="juanniang", group_id="123456"} | json | line_format "{{.ts}} {{.direction}} {{.source}} {{.text}}"

# 按群查询 Agent 回复原文（含触发消息 reply_to）
{job="juanniang", group_id="123456", direction="reply"} | json | line_format "{{.text}}  <-  {{.reply_to}}"
```

> 集群大时建议为群消息/回复统计单独建 Grafana 面板（LogQL 聚合 + `rate` 时序），并给 `{job="juanniang"}` 加告警（如某群回复率异常低 → 机器人可能被禁言）。

注意事项：

- `group_id` / `direction` 做 label（群数量有限，基数可控）；`user_id` 与文本内容**不做 label**（高基数撑爆 Loki 索引），留在 JSON 字段按需过滤
- Loki 侧设 `retention_period`（如 168h）控制保留期；每群长期趋势建议用 Prometheus（`juanniang_chat_replies_total`，`message_type` 维度）
- 统计事件丢弃数（队列满/写失败）可查 `juanniang_chat_stats_dropped_total`；主流程不受影响

## 日志排查

- 使用 `internal/logging` 自定义日志包（底层 `github.com/fatih/color`），支持彩色 stdout、JSON 格式化、WARN+ 调用栈
- 双写到 stdout 与 `logging.Default` Hub（环形 250 条 + SSE 实时订阅）；GORM SQL 语句也可通过 Hub 订阅
- 通过 `logging.NewModule("name")` 创建模块 logger，Web UI 可集中查看所有模块日志
- 前端查看：Web 面板"日志"页（`GET /api/v1/logs` 最近 250 + `GET /api/v1/logs/stream` SSE）
- 命令行查看：`docker logs -f juan-niang-neo` 或 `journalctl -u juan-niang-neo -f`（systemd）
- 插件日志带 `[plugin:<name>]` 前缀
- 启动日志会打印各模块就绪状态、adapter 监听地址、加载的插件数与 Adapter Admins 列表

## Debug 模式

启动时加 `-debug` 标志开启：

```bash
make run-debug
# 或
./bin/juan-niang-neo -debug
# 自定义 pprof 端口
./bin/juan-niang-neo -debug -pprof-addr :6061
```

Debug 模式下：

| 功能 | 说明 |
|------|------|
| 日志级别 | Debug，所有 Debug 级别日志可见（插件图片处理耗时、异步消息发送耗时、Eino tool call 详情等） |
| pprof | HTTP 服务 `:6060`，支持 CPU/heap/goroutine 等 profile |
| 启动详情 | 打印 Go 版本、CPU 核数、每个插件的 name/version/permissions |

pprof 常用命令：

```bash
# CPU 火焰图（采集 30s）
go tool pprof -http :8080 http://127.0.0.1:6060/debug/pprof/profile

# goroutine 快照
go tool pprof -http :8080 http://127.0.0.1:6060/debug/pprof/goroutine

# 内存分配
go tool pprof -http :8080 http://127.0.0.1:6060/debug/pprof/heap
```

## 优雅退出

`SIGINT`/`SIGTERM` → 反向关闭顺序（`cmd/server/main.go:287` `shutdown`）：

1. `hago.Stop()`（当前为占位，仅打日志；事件循环/CronJob 退出依赖外层 ctx 取消）
2. `WebhookAdapter.Stop`（3s graceful）
3. `Adapter.Stop`（5s；先停 adapter 再停 web，避免 web 请求持 adapter 锁死锁）
4. `webEngine.Shutdown`（5s）

外层 watchdog 15s 超时强退，避免任一 Stop 调用挂死拖垮整体。

## 常见故障

| 现象 | 原因与解决 |
|------|-----------|
| 前端访问 404 但 API 正常 | `WEB_DIR` 未构建或不正确；`cd web && npm install && npm run build` |
| 前端显示引导提示页 | `web/dist/index.html` 缺失，同上 |
| 启动报 "Postgres 连接失败" | DB_HOST/PORT/USER/PASSWORD/NAME 错；compose 用 `postgres` 主机名 |
| 启动报 "Redis 連接失败" | REDIS_ADDR/PASSWORD 错；compose 用 `redis:6379` |
| OneBot 客户端连不上 8081 | OB_TOKEN 不匹配；浏览器访问无 `Authorization: Bearer`；检查防火墙 |
| LLM 不回复消息 | 1) 没配置/激活 text_model Provider；2) 参与窗口静默（安静释放受参与概率影响，LLM 也可输出 `__NO_REPLY__`）；3) ACL 黑名单拒绝 |
| Agent 提示"未启用 T2I" | Web 面板 T2I 配置未启用 / `base_url` 不可达；`GET /t2i/health` 为 false |
| CronJob 不触发 | 留意这是 6 字段（秒级）cron；`0 0 9 * * *` 才是每天 9:00 |
| 插件改了不生效 | 改 `pluggin.yaml` 必须 reload；改 Lua 文件也要 toggle 后才重新 DoFile |
| `__NO_REPLY__` 类静默 | Agent LLM 主动判定不回复，检查 system prompt 与回复策略 |
| Adapter 重启后事件丢失 | 不会丢——EventLoop 检测 channel 关闭后 sleep 1s 重新获取句柄 |

## 反向代理

生产可用 nginx 在 `8081` / `8090` / `8091` 前：

```nginx
# API + 仪表板
location / {
    proxy_pass http://127.0.0.1:8090;
    proxy_set_header Host $host;
}
# SSE 日志流
location /api/v1/logs/stream {
    proxy_pass http://127.0.0.1:8090;
    proxy_buffering off;
    proxy_read_timeout 24h;
}
# OneBot11 反向 WS
location /ws {
    proxy_pass http://127.0.0.1:8081;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
}
```

## 系统服务（示例 systemd unit）

```ini
[Unit]
Description=JuanNiang-Neo
After=network-online.target postgresql.service redis.service
Wants=network-online.target

[Service]
Type=simple
User=jn
WorkingDirectory=/opt/juan-niang-neo
EnvironmentFile=/opt/juan-niang-neo/.env
ExecStart=/opt/juan-niang-neo/juan-niang-neo
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

## 首次启动

1. 准备 Postgres + Redis（或直接 `make docker-up` 用 compose 拉起它们）
2. 设置 `.env`（至少改 `JWT_SECRET`）
3. 启动进程；首次会 AutoMigrate 23 张表 + 创建 `admin / Admin123`
4. 立即登录 Web 面板，`POST /change-password` 改默认密码
5. 在"Providers"页配置 LLM Provider（OpenAI 兼容端点），激活
6. 在"Adapter"页配置 OB_TOKEN 与 admin QQ，启用
7. 让 OneBot11 实现（NapCat/Lagrange 等）反向 WS 连 `ws://host:8081/`，带 `Authorization: Bearer <OB_TOKEN>`
8. 在"回复设置"页配置参与窗口参数（安静间隔/插话计数/最迟必发/随机性等群聊行为）

## FAQ

**Q: 为什么 LLM 拒绝调用某个工具？** A: ACL 规则把它拒绝了，或它在 MCP 但 MCP 断连；可在"ACL"页或"日志"流查看。

**Q: Agent 在群里不回我？** A: 先确认 `isAtSelf` 精确匹配 `[CQ:at,qq=<bot>]`（@/命令/提及名字必回）；非必回消息走参与窗口——检查"回复设置"页的参与概率与窗口参数（安静间隔/插话计数/最迟必发），以及群聊 LLM 是否输出 `__NO_REPLY__` 静默。

**Q: 想只换前端不重编 Go？** A: 可以——前端是磁盘文件，`WEB_DIR` 指向新 `web/dist` 即可；二进制不嵌入它。

**Q: `internal/core/handler/` 是什么？** A: 当前为空目录占位，天真以为有 handler 包会失望；核心逻辑在 `dao`/`acl`/`cache` 里。