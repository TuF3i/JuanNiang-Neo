# JuanNiang-Neo

![banner](docs/img/banner.png)

> 复活吧卷娘 — 基于 OneBot11 协议的 LLM QQ 聊天 Agent

***JuanNiang-Neo 是一个以 Go 1.25 构建的 QQ 机器人项目，红岩网校的吉祥物卷娘。***

核心由 LLM 驱动的对话 Agent（`HagoCenter` 聚合 Provider / MCP / Memory / Prompt / Session / Skill / Tool）与 OneBot11 反向 WebSocket 适配器组成，基于 [Eino ADK](https://github.com/cloudwego/eino) 框架构建 `ChatModelAgent`，工具调用在 ReAct 循环内同步执行，每个聊天区域最多 8 个 Agent goroutine 并发处理（由 `ConcurrencyManager` 控制）。事件流经三阶段管线：Plugin 拦截 → 回复策略检查 → 异步派发 Agent。项目同时包含 Lua 插件引擎、Vue 3 管理面板，以及 Postgres + Redis + Sandbox + T2I 等可插拔基础设施，并内置知识库、图床、表情包库、摸鱼人日历、定时消息（积木式编排）等开箱即用的功能模块。所有持久化状态落 Postgres + Redis，配置与运行时状态均可在 Web 面板热切换。

## 贡献指南（分支保护）

本仓库对主分支（`main`）启用了严格分支保护，**禁止直接向主分支提交代码**：

- **仓库内贡献者（读写权限）**：所有代码修改必须在**新建的分支**（如 `feature/xxx`、`fix/xxx`、`docs/xxx`）上进行，然后通过 **Pull Request** 合并到主分支；直接 push 到 `main` 会被拒绝。
- **Fork 贡献者（含 agent 协作）**：可在自 fork 仓库的**主分支**上自由开发、提交（fork 的 `main` 不受上游分支保护限制）；但向本仓库贡献改动时，必须**基于功能分支**向本仓库发起 Pull Request；**禁止从 fork 仓库的主分支（`main`/`master`）直接发起 PR**，此类 PR 将被拒绝。
- **重要（agent 协作）**：当用户要求「发起 PR / 合并 PR」时，**不得把功能分支直接合并进 fork 自己的 `main`**——那只是本地合并，并不会把改动贡献给上游。应基于该功能分支向 **上游仓库（upstream）** 发起 Pull Request，由上游维护者合并。
- 主分支的合并只能通过 Pull Request 完成。
## 同步规则（上游更新处理）

- 提交 PR 前若上游（`upstream`）有更新，必须先在 GitHub 上点击 **Update branch**，再在本地执行 `git fetch upstream && git rebase upstream/main` 变基拉取，确保基于最新上游代码，避免冲突与覆盖他人提交。
- 必须使用 rebase 变基拉取上游提交，**禁止将上游提交 merge 到本地仓库**。
- 禁止在本地直接推送旧基点分支后绕过 rebase 发起 PR。

## 主要特性

- **Agent 系统**：基于 Eino ADK 的 `ChatModelAgent`（OpenAI 兼容），支持 Provider / MCP / Tool / Skill / Prompt / Plugin 多模块组合，工具调用在 ReAct 循环内同步完成
- **异步并发处理**：`ConcurrencyManager` 控制每 ChatArea 最多 8 个 Agent goroutine 并发，事件经三阶段管线（Plugin 拦截 → 回复策略 → Agent 派发）高效分流
- **参与窗口群聊**：@/命令/提及名字/私聊必回，其余消息攒入参与窗口，安静间隔后整窗一次交给 Agent 参与（带随机静默/打字延迟，插话计数与最迟必发保证不冷场）
- **四层记忆体系**：短期记忆（Redis 滑动窗口，默认 100 条，自动 Compact）/ 长期记忆（Postgres + 内存 LRU）/ 技能记忆（SkillMemory，Compact 时自动提取）/ 会话记录（Postgres 审计）
- **OneBot11 反向 WebSocket 适配器**：与 QQ 机器人框架对接，OneBot11 API 作为 Agent 工具注册
- **Lua 插件系统**：gopher-lua 驱动，支持多级命令、Lua SDK（带 LuaCATS 注解）、插件目录文本文件读写（`jn.file`）、系统插件保护
- **插件商店**：从 GitHub 仓库浏览/安装社区插件（统一 5 件套格式），国内镜像源手动选择 + 连通性测试，每晚自动更新元数据；插件动态配置（bool/string/list）由 Web 面板按 `config.yaml` 动态渲染
- **Web 管理后台**：Vue 3 + Vuetify 3，JWT 鉴权（可选 OIDC SSO），管理全部配置与运行时状态
- **基础设施**：Postgres 持久化 + Redis 缓存 + Sandbox 代码沙箱 + T2I 文生图，未配置时自动返回未启用提示
- **彩色日志系统**：基于 `fatih/color` 的自定义日志，彩色输出、JSON 自动格式化、WARN+ 调用栈、模块日志器、GORM SQL 日志集成
- **SQL 驱动知识库**：Web 存知识 → Agent 异步提取关键词 → 对话前模糊匹配注入提示词；50 条 LRU 加速检索
- **图床服务**：`data/imgs` 存储 + MIME/大小校验 + 虚拟文件夹；`imgs://<ID>` 引用由发送层自动转 base64（Plugin / Agent 无感）
- **表情包库**：图床二次封装（名称/简介/标签）；短 UUID 对外，`stk://` 引用自动映射图床长 UUID（表情段 subType=1）；Agent 工具 + Plugin API 齐备
- **摸鱼人日历**：独立每日定时任务，模板 → T2I 渲染 682×757 纸张朱印质感图片（洛谷式日历卡）→ 富文本发送（不 @全体成员）；多群、按天群务、一言金句、农历/法定假日倒计时
- **定时消息（积木式编排）**：触发器 → 消息块（一条消息多段：文字 / 图片[T2I·URL·图床] / CQ 表情）→ 延时块链；Web 可视化编排 + 325 个 CQ 表情缩略图
- **示例插件库**：`data/pluggins/` 下 10 个示例插件覆盖全部插件功能（含 `xxx_async` 异步调用示范），每个含 README.md
- **开发配置**：`dev.yaml` 本地开发配置文件（数据库、Redis、OneBot11 等），`make run` 自动读取

## 效果图

- **Login UI**

![login-ui](docs/img/login.png)

- **Home**

![login-ui](docs/img/home.png)

- **Chat**

<img src="docs/img/chat.png" alt="login-ui" style="zoom: 100%;" />

## 文档导航

| 分类 | 文档 | 说明 |
|------|------|------|
| Web API | [api.md](docs/api.md) | Web API 全路由文档（响应信封、错误码、各资源 CRUD、SSE 日志流、SPA 静态服务）；含知识库(§23)/图床(§24)/表情包库(§25)/摸鱼人日历(§26)/定时消息(§27) 各功能章节 |
| 项目细节 | [project-details.md](docs/project-details.md) | Eino ADK Agent 架构 / 三阶段事件循环 / 数据模型 / HagoCenter 运行时拓扑（mermaid）；关键调用栈（ASCII）；processEvent 决策树、CronJob/Webhook 注入时序图（mermaid）；Lua 插件系统架构生命周期与命令树（mermaid） |
| 日志系统 | `internal/logging/` | fatih/color 彩色终端输出 / JSON 结构化格式化 / 完整调用栈追踪 / Hub SSE 实时推送至前端 |
| 插件开发 | [plugin-development.md](docs/plugin-development.md) | 快速开始 + 完整 Lua API 参考（log/json/onebot11/http/database/cache/t2i/sandbox/agent/file/command）+ 引擎实现细节 + 常见坑；示例插件见 `data/pluggins/`（10 个，覆盖全部功能，含 `xxx_async` 异步示范） |
| 插件商店 | [plugin-store.md](docs/plugin-store.md) | 统一插件格式（5 件套）/ 动态配置（config.yaml + jn.config）/ 商店与管理页 Web 界面与 API / 镜像源与国内加速 / 插件仓库元数据每晚自动更新 / PR 审核流程 |
| 部署 | [deployment.md](docs/deployment.md) | 部署模式、环境变量、构建流程、健康检查、日志排查、反向代理、systemd、FAQ |
| 二次开发 | [development.md](docs/development.md) | 该读什么 / 该改什么 / 不该动什么、当前实现状态、约定、写 Agent 工具与 Web API 的最小范式 |
| 外部服务 | [external-services.md](docs/external-services.md) | 各外部服务的客户端构造、热更新机制、HagoCenter 与 Service 共享指针、鉴权与健康检查 |
| Webhook / CronJob | [webhook-cronjob.md](docs/webhook-cronjob.md) | Webhook 接外部 HTTP 触发 Lua 插件；CronJob 6 字段秒级 cron 定时触发 Lua 插件 `on_cronjob` 回调（不经过 Agent）；含 GitHub PR / 早安提醒示例 |

## 快速部署（Docker Compose）

镜像已发布至 `ghcr.io/juanniangdev/juan`。在任意目录新建 `docker-compose.yaml`：

```yaml
name: juanniang-neo

services:
  postgres:
    image: postgres:16-alpine
    restart: unless-stopped
    environment:
      POSTGRES_USER: ${DB_USER:-postgres}
      POSTGRES_PASSWORD: ${DB_PASSWORD:-postgres}
      POSTGRES_DB: ${DB_NAME:-juan}
    volumes:
      - pgdata:/var/lib/postgresql/data
    ports:
      - "5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U $${POSTGRES_USER} -d $${POSTGRES_DB}"]
      interval: 10s
      timeout: 10s
      retries: 5
    networks: [juanniang-net]

  redis:
    image: redis:7-alpine
    restart: unless-stopped
    command: redis-server --requirepass ${REDIS_PASSWORD:-root}
    ports:
      - "6379:6379"
    healthcheck:
      test: ["CMD", "redis-cli", "-a", "${REDIS_PASSWORD:-root}", "ping"]
      interval: 10s
      timeout: 10s
      retries: 5
    networks: [juanniang-net]

  juan-niang-neo:
    image: ghcr.io/juanniangdev/juan:latest
    restart: unless-stopped
    init: true
    ports:
      - "8081:8081"   # OneBot11 反向 WS
      - "8090:8090"   # Web API + 仪表板
    environment:
      WEB_DIR: /app/web/dist
      OB_PORT: "8081"
      API_ADDR: ":8090"
      DB_HOST: postgres
      DB_PORT: "5432"
      DB_USER: ${DB_USER:-postgres}
      DB_PASSWORD: ${DB_PASSWORD:-postgres}
      DB_NAME: ${DB_NAME:-juan}
      REDIS_ADDR: redis:6379
      REDIS_PASSWORD: ${REDIS_PASSWORD:-root}
      JWT_SECRET: ${JWT_SECRET:-change-me-in-production}
      OB_TOKEN: ${OB_TOKEN:-}
      OB_ADMINS: ${OB_ADMINS:-}
    depends_on:
      postgres: { condition: service_healthy }
      redis:    { condition: service_healthy }
    volumes:
      - ./data:/app/data                     # 插件/图床/商店配置跨升级保留
    healthcheck:
      test: ["CMD-SHELL", "wget -qO- http://127.0.0.1:8090/health || exit 1"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 20s
    networks: [juanniang-net]

volumes:
  pgdata:

networks:
  juanniang-net:
    driver: bridge
```

同目录放 `.env`（按需修改）。**首次启动前**需创建插件目录并赋权：

```bash
mkdir -p data && chmod 777 data
docker compose up -d
# 仪表板  http://localhost:8090   初始账号 admin / Admin123（首次启动务必改密码）
# OneBot11 反向 WS  ws://localhost:8081/   带头 Authorization: Bearer <OB_TOKEN>
```

更多部署细节（裸机、反代、systemd、故障排查）见 [deployment.md](docs/deployment.md)。

## 开发

```bash
cp dev.yaml.example dev.yaml   # 复制并按需修改数据库等配置
make dev                        # Vite (:3000) + Go (:8090) 并行
make run-debug                  # 仅后端 debug 模式 (pprof :6060)
```

dev.yaml 优先级：环境变量 > dev.yaml > 内置默认值。详见 [development.md](docs/development.md)。

## 许可证

本项目采用 [MIT License](LICENSE)。
