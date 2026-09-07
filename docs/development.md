# JuanNiang-Neo 项目开发文档

本文档面向二次开发者，整合原 `docs/implementation.md`、`docs/dev/guidance.md`、`docs/dev/provider.md` 中对开发最有价值的内容，按"该读什么、该改什么、不该动什么"组织。代码为准；与 `docs/` 冲突时以代码为准。

## 工具链

- Go 1.25（见 `go.mod`）。模块路径 `JuanNiang-Neo`（大小写与连字符都重要，如 `JuanNiang-Neo/internal/adapter`）
- 前端：Node 18+ / npm，Vue 3 + Vite 6 + Vuetify 3（位于 `web/`）
- 基线检查（根 `Makefile`）：
  - `make build` — 前端 → `web/dist` + Go 二进制 `bin/juan-niang-neo`
  - `make vet` — `go vet ./...`
  - `make lint` — `go vet` + `web-typecheck` (`vue-tsc` ≥ 2.x)
  - `make dev` — Vite + Go 并行
- **CI**：GitHub Actions（`.github/workflows/`）`pr-check.yml` 在每个 PR 跑 go build/vet/test/gofmt/tidy + 前端 typecheck/build + Docker 镜像构建；`docker-build.yml` 在 main 合并后推送 `ghcr.io/juanniangdev/juan`。已有 17 个 `*_test.go`（多数用内存 SQLite），`make test` 直接跑它们

## 术语陷阱（别再被坑）

- 原始设计文档 `docs/guidance.md` / `docs/provider.md` 已合并进本文档，不再单独存在；其历史拼写错误（`inferstructure` 应为 `infrastructure/`、`internal/provider` 应为 `internal/adapter`）只保留在下方说明中。
- `internal/adapter.Provider` ≠ `internal/agent/provider.ProviderGroup`：前者是 OneBot11 反向 WS 适配器，后者是 LLM Provider 组。永远按完整 import path 解析，别只看 "Provider" 这个词。
- `pluggin`（双 g 单 n）是**有意**拼写：模块 `internal/pluggin`、配置 `pluggin.yaml`、插件目录 `data/pluggins`。不要"修复"为 `plugin`。
- `web/dist` 不嵌入二进制；改前端不必重编 Go。

## 该读什么（按开发目标定位）

| 你想做 | 起点 |
|--------|------|
| 加一条 Web API | `internal/api/router/router.go`（121 路由在此注册 + `/health`）、`internal/api/service/service.go`（handler）、`internal/api/dto/` |
| 加 Agent 内置工具 | `internal/agent/tool/builtin.go::RegisterBuiltinTools`；参考既有 `send_*_msg`/`browser_search` 等 |
| 接新 LLM 协议 | `internal/agent/provider/provider.go`（现 OpenAI 兼容 + Eino ADK adapter），实现 `Provider` 接口 |
| 加记忆类型 | `internal/agent/memory/` 子包：`shortterm/`（Redis 滑窗，默认 100 条 + AutoCompact）、`longterm/`（PG + HotArea）、`skillmem/`（技能记忆） |
| 加日志模块 | `internal/logging/`（`github.com/fatih/color` 彩色 stdout + JSON + WARN+ 调用栈 + Hub/SSE + GORM SQL + Web UI） |
| 调整 Agent 并发限制 | `internal/agent/concurrency.go`（默认 8/ChatArea） |
| 调整相关性判断优化 | `internal/agent/reply_strategy.go`（规则快路径/批量判断/超时/失败策略）+ `event.go::filterRelevant`（缓存/冷却/刷屏降级编排）+ `agent.go`（判断信号量/热聊统计） |
| 修改分段回复算法 | `internal/agent/event.go::splitMessages`（Maibot 式自然断句） |
| 加 ACL 维度 | `internal/core/acl/acl.go::Check` + `models.ACLRule` |
| 写 Lua 插件 | 读 [plugin-development.md](plugin-development.md) |
| 改前端页面 | `web/src/views/*.vue`（28 页）、`web/src/api/*`（typed endpoints）、`web/src/router/index.ts` |
| 改 Plugin SDK | `internal/pluggin/sdk/jn.lua`（`//go:embed`，带 LuaCATS 注解） |
| 加数据模型 | `internal/core/models/` 加 GORM model + `core.go::AutoMigrate` 注册 + `internal/core/dao/` DAO + `dao.NewBundle` 接入 |
| 加知识库内容/调匹配策略 | `internal/core/dao/knowledgeDao.go::Match`（关键词+ILIKE 匹配）；`internal/agent/knowledge.go`（LRU/异步提取/注入） |
| 改图床（存储/API/引用解析） | `internal/core/imgstore/`（文件存储）+ `internal/core/dao/imageDao.go`（元数据）+ `internal/api/service/service.go`（上传/校验）+ `internal/adapter/api.go::resolveImageAssets`（imgs:// 解析） |
| 改表情包库 | `internal/core/dao/stickerDao.go` + `internal/agent/sticker.go`（Agent 工具）+ `internal/pluggin/pluggin.go::injectOneBot11`（send_*_sticker） |
| 改摸鱼人日历 | `internal/agent/fishcal/fishcal.go`（独立调度器/模板/农历/节假日）+ `internal/core/dao/fishCalDao.go`（配置+按天群务） |
| 改定时消息（积木编排） | `internal/agent/scheduledmsg/scheduledmsg.go`（块链调度/段渲染）+ `internal/core/dao/scheduledMsgDao.go` + 前端 `web/src/views/ScheduledMessagesPage.vue` |

## 项目目录速查

```
cmd/server/main.go            入口 (组装/启动/退出)
internal/
  adapter/      OneBot11 反向 WS + Webhook (Adapter / WebhookAdapter / Event / Segment)
  agent/        Agent 核心 — Eino ADK (adk.ChatModelAgent + adk.Runner + ConcurrencyManager)
    provider/   OpenAI 兼容客户端 (Chat / Vision + Eino model adapter)
    mcp/        MCP 客户端 (mark3labs/mcp-go SSE)
    memory/     记忆子系统: shortterm(Redis 滑窗 100 条 + AutoCompact) / longterm(PG+HotArea) / skillmem(技能记忆)
    prompt/     系统锁定提示词 + 拼接
    session/    会话 + ChatRecord 持久化
    skill/      关键词/正则技能匹配
    tool/       ToolRegistry + 内置工具 + Eino InvokableTool 适配 (BuildEinoTools)
    cronjob/    robfig/cron 调度器 (on_cronjob 事件注入)
    fishcal/    摸鱼人日历 (独立 cron + T2I 渲染 + 模板/农历/节假日/金句)
    scheduledmsg/ 定时消息 (独立 cron + 积木式块链调度)
    agent.go            HagoCenter 聚合 (Init/Stop/buildEinoAgent)
    agent_operator.go   插件 Agent 操作 (SetProviderActive/SwitchProvider/SetMCPActive/SetToolActive/CompactMemory…)
    concurrency.go      每 ChatArea 并发控制 (默认 8 goroutine)
    eino_middleware.go  Eino ADK 中间件 (BeforeAgent 动态指令注入 / AgentLite 工具过滤 / WrapInvokableToolCall 同步执行包装)
    event.go            三阶段事件循环 (Plugin.Dispatch → ReplyStrategy → dispatchToAgent)
    reply_strategy.go   回复策略 (收敛为仅 Relevance：规则快路径 + 批量判断/缓存/降级)
  api/          Hertz Web (engine + middleware + router + service)
  core/         Init / dao.Bundle / models (31 表) / acl / cache / imgstore(图床文件存储)
  pluggin/      Lua 引擎 + 命令树 + 内嵌 SDK + 系统插件
  web/          SPAHandler (NoRoute 兜底)
  logging/      fatih/color 彩色 stdout + JSON 格式化 + 调用栈 + Hub(SSE)
infrastructure/
  postgres/ redis/      基础客户端 (功能选项)
  sandbox/ t2i/         含 /handler (caller 子包, 真正 Client)
web/                    Vue 3 SPA (28 views)
data/pluggins/          Lua 插件 (示例插件入仓, sdk/system 运行时生成)
deployments/            Dockerfile (3 段) + docker-compose.yaml
docs/                   本文档树
```

## 当前实现状态（哪些是真实现，哪些是桩）

| 项 | 状态 |
|----|------|
| Adapter 反向 WS / Webhook / API | ✅ 完整实现 |
| EventLoop / processEvent 三阶段 (Plugin.Dispatch → ReplyStrategy → dispatchToAgent) | ✅ |
| Eino ADK Agent (adk.ChatModelAgent + adk.Runner + ConcurrencyManager) | ✅ |
| CronJobManager (robfig/cron + on_cronjob 回调) | ✅ |
| Provider (OpenAI 兼容 + 流式 + Vision) | ✅ |
| MCP (SSE) | ✅ |
| Memory (shortterm Redis 100 条滑窗 + AutoCompact / longterm PG+HotArea / skillmem) | ✅ |
| Prompt (SystemLocked + BuildFullContext，工具感知走 Eino tools 参数不拼入提示词) | ✅ |
| ToolRegistry + 内置工具 | ✅（除 `vision` builtin 只返回提示，真 Vision 走 reply_strategy.go）|
| Lua 插件引擎 + 命令树 + 系统 SDK + 系统插件 | ✅ |
| Web API 121 路由 (+`/health`) + JWT + SSE 日志 | ✅ |
| 前端 28 页 (Vue 3 + Vuetify 3) | ✅ |
| AgentLite 模式 / StripMarkdown / 分消息段 | ✅ |
| relevance 判断优化（L1 规则快路径 / L2 批量判断+结果缓存+冷却 / L3 并发限流+超时 / L4 刷屏降级+失败策略） | ✅ |
| 工具"仅管理员"开关（admin_only，Tools 页逐工具切换，防提示词注入） | ✅ |
| SQL 知识库（Web CRUD + Agent 异步提取关键词 + 对话前 LRU/模糊匹配注入提示词） | ✅ |
| 图床（data/imgs 存储 + 1.5MB/MIME 校验 + 虚拟文件夹 + imgs:// 发送层解析） | ✅ |
| 表情包库（短 UUID 表情 + 标签 + send_sticker 工具 + Plugin send_*_sticker + subType=1） | ✅ |
| 摸鱼人日历（独立 cron + T2I 渲染 + 多群 + 按天群务 + 农历/节假日/金句） | ✅ |
| 定时消息（积木式编排：触发器/消息块/延时块 + 独立调度器） | ✅ |
| 示例插件（data/pluggins 下 8 个，覆盖命令/事件/HTTP/存储/媒体/Agent/Webhook/Cron） | ✅ |
| `internal/agent/memory/root.go::Memory` 接口 | ⚠ 空 stub (无方法) |
| `internal/agent/skill/root.go`、`prompt/root.go` | ⚠ 占位 (实现在 .go) |
| `HagoCenter.Stop()` | ⚠ 空实现（仅打日志），事件循环/CronJob 退出依赖外层 ctx 取消 |
| `HagoCenter.SetToolActive` | ⚠ 停用只能 Unregister，无法重新注册已 Unregister 的 builtin |
| `internal/core/handler/` | ⚠ 空目录占位 |
| `database` 插件权限的 `prefixSQL` | ⚠ 桩，未生效，任意 SQL |
| 内置 `vision` 工具 | ⚠ 返回提示，不真正取图（真 Vision 见 reply_strategy.go:70） |

## 约定（必须遵守）

- **持久化**：所有有状态模块（Agent/Session/Skill/Memory/Provider/MCP/Plugin 元数据等）的状态必须同步回 Postgres；**不引入纯内存状态**。Redis 仅作 Session 消息历史、短期记忆窗口、PubSub 与插件/Agent 缓存。
- **日志**：使用 `internal/logging` 自定义日志包（底层 `github.com/fatih/color`），通过 `logging.NewModule("name")` 创建模块 logger；**不引入** `fmt.Println` 或 `log/slog`。
- **导入顺序**：std → 第三方 → `JuanNiang-Neo/...` 三段。见 `internal/adapter/provider.go`。
- **注释与标识符**：混合中英文，保持所在文件原有风格，**不要翻译**。
- **OneBot11 API 复用**：新增 OneBot11 能力应包装 `internal/adapter.Provider` 方法，不要再实现一份。
- **Agent 并发**：`dispatchToAgent` 通过 goroutine + `ConcurrencyManager.Acquire/Release` 异步执行，每 ChatArea 默认并发上限 8。所有工具调用在 Eino ReAct 循环内同步完成，无独立后台任务管道。
- **开发配置**：开发时使用 `dev.yaml` 配置基础设施连接端点（`make run` 自动读取；`cp dev.yaml.example dev.yaml`）。
- **Web 控制台**：JWT 鉴权，单管理员，初始化 `admin / Admin123`（首次启动务必改）。
- **插件配置仍走磁盘**：`data/pluggins/<name>/pluggin.yaml`，不入 DB。
- **错误码**：业务错误用 `dto.Response{...}`（如 `dto.AdapterNotInitialized`），通过 `dto.GenFinalResponse` 包装，HTTP 200。

## 数据模型与持久化策略

- Postgres 拥有**所有**持久状态（23 张表，见 `internal/core/core.go::AutoMigrate`）
- Redis 仅作：短期记忆滑动窗口（`shortterm:msgs:<areaID>` List）、PubSub 任务结果通知、插件/Agent 任意缓存
- `ChatRecord.id` 为自增 int64（不是 UUID，多数表用 UUID），保留这个差异
- 单行配置表（`Onebot11Adapter`/`WebhookConfig`/`T2IConfig`/`SandboxConfig`）固定 `id=1`，首次 `InitConfig` 用 `OnConflict DoNothing` 建默认行
- `ReplyStrategyConfig` 无 `DeletedAt`（单例）
- 长期记忆 `Embedding []byte` 字段已就位但**当前未做向量检索**；对话召回走**语义匹配**（消息 gram → pg_trgm GIN 倒排候选 + `similarity` 排序，空候选回退最近条目；环境变量 `LTM_RECALL_MODE=recent` 可回退为纯最近召回）

## 改动检查流程

改完代码后推荐：

```bash
make vet           # go vet
make lint          # go vet + 前端 typecheck
make build         # 全量构建验证 (前端 + Go 二进制)
# 若改了 web/src: make web-lint
```

没有单元测试 CI，请在日志 + Web 面板手工验证关键路径。

## 写 Agent 工具的最小范式

```go
// internal/agent/tool/builtin.go, 在 RegisterBuiltinTools 内:
h.Register(...)
tool := NewTool("my_tool", "用途描述",
    StringParam("arg1"), /* JSON Schema 参数 */
    func(ctx context.Context, args map[string]any) (string, error) {
        // args["arg1"].(string)
        // 调 adapter / MCP / DB
        return "结果文本（喂给 LLM 的 tool-role msg）", nil
    })
// 工具通过 tool.BuildEinoTools() 自动适配为 Eino InvokableTool，在 ReAct 循环内同步调用
```

参数帮助函数：`StringParam/Int64Param/MessageParam/GroupIDUserIDParams/TimeParams`（`tool/root.go`）。

## 写 Web API 的最小范式

```go
// internal/api/router/router.go 在 /api/v1 group 内注册:
g.POST("/myresource", svc.AddMyResource)

// internal/api/service/service.go 在 *Service 上加 handler:
func (s *Service) AddMyResource(ctx *app.RequestContext) {
    var req MyReq
    if err := ctx.BindAndValidate(&req); err != nil {
       c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{...}))
       return
    }
    // s.DAO.<...> 操作
    c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, resp))
}
```

DTO 转换集中在 `dto/transfer.go`，枚举预定义在 `dto/response.go`。

## 提交时注意

- 不要把 `web/node_modules/`、`web/dist/`、`bin/`、`data/`、`docs/` 改动意外提交（`.gitignore` 已排除 `data/pluggins/`）
- 系统插件 `internal/pluggin/systemplugin/` 是内嵌资源，修改需在 `go:embed` 范围内
- 不要修复 `pluggin` 拼写、不要翻译中英混排的注释/标识符