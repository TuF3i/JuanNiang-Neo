# 插件开发指南

本文档整合 JuanNiang-Neo Lua 插件的开发流程、API 参考与引擎实现细节，是二次开发插件的完整参考。插件系统的架构概览见 [project-details.md](project-details.md#四插件系统)。

## 目录

- [一、快速开始](#一快速开始)
- [二、API 参考](#二api-参考)
- [三、引擎实现细节](#三引擎实现细节)
- [四、常见坑](#四常见坑)

---

# 一、快速开始

## 1. 创建插件目录

每个插件是 `data/pluggins/<plugin-name>/` 下的一个独立目录，至少含 `pluggin.yaml`（清单）和 Lua 入口（默认 `main.lua`）。

```
data/pluggins/
└── my-hello/
    ├── pluggin.yaml
    └── main.lua
```

## 2. 编写 manifest — `pluggin.yaml`

```yaml
name: my-hello
version: "1.0.0"
author: me
description: "示例插件：回复 hello 命令"
entry: main.lua
system: false
enabled: true
permissions:
  - onebot11
  - cache
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `ppid` | string | 稳定 UUID（空时自动生成并写回） |
| `name` | string | 必须与目录名一致（作为 `id`） |
| `version` | string | 默认 `"1.0.0"` |
| `author` / `description` | string | 元数据 |
| `entry` | string | Lua 入口，默认 `main.lua` |
| `permissions` | string[] | 申请的权限，决定哪些全局表被注入 |
| `system` | bool | 系统插件（undeletable / unstoppable），仅内置 `system` 用 |
| `enabled` | bool | 是否在 `LoadAll` 时加载 |

## 3. 编写入口 — `main.lua`

```lua
local jn = require("jn")

-- 注册命令 /hello
jn.command.register("hello", function(args, event)
    return true, "你好，" .. (event.user_id or "陌生人") .. "！"
end, { description = "打招呼", usage = "/hello" })

-- 消息事件回调（返回 consumed, skip_reply）
-- consumed=true → 消息不进 Agent（不短路，其余插件仍会执行）
-- skip_reply=true → 标记为必回（mustKeep），跳过参与窗口直接进入 Agent
function on_message(event)
    if event.raw_message == "ping" then
        return true, false  -- 消费：不进 Agent
    end
    return false, false
end

-- webhook 事件回调（需在 permissions 申请 webhook）
function on_webhook(event)
    jn.log.info("webhook payload received")
    return false, nil
end
```

## 4. 部署

- **本地开发**：放目录进 `data/pluggins/`，重启进程或在 Web API 上 `POST /plugins/upload`（ZIP）或 `PUT /plugins/:id/toggle` 启用。
- **Docker 部署**：通过 `docker-compose.yaml` 的 `../data/pluggins:/app/data/pluggins` bind-mount 注入；镜像升级不丢插件（注意 `system/` 子目录由二进制在每次启动覆盖，勿修改）。
- **热加载**：`PUT /plugins/:id/toggle` 触发 Load/Unload；改了 Lua 源码后 toggle 先停再启即可（暂无单文件 reload API，整个插件 Reload）。

## 引入 SDK

```lua
local jn = require("jn")

-- jn.<table>.<func> 与全局 <table>.<func> 完全等价，可混用
jn.log.info("插件启动")    -- 推荐写法（IDE 有提示 via LuaCATS）
log.info("插件启动")        -- 等价
```

SDK 仅是 Go 注入全局表的再导出（`jn.log = log` 等），不引入额外行为。

| SDK 字段 | 全局表 | 说明 |
|----------|--------|------|
| `jn.log` | `log` | 日志 |
| `jn.json` | `json` | JSON |
| `jn.onebot11` | `onebot11` | OneBot11 协议 |
| `jn.http` | `http` | HTTP 请求 |
| `jn.database` | `database` | 数据库 |
| `jn.cache` | `cache` | Redis 缓存 |
| `jn.t2i` | `t2i` | 文生图 |
| `jn.sandbox` | `sandbox` | 代码沙箱 |
| `jn.agent` | `agent` | Agent 操作接口 |
| `jn.llm` | `llm` | LLM 调用（复用 Bot Provider 配置） |
| `jn.file` | `file` | 插件目录内文本文件读写 |
| `jn.command` | — | 命令注册 |

## 注册命令

`jn.command.register(path, handler, opts)`：

- `path`：string 或 table，多级命令如 `"system provider switch"` 或 `{"system","provider","switch"}`
- `handler`：Lua 函数 `(argsTable, eventTable) → (consumedBool, replyString)`
- `opts`：`{ description = "...", usage = "..." }`

最长前缀匹配；未命中 handler 但停在非根节点时返回该节点子命令列表。`/help` 列出所有顶级命令；`/help <cmd> [sub...]` 列子命令。

```lua
-- 多级命令
jn.command.register({"weather", "today"}, function(args, event)
    local city = args[1] or "北京"
    return true, city .. "今日晴"
end, { description = "查今日天气", usage = "/weather today <city>" })

-- /weather today 重庆 → "重庆今日晴"
```

## 打包上传

把整个插件目录打成 ZIP（目录在根），调用 `POST /api/v1/plugins/upload`：

```bash
cd data/pluggins
zip -r my-hello.zip my-hello
curl -X POST http://localhost:8090/api/v1/plugins/upload \
  -H "Authorization: Bearer <token>" -F "file=@my-hello.zip"
```

## 系统插件示例

内置 `system` 插件（`internal/pluggin/systemplugin/`）展示了完整用法，命令包括：

- `/system status` — 系统总览
- `/system provider list|switch <id>`
- `/system mcp list|toggle <id>`
- `/system tool list|toggle <name>`
- `/system memory compact`
- `/system t2i status|toggle on|off`
- `/system sandbox status|toggle on|off`
- `/system session list|info`

可用它作为多级命令 + 调用 agent 接口的范本。

## 调试

- 日志：`jn.log.*` 输出会进 stdout 与前端 SSE 流（带 `[plugin:<name>]` 前缀）
- `GET /api/v1/logs/stream` 实时查看
- `GET /api/v1/plugins` 看到每个插件注册的 `commands` 列表
- Web 面板"插件"页可直接 toggle 启停，无需改 `pluggin.yaml`

---

# 二、API 参考

JuanNiang-Neo 暴露给 Lua 插件的 API 函数。**可用性由 `pluggin.yaml` 中 `permissions` 控制**。

> **SDK 区分**：`jn.command` 是命令注册的唯一入口（内部委托到 Go 侧 `__jn_internal.register_command` 全局函数）。直接调用 `__jn_internal.*` 不被推荐，签名可能随版本调整。其他 `jn.<table>` 仅是 Go 注入全局表的再导出。

## 全局表: `log`

权限：**始终可用**。日志输出到服务器 slog。

| 函数 |
|------|
| `log.info(msg)` / `log.warn(msg)` / `log.error(msg)` |

```lua
log.info("插件启动")
log.warn("配置缺失")
log.error("操作失败: " .. err)
```

## 全局表: `json`

权限：**始终可用**。

| 函数 |
|------|
| `json.encode(value) → string` Lua 值→JSON |
| `json.decode(str) → table` JSON→Lua table |

## 全局表: `file`

权限：`file`。插件读写**自身目录**（`data/pluggins/<name>/`）下的文本文件（txt/json/log/csv 等）。

> **安全边界**：`path` 必须为相对路径，禁止绝对路径与 `..` 越界；越权访问返回错误。目录自动创建，行号均从 1 开始。

| 函数 | 返回 | 说明 |
|------|------|------|
| `file.read(path) → string [, err]` | string | 读取整个文件内容（文件不存在返回 err） |
| `file.read_lines(path) → []string [, err]` | string[] | 读取全部行，自动去除行尾 `\n`/`\r` |
| `file.read_line(path, n) → string [, err]` | string | 读取第 n 行；**越界返回 nil**（非错误，便于循环读取） |
| `file.write(path, content) → bool [, err]` | bool | 覆盖写入整个文件（自动创建父目录） |
| `file.write_lines(path, lines) → bool [, err]` | bool | 覆盖写入多行（每行自动补 `\n`） |
| `file.write_line(path, n, content) → bool [, err]` | bool | 改写第 n 行；文件不足自动补空行 |
| `file.append(path, content) → bool [, err]` | bool | 追加内容到文件末尾（**不自动补换行**） |
| `file.append_line(path, content) → bool [, err]` | bool | 追加一行（末尾无换行时自动补） |
| `file.exists(path) → bool` | bool | 判断文件是否存在 |
| `file.remove(path) → bool [, err]` | bool | 删除文件 |

```lua
-- 逐行读取（read_line 越界返回 nil，可用作循环终止条件）
local i = 1
while true do
    local line = jn.file.read_line("data/notes.txt", i)
    if line == nil then break end
    log.info(line)
    i = i + 1
end

-- 整体读写
jn.file.write("data/state.txt", "hello")
local content = jn.file.read("data/state.txt")

-- 追加一行
jn.file.append_line("data/log.txt", "事件发生于 " .. os.date())
```

## 全局表: `onebot11`

权限：`onebot11`。所有函数返回 `(result, err)` —— 成功时 err 为 nil。

### 消息发送

| 函数 | 说明 |
|------|------|
| `onebot11.send_private_msg(user_id, message, reply_to?) → bool, string` | **异步发送**私聊，立即返回。`message` 可为 string 或消息段数组；可选 `reply_to`（消息 ID）让本条消息**引用回复**指定消息 |
| `onebot11.send_group_msg(group_id, message, reply_to?) → bool, string` | **异步发送**群聊，立即返回；可选 `reply_to` 同上 |
| `onebot11.send_private_msg_sync(user_id, message, reply_to?) → bool [, err]` | **同步发送**私聊，阻塞等待结果返回；可选 `reply_to` 同上 |
| `onebot11.send_group_msg_sync(group_id, message, reply_to?) → bool [, err]` | **同步发送**群聊，阻塞等待结果返回；可选 `reply_to` 同上 |
| `onebot11.send_group_forward_msg(group_id, nodes) → bool, string` | **异步发送**群合并转发（转发卡片），立即返回。`nodes` 为节点数组：构造节点 `{user_id=…, nickname=“…”, content=“文本或消息段数组”}`；引用节点 `{id=群内已有消息ID}`（引用既有消息作为转发节点） |
| `onebot11.send_group_forward_msg_sync(group_id, nodes) → number [, err]` | **同步发送**群合并转发，阻塞等待，返回 `message_id`（部分实现不返回时可能为 0，请以 `err` 判断成功） |
| `onebot11.delete_msg(message_id) → bool [, err]` | 撤回消息；`message_id` 接受数字或字符串（事件表的 `message_id` 为字符串，避免 QQ 长 ID 精度丢失） |
| `onebot11.read_file_base64(path) → string, err` | 从插件目录读取文件并返回 `base64://...` 字符串 |

> **异步 vs 同步**：默认 `send_xxx_msg` 为异步（fire-and-forget），适合大多数场景。需要确认发送结果或获取 `message_id` 时用 `send_xxx_msg_sync`。

#### 消息段格式（富文本）

`message` 参数支持 Lua 数组格式的消息段：

```lua
-- 纯文本
jn.onebot11.send_group_msg(123456, "Hello")

-- 富文本消息段
jn.onebot11.send_group_msg(123456, {
    { type = "text", data = { text = "看图：" } },
    { type = "at", data = { qq = "123456789" } },
    { type = "image", data = { file = "img/cat.png" } },  -- 插件目录下文件自动转 base64
    { type = "image", data = { file = "https://example.com/dog.jpg" } },
    { type = "face", data = { id = "66" } },  -- CQ 表情 ID
})
```

图片 `file` 字段支持三种来源：
- `http://` / `https://` → 直接透传 URL
- `base64://` → 直接透传
- 相对路径（如 `img/photo.png`）→ 从插件目录自动读取并转 base64

### 群信息查询

| 函数 | 返回 |
|------|------|
| `onebot11.get_group_info(group_id) → table [, err]` | `{group_id, group_name, member_count, max_member_count}` |
| `onebot11.get_group_member_list(group_id) → []table [, err]` | `[{user_id, nickname, card, role, ...}]` |
| `onebot11.get_group_member_info(group_id, user_id) → table [, err]` | 单个成员信息 |
| `onebot11.get_group_honor_info(group_id) → table [, err]` | `{current_talkative, talkative_list, ...}` |

### 群管理

| 函数 | 说明 |
|------|------|
| `onebot11.kick_group_member(group_id, user_id [, reject_add]) → bool [, err]` | reject_add 默认 false |
| `onebot11.ban_group_member(group_id, user_id, duration) → bool [, err]` | duration 秒，0 表示解除禁言 |
| `onebot11.set_group_whole_ban(group_id, enable) → bool [, err]` | 全员禁言开关 |
| `onebot11.set_group_card(group_id, user_id, card) → bool [, err]` | 设群名片 |

### 请求处理

| 函数 | 说明 |
|------|------|
| `onebot11.handle_friend_request(flag, approve, remark) → bool [, err]` | |
| `onebot11.handle_group_request(flag, sub_type, approve, reason) → bool [, err]` | `sub_type` ∈ `"add"`/`"invite"` |

### 用户信息与其他

| 函数 | 返回 |
|------|------|
| `onebot11.get_login_info()` | `{user_id, nickname}`（机器人自身） |
| `onebot11.get_stranger_info(user_id)` | 陌生人信息 |
| `onebot11.get_friend_list()` | `[]table` |
| `onebot11.get_group_list()` | `[]table` |
| `onebot11.send_like(user_id, times)` | 发赞 |
| `onebot11.get_status()` | 适配器运行状态 |
| `onebot11.get_version_info()` | 协议版本 |

```lua
local jn = require("jn")
jn.onebot11.send_group_msg(987654321, "群通知")
local info, err = jn.onebot11.get_group_info(987654321)
```

## 全局表: `http`

权限：`http`。

| 函数 | 返回 | 说明 |
|------|------|------|
| `http.get(url [, proxy]) → table` | `{status=number, body=string}` | GET，30s 超时；可选 `proxy` 走代理（见下方代理说明） |
| `http.post(url [, content_type, body, proxy]) → table` | `{status, body}` | POST，30s 超时；可选第 4 位 `proxy` |
| `http.get_async(url [, ctx, headers, proxy]) → number` | `req_id` | GET 异步版：立即返回，完成回调 `on_http_response`（不阻塞事件循环）。第 2 位 `ctx` 调用现场表（旧签名）、第 3 位 `headers` 表、第 4 位 `proxy`；也可用 opts 表 `get_async(url, {proxy=…, headers=…, ctx=…})` |
| `http.post_async(url [, content_type, body, proxy, ctx]) → number` | `req_id` | POST 异步版；第 4 位 `proxy` 字符串，尾部 table 仍为 `ctx`（有 proxy 时后移至第 5 位） |

**代理参数**（`proxy`，可选）：不传或传空串 = 直连（默认）；支持 `http://host:port`、`https://host:port`、`socks4://host:port`（或 `socks4a://`，域名目标）、`socks5://[user:pass@]host:port`。非法协议或地址直接返回错误。

```lua
local r, err = jn.http.get("https://api.github.com/repos/x/y")
local r, err = jn.http.post("https://httpbin.org/post", "application/json",
                            '{"k":"v"}')

-- 走代理：socks5 / socks4 / http 均可
local r, err = jn.http.get("https://www.google.com", "socks5://127.0.0.1:1080")

-- 异步 + 代理（opts 表写法）
local rid = jn.http.get_async("https://www.google.com", { proxy = "socks4://127.0.0.1:1081", ctx = { from = "demo" } })
function on_http_response(req_id, ctx, result, err)
    if err then jn.log.warn("HTTP 请求失败: " .. err) return end
    jn.log.info("status=" .. result.status .. " body=" .. result.body)
end
```

## 全局表: `database`

权限：`database`。

> **⚠ 真实状态**：`database.query/exec` 跑在**共享库**上，没有真正的命名空间隔离（`prefixSQL` 桩未生效）。请给自定义表加自己的前缀（如 `my_plugin_state`），并在加载时用 `CREATE TABLE IF NOT EXISTS` 创建。

| 函数 | 返回 |
|------|------|
| `database.query(sql) → []table [, err]` | SELECT 查询行数组 |
| `database.exec(sql) → number [, err]` | INSERT/UPDATE/DELETE，返回影响行数 |

```lua
-- 加载时自动建表
database.exec([[CREATE TABLE IF NOT EXISTS my_plugin_state (
  k TEXT PRIMARY KEY, v TEXT NOT NULL
)]])

local rows = database.query("SELECT k, v FROM my_plugin_state WHERE k = 'last_seen'")
for _, row in ipairs(rows) do
  log.info(row.v)
end
```

## 全局表: `cache`

权限：`cache`。**键自动加 `pluggin:<name>:` 前缀**，与系统缓存严密隔离。

| 函数 | 说明 |
|------|------|
| `cache.get(key) → table` | 读取（自动反序列化 JSON） |
| `cache.set(key, value [, ttl]) → bool [, err]` | 写入；`ttl` 秒，默认 0=永不过期 |
| `cache.del(key) → bool [, err]` | |
| `cache.exists(key) → number` | 0 或 1 |

```lua
jn.cache.set("last_seen", "2026-07-26", 3600)   -- 1h TTL
local v = jn.cache.get("last_seen")
jn.cache.del("last_seen")
if jn.cache.exists("last_seen") then ... end
```

## 全局表: `t2i`

权限：`t2i`。**未启用时** `generate`/`generate_url` 返回 `(nil, "T2I 服务未启用")`；运行时通过 `AgentOperator.GetT2IClient()` 获取最新实例，支持热更新。

| 函数 | 返回 | 说明 |
|------|------|------|
| `t2i.generate(html) → string [, err]` | 图片 ID | HTML→图片 |
| `t2i.generate_url(html) → string [, err]` | 公开 URL | |
| `t2i.generate_async(html [, opts, ctx]) → number` | `req_id` | 异步版：立即返回，完成回调 `on_t2i_response`（不阻塞事件循环） |
| `t2i.generate_url_async(html [, opts, ctx]) → number` | `req_id` | 异步版，回调返回 URL |
| `t2i.toggle(active) → bool [, err]` | bool | 启用/停用，委托 `SetT2IActive`（同步 DB + 重建客户端） |
| `t2i.is_active() → bool` | bool | 从 DB 读配置；`dao` 不可用时 false |
| `t2i.get_config() → table [, err]` | base_url/timeout/is_active 等 | |

```lua
local id, err = jn.t2i.generate("<h1 style='color:red'>卷娘</h1>")
local url = jn.t2i.generate_url(...)
jn.onebot11.send_group_msg(987654321, "[CQ:image,file=" .. url .. "]")
local active = jn.t2i.is_active()
local cfg = jn.t2i.get_config()

-- 异步：立即返回 req_id，完成回调 on_t2i_response（渲染较慢，适合异步）
local rid = jn.t2i.generate_async("<h1>卷娘</h1>", nil, { group_id = 987654321 })
function on_t2i_response(req_id, ctx, result, err)
    if err then jn.log.warn("T2I 渲染失败: " .. err) return end
    if ctx and ctx.group_id then
        jn.onebot11.send_group_msg(ctx.group_id, "[CQ:image,file=" .. result .. "]")
    end
end
```

## 全局表: `sandbox`

权限：`sandbox`。**未启用时** `create`/`exec_shell`/`exec_python` 返回 `(nil, "Sandbox 服务未启用")`。

| 函数 | 返回 | 说明 |
|------|------|------|
| `sandbox.create() → table [, err]` | `{sandbox_id, status}` | 新沙箱 |
| `sandbox.exec_shell(sandbox_id, command) → (output, exit_code) \| (nil, err)` | | |
| `sandbox.exec_python(sandbox_id, code) → (output, error) \| (nil, err)` | | |
| `sandbox.create_async([ctx]) → number` | `req_id` | 异步版：立即返回，完成回调 `on_sandbox_response`（不阻塞事件循环） |
| `sandbox.exec_shell_async(sandbox_id, command [, ctx]) → number` | `req_id` | 异步执行 shell（默认超时 120s），回调 result=`{output, exit_code}` |
| `sandbox.exec_python_async(sandbox_id, code [, ctx]) → number` | `req_id` | 异步执行 python，回调 result=`{output, error}` |
| `sandbox.toggle(active) → bool [, err]` | 启停：`SetSandboxActive` |
| `sandbox.is_active() → bool` | 从 DB 读配置 |
| `sandbox.get_config() → table [, err]` | base_url/api_key/timeout/is_active 等 |
| `sandbox.list() → table [, err]` | 沙箱列表 |
| `sandbox.delete(sandbox_id) → bool [, err]` | 删沙箱 |

```lua
local sb, err = jn.sandbox.create()
local sid = sb.sandbox_id
local out, exit = jn.sandbox.exec_shell(sid, "ls -la /")
local out, e = jn.sandbox.exec_python(sid, "print(1+1)")
jn.sandbox.delete(sid)

-- 异步（执行代码可能很慢，建议异步）：完成回调 on_sandbox_response
local rid = jn.sandbox.exec_shell_async(sid, "ls -la /", { sid = sid })
function on_sandbox_response(req_id, ctx, result, err)
    if err then jn.log.warn("沙箱执行失败: " .. err) return end
    jn.log.info("exit=" .. result.exit_code .. " output=" .. result.output)
end
```

## 全局表: `metrics`

权限：无需权限，默认注入。插件自定义 **Prometheus 指标**（暴露在主程序 `/metrics`，Grafana 可查）。
指标名自动加前缀 `juanniang_plugin_<插件名>_`（插件名非法字符转 `_`，插件内只写短名）；
同名幂等注册（返回已有句柄，计数跨插件重载延续）；短名仅允许字母/数字/下划线。

| 函数 | 返回 | 说明 |
|------|------|------|
| `metrics.counter(name, help?) → handle, err` | 计数器 | `handle:inc()` +1；`handle:add(n)` +n（不能为负） |
| `metrics.gauge(name, help?) → handle, err` | 仪表 | `handle:set(n)` 设置值；`handle:inc()` / `handle:add(n)` 增减 |
| `metrics.histogram(name, help?) → handle, err` | 直方图 | `handle:observe(n)` 观测一个值（耗时/大小分布） |

```lua
-- 统计本插件消息处理量与耗时（Grafana 面板：juanniang_plugin_xxx_*）
local msg_count = jn.metrics.counter("msg_count", "消息处理数")
local latency = jn.metrics.histogram("handle_latency", "处理耗时秒")

function on_message(event)
    local start = os.clock()
    -- ... 处理逻辑 ...
    msg_count:inc()
    latency:observe(os.clock() - start)
end
```

> 提示：指标为纯观测性、无副作用，默认所有插件可用（无需 manifest 权限声明）；
> 注册后常驻（卸载插件指标仍保留计数，避免重复注册 panic 与指标空洞）。

## 全局表: `rag`

权限：`rag`。面向原始 JuanNiang-RAG-Service API（tag 必须是 **UUID** 字符串，全文入库自动分块）；所有读写统一落在独立**分库 `plugin`**（`ragtag.ScoopPlugin`），与知识/记忆/群管理集合物理隔离——插件数据不污染业务检索，业务集合也不会干扰插件检索。**未启用时** 函数返回 `(nil, "RAG 服务未启用")`。不要与主程序知识/记忆集合的派生 tag 混用（跨分库归属冲突会返回 409）。

| 函数 | 返回 | 说明 |
|------|------|------|
| `rag.add(tag, text) → bool, err` | | 同步写入（幂等 upsert，长文自动分块） |
| `rag.add_async(tag, text [, ctx]) → number` | `req_id` | 异步写入：立即返回，完成回调 `on_rag_response(req_id, ctx, tag, err)` |
| `rag.search(query, k?, min_score?) → table [, err]` | `[{tag, score}]` | 同步检索，按分数降序（k 默认 10，上限 100） |
| `rag.search_async(query, k?, min_score? [, ctx]) → number` | `req_id` | 异步检索：回调 `on_rag_response(req_id, ctx, results, err)`，results=`[{tag, score}]` |

```lua
-- 同步写入 + 检索
local ok, err = jn.rag.add("7f6b2f8a-1111-4950-9c1f-000000000001", "这是一段用于入库的中文文本……")
local hits, err = jn.rag.search("入库的中文文本", 5)
for _, h in ipairs(hits or {}) do
    jn.log.info(h.tag .. " score=" .. h.score)
end

-- 异步：完成回调 on_rag_response（写入与检索共用）
local rid = jn.rag.add_async("7f6b2f8a-1111-4950-9c1f-000000000001", "异步入库内容", { from = "demo" })
local rid2 = jn.rag.search_async("异步入库内容", 5, nil, { from = "demo" })
function on_rag_response(req_id, ctx, result, err)
    if err then jn.log.warn("RAG 请求失败: " .. err) return end
    jn.log.info("result=" .. tostring(result))
end
```

## 全局表: `agent`

权限：`agent`。提供 Agent 配置查询与运行时管理（共 17 个函数）。

### 配置查询（从 DB 读取）

| 函数 | 返回 |
|------|------|
| `agent.get_providers() → []table` | 所有 LLM Provider 配置 |
| `agent.get_mcp_servers() → []table` | 所有 MCP 服务器配置 |
| `agent.get_skills() → []table` | 所有 Skill 配置 |
| `agent.get_sessions() → []table` | 所有 Session |
| `agent.get_prompts() → []table` | 所有 Prompt 模板 |
| `agent.get_tools() → []table` | 所有 Tool 配置 |
| `agent.get_plugins() → []table` | 所有已安装插件信息 |

### Provider 管理

```lua
jn.agent.set_provider_active("uuid", false)  -- 停用
jn.agent.set_provider_active("uuid", true)   -- 启用
```

| 函数 | 说明 |
|------|------|
| `agent.set_provider_active(id, active) → bool [, err]` | 启用→加载；停用→运行环境移除 |
| `agent.list_runtime_providers() → []table [, err]` | 当前运行时已加载（仅 active） |

返回每项：

```lua
{ id="...", name="openai", type="text_model", model="gpt-4", active=true }
```

| 函数 | 说明 |
|------|------|
| `agent.switch_provider(id) → bool [, err]` | 切换主 Provider；同类型自动停其他 |

### MCP 管理

| 函数 | 说明 |
|------|------|
| `agent.set_mcp_active(id, active) → bool [, err]` | 启用→连接；停用→断开 |
| `agent.list_mcps() → []table [, err]` | 运行时已加载的 MCP 列表 |
| `agent.toggle_mcp(id, active) → bool [, err]` | 同 set_mcp_active 的语义别名 |

返回每项：`{ id, name, url, active }`

### Tool 管理

| 函数 | 说明 |
|------|------|
| `agent.list_tools() → []table [, err]` | 运行时已注册的 Tool |
| `agent.toggle_tool(name, active) → bool [, err]` | `name` 是工具名（非 ID）；停用从 ToolRegistry 移除 |

返回每项：`{ name, description, builtin, long_running, active }`

> 注意：内置工具运行时常驻，停用后仍保留在注册表；用户自定义工具停用后会被 Unregister。

### 上下文与记忆

| 函数 | 返回 | 说明 |
|------|------|------|
| `agent.get_current_chat_area() → table` | `{post_type, message_type, user_id, group_id, chat_area_id}` | 当前正在处理的消息所属 ChatArea |
| `agent.compact_memory() → string [, err]` | | Compact 当前 ChatArea 短期记忆：LLM 压缩为摘要写入长期记忆，随后窗口清理为只保留最近 10 条消息（需 Text LLM Provider） |

## SDK 模块: `jn.llm`

权限：`llm`。通过 **Bot 自身启用的文本模型 Provider** 调用 LLM——模型、采样参数、密钥全部复用 Bot 配置，插件不接触任何密钥。适合内容审查、二次判断等场景。

```lua
local jn = require("jn")

-- 同步调用（适合命令等低频路径）
local content, err = jn.llm.chat("你好", { timeout = 30 })

-- 异步调用（高频路径必须用异步：不阻塞事件循环与其它插件）
local rid = jn.llm.chat_async(
    { { role = "system", content = "你是审查员" }, { role = "user", content = "待审查消息" } },
    { timeout = 60 }
)  -- 立即返回 req_id（失败返回 0）

-- 完成后引擎调用插件入口函数（引擎级异步注册表派发）：
function on_chat_response(req_id, content, err)
    if err then
        log.warn("LLM 调用失败: " .. err)
        return
    end
    log.info("LLM 返回: " .. content)
end
```

| 函数 | 说明 |
|------|------|
| `llm.available() → bool` | 当前是否有可用的文本模型 Provider |
| `llm.chat(messages, opts?) → string?, string?` | 同步调用，返回 `(content, err)`；err 为 nil 表示成功 |
| `llm.chat_async(messages, opts?) → number` | 异步提交，立即返回 `req_id`（失败返回 `0`）；完成后引擎派发到插件入口 `on_chat_response(req_id, content, err)` |

参数：

- `messages`：单字符串（role=`user`）或数组。数组元素可为字符串（role=`user`）或 `{role="system|user|assistant", content="..."}`。
- `opts`：`{temperature=?, max_tokens=?, timeout=?秒}`；**缺省采样参数回退 Bot Provider 配置**，`timeout` 缺省 60s。
- `on_chat_response` 的 `req_id` 与 `chat_async` 返回值一致，用于关联请求上下文（例如维护 `req_id → 消息/关键词` 映射表，回调时取回）。

### 异步 API 注册表

`xxx_async` 由引擎级**异步注册表**驱动（`PluginEngine.RegisterAsyncAPI`）：

- 提交后立即返回 `req_id`，阻塞操作（HTTP 调用 / T2I 渲染 / LLM）在独立 goroutine 完成，**不阻塞事件循环**；
- 完成后引擎串行派发到插件 Lua 入口函数 `on_xxx_response`（与事件派发互斥，保证 LState 安全）；
- 插件卸载后未派发的任务自动丢弃。

**异步 API 一览**（`xxx_async(...) → req_id`，完成后引擎调用对应回调）：

| kind | 异步函数 | 回调入口 | 回调参数 |
|------|---------|---------|---------|
| `chat` | `llm.chat_async(messages, opts?)` | `on_chat_response` | `(req_id, content, err)` |
| `t2i` | `t2i.generate_async` / `t2i.generate_url_async` | `on_t2i_response` | `(req_id, ctx, result, err)` |
| `http` | `http.get_async` / `http.post_async` | `on_http_response` | `(req_id, ctx, result, err)` |
| `rag` | `rag.add_async` / `rag.search_async` | `on_rag_response` | `(req_id, ctx, result, err)` |
| `sandbox` | `sandbox.create_async` / `exec_shell_async` / `exec_python_async` | `on_sandbox_response` | `(req_id, ctx, result, err)` |

> **调用现场保存（ctx）**：`t2i` / `http` 异步回调带 `ctx` 参数——调用时把要保留的变量打包成一张表作为最后一个参数传入（如 `generate_async(html, opts, ctx)`），引擎按 `req_id` 关联保存，回调时**原样带回**（不序列化，可含函数）。用于延续调用前的业务状态（待处理消息、群号、临时标记等）；不传则为 `nil`。`llm.chat_async` 不带 `ctx`（回调签名保持 `(req_id, content, err)`，兼容现有插件）。
>
> **顺序语义**：多个异步任务并发提交时，回调按**完成顺序**派发（FIFO），不保证与提交顺序一致，插件勿依赖提交顺序。

## SDK 模块: `jn.command`

多级命令注册。需先 `local jn = require("jn")`。

`CommandRegistry` 维护一棵 `CommandNode` 树。`PluginEngine.OnMessage` 在派发到 `on_message` 之前，先检查 `event.RawMessage` 是否以 `/` 开头，若是则调 `commands.Dispatch` 最长前缀匹配：

- 命中可执行 handler → 自动回复 `reply`（非空时）+ `consumed=true` 跳过 Agent 与 `on_message`
- 未命中 handler 但停在某个非根节点 → 自动列出该节点的子命令作为提示
- 完全未命中 → fallback 到插件的 `on_message` 回调

### `jn.command.register(path, handler [, opts]) → bool [, err]`

| 参数 | 说明 |
|------|------|
| `path` | 命令路径，string（按空格切分）或 string[]（多级） |
| `handler` | `function(args, event) → (consumed, reply)`；`args` 是路径之后的所有空格分隔 token `string[]` |
| `opts` | `{ description="...", usage="..." }`，用于 `/help` 自动生成 |

handler 返回：
- `consumed` — 是否消费此命令（true 跳过 Agent）
- `reply` — 若非空，由系统自动回复

```lua
local jn = require("jn")

jn.command.register("greet", function(args, event)
    local name = args[1] or "朋友"
    return true, "你好，" .. name .. "！"
end, { description = "打招呼", usage = "/greet [名字]" })

jn.command.register({"myplugin", "subcmd1", "subcmd2"}, function(args, event)
    return true, "收到参数: " .. table.concat(args, " ")
end, { description = "多级命令", usage = "/myplugin subcmd1 subcmd2 [args...]" })
```

> handler 引用保活：Go 侧通过 `L.SetGlobal(refKey, handlerFn)` 保留引用，防止 Lua GC 回收。

### 内置 `/help` 命令

`PluginEngine.registerBuiltinCommands()` 启动时注册到 `system/help`：

- `/help` — 列出所有顶层命令
- `/help <cmd>` — 查看 `<cmd>` 的子命令与用法
- `/help <cmd> <subcmd>` — 查看更深层级

## 回调: `on_message`

```lua
function on_message(event) → (consumed, skip_reply)
```

| event 字段 | 类型 | 说明 |
|----------|------|------|
| `post_type` | string | `"message"` |
| `message_type` | string | `"private"` / `"group"` |
| `user_id` | number | 发送者 QQ |
| `group_id` | number | 群号 |
| `raw_message` | string | 消息原文 |
| `message_id` | number | 消息 ID |
| `sender` | table | 发送者信息 `{user_id, nickname, sex, age, card}` |
| `admins` | []string | admin QQ 列表（透传 OB AdminQQNumbers） |

**返回值：**
- `consumed` (bool): `true` → 消息**不进 Agent**。注意：**不短路**——即使某个插件返回 `true`，其余插件的 `on_message` 仍会全部执行完（适合"多个监听插件都要看到消息"的场景）。
- `skip_reply` (bool): `true` → 标记为必回（mustKeep），**跳过参与窗口直接进入 Agent 处理**；当 `consumed=true` 时以 `consumed` 为准（消息不进 Agent）。

> **已移除**：`modified_event`（修改事件）不再支持——插件不得中途改写事件内容（防止上下文失真）。需要拦截/处理消息时，在 `on_message` 中直接调用 `jn.onebot11` API 产生副作用（如 `delete_msg` 撤回、`ban_group_member` 禁言）。

> **命令优先**：`/` 开头的 RawMessage 会**先**进 `commands.Dispatch`，命中命令后直接 sendReply 并短路，`on_message` 不会被调用。插件应优先用 `jn.command.register` 注册命令式交互，将 `on_message` 用于纯事件监听。

## 回调: `on_webhook`

```lua
function on_webhook(event) → (consumed, reply)
```

| event 字段 | 说明 |
|----------|------|
| `event.webhook.path` | 接收路径 |
| `event.webhook.method` | HTTP 方法 |
| `event.webhook.payload` | body 解析结果（JSON 失败则 `{raw="<原文>", type="non-json"}`） |

Webhook 事件永远不走 LLM Agent，是给插件做外部集成（如 GitHub push 通知）。

**两种路由模式：**
- **定向模式** (`/webhook/{plugin_name}`)：请求按插件名称精确路由，只有同名插件收到事件
- **广播模式** (`/webhook` 或 `/`)：无插件名时，广播给所有有 `webhook` 权限的插件

**返回值：**
- `consumed` (bool): 是否已消费事件。广播模式下返回 `true` 会停止遍历后续插件
- `reply` (string, 可选): 定向模式下，reply 会作为 HTTP 响应的 `metadata` 返回给调用方

```lua
function on_webhook(event)
    local p = event.webhook and event.webhook.payload or {}
    if p.action == "opened" then
        jn.onebot11.send_group_msg(987654321, "新 PR: " .. (p.title or "?"))
        return true, "PR opened notification sent"
    end
    return false, "unhandled action"
end
```

## 回调: `on_cronjob`

定时任务回调，由 CronJob 通过统一事件循环 → `Plugin.Dispatch` 分发触发。

```lua
function on_cronjob(event)  -- 无返回值
```

| event 字段 | 类型 | 说明 |
|----------|------|------|
| `post_type` | string | `"cronjob"` |
| `payload` | table | CronJob 配置的 Payload JSON 对象 |
| `admins` | []string | admin QQ 列表 |

- CronJob 事件**不**经过 Agent，仅通过 `Plugin.Dispatch` 分发到插件
- 只有定义了 `on_cronjob` 全局函数且已加载的插件才会被 CronJob 调用
- 前端多选下拉框自动过滤 `supports_cronjob=true` 的已启用插件
- 新增/修改 `on_cronjob` 后需通过前端"重载全部"或 `POST /api/v1/plugins/reload` 热重载

```lua
-- 示例：向 Payload 指定的 QQ 发定时消息
function on_cronjob(event)
    local p = event.payload or {}
    if p.target_qq and p.message then
        jn.onebot11.send_private_msg(p.target_qq, p.message)
    end
end
```

完整示例见 `data/pluggins/webhook-cron/`（on_cronjob + on_webhook）。

## 回调: `on_notice`

通知事件回调，由 OneBot11 的 notice 事件触发（群成员增减、禁言、文件上传、戳一戳等）。

```lua
function on_notice(event)  -- 无返回值
```

| event 字段 | 类型 | 说明 |
|----------|------|------|
| `post_type` | string | `"notice"` |
| `notice_type` | string | `group_upload` / `group_admin` / `group_decrease` / `group_increase` / `group_ban` / `friend_add` / `group_recall` / `friend_recall` / `notify` |
| `sub_type` | string | 子类型（如 `approve`/`invite` 对应 group_increase，`poke` 对应 notify） |
| `user_id` | number | 触发事件的 QQ（如加入群的人） |
| `group_id` | number | 群号 |
| `operator_id` | number | 操作者 QQ（如邀请人、管理员） |
| `target_id` | number | 被操作者 QQ（如被禁言的人） |
| `duration` | number | 禁言时长（秒，仅 group_ban） |
| `file` | table | 文件信息 `{id, name, size, busid}`（仅 group_upload） |
| `admins` | []string | admin QQ 列表 |

```lua
-- 示例：入群欢迎
function on_notice(event)
    if event.notice_type == "group_increase" then
        jn.onebot11.send_group_msg(event.group_id,
            "欢迎 [CQ:at,qq=" .. event.user_id .. "] 加入！")
    end
end
```

完整示例见 `data/pluggins/group-manager/`（on_notice + on_request + 群管理）。

## 回调: `on_request`

请求事件回调（加好友申请、加群邀请）。

```lua
function on_request(event)  -- 无返回值
```

| event 字段 | 类型 | 说明 |
|----------|------|------|
| `post_type` | string | `"request"` |
| `request_type` | string | `friend` / `group` |
| `sub_type` | string | `add` / `invite` |
| `user_id` | number | 请求发起者 QQ |
| `group_id` | number | 群号 |
| `comment` | string | 验证消息 |
| `flag` | string | 请求标识（传给 `handle_friend_request` / `handle_group_request`） |
| `admins` | []string | admin QQ 列表 |

```lua
-- 示例：自动同意加好友
function on_request(event)
    if event.request_type == "friend" and event.comment == "暗号" then
        jn.onebot11.handle_friend_request(event.flag, true, "欢迎")
    end
end
```

## 回调: `on_xxx_response`（异步 API 完成回调）

`xxx_async` 提交的阻塞操作完成后，引擎派发到插件入口回调（与事件派发互斥，保证 LState 安全）。`req_id` 与 `xxx_async` 返回值一致；`ctx` 为调用时保存的现场表（原样带回，未传则 `nil`）。

```lua
function on_t2i_response(req_id, ctx, result, err)
    -- result: 图片 ID（generate_async）或公开 URL（generate_url_async）；err 非 nil 表示失败
end

function on_http_response(req_id, ctx, result, err)
    -- result: {status=number, body=string}；err 非 nil 表示失败
end

function on_sandbox_response(req_id, ctx, result, err)
    -- result: create→{sandbox_id, status}、exec_shell→{output, exit_code}、
    --         exec_python→{output, error}；err 非 nil 表示失败
end
```

| 回调 | req_id 来源 | ctx | result |
|------|-----------|-----|--------|
| `on_t2i_response(req_id, ctx, result, err)` | `t2i.generate_async` / `t2i.generate_url_async` | 调用现场表（原样带回） | 图片 ID / URL |
| `on_http_response(req_id, ctx, result, err)` | `http.get_async` / `http.post_async` | 同上 | `{status, body}` |
| `on_sandbox_response(req_id, ctx, result, err)` | `sandbox.create_async` / `exec_shell_async` / `exec_python_async` | 同上 | 见上注释 |

```lua
-- 示例：http 异步 + 现场保存（url 与回调目标一起带到回调）
function on_message(event)
    local ctx = { url = "https://api.github.com/repos/x/y", group_id = event.group_id }
    local rid = jn.http.get_async(ctx.url, ctx)
    return false, false
end

function on_http_response(req_id, ctx, result, err)
    if err or not ctx then return end
    jn.onebot11.send_group_msg(ctx.group_id, "仓库信息: " .. result.body)
end
```

## 权限速查

| 权限 | 暴露的全局表 |
|------|-------------|
| `*` | 所有 |
| `onebot11` | `onebot11.*` |
| `http` | `http.*` |
| `database` | `database.*` |
| `cache` | `cache.*` |
| `t2i` | `t2i.*` |
| `sandbox` | `sandbox.*` |
| `agent` | `agent.*` |
| (webhook 调用层过滤) | `on_webhook` 会被调用 |
| (cronjob 调用层过滤) | `on_cronjob` 会被调用 |

---

# 三、引擎实现细节

## 核心结构（`internal/pluggin/pluggin.go`）

```go
type PluginEngine struct {
    mu         sync.RWMutex
    plugins    map[string]*LoadedPlugin
    basePath   string                   // 默认 "data/pluggins"
    adapter    SendAdapter              // OneBot11 完整 API
    db         *gorm.DB                 // 共享数据库（⚠ 无真实命名空间）
    cache      *cache.Cache             // Redis（带 pluggin:<name>: 前缀）
    t2i        *t2icaller.Client        // 启动时通常 nil; 运行时通过 agentOp 取
    sandbox    *sandboxcaller.Client
    dao        *dao.Bundle              // Agent 配置查询
    agentOp    AgentOperator            // Provider/MCP/Tool/T2I/Sandbox 切换 + Compact
    currentEv  EventData                // 当前事件上下文
    commands   *CommandRegistry         // 多级命令注册表
}

type LoadedPlugin struct {
    Manifest Manifest
    State    *lua.LState   // 独立 LState（VM 隔离）
    Dir      string
}

type Manifest struct {
    PPID        string   // 稳定 UUID，缺省时自动生成并写回
    Name        string
    Version     string
    Author      string
    Description string
    Entry       string   // 默认 main.lua
    Permissions []string
    System      bool     // true = 系统插件，三层守卫
    Enabled     bool
}
```

## 关键实现点

### 1. 独立 Lua VM（LState 隔离）

每个插件用独立 `*lua.LState`（`lua.NewState()`），VM 间完全隔离，一个插件崩不影响其他。

### 2. SDK 注入（`injectSDK`）

```go
//go:embed sdk/jn.lua
var jnSDKSource string
```

`ensureEmbeddedAssets`（`pluggin.go:2200`）每次启动强制覆盖落盘：`<basePath>/sdk/jn.lua` 与 `system/{pluggin.yaml,main.lua}`，确保 Docker 镜像在不同 bind-mount 上一致。`injectSDK` 把 `<basePath>/sdk/?.lua` 追加到 `package.path`，使 `require("jn")` 可用。

### 3. 按 permissions gate 注入全局表（`injectBaseAPI`）

```go
// pluggin.go:973
func (pe *PluginEngine) injectBaseAPI(L *lua.LState, pluginName string, permissions []string) {
    // log / json 始终
    if plugin.HasPermission("onebot11") { ... }
    if plugin.HasPermission("http")    { ... }
    if plugin.HasPermission("database")&&pe.db!=nil     { ... }
    if plugin.HasPermission("cache")&&pe.cache!=nil     { ... }
    if plugin.HasPermission("t2i")                     { ... }
    if plugin.HasPermission("sandbox")                 { ... }
    if plugin.HasPermission("agent")&&pe.dao!=nil      { ... }
}
```

`HasPermission(perm)`（`pluggin.go:960`）支持精确匹配或 `"*"` 通配。多余申请不会注入，日志会有提示。

### 4. 命令 API（`injectCommandAPI`）

```go
// pluggin.go:1055
__jn_internal.register_command(path, handlerFn, opts)
   ├─ path 转 CommandNode 路径，逐级创建
   ├─ handler 注册到全局 key __jn_cmd_handler_<plugin>_<path> 保活（防 GC）
   └─ 设置 Opts{Description, Usage}
```

SDK `jn.command.register` 是它的薄包装。

### 5. t2i / sandbox 客户端运行时获取

```go
// pluggin.go:1587 (t2i)
getCurrentClient := func() *t2icaller.Client {
    if agentOp != nil {
        if c := genT2IClient(); c != nil { return c }
    }
    return pe.t2i   // 启动期可能为 nil
}
```

→ 插件 `t2i.generate` 每次调用都拿最新客户端，与 HagoCenter / Service 共享指针，支持热更新（API 改 T2I 配置后立即对所有插件生效）。

### 6. CommandRegistry 派发（`command.go`）

```
Dispatch(raw, event):
  分词 -> 走树
  最长前缀匹配带 handler 的节点 -> 调用 handler(剩余 args, event)
  若停在非根但无 handler -> 返回子命令列表作 /help
```

`UnregisterPlugin(name)` 在 Unload 时递归清理该插件所有命令并修剪空叶子。

### 7. 系统插件三层守卫

| 层 | 位置 | 作用 |
|----|------|------|
| Manifest.System | `pluggin.yaml` `system: true` | 标记 |
| `PluginEngine.IsSystem(name)` | `pluggin.go:187` | 引擎层 Unload 拒绝 |
| Service Toggle/Delete | `internal/api/service` | API 层拒绝（返回 40028 PluginIsSystem） |

确保 `system` 插件不可删/停，但**可启用**（支持 idempotent 场景）。

### 8. 事件回调的 PCall 安全

`OnMessage`/`OnWebhook`/`OnCronjob` 用 `L.PCall` 保护调用，handler 抛错只记录日志不影响后续插件或 Agent。

---

# 四、常见坑

1. **QQ 号是 number 而非 string**：`send_private_msg("10086", ...)` 无效，Lua 字符串无法转 Go int64
2. **`on_message` 只有有 `onebot11` 权限的插件才被调用**
3. **`/cmd` 路径与 `on_message` 互斥**：命令命中后不进 `on_message`
4. **`cache` 命名空间隔离（`pluggin:<name>:`），`database` 没有** — 后者权限请谨慎使用并在插件侧加自己的表前缀
5. **改 `pluggin.yaml` 后必须 reload**（Web toggle 或重启），不重新加载不会生效
6. **系统插件目录 `system/` 每次启动被二进制覆盖** — 不要用它存自定义命令，自建插件目录
7. **`database` 权限声称有命名空间隔离，但 `prefixSQL` 是桩未生效** — 任意 SQL，请重度谨慎
8. **改 Lua 文件不 reload 看不到效果**：`PUT /plugins/:id/toggle` 先停再启才会重新 `DoFile`
9. **handler 返回值约定** `(consumed, skip_reply)`（`modified_event` 已移除）：consumed=true 不进入 Agent（其余插件回调仍会继续执行）；skip_reply=true 标记必回，跳过参与窗口直接进 Agent
10. **Webhook 与 CronJob 都不走 LLM**：仅喂插件，是外部集成与定时任务的钩子。若要让 Agent 处理外部输入，插件内 `onebot11.send_*_msg` 自己转发