# AGENTS.md

面向在本仓库中工作的 Agent 会话的指南。代码库已完整实现并处于活跃开发中——`cmd/server/main.go` 将 Postgres / Redis / Core / Adapter / Agent / Plugin / Web API 串接在一起。当文档与代码冲突时以代码为准：文档是批量维护的，可能落后于最近的提交。

## 工具链

- Go 1.25（见 `go.mod`）。模块路径是字面量 `JuanNiang-Neo`（导入时大小写与连字符敏感，例如 `JuanNiang-Neo/internal/adapter`）。
- 前端：Node 18+ / npm（Vue 3 + Vite 6 + Vuetify 3，位于 `web/`）。
- 基线检查通过根目录 `Makefile`：
  - `make build` — 完整构建（前端 → `web/dist` + Go 二进制 `bin/juan-niang-neo`）
  - `make vet` — `go vet ./...`
  - `make lint` — `go vet` + `web-typecheck`（`vue-tsc` ≥ 2.x，支持 Node 18–24）
  - `make test` — `go test ./...`（已有 17 个 `*_test.go`，多数用内存 SQLite，可直接运行）
  - `make docker-up` — 通过 `deployments/docker-compose.yaml` 启动全栈
- CI：GitHub Actions（`.github/workflows/`）——`pr-check.yml` 在每个 PR 上运行 go build/vet/test/gofmt/tidy + 前端 typecheck/build + Docker 镜像构建；`docker-build.yml` 在 main 分支上构建并推送 `ghcr.io/juanniangdev/juan`。

## 术语陷阱（历史读者踩过的坑）

- 原始设计文档 `docs/guidance.md` / `docs/provider.md` 已并入 `docs/development.md`，不再存在。其历史拼写错误（`inferstructure` 指 `infrastructure/`，`internal/provider` 指 `internal/adapter`）只作为文字残留在合并后的文档中。
- 本代码库中 "Provider" 一词存在重载：
  - `internal/adapter.Provider` = OneBot11 反向 WebSocket 适配器。
  - `internal/agent/provider` = LLM Provider 组（OpenAI 兼容 / Anthropic / Gemini 协议，由 `api_mode` 区分；含 Eino ADK 适配器）。
  始终按完整导入路径解析，而不是按 "provider" 这个单词。
- `pluggin`（双 g、单 n）是 Lua 插件系统的*刻意*拼写——模块 `internal/pluggin`、配置文件 `pluggin.yaml`、插件目录 `data/pluggins`。不要把它"修正"为 `plugin`。

## 目录结构

顶层：
- `cmd/server/main.go` — 程序入口，完全装配（基础设施 → core → adapter → agent → 插件 → Web API + 带 watchdog 的优雅退出）；`devconfig.go` 负责加载 `dev.yaml`（优先级：环境变量 > dev.yaml > 内置默认值）。
- `internal/` — 应用代码，模块外不导出任何内容：
  - `adapter/` — OneBot11 反向 WS 服务端 + Webhook 适配器 + API 客户端 + 事件 + 消息段 + `imgs://`/`stk://` base64 解析器。
  - `agent/` — 基于 Eino ADK 的 Agent 核心；子包 `cronjob`、`fishcal`、`mcp`、`memory`（shortterm/longterm/skillmem，长期记忆对话语义召回走 pg_trgm GIN 倒排）、`prompt`、`provider`、`scheduledmsg`、`session`、`skill`、`tool`。`ConcurrencyManager` 限制每个 ChatArea 的并行 Agent 循环数（默认 8）；`reply_strategy.go` 实现 relevance 快路径/批量判断/缓存/降级管线；`event.go` 是批处理事件循环。由 `agent.go` 中的 `HagoCenter` 聚合。
  - `api/` — Hertz Web 引擎 + `middleware`（JWT）+ `router` + `service` + `dto`。`/api/v1` 下共 121 条路由（`internal/api/router/router.go`），根路径仅 `GET /health`。OpenAPI 规范见 `api/openapi.yaml`（与路由对齐）。
  - `core/` — `acl`、`cache`、`dao`、`handler`、`imgstore`、`models`（31 张表，由 `core.go::AutoMigrate` 创建）。
  - `pluggin/` — gopher-lua 引擎：加载器、命令树、内嵌 SDK + 系统插件（`//go:embed`）、插件商店客户端。
  - `logging/` — fatih/color 彩色输出 + JSON 格式化 + 调用栈 + Hub（SSE）。
  - `web/` — 前端 SPA 服务辅助（`SPAHandler`）。运行时读取 `WEB_DIR`（默认 `web/dist`）；`engine.New(addr, webDir, svc)` 注册 `h.NoRoute` 兜底：`/api/*` → 标准 404 JSON 信封，其余路径 → 文件或 `index.html`（Vue Router history 模式）。若 `index.html` 缺失则返回"请先构建前端"的提示页。未通过 `//go:embed` 内嵌。
- `infrastructure/` — `postgres`、`redis`、`sandbox`、`t2i` 适配器（每个含 `handler` 调用方子包）。
- `web/` — Vue 3 + Vite 6 + Vuetify 3 管理面板（28 个视图页，完整实现）。构建产物输出到 `web/dist/`（已被 gitignore）。开发模式下 `vite.config.ts` 将 `/api` 代理到 `http://127.0.0.1:8090`。
- `data/` — 运行时数据；`data/pluggins/` 存放 Lua 插件（13 个示例 + `sdk` + `system`）及其 `pluggin.yaml` 配置（未提交）；`data/imgs/` 图床图片；`data/plugin_store.json` 插件商店配置。
- `docs/` — 完整文档：
  - `api.md` — Web API 全路由文档（27 章，与 121 条路由对齐）
  - `project-details.md` — 架构 / 事件循环 / 调用栈 / 数据模型（mermaid/ASCII）
  - `plugin-development.md` — Lua 插件开发 + API 参考
  - `plugin-store.md` — 插件商店机制与动态配置
  - `deployment.md` — 部署与调试指南（env var / docker / systemd / 反代 / FAQ）
  - `development.md` — 二次开发指南（该读什么/该改什么/不该动什么）
  - `external-services.md` — 外部服务接入（T2I/Sandbox/Provider 热更新）
  - `webhook-cronjob.md` — Webhook / CronJob 触发 Lua 插件
  - `llm-provider-adaptation-plan.md` — LLM 协议适配规划
  - `changelog/` — 按日期记录的变更日志（问题-根因-修复）
- `api/` — 存放 `openapi.yaml`（覆盖全部 121 条路由的 OpenAPI 3.0 规范）。不是 Go 包。
- `sql/` — `init.sql` 仅作文档参考；表实际由 GORM `AutoMigrate` 在启动时创建。
- `config/` — `config.yaml` 参考文件；二进制在运行时读取环境变量，此文件仅供参考。
- `deployments/` — `Dockerfile`（3 阶段：node → go → alpine runtime，前端通过 `WEB_DIR` 从 `/app/web/dist` 运行）+ `docker-compose.yaml`（postgres + redis + app、健康检查、重启策略、命名网络、bind-mount `../data` → `/app/data`）。
- `pkg/` — 空占位目录。`src/` 与 `scripts/` 已移除。

## 前端服务

- `cmd/server/main.go` 读取 `WEB_DIR`（默认 `web/dist`）并传给 `engine.New(addr, webDir, svc)`。
- `internal/web/web.go` 提供 `SPAHandler(webDir)`：
  - 对任何未匹配的 `/api/*` 路由返回标准 `{status,info,data}` 404 信封（保持 API 错误格式统一）。
  - 对其他所有路径：文件存在则直接服务，否则回退到 `index.html` 以支持客户端路由。通过 `filepath.Rel` 阻止路径穿越。
  - 若 `web/dist` 不存在或缺少 `index.html`，返回 200 提示页（让运维看到可操作的指引而不是裸 404）。
- **未内嵌**：二进制依赖磁盘上存在的 `web/dist`。这是刻意设计——纯前端更新无需重新构建二进制，符合项目"配置在磁盘上"的理念。
- 开发模式：Vite 在 `:3000` 服务 SPA 并将 `/api` 代理到 `:8090`，因此永远不会走到 Go 的兜底逻辑。

## Makefile 与 Docker

- 根目录 `Makefile` 编排前端 + 后端 + docker。`make build` = `web-build` → `build-go`；`make dev` 并行运行 Vite + Go；`make docker-up` 构建并启动整个 compose 栈。
- `.dockerignore` 排除 `web/node_modules`、`web/dist`、`.git`、`bin/`、`docs/`、`data/` 等，以保持构建上下文精简。
- `.env.example` 记录 compose 支持的所有环境变量；`docker compose` 自动读取同目录的 `.env`。

## 事实来源规则

- 持久化状态落 Postgres（Redis 承担缓存与短期记忆）。Agent / session / skill / plugin 配置必须同步回 DB。运行时*热*数据——群成员信息缓存、热聊统计、知识库 LRU、活跃循环监控（`LoopTracker`）——是有意的内存态（见 `internal/agent/agent.go`），不要把它当持久化。
- Lua 插件配置是例外：`data/pluggins/<name>/pluggin.yaml`。
- Web 控制台认证：JWT（HS256），单个管理员用户，初始密码 `Admin123`（首次启动后务必修改）。生产环境必须设置 `JWT_SECRET`。
- OneBot11 API 函数注册为 Agent 工具；新增 OneBot11 能力工具应包装 `internal/adapter.Provider` 的方法，而不是重新实现。
- 工具调用在 Eino ADK ReAct 循环内同步执行；Agent 的消息发送统一走 `DeferredSendQueue`（任务执行完再按序发送）。`ConcurrencyManager` 限制每 ChatArea 的并行 Agent 循环数。

## 约定

- 日志使用 `log/slog`（结构化）或 `internal/logging` 模块日志器（`logging.NewModule`）。沿用现有的 `slog.Info/Error(..., "key", val)` 风格；不要引入 `fmt.Println` 或第三方日志库。
- 导入顺序遵循 `internal/adapter/provider.go` 中的 std → 第三方 → `JuanNiang-Neo/...` 分块布局。保持该顺序。
- 注释和标识符中英混用——在你编辑的文件中保持该风格，不要翻译。
- 开发时 `cp dev.yaml.example dev.yaml`；`make run` 自动读取 dev.yaml 中的基础设施连接端点。

## 分支保护与贡献规则

主分支（`main`）已启用严格分支保护，**禁止直接向主分支提交代码**：

- **仓库内贡献者（读写权限）**：所有代码修改必须在**新建的分支**（如 `feature/xxx`、`fix/xxx`、`docs/xxx`）上进行，然后通过 **Pull Request** 合并到主分支；直接 push 到 `main` 会被拒绝。
- **Fork 贡献者（含 agent 协作）**：可在自 fork 仓库的**主分支**上自由开发、提交（fork 的 `main` 不受上游分支保护限制）；但向本仓库贡献改动时，必须**基于功能分支**向本仓库发起 Pull Request；**禁止从 fork 仓库的主分支（`main`/`master`）直接发起 PR**，此类 PR 将被拒绝。
- **重要（agent 协作）**：当用户要求「发起 PR / 合并 PR」时，**不得把功能分支直接合并进 fork 自己的 `main`**——那只是本地合并，并不会把改动贡献给上游。应基于该功能分支向**上游仓库（`upstream`）**发起 Pull Request，由上游维护者合并。
- 主分支的合并只能通过 Pull Request 完成（详见 README「贡献指南」）。
## 同步规则（上游更新处理）

- 提交 PR 前若上游（`upstream`）有更新，必须先在 GitHub 上点击 **Update branch**，再在本地执行 `git fetch upstream && git rebase upstream/main` 变基拉取，确保基于最新上游代码，避免冲突与覆盖他人提交。
- 必须使用 rebase 变基拉取上游提交，**禁止将上游提交 merge 到本地仓库**。
- 禁止在本地直接推送旧基点分支后绕过 rebase 发起 PR。

## 提交信息规范（重要）

遵循 [Conventional Commits 约定式提交](https://www.conventionalcommits.org/zh-hans/v1.0.0/)。

- **格式**：`<type>(<scope>): <subject>`，subject 后空一行接 body，末尾可选 footer
- **type**（必选）：

  | type | 用途 |
  |---|---|
  | `feat` | 新功能 |
  | `fix` | 缺陷修复 |
  | `docs` | 仅文档变更 |
  | `style` | 格式/样式，不影响逻辑 |
  | `refactor` | 重构，不改行为 |
  | `perf` | 性能优化 |
  | `test` | 测试 |
  | `build` | 构建系统/依赖 |
  | `ci` | CI 配置 |
  | `chore` | 其他不修改 src/test 的变更 |
  | `revert` | 回退先前的提交 |

- **scope**（可选）：影响范围（模块/组件/文件名）。本项目常用：
  `web`（前端面板）、`agent`（Agent 核心）、`api`（Web API）、`pluggin`（Lua 插件系统）、
  `docs`（文档内容）、`config`（工程配置）、`deps`（依赖）
- **subject**：中文、简短（≤50 字），概括本次提交的动机而非过程
- **body**：说明改动点、影响范围与必要背景；用**多个独立 `-m`** 组织
  （第一个 `-m` 为标题，后续每个 `-m` 一段无序列表项）；
  **禁止**用 `\n` 把多条说明塞进单个 `-m` 伪装多段
- **footer**（可选）：`BREAKING CHANGE:` 等；如需决策记录可用
  `Constraint:` / `Rejected:` / `Directive:` / `Tested:` trailer
- 示例：

  ```bash
  git commit \
    -m "fix(web): 修复登录页在暗色模式下对比度不足" \
    -m "- 调整按钮与背景色阶" \
    -m "- 补充 hover 态边框"
  ```
