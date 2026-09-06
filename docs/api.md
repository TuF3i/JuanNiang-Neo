# JuanNiang-Neo Web API 文档

**Base URL:** `http://localhost:8090/api/v1`
**Content-Type:** `application/json`（上传文件为 `multipart/form-data`）

## 统一响应格式

所有接口返回 `FinalResponse`：

```json
{ "status": 0, "info": "OK", "data": <任意类型或 null> }
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `status` | uint | 0=成功，非 0=错误码 |
| `info` | string | 状态描述（成功为 `"OK"`，失败为错误信息） |
| `data` | any | 业务数据；失败时可为 `null` 或 `{"error_detail": "..."}` |

> 注意：逻辑错误也使用 HTTP 200，全部以信封中的 `status` 判定结果。

### 错误码表

| Code | 说明 |
|------|------|
| 0 | 成功 |
| 40001 | 参数格式错误（BindJSONErr） |
| 40002 | 用户名或密码错误 |
| 40003 | token 生成失败 |
| 40004 | 用户不存在 |
| 40005 | 原密码错误 |
| 40006 | 密码更新失败 |
| 40007 | 无效的 QQ 号 |
| 40008 | adapter 未初始化 |
| 40009 | provider 不存在 |
| 40010 | MCP 服务器不存在 |
| 40011 | Session 不存在 |
| 40012 | 缺少上传文件 |
| 40013 | 临时文件创建失败 |
| 40014 | 文件写入失败 |
| 40015 | 无效的 ZIP 文件 |
| 40016 | 无效的 ACL ID |
| 40017 | onebot11 适配器配置更新失败 |
| 40018 | Skill 不存在 |
| 40019 | Prompt 不存在 |
| 40020 | Tool 不存在 |
| 40021 | Plugin 不存在 |
| 40022 | 插件加载失败 |
| 40023 | ChatArea 不存在 |
| 40024 | Memory 配置不存在 |
| 40025 | adapter 配置不存在 |
| 40026 | T2I 配置不存在 |
| 40027 | Sandbox 配置不存在 |
| 40028 | 系统插件不允许删除或停用 |
| 40029 | 系统提示词不允许修改或删除 |
| 40050 | RAG 配置不存在 |
| 40051 | 词库导入文件过大（≤1MB）或行数超限（≤20000） |
| 40030 | 内置工具运行时常驻，不支持启停 |
| 40031 | 无效的回复策略（已废弃：策略收敛为参与窗口，不再返回） |
| 40032 | 参与窗口参数非法（越界或不在允许范围） |
| 40033 | 知识内容不能为空 |
| 40034 | 图片大小不能超过 1.5MB |
| 40035 | 不支持的图片格式（仅支持 jpg/png/gif/webp） |
| 40036 | 图片不存在 |
| 40037 | 文件夹已存在 |
| 40038 | 文件夹不存在 |
| 40039 | 表情不存在 |
| 40040 | 标签已存在 |
| 40041 | 标签不存在 |
| 40042 | 该图床图片已被其他表情引用 |
| 40043 | 摸鱼日历配置不存在 |
| 40044 | 定时消息任务不存在 |
| 40045 | 插件名不合法（仅允许字母/数字/下划线/连字符） |
| 40046 | 插件包包含非法路径（疑似 zip-slip 攻击） |
| 40047 | 系统内置标签不可删除 |
| 50000 | 服务器内部错误 |

## 认证

除 `POST /login` 和根路径下的 `GET /health` 外，**所有接口需要** `Authorization: Bearer <token>` 头。Token 由 `POST /login` 获取。
系统初始化时默认账号 `admin / Admin123`，首次启动后请尽快通过 `POST /change-password` 修改。

`JWT_SECRET` 用于 HMAC 签名；Token 有效期 72 小时（`internal/api/middleware/auth.go`）。

---

## 通用数据类型

| 类型 | JSON 形式 | 说明 |
|------|-----------|------|
| `JSONMap` | `{"k":"v"}` | `map[string]any`（GORM jsonb） |
| `JSONSlice` | `["a","b"]` | `[]string`（GORM jsonb） |
| `time.Time` | RFC3339 字符串 | `"2026-07-20T12:00:00Z"` |

**枚举类型**

- `ModelType`: `text_model` | `image_model` | `embedding_model`
- `PromptType`: `system` | `personality` | `custom`（`system` 保留给系统锁定提示词，禁止新建）
- `AreaType`: `private` | `group`
- `ACLScope`: `chat` | `tool` | `mcp`（**当前仅 `chat` 生效**，`tool`/`mcp` 为历史保留）
- `ACLPermission`: `allow` | `deny`
- `ACLTargetType`: `all` | `list`（`list` 时 `user_ids` 才有效）
- `ReplyStrategy`: 参与窗口已取代旧 `relevance` 相关性判断（`strategy` 字段已从配置移除）

**ACL 语义**（当前仅聊天黑名单）：无规则=允许所有；仅 `deny` 规则生效（`all`=禁止所有人、`list`=禁止指定 `user_ids`）；`allow` 规则不再生效；黑名单对所有用户生效（**管理员不豁免**）。

---

## 目录

1. [认证](#1-认证) · [健康检查](#2-健康检查)
2. [Adapter](#3-adapter) · [Webhook](#17-webhook)
3. [Providers](#4-providers) · [MCP](#5-mcp) · [Tools](#10-tools) · [Skills](#9-skills)
4. [Prompts](#7-prompts) · [Sessions](#8-sessions)
5. [Memory](#6-memory) · [聊天记录](#12-聊天记录) · [Chat Areas](#13-chat-areas) · [Overview](#14-overview)
6. [Plugins](#11-plugins) · [ACL](#15-acl)
7. [T2I](#18-t2i) · [Sandbox](#19-sandbox)
8. [Logs](#16-日志) · [Agent 活跃循环](#20-agent-活跃循环) · [CronJob](#21-cronjob) · [回复策略](#22-回复策略) · [知识库](#23-知识库)
9. [图床](#24-图床) · [表情包库](#25-表情包库) · [摸鱼人日历](#26-摸鱼人日历) · [定时消息](#27-定时消息)

---

## 1. 认证

### POST /login
管理员登录，返回 JWT。

**Body** `LoginReq`:

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `username` | string | 是 | 用户名 |
| `password` | string | 是 | 明文密码 |

**data** `TokenResp`:

| 字段 | 类型 | 说明 |
|------|------|------|
| `token` | string | JWT，放入后续 `Authorization: Bearer <token>` |

```bash
curl -X POST http://localhost:8090/api/v1/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"Admin123"}'
# {"status":0,"info":"OK","data":{"token":"eyJhbGciOi..."}}
```

### POST /change-password
修改当前登录用户密码。

**Body** `ChangePasswordReq`: `old_password` string、`new_password` string（均必填）。
**data** `null`。

---

## 2. 健康检查

### GET /health
（**不在 `/api/v1` 前缀下**，无需认证）服务存活检查。

```json
{"status":"ok"}
```

---

## 3. Adapter

OneBot11 反向 WebSocket 适配器状态查询与配置更新。

> 说明：`listen_addr` 由 `Adapter.listenAddr()` 规范化为 `host:port`；管理员 QQ 列表持久化在 DB 的 `AdminQQNumbers` 字段；`SyncConfig` 在启用时 Stop+Start 重启，禁用时仅 Stop。

### GET /adapter
返回适配器运行状态（不含配置）。

**data** `AdapterStatus`:

| 字段 | 类型 | 说明 |
|------|------|------|
| `running` | bool | 是否在运行 |
| `listen_addr` | string | 规范化后的 `host:port` |
| `self_id` | int64 | 机器人 QQ |
| `conn_count` | int | WS 连接数 |
| `conn_ids` | int64[] | 已连接客户端 QQ 列表 |
| `conns` | ConnDetail[] | 每条连接详情 `{id, ip, self_id}` |

### GET /adapter/config
读取持久化的适配器配置。

**data** `AdapterConfigResp`: `addr` string、`port` int、`token` string、`admin_qq_numbers` string[]、`enabled` bool。

### PUT /adapter
更新适配器配置并同步到运行时。

**Body** `UpdateAdapterConfigReq`:

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `addr` | string | 是 | 监听地址（支持 host / `:port` / `host:port`） |
| `port` | int | 是 | 监听端口 |
| `token` | string | 是 | OneBot access token |
| `admin_qq_numbers` | string[] | 是 | 管理员 QQ 列表 |
| `enabled` | bool | 是 | 是否启用 |

**data** `null`。

### POST /adapter/restart
重启 OneBot11 适配器。**data** `null`。

---

## 4. Providers

LLM Provider (text/image/embedding) CRUD。同类型只能一个 Active，激活时自动停用同类型其他 Provider。

### GET /providers
列出所有 Provider。

**data** `ProviderResp[]`:

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | string | UUID |
| `name` | string | 名称 |
| `type` | ModelType | 类型 |
| `endpoint` | string | API 地址 |
| `token` | string | API token |
| `model` | string | 模型名 |
| `temperature` | float32 | 温度（默认 0.7） |
| `is_active` | bool | 是否激活 |
| `created_at` | time | 创建时间 |

### GET /providers/:id
获取单个 Provider。`data` `ProviderResp`。

### POST /providers
新增 Provider。若 `is_active=true` 自动停用同类型其他 Provider。

**Body** `AddProviderReq`: `name`、`type`、`endpoint`、`token`、`model`（均必填），`temperature` float32（可选），`isActive` bool（必填）。

**data** `ProviderResp`（含生成的 UUID）。

### PUT /providers/:id
覆盖更新。**Body** `UpdateProviderReq`（同 Add）。**data** `null`。

### DELETE /providers/:id
删除 Provider 并从运行时 ProviderGroup 移除。**data** `null`。

### PUT /providers/:id/toggle
启停 Provider。

**Body** `ToggleProviderReq`: `is_active` bool（必填）。**data** `null`。

---

## 5. MCP

MCP（Model Context Protocol，SSE 传输）服务器配置 CRUD，支持运行时连接/断开。

### GET /mcp
列出所有 MCP 服务器。注意：返回会合并运行时连接状态。

**data** `MCPServerResp[]`:

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | string | UUID |
| `name` | string | 名称 |
| `server_url` | string | SSE 端点 |
| `headers` | JSONMap | 自定义请求头 |
| `timeout` | int | 超时毫秒 |
| `retry_count` | int | 重试次数 |
| `tool_filter` | string[] | 工具白名单（空=全量） |
| `auto_reconnect` | bool | 自动重连 |
| `is_active` | bool | 是否激活 |
| `created_at` | time | 创建时间 |

### GET /mcp/:id
获取单个。**data** `MCPServerResp`。

### POST /mcp
新增 MCP。若 `is_active=true` 立即建立 SSE 连接。

**Body** `AddMCPServerReq`：`name`、`server_url`（必填）；`headers` JSONMap、`timeout` int、`retry_count` int、`tool_filter` string[]、`auto_reconnect` bool（可选）；`is_active` bool（必填）。

**data** `MCPServerResp`。

### PUT /mcp/:id
覆盖更新。断开旧连接，若 `is_active=true` 重新建立 SSE。**data** `MCPServerResp`。

### DELETE /mcp/:id
断开连接并删除。**data** `null`。

### GET /mcp/:id/check
实时检测指定 MCP SSE 连接状态。**data** `{"connected": bool}`。

> 注：`GET /mcp/:id/check` 与 `PUT /mcp/:id/toggle` 配合使用，前者探活，后者启停。

### PUT /mcp/:id/toggle
启停 MCP，对应建立/断开 SSE 连接。
**Body** `ToggleMCPServerReq`: `is_active` bool。**data** `null`。

---

## 6. Memory

短期/长期记忆**配置**管理（按 ChatArea）。短期消息实际存 Redis，长期条目存 Postgres，本组接口只管理配置元数据。对话时长期记忆默认**按消息语义召回**（gram → pg_trgm 倒排候选 + similarity 排序，空候选回退最近 5 条），环境变量 `LTM_RECALL_MODE=recent` 可回退旧行为。

### GET /memory/:chatAreaID/short-term
获取短期记忆配置，不存在则自动创建（`window_size=100, auto_compact=true`）。

**data** `ShortTermMemoryResp`: `id`、`chat_area_id`、`window_size` int、`auto_compact` bool、`created_at`。

### PUT /memory/:chatAreaID/short-term
更新短期记忆配置，同步运行时 MemoryGroup。

**Body**: `window_size` int、`auto_compact` bool（均必填）。**data** `ShortTermMemoryResp`。

### GET /memory/:chatAreaID/long-term
获取长期记忆配置，不存在自动创建（`hot_area_size=10, hot_memory_ttl=86400`）。

**data** `LongTermMemoryResp`: `id`、`chat_area_id`、`hot_area_size` int、`hot_memory_ttl` int（秒）、`created_at`。

### PUT /memory/:chatAreaID/long-term
更新长期记忆配置。

**Body**: `hot_area_size` int、`hot_memory_ttl` int（均必填）。**data** `LongTermMemoryResp`。

---

## 7. Prompts

Prompt 模板 CRUD。

> SystemLocked 提示词：启动时 `EnsureSystemPrompt` 幂等播种名为 `__system_locked__`、`IsSystem=true` 的种子，不受 `IsActive` 影响强制拼接。Service 层 Update/Delete/Toggle 拒绝 `IsSystem` 行；新建 Prompt 禁止使用 `type=system`（返回 40029）。拼接顺序：**SystemLocked → system → personality → custom**。

### GET /prompts
列出所有 Prompt（含系统锁定，前端只读）。

**data** `PromptResp[]`:

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | string | UUID |
| `name` | string | 名称 |
| `content` | string | 内容 |
| `type` | PromptType | 类型 |
| `is_active` | bool | 是否激活 |
| `is_system` | bool | 系统锁定（禁改/删/停用） |
| `created_at` | time | 创建时间 |

### POST /prompts
新增 Prompt。**禁止 `type=system`**。

**Body** `AddPromptReq`: `name`、`content`、`type`（personality/custom）、`is_active`（均必填）。

**data** `PromptResp`。

### PUT /prompts/:id
更新。若目标行 `IsSystem=true` 或请求 `type=system` 返回 40029。**Body** `UpdatePromptReq`（同 Add）。**data** `PromptResp`。

### DELETE /prompts/:id
删除。`IsSystem=true` 返回 40029。**data** `null`。

### PUT /prompts/:id/toggle
启停。系统锁定提示词**允许启用、不允许停用**（停用返回 40029）。
**Body** `TogglePromptReq`: `is_active` bool。**data** `null`。

---

## 8. Sessions

每个 ChatArea 对应一个 Session。

### GET /sessions
列出所有 Session（Preload ChatArea）。

**data** `SessionResp[]`: `id`、`chat_area_id`、`model`、`token_usage` int64、`meta_data` JSONMap、`created_at`。

### GET /sessions/:id
**data** `SessionResp`。

### DELETE /sessions/:id
删除 Session，同时清除 Redis 短期消息缓存。**data** `null`。

---

## 9. Skills

Skill = 关键词/正则触发的 Prompt+Tool 组合配置。`priority` 越大越优先。`prompt_refs` 支持引用多个 Prompt。

### GET /skills
**data** `SkillResp[]`: `id`、`name`、`description`、`keywords` string[]、`regex_pattern`、`prompt_refs` string[]、`tool_refs` string[]、`mcp_refs` string[]、`is_active`、`is_system`、`priority` int、`created_at`。

### POST /skills
**Body** `AddSkillReq`: `name`（必填）；`description`、`keywords`、`regex_pattern`、`prompt_refs`、`tool_refs`、`mcp_refs`（可选）；`is_active`（必填）；`is_system`、`priority`（可选）。

**data** `SkillResp`。

### PUT /skills/:id
覆盖更新。**Body** `UpdateSkillReq`（同 Add）。**data** `SkillResp`。

### DELETE /skills/:id
**data** `null`。

---

## 10. Tools

工具配置查看与启停。

> `GET /tools` 合并两份数据源：运行时 `ToolRegistry.List()` 中的内置工具（ID 形如 `builtin:<name>`，`is_builtin=true`，常驻不可启停）+ DB 中 `ToolConfig` 表（自定义工具与历史条目）。同名条目用 DB 的 ID/`is_active`/`admin_only`/`created_at`，但 `is_builtin` 与 `parameters` 以运行时注册表为准。

### GET /tools
**data** `ToolConfigResp[]`: `id`、`name`、`description`、`parameters` JSONMap、`timeout` int、`is_active`、`is_builtin`、`admin_only`（仅管理员可调用）、`created_at`。

### PUT /tools/:id/toggle
启停 Tool。**内置工具（`id` 以 `builtin:` 开头）运行时常驻，不支持启停**，返回 40030。

**Body** `ToggleToolReq`: `is_active` bool。**data** `null`。

### PUT /tools/:id/admin-only
更新工具"仅管理员"标志（内置/自定义工具均可）。开启后该工具只能由 Admins 列表内用户触发，防止提示词注入诱导 Agent 执行敏感操作；内置群管理工具（踢人/禁言/全员禁言/群名片/好友与加群请求/撤回）默认开启。

**Body** `UpdateToolAdminOnlyReq`: `admin_only` bool。**data** `null`。

---

## 11. Plugins

Lua 插件管理。插件通过 ZIP 上传，自动解压到 `data/pluggins/<name>/`。

> `GET /plugins` 改用 `PluginEngine.ListMaps()`：不再有 `path`/`config`/`created_at`，新增 `permissions`/`commands`/`is_system`/`author`/`description`。系统插件（`is_system=true`）三层保护（Manifest.System + `PluginEngine.IsSystem()` + Service 守卫）禁止删除与停用，违规返回 40028。`POST /plugins/upload` 用 `multipart/form-data`。CronJob/Provider/MCP 新建后自动 reload 对应调度器/运行时。

### GET /plugins
列出所有插件配置。

**data** `PluginListMap[]`:

| 字段 | 类型 | 说明 |
|------|------|------|
| `name` | string | 插件名（=目录名，作为 `id`） |
| `ppid` | string | 稳定 UUID |
| `version` | string | 版本 |
| `author` | string | 作者 |
| `description` | string | 描述 |
| `permissions` | string[] | 权限列表 |
| `is_system` | bool | 系统插件（禁删/停） |
| `is_active` | bool | 是否激活 |
| `commands` | PluginCommandInfo[] | 注册命令列表 |

`PluginCommandInfo`: `path` string[]、`description`、`usage`、`is_leaf` bool。

### POST /plugins/upload
**Body**: `multipart/form-data`，字段 `file` 为 ZIP。
**data** `PluginUploadResp`: `name`、`status`（`loaded`）。

```bash
curl -X POST http://localhost:8090/api/v1/plugins/upload \
  -H "Authorization: Bearer <token>" -F "file=@my-plugin.zip"
```

### PUT /plugins/:id/toggle
启停插件。启用 `Load`，停用 `Unload`。系统插件禁停用（40028）。
**Body** `TogglePluginReq`: `is_active` bool。**data** `null`。

### DELETE /plugins/:id
卸载并删除插件配置（**不删磁盘文件**）。系统插件禁删（40028）。**data** `null`。

### POST /plugins/reload
**热重载所有非系统插件**。先卸载全部非系统插件，再调用 `LoadAll()` 重新扫描并加载。
适用于：新增/修改 `on_cronjob` 或注册了新命令后无需重启进程即可生效。
**Body** 无。**data** `null`。

### 插件商店（/plugin-store）

> 商店从 GitHub 仓库（默认 `JuanNiangDev/JuanNiang-Plugins`）经镜像源实时拉取元数据与插件文件；列表元数据每晚由仓库 workflow 自动更新。详见 [plugin-store.md](plugin-store.md)。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/plugin-store` | 商店插件列表（合并元数据分片，按名称排序） |
| GET | `/api/v1/plugin-store/readme?path=` | 仓库内 `plugins/<name>/README.md` |
| GET | `/api/v1/plugin-store/avatar?path=` | 仓库内 `plugins/<name>/avatar.png`（`Cache-Control: no-store`，每次实时拉取，仓库更新后刷新可见） |
| POST | `/api/v1/plugin-store/install?path=` | 下载 `dist/<name>.zip` 并安装到 `data/pluggins/<name>/` |
| GET | `/api/v1/plugin-store/config` | 商店配置 + 镜像列表（`config`/`mirrors`） |
| PUT | `/api/v1/plugin-store/config` | 更新仓库配置（`repo_owner`/`repo_name`/`branch`） |
| POST | `/api/v1/plugin-store/mirror` | 添加自定义镜像（需含 `{path}` 占位符） |
| POST | `/api/v1/plugin-store/mirror/test` | 测试镜像连通性，返回 `latency_ms` |
| POST | `/api/v1/plugin-store/mirror/select` | 手动指定生效镜像源（空 = 恢复自动按序尝试） |
| DELETE | `/api/v1/plugin-store/mirror` | 删除自定义镜像 |

---

## 12. 聊天记录

按 ChatArea 分页查询持久化聊天记录（Postgres）。

### GET /chat-records/:chatAreaID
| Query | 类型 | 默认 | 说明 |
|-------|------|------|------|
| `limit` | int | 20 | 每页数量 |
| `offset` | int | 0 | 偏移 |
| `role` | string | (空) | 过滤角色 `user`/`assistant`/`tool` |

**data** `ChatRecordListResp`: `total` int64、`list` ChatRecordResp[]。

`ChatRecordResp`: `id` int64、`chat_area_id`、`user_id` int64、`role`、`content`、`token_count` int、`tool_calls` JSONMap、`created_at` time。

### GET /chat-records/:chatAreaID/token-usage
返回该 ChatArea 的会话 Token 用量（实际是 `Session.GetOrCreate`）。**data** `SessionResp`。

---

## 13. Chat Areas

聊天区域自动由消息驱动创建（私聊/群聊各一个）。

### GET /chat-areas
**data** `ChatAreaResp[]`: `id`、`area_type`、`target_id` int64、`created_at`。

---

## 14. Overview

### GET /overview
返回系统全局概览（资源计数 + 系统状态 + T2I/Sandbox 健康）。

**data** `OverviewResp`: `chat_area_count`、`mcp_count`、`adapter_count`（固定 1）、`plugin_count`、`provider_count`、`skill_count`、`session_count`、`total_token_usage` int64、`cpu_count`、`goroutine_num`、`mem_alloc_bytes`、`mem_sys_bytes`、`mem_heap_inuse_bytes` uint64、`go_version`、`t2i_active`、`t2i_healthy`、`sandbox_active`、`sandbox_healthy` bool。

### GET /overview/daily-token-usage
近 N 天每日 Token 用量（折线图数据点）。

| Query | 类型 | 默认 | 说明 |
|-------|------|------|------|
| `days` | int | 7 | 天数，范围 1–30 |

**data** `DailyTokenUsageResp[]`: `date` string（`YYYY-MM-DD`）、`token_count` int64。

---

## 15. ACL

访问控制规则管理。规则以 ChatArea 为单位组织。

### GET /acl
**data** `ACLRuleResp[]`: `id` int64、`chat_area_id`、`scope`、`permission`、`target_type`、`user_ids` string[]、`created_at`。

### POST /acl
新增或覆盖规则（同 ChatArea + Scope 已存在则覆盖）。

**Body** `AddACLRuleReq`: `chat_area_id`、`scope`、`permission`、`target_type`（均必填）；`user_ids` string[]（`target_type=list` 时有效）。**data** `ACLRuleResp`。

### DELETE /acl/:id
删除规则并同步运行时 ACL 管理器。**data** `null`。

---

## 16. 日志

日志由 `internal/logging` Hub 维护，环形缓冲区保留最近 250 条。

### GET /logs
返回最近 250 条，**最新排在最前**。

**data** `LogEntryResp[]`: `time` time、`level` string、`message` string、`attrs` map。

### GET /logs/stream
SSE 实时日志流。`text/event-stream`。

- 先按时间顺序发送最近 250 条历史
- 再订阅 Hub 实时推送；每 15 秒发送一次 keepalive 心跳
- 客户端断开或服务停止时退出

事件：

```
event: log
data: {"time":"2026-07-20T12:00:00Z","level":"INFO","message":"...","attrs":{}}
```

```javascript
const es = new EventSource('/api/v1/logs/stream', {
  headers: { Authorization: 'Bearer ' + token }
});
es.addEventListener('log', (e) => {
  const entry = JSON.parse(e.data);
  console.log(entry.time, entry.level, entry.message);
});
```

---

## 17. Webhook

Webhook 适配器配置（监听独立端口接收外部 HTTP 事件）。详见 [webhook-cronjob.md](webhook-cronjob.md)。

### GET /webhook/config
**data** `WebhookConfigResp`: `addr`、`port` int、`token`、`enabled` bool、`running` bool。

### PUT /webhook/config
**Body** `UpdateWebhookConfigReq`: `addr`、`port`、`token`、`enabled`（均必填）。**data** `WebhookConfigResp`（含最新 `running`）。

---

## 18. T2I

Text-to-Image 配置与健康管理。单行配置（ID=1）。详见 [external-services.md](external-services.md#t2i)。

### GET /t2i/config
**data** `T2IConfigResp`: `base_url`、`timeout` int、`is_active` bool、`healthy` bool。

### PUT /t2i/config
更新配置。运行时若启用则重建客户端并注入 HagoCenter，停用则置空。
**Body** `UpdateT2IConfigReq`: `base_url`（必填）、`timeout` int（可选）、`is_active` bool（必填）。**data** `T2IConfigResp`。

### GET /t2i/health
实时健康检查。**data** `{"healthy": bool}`。

---

## 19. Sandbox

代码沙箱配置与健康管理。单行配置（ID=1）。详见 [external-services.md](external-services.md#sandbox)。

### GET /sandbox/config
**data** `SandboxConfigResp`: `base_url`、`api_key`、`timeout` int、`is_active` bool、`healthy` bool。

### PUT /sandbox/config
**Body** `UpdateSandboxConfigReq`: `base_url`、`api_key`、`is_active`（必填），`timeout`（可选）。**data** `SandboxConfigResp`。

### GET /sandbox/health
**data** `{"healthy": bool}`.

---

## 19.5 RAG 向量检索

RAG-Service 配置与健康管理（记忆/知识语义检索）。单行配置（ID=1，**默认未启用**）。未启用或服务不可达时：记忆召回降级 pg_trgm 语义匹配、知识库检索降级 SQL 匹配，不影响正常对话。

### GET /rag/config
**data** `RAGConfigResp`: `base_url`、`timeout` int、`is_active` bool、`healthy` bool。

### PUT /rag/config
**Body** `UpdateRAGConfigReq`: `base_url`、`is_active`（必填），`timeout`（可选）。启用且健康检查通过才注入客户端（健康失败置 nil → 走降级路径）。**data** `RAGConfigResp`。

### GET /rag/health
**data** `{"healthy": bool}`。

### GET /rag/info
查询 RAG-Service 运行状态：`{status, model:{ready, model_name, dim, n_params, n_threads, error}, memory:{rss_kb, vsize_kb}, scoops:{<分库名>:{tags, chunks}}}`（自 v2 起规模按分库上报：knowledge/memory/groupmgr/plugin）；未启用返回 `{ready:false}`。

> 服务本体 `JuanNiang-RAG-Service`（Rust）需独立部署：`make download && cargo run --release`（默认 `127.0.0.1:3000`）。知识与记忆等集合按**分库（scoop）隔离**存储与检索（`internal/core/ragtag` 提供 `Scoop*` 常量 + UUID v5 派生 tag）。

---

## 20. Agent 活跃循环

当前正在执行的 Agent ReAct 循环（监控展示，原后台任务页改造）。对应前端页面 `web/src/views/AgentLoopsPage.vue`，实现见 `internal/agent/loop_tracker.go`。

### GET /agent/loops
返回当前所有活跃的 Agent ReAct 循环。

**data** `AgentLoopResp[]`:

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | string | 循环 ID |
| `chat_area_id` | string | 所属 ChatArea |
| `message_type` | string | `private` / `group` |
| `target_id` | int64 | 私聊: user_id；群聊: group_id |
| `user_id` | int64 | 发起者 QQ |
| `user_msg` | string | 触发消息（批内合并） |
| `current_tool` | string | 当前正在执行的工具；空=思考/生成中 |
| `started_at` | time | 开始时间 |

---

## 21. CronJob

定时任务管理。详见 [webhook-cronjob.md](webhook-cronjob.md)。

CronJob 增删改/toggle 后**自动 reload** 调度器（`robfig/cron`，6 字段：秒 分 时 日 月 周）。

### GET /cronjobs
**data** `CronJobResp[]`。

### GET /cronjobs/:id
**data** `CronJobResp`。

### POST /cronjobs
新增，自动同步调度器。

**Body** `AddCronJobReq`:

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | 是 | 名称 |
| `cron_expr` | string | 是 | 6 字段 cron，如 `0 0 9 * * *` 每天 9:00 |
| `is_active` | bool | 是 | 是否立即启用 |
| `message` | string | 否 | 合成消息内容（透传给插件 `event.raw_message`） |
| `message_type` | string | 否 | `private`（默认）/ `group` |
| `target_id` | int64 | 否 | 消息目标：私聊=QQ 号，群聊=群号 |
| `plugin_ids` | string[] | 否 | 触发插件列表（插件目录名），到点时调用其 `on_cronjob` 回调 |
| `payload` | string | 否 | JSON 字符串，传递给插件 `on_cronjob(event)` 的 `event.payload` |

**data** `CronJobResp`。

`CronJobResp`: `id`、`name`、`cron_expr`、`plugin_ids` JSONSlice、`payload` JSONMap、`is_active`、`last_run_at` *time、`last_error`、`created_at`、`updated_at`。

### PUT /cronjobs/:id
覆盖更新，自动 reload。**Body** `UpdateCronJobReq`（同 Add）。**data** `CronJobResp`。

### DELETE /cronjobs/:id
删除，自动 reload。**data** `null`。

### PUT /cronjobs/:id/toggle
启停，自动 reload。**Body** `ToggleCronJobReq`: `is_active` bool。**data** `null`。

---

## 22. 回复策略

系统回复策略（单例，仅一行）。控制群聊中 Agent 的参与/回复行为。

**参与模式**（已取代旧 relevance 相关性判断）

@/命令/提及名字/私聊必回（立即回复，不攒窗）；其余群聊消息攒进每群一个的参与窗口：
等待「安静间隔」到期、或攒够「插话计数」条、或到达「最迟必发」后，把整窗消息一次性交给
Agent 由 LLM 自门控（输出 `__NO_REPLY__` 即静默）决定参与或附和。随机参数用于让释放时机
不那么"人机"，全部置 0/1 即确定性模式。噪音过滤覆盖剥离 CQ 码/URL 后无任何文字的消息
（纯图/纯 sticker/表情）、≤2 字短消息与纯 emoji/符号（水群刷屏直接丢弃）；含 @（CQ:at）的消息及
剥离 CQ 码/URL 后仍含文字（CJK/字母数字）的消息不算噪音，照常进窗口由 LLM 自门控。

### GET /reply-strategy

获取配置。首次 GET 不存在时自动创建（参与窗口默认参数）。

**data** `ReplyStrategyResp`: `bot_name`、`strip_markdown` bool、`agent_lite` bool、
`quiet_gap_seconds` int（安静间隔，默认 5）、`force_count` int（插话计数强发，默认 5）、
`max_age_seconds` int（最迟必发，默认 20）、`window_max_msgs` int（窗口消息数上限，默认 20）、
`jitter_seconds` int（安静间隔随机抖动，默认 2）、`force_count_jitter` int（计数抖动，默认 1）、
`participate_probability` float（安静释放参与概率，默认 0.8）、`typing_delay_max_ms` int（打字延迟上限，默认 1500）。

### PUT /reply-strategy

更新参与窗口参数。

**Body** `UpdateReplyStrategyReq`: `bot_name`、`strip_markdown`、`agent_lite`（可选）；
`quiet_gap_seconds`（0=默认 5，范围 1-30）、`force_count`（0=默认 5，范围 2-20）、
`max_age_seconds`（0=默认 20，范围 5-120）、`window_max_msgs`（0=默认 20，范围 5-50）、
`jitter_seconds`（0=关闭，范围 0-10）、`force_count_jitter`（0=关闭，范围 0-5）、
`participate_probability`（0-1，0=从不主动参与）、`typing_delay_max_ms`（0=关闭，范围 0-5000）。

**data** `ReplyStrategyResp`。

```bash
curl -X PUT http://localhost:8090/api/v1/reply-strategy \
  -H "Authorization: Bearer <token>" -H "Content-Type: application/json" \
  -d '{"bot_name":"小卷","quiet_gap_seconds":5,"force_count":5,"participate_probability":0.8}'
```

---

## 23. 知识库

Web 存入知识条目，Agent 异步提取关键词；对话前**首选 RAG 语义检索**（限定 `knowledge` 分库，向量命中按分数注入），未启用/未命中降级为关键词 + 内容前 20 字模糊匹配（LRU 50 条缓存加速）。新增/编辑/删除知识时同步双写/双删 RAG-Service 向量（未配置时静默跳过）。

> `keyword_status`：`pending`（提取中，暂不参与匹配）→ `ready`（可匹配）→ `failed`（提取失败，可手动重试）。新增/编辑后自动异步提取关键词。

### GET /knowledge
分页列出。**Query** `page`（默认 1）、`page_size`（默认 20，上限 100）。

**data** `{total int64, list KnowledgeResp[]}`。

### GET /knowledge/:id
详情。**data** `KnowledgeResp`。

### POST /knowledge
新增，触发异步关键词提取。

**Body** `AddKnowledgeReq`: `title` string（可选）、`content` string（必填，非空否则 40033）。**data** `KnowledgeResp`（`keyword_status=pending`）。

### PUT /knowledge/:id
编辑，重新触发异步提取。**Body** `UpdateKnowledgeReq`（同 Add）。**data** `KnowledgeResp`。

### DELETE /knowledge/:id
删除（软删）。**data** `null`。

### POST /knowledge/:id/re-extract
手动重试关键词提取（`failed` 状态时用）。**data** `null`。

### POST /knowledge/vector-sync
手动全量同步知识库到 RAG 向量库（页面「同步向量库」按钮）：全部条目按 50 条一批 `BatchUpsert`（一次嵌入一次发布）。RAG 未启用时返回 `{ready:false, message}`（不报错）。**data** `{ready bool, sync_total int, synced int, failed int}`。

`KnowledgeResp`: `id`、`title`、`content`、`keywords` string[]、`keyword_status`、`created_at`、`updated_at`。

---

## 24. 图床

图片二进制存储在 `data/imgs`（`IMG_DIR` 可覆盖），元数据在 Postgres `image_assets` / `image_folders` 表。
虚拟文件夹仅一层：图片默认在根 `/`，根下可创建文件夹（如 `/meme`），文件夹下不能再建文件夹。

### 上传约束

- 大小 ≤ **1.5MB**（超出返回 40034）
- MIME 白名单：`image/jpeg` / `image/png` / `image/gif` / `image/webp`（以文件内容嗅探为准，不信任扩展名；不支持返回 40035）

### 消息引用（imgs://）

Plugin 与 Agent 发送消息时，用 `[CQ:image,file=imgs://<id>]` 引用图床图片。发送层（`internal/adapter`）
检测到 `imgs://` 前缀后自动从图床加载图片并转成 `base64://` 再发给 OneBot11 客户端——
对 Plugin / Agent 无感，无需关心 Onebot11 与机器人之间的网络互通。

### GET /images
分页列出。**Query** `folder`（默认 `/`）、`page`（默认 1）、`page_size`（默认 48，上限 100）。

**data** `{total int64, list ImageResp[]}`。

### GET /images/:id
图片元数据详情。**data** `ImageResp`。

### GET /images/:id/file
图片文件流（Web 预览用，响应 `Content-Type` 为该图片 MIME）。

### POST /images
上传图片。**multipart/form-data**：`file`（必填）、`name`（可选，默认文件名）、`folder`（可选，默认 `/`）。

**data** `ImageResp`。

### PUT /images/:id
编辑（重命名 / 移动文件夹）。**Body** `UpdateImageReq`: `name` string（可选）、`folder` string（可选，`/` 或 `/<name>`，目标文件夹需存在）。**data** `ImageResp`。

### DELETE /images/:id
删除（DB 软删 + 删除磁盘文件）。**data** `null`。

### GET /image-folders
列出全部虚拟文件夹。**data** `ImageFolderResp[]`。

### POST /image-folders
创建虚拟文件夹。**Body** `CreateImageFolderReq`: `name` string（必填，不能含 `/`，重名返回 40037）。**data** `ImageFolderResp`。

### DELETE /image-folders/:id
删除文件夹（其下图片自动移到根 `/`，不存在返回 40038）。**data** `null`。

`ImageResp`: `id`、`name`、`folder`（虚拟路径，`/` 为根）、`mime_type`、`size_bytes`、`created_at`、`updated_at`。
`ImageFolderResp`: `id`、`name`、`created_at`。

---

## 25. 表情包库

基于图床的二次封装：表情引用图床图片（`image_id` 长 UUID），对外暴露短 UUID（8 位 hex）作为表情 ID。
发送时用 `[CQ:image,file=stk://<短UUID>,subType=1]`（OneBot11 以 `subType=1` 区分表情与普通图片），
发送层（`internal/adapter`）自动把短 UUID 解析为图床长 UUID 并转 base64，Plugin / Agent 只接触表情 ID。

### Agent 工具

- `send_sticker`：单独发送表情（参数 `sticker_id` 短 UUID + 可选 `message_type`/`target_id`）
- `send_sticker_by_keyword`：**一步发送**——按关键词搜索表情包库并直接发送最匹配的一个（参数 `keyword` + 可选 `message_type`/`target_id`），接梗/回应情绪时优先使用
- `list_sticker_tags`：获取全部标签
- `list_stickers`：按标签分页获取表情（`tag`/`page`/`page_size`）
- `search_stickers`：关键词模糊匹配表情名称/简介/标签（`keyword`/`limit`）

### 每轮对话注入的表情包上下文

`handleMessage` 构建系统指令时会注入表情包上下文（`buildStickerContext`）：

1. **全部标签列表** → 引导 Agent 优先用 `send_sticker_by_keyword` 按意图发送，或 `list_stickers` 按标签浏览；
2. **「常用」标签下的表情（ID/名称/简介，最多 20 个）**，按表情自身标签分组 → Agent 命中场景时可直接用 `send_sticker + ID` 发送。

使用方式：「常用」为**系统内置标签**（启动时自动创建、不可删除）；把常用表情加入该标签即可。其余标签可自由创建/删除。没有「常用」标签内容时不注入对应部分。

### Plugin API

- `onebot11.send_group_sticker(group_id, sticker_id)`
- `onebot11.send_private_sticker(user_id, sticker_id)`
- 消息段方式：`{{type="image", data={file="stk://<短UUID>", subType=1}}}`

### GET /stickers
分页列出表情。**Query** `tag`（标签过滤）、`keyword`（名称/简介模糊匹配）、`page`（默认 1）、`page_size`（默认 24，上限 100）。

**data** `{total int64, list StickerResp[]}`。

### GET /stickers/:id
表情详情。**data** `StickerResp`。

### POST /stickers
新建表情。**Body** `CreateStickerReq`: `image_id` string（必填，图床图片长 UUID）、`name` string（必填）、`desc` string（可选）、`tags` string[]（可选）。
图床图片不存在返回 40036；已被其他表情引用返回 40042。**data** `StickerResp`（`id` 为短 UUID）。

### PUT /stickers/:id
编辑表情。**Body** `UpdateStickerReq`: `name` / `desc` / `tags`。**data** `StickerResp`。

### DELETE /stickers/:id
删除表情（软删，不影响图床图片）。**data** `null`。

### GET /sticker-tags
列出全部标签。**data** `StickerTagResp[]`。

### POST /sticker-tags
创建标签。**Body** `CreateStickerTagReq`: `name` string（必填，重名返回 40040）。**data** `StickerTagResp`。

### DELETE /sticker-tags/:id
删除标签（所有表情中的该标签一并移除，不存在返回 40041）。**data** `null`。

`StickerResp`: `id`（短 UUID）、`image_id`（图床长 UUID）、`name`、`desc`、`tags` string[]、`created_at`、`updated_at`。
`StickerTagResp`: `id`、`name`、`created_at`。

---

## 26. 摸鱼人日历

独立于 CronJob 系统的每日定时任务（`internal/agent/fishcal`）：按配置的 cron 表达式触发，
用模板组装日历内容 → 通过 T2I 服务渲染成 JPEG 图片 → 发送到目标群。

日历图片内容：标题 / 宜划水·忌内卷朱印 / 洛谷式「月·超大日期·星期」日历卡（含农历，lunar-go）/ 本周进度 /
距周末与距法定假日倒计时（内置 2025-2026 节假日表）/ 今日金句（[一言 API](https://v1.hitokoto.cn/)，失败回退内置句子）/ 今日群务 / 落款。

### GET /fish-calendar/config
读取配置（未初始化时写入默认配置）。**data** `FishCalendarConfigResp`。

### PUT /fish-calendar/config
更新配置并重新调度。**Body** `UpdateFishCalendarConfigReq`: `enabled` bool、`cron_expr` string（6 字段秒级 cron）、`target_groups` string[]（目标群号列表）。**data** `null`。

### POST /fish-calendar/trigger
手动触发一次立即生成并发送（测试用），失败返回 50000 + `error_detail`。**data** `null`。

### GET /fish-calendar/affairs
列出某月已配置的群务。**Query** `month`（必填，YYYY-MM）。**data** `FishCalendarAffairResp[]`。

### PUT /fish-calendar/affairs
设置某天群务（content 为空则清除当天）。**Body** `SetFishCalendarAffairReq`: `date` string（YYYY-MM-DD）、`content` string。**data** `null`。

`FishCalendarConfigResp`: `enabled`、`cron_expr`、`target_groups` string[]、`last_run_at`（可空）、`last_error`。
`FishCalendarAffairResp`: `date`、`content`。

发送消息为富文本：`今日份摸鱼人日历来了~` + 日历图片（682×757，纸张朱印质感模板）。

---

## 27. 定时消息

独立于 CronJob 系统的定时任务（`internal/agent/scheduledmsg`），采用**积木式编排**：
任务从触发器（cron 表达式）开始，按序执行编排块链，最后一个块执行完任务即结束。

编排块（`ScheduledBlockReq`）：

| type | 字段 | 说明 |
|------|------|------|
| `message` | `segments` | 消息块：块内所有段拼成**一条**富文本消息 |
| `delay` | `delay_seconds` | 延时块：等待 N 秒后继续下一个块（1~3600） |

消息块内的段（`ScheduledSegmentReq`）：

| type | source | content |
|------|--------|---------|
| `text` | - | 文字内容 |
| `image` | `t2i` | HTML 模板（T2I 服务渲染成图片） |
| `image` | `url` | 图片直链 |
| `image` | `imgstore` | 图床引用（`imgs://<图片ID>`，发送层自动转 base64） |
| `face` | - | CQ 码表情（如 `[CQ:face,id=66]`） |

### GET /scheduled-messages
分页列出。**Query** `page`、`page_size`（默认 20）。**data** `{total, list ScheduledMessageResp[]}`。

### GET /scheduled-messages/:id
任务详情。**data** `ScheduledMessageResp`。

### POST /scheduled-messages
新建任务。**Body** `AddScheduledMessageReq`: `name`、`enabled`、`cron_expr`（6 字段秒级触发器）、`target_type`（group/private）、`target_id`、`blocks` ScheduledBlockReq[]。**data** `ScheduledMessageResp`。

### PUT /scheduled-messages/:id
编辑任务。**Body** `UpdateScheduledMessageReq`（同 Add）。**data** `ScheduledMessageResp`。

### DELETE /scheduled-messages/:id
删除任务。**data** `null`。

### PUT /scheduled-messages/:id/toggle
启停任务。**Body** `{enabled bool}`。**data** `ScheduledMessageResp`。

### POST /scheduled-messages/:id/trigger
手动触发立即执行（沿块链顺序：消息块发一条消息，延时块等待）。**data** `null`。

`ScheduledMessageResp`: `id`、`name`、`enabled`、`cron_expr`、`target_type`、`target_id`、`blocks`、`last_run_at`、`last_error`、`created_at`、`updated_at`。

---

## 28. 群管理

系统级群违规检测（Phase 0.5 检测闸门，先于所有 Lua 插件）。判定链路：卡片文本化 → RAG 黑白名单语义匹配（首选：黑名单命中处罚 / 白名单命中放行）→ LLM 批量判定兜底（3s 批窗口逐条独立，learn 学习闭环异步写回语录）；RAG/LLM 均不可用时降级关键词路径（关键词词库仅从内置 txt 加载到内存作兜底，不入 DB/RAG/samples，不可 Web 修改）。含图片刷屏、+1 复读（跳过命令消息）、三级惩罚、白名单/管理员豁免、入群统计。

### GET /group-mgr/config

群管理配置。**data** `GroupMgrConfigResp`: `enabled`、`llm_review`、`black_min_score`（黑名单语录命中阈值，默认 0.7）、`white_min_score`（白名单语录命中阈值，默认 0.75）、`llm_batch_window`（LLM 判定批窗口秒数，默认 3）、`exclude_groups`（排除检测的群 ID 列表）、`llm_prompt`（统一检测提示词）、`white_gc_interval_days`（白名单语录 GC 周期天，默认 7）。旧字段 `high_score`/`low_score`/`fallback_score`/`llm_criteria`/`llm_gray_prompt`/`llm_high_risk_prompt` 已废弃（保留兼容）。

### PUT /group-mgr/config

更新配置并热重载。**Body** 同 `GroupMgrConfigResp`。**data** `GroupMgrConfigResp`。

### GET /group-mgr/words?category=

已废弃：关键词词库改为只读内存兜底（从内置 txt 加载，不入 DB/RAG/samples），接口返回空列表兼容旧调用。**data** `GroupMgrWordResp[]`（恒空）。

### POST /group-mgr/words

已废弃（同 GET 说明）。关键词词库不可 Web 修改，接口返回错误。**data** `null`。

### DELETE /group-mgr/words/:id

已废弃（同 GET 说明）。关键词词库不可 Web 修改，接口返回错误。**data** `null`。

### POST /group-mgr/words/import?category=

已废弃（同 GET 说明）。关键词词库不可 Web 修改，接口返回错误。**data** `null`。

### POST /group-mgr/sync-rag

手动全量同步违禁语录到向量库（50 条/批幂等 upsert），成功条目标记 `rag_synced=true`。关键词词库不入 RAG，不参与同步。RAG 未配置返回错误。**data** `GroupMgrSyncResp`: `total`、`failed`。

### GET /group-mgr/sync-rag/stream

SSE 流式同步进度（逐批推送 `data: {done, failed}`，结束推 `data: {total, failed}`；出错推 `error` 事件 `{error}`）。群管理未初始化返回普通 JSON 错误；RAG 未配置时流内推送 `error` 事件（前端按 `data.error` 中断）。

### GET /group-mgr/samples?list_type=

违禁语录列表（`list_type` 可选 black/white，缺省全部）。**data** `GroupMgrSampleResp[]`: `id`、`word_id`、`list_type`（black/white）、`text`、`category`、`source`（seed/learn/import）、`hit_count`、`rag_synced`（已同步到 RAG 向量库）、`rag_tag`（派生 RAG tag UUID，black=`ragtag.Sample(id)` / white=`ragtag.WhitePhrase(id)`）、`last_used_at`（最近命中时间，GC 用）、`created_at`。

### POST /group-mgr/phrases

新增违禁语录。**Body** `{text string, list_type string(black/white), category string(可选 ad/sensitive)}`。RAG 可用时同步写向量库并标记 `rag_synced=true`，不可用仅存库（可手动同步）。**data** `null`。

### POST /group-mgr/phrases/import?list_type=

txt 导入违禁语录（multipart `file`，一行一个，≤1MB/≤20000 行；`?list_type=` 指定集合，`?category=` 可选 ad/sensitive，默认 ad）。**data** `{imported int, skipped int}`。

### DELETE /group-mgr/samples/:id

删除语录（Postgres + RAG 双删，未配置静默跳过）。**data** `null`。

### GET /group-mgr/violations

违规记录。**data** `GroupMgrViolationResp[]`: `id`、`group_id`、`user_id`、`username`（处罚时群名片/昵称）、`count`（当前违规等级）、`detection_path`（判定来源：rag / llm / keyword）、`llm_reason`（LLM 审核返回的 reason，`detection_path=llm` 时有值）。

### DELETE /group-mgr/violations/:id
删除某条违规记录（重置该用户违规）。**data** `null`。

### GET /group-mgr/whitelist
白名单 QQ 列表。**data** `{qq_list []int64}`。

### PUT /group-mgr/whitelist
白名单全量覆盖。**Body** `{qq_list []int64}`。**data** `null`。

### GET /group-mgr/admins
手动管理员 QQ 列表。**data** `{qq_list []int64}`。

### PUT /group-mgr/admins
手动管理员全量覆盖。**Body** `{qq_list []int64}`。**data** `null`。

### POST /group-mgr/admins/sync-from-adapter
把 Adapter.Admins（系统管理员 QQ）合并到手动管理员表（去重，已存在跳过）。**data** `{added int}`（新增数量）。

### GET /group-mgr/stats?group_id=
统计（与 /groupstats 命令同源）。**data** `GroupMgrStatsResp`: `group_id`、`date`、`join_today`、`warns`、`mutes`、`copy_warns`、`ad`、`sensitive`、`kicks`。

### POST /group-mgr/test
链路测试（不处罚、不写库）。**Body** `{text string}`。**data** `GroupMgrTestResp`: `text`、`card`、`word`、`word_cat`、`rag_ok`、`black_score`、`black_phrase`、`white_score`、`white_phrase`、`verdict`（punish/review/pass）、`reason`。

### POST /memory/sync-rag

长期记忆手动全量同步向量库（`LongTermMemItem` 按 50 条/批幂等 upsert，补齐 Compact 双写前的历史记忆）。RAG 未启用返回 `ready:false` + 提示。**data** `{ready bool, synced int, failed int, total int}`。

### 记忆 / 白名单 GC

- 长期记忆 GC（`HagoCenter.StartLongTermMemoryGC`）：默认 7 天执行一次，清理最近周期内未召回（`last_recalled_at` 为空或超期）的 5 条（Postgres + RAG 双删）。周期 `LongTermMemory.GCIntervalDays`，记忆页可配置。
- 白名单语录 GC（`Manager.StartWhiteGC`）：默认 7 天执行一次，清理最近周期内未命中（`last_used_at` 为空或超期）的 5 条白名单语录（Postgres + RAG 双删）。周期 `GroupMgrConfig.WhiteGCIntervalDays`，群管理参数面板可配置。

---

## 附：前端 SPA 静态服务

后端复用 Hertz 引擎同端口（`:8090`）服务前端 SPA：

| 请求路径模式 | 行为 |
|--------------|------|
| `/api/v1/<已注册路由>` | Hertz 路由，JWT 鉴权（除 `/login`） |
| `/health` | 内联健康检查（root，无需鉴权） |
| `/api/*`（未命中） | 标准信封 404：`{"status":40400,"info":"资源不存在","data":null}` |
| 其它任何路径 | 文件存在→serve 文件；不存在→回退 `index.html` |
| 前端未构建（`index.html` 缺失） | 200 + 引导提示页（"请先构建前端"） |

实现：`internal/web/web.go::SPAHandler(webDir)`，在 `engine.New` 中通过 `h.NoRoute(...)` 注册；不嵌入二进制，磁盘上 `WEB_DIR`（默认 `web/dist`）为准；开发期 Vite `:3000` 代理 `/api`→`:8090`，Go 的 fallback 不会被触发。