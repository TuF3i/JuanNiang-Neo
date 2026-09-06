package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	ragcaller "JuanNiang-Neo/infrastructure/rag/handler"
	sandboxcaller "JuanNiang-Neo/infrastructure/sandbox/handler"
	t2icaller "JuanNiang-Neo/infrastructure/t2i/handler"
	"JuanNiang-Neo/internal/adapter"
	"JuanNiang-Neo/internal/agent/cronjob"
	"JuanNiang-Neo/internal/agent/groupmgr"
	"JuanNiang-Neo/internal/agent/mcp"
	"JuanNiang-Neo/internal/agent/memory"
	"JuanNiang-Neo/internal/agent/memory/longterm"
	"JuanNiang-Neo/internal/agent/memory/shortterm"
	"JuanNiang-Neo/internal/agent/memory/skillmem"
	"JuanNiang-Neo/internal/agent/prompt"
	"JuanNiang-Neo/internal/agent/provider"
	"JuanNiang-Neo/internal/agent/session"
	"JuanNiang-Neo/internal/agent/skill"
	"JuanNiang-Neo/internal/agent/stats"
	"JuanNiang-Neo/internal/agent/tool"
	"JuanNiang-Neo/internal/core/acl"
	"JuanNiang-Neo/internal/core/cache"
	"JuanNiang-Neo/internal/core/dao"
	"JuanNiang-Neo/internal/pluggin"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/google/uuid"
)

// HagoCenter 是 Agent 系统的中央调度器，聚合所有子模块。
type HagoCenter struct {
	Adapter        *adapter.Adapter
	WebhookAdapter *adapter.WebhookAdapter
	Providers      *provider.ProviderGroup
	MCP            *mcp.MCPGroup
	Memory         *memory.MemoryGroup
	Prompt         *prompt.PromptManager
	Session        *session.SessionManager
	Tools          *tool.ToolRegistry
	Skills         *skill.SkillEngine
	ACL            *acl.ACL
	DAO            *dao.Bundle

	// T2I 和 Sandbox 运行时客户端（可通过 API 热更新）
	SandboxClient *sandboxcaller.Client
	T2IClient     *t2icaller.Client
	// RAGClient 向量检索服务客户端（可通过 API 热更新）；Load() 为 nil=未启用，
	// 记忆/知识检索自动降级到非 RAG 路径（pg_trgm / SQL 匹配）。
	// 原子指针：Web 配置热更新（HTTP goroutine）与 Compact/召回（agent goroutine）并发读写无竞争。
	RAGClient atomic.Pointer[ragcaller.Client]

	Concurrency    *ConcurrencyManager
	CronJobManager *cronjob.Manager
	CronJobEvents  chan adapter.Event // CronJob → 主 Agent 事件循环
	PluginEngine   *pluggin.PluginEngine
	GroupMgr       *groupmgr.Manager // 群管理系统功能（Phase 0.5 检测闸门）
	Loops          *LoopTracker      // 当前活跃的 Agent ReAct 循环（监控展示）

	// Stats 群消息 / Agent 回复统计事件写入器（Loki+Promtail 专用通道，独立于主日志 pipeline）。
	// nil = 未启用（配置关闭时）；Emit 调用方需判 nil。
	Stats *stats.Writer

	// SelfID 和 SelfNickname 从 Adapter 获取后缓存
	SelfQQ       int64
	SelfNickname string
	// 消息批处理：同一 ChatArea 在短窗口内的消息合并为一次 Agent 处理
	batches map[string]*pendingBatch
	batchMu sync.Mutex

	// sendMu 全局发送互斥锁：所有批次的发送动作串行执行，
	// 避免多批次/多分组并行完成时回复交叉乱序（如一条完整回复被另一条插入打断）。
	// 只保护"发送动作"（毫秒级），不阻塞 ReAct 循环，不影响多群并行处理。
	sendMu sync.Mutex

	// 相关性判断结果缓存（Redis，L2.3/L4.2）
	Cache *cache.Cache

	// 工具"仅管理员"名单（DB 驱动，Tools 页可切换）：admin_only=true 的工具
	// 只能由 Admins 列表内用户调用（防提示词注入诱导敏感操作）
	toolAdminOnlyMu sync.RWMutex
	toolAdminOnly   map[string]bool

	// 相关性判断并发闸门（L3.1）：限制全局并发，避免热聊时打爆 provider
	relevanceSem chan struct{}

	// 热聊统计（内存，L2.2/L4.1）：1s 滑动窗口消息计数，用于动态批窗口与刷屏降级
	hotMu    sync.Mutex
	hotStats map[string]*hotStat

	// 知识库 LRU（50 条，缓存对话前检索结果，加速匹配）
	knowledgeLRU *knowledgeLRU

	// RAG 候选集缓存（内存态，丢失可重建）：知识侧随知识变更失效，记忆侧 TTL 兜底。
	knowledgeRagSetMu sync.RWMutex
	knowledgeRagSet   map[uuid.UUID]string // 知识 v5 tag → itemID
	memoryRagSetMu    sync.RWMutex
	memoryRagSet      map[uuid.UUID]string // 记忆 v5 tag → itemID
	memoryRagSetAt    time.Time

	// EinoAgent 是 Eino ADK 的 ChatModelAgent，替代手写的 ReAct 循环。
	EinoAgent *adk.ChatModelAgent

	// msgDedup 消息去重器：过滤 WS 断线重连/多连接导致的同一条消息重复投递。
	// 接口类型，由 Init 时按 cfg.Cache 是否可用选 memory/redis 实现。
	msgDedup Deduper

	// 回复策略内存缓存（TTL 20min）：策略为单例配置且极少变更，
	// 避免每条消息都同步查 DB 阻塞事件循环；Web 面板更新策略时通过
	// InvalidateReplySettings 立即失效（TTL 仅作兜底）。
	replySettingsMu  sync.Mutex
	replySettings    ReplySettings
	replySettingsExp time.Time
}

// Config HagoCenter 初始化配置。
type Config struct {
	Adapter        *adapter.Adapter
	WebhookAdapter *adapter.WebhookAdapter
	Sandbox        *sandboxcaller.Client
	T2I            *t2icaller.Client
	RAG            *ragcaller.Client
	Providers      *provider.ProviderGroup
	MCPGroup       *mcp.MCPGroup
	DAO            *dao.Bundle
	ACL            *acl.ACL
	Cache          *cache.Cache
	Stats          *stats.Writer // 群消息/回复统计写入器（nil = 不启用）
}

// NewHagoCenter 创建并初始化 HagoCenter。
func NewHagoCenter() *HagoCenter {
	return &HagoCenter{
		Providers:     provider.NewProviderGroup(),
		MCP:           mcp.NewMCPGroup(),
		Tools:         tool.NewToolRegistry(),
		Skills:        skill.NewSkillEngine(),
		CronJobEvents: make(chan adapter.Event, 64),
		Loops:         NewLoopTracker(),
		batches:       make(map[string]*pendingBatch),
		hotStats:      make(map[string]*hotStat),
		relevanceSem:  make(chan struct{}, relevanceSemLimit),
		toolAdminOnly: make(map[string]bool),
		knowledgeLRU:  newKnowledgeLRU(50),
		msgDedup:      newMemoryDedup(dedupWindow), // 占位，Init 时按 Cache 可用性覆盖为 redisDedup
	}
}

// dedupWindow 消息去重窗口：需大于 WS 断线重连 + 重推积压的最长间隔。
// 5 分钟覆盖一次 Agent ReAct 处理周期（数十秒到数分钟）+ WS 重连重推窗口；
// 原值 60s 过短，Agent 还在跑时 entry 已过期，重推穿透导致重复消费。
const dedupWindow = 5 * time.Minute

// Init 从 DB 加载配置并初始化所有子模块。
func (h *HagoCenter) Init(ctx context.Context, cfg Config) error {
	h.Adapter = cfg.Adapter
	h.WebhookAdapter = cfg.WebhookAdapter
	h.DAO = cfg.DAO
	h.ACL = cfg.ACL
	h.Providers = cfg.Providers
	h.MCP = cfg.MCPGroup
	h.Cache = cfg.Cache
	h.Stats = cfg.Stats

	// 去重器升级：Cache 可用时切换为 Redis 实现（持久化 + 多实例共享 + 原子无锁），
	// 不可用时保留 NewHagoCenter 里默认的 memoryDedup（降级）。
	// 必须在事件循环启动前完成切换，避免运行时类型断言竞态。
	if cfg.Cache != nil {
		h.msgDedup = newRedisDedup(cfg.Cache, dedupWindow)
		log.Info("消息去重器已启用 Redis 模式", "ttl", dedupWindow)
	} else {
		log.Warn("Cache 未注入，消息去重器降级为内存模式（重启即丢失）")
	}

	// 缓存机器人自己的 QQ 号和昵称
	h.SelfQQ = h.Adapter.SelfID()
	if info, err := h.Adapter.GetLoginInfo(); err == nil && info != nil {
		h.SelfNickname = info.Nickname
	}
	log.Info("机器人身份信息", "self_qq", h.SelfQQ, "self_nickname", h.SelfNickname)

	// 存储 T2I/Sandbox/RAG 运行时客户端
	h.SandboxClient = cfg.Sandbox
	h.T2IClient = cfg.T2I
	h.RAGClient.Store(cfg.RAG)

	// Session 管理器: 同时维护 Postgres Session 表 + ChatRecord 表 + Redis (历史路径) + 每日 Token 统计
	h.Session = session.NewSessionManager(cfg.DAO.Session, cfg.DAO.ChatRecord, cfg.DAO.TokenUsageDaily, cfg.Cache)

	// Memory 组: 短期记忆 (Redis) + 长期记忆 (Postgres + 内存 HotArea)
	stConf := shortterm.Config{WindowSize: 100, AutoCompact: true}
	// 长期记忆对话召回：默认语义召回（pg_trgm 倒排 + similarity 排序）;
	// 环境变量 LTM_RECALL_MODE=recent 可回退为旧"最近 N 条"行为（灰度/故障逃生）。
	ltConf := longterm.Config{HotAreaSize: 10, RecallMode: longterm.RecallModeSemantic}
	if strings.EqualFold(os.Getenv("LTM_RECALL_MODE"), "recent") {
		ltConf.RecallMode = longterm.RecallModeRecent
		log.Info("长期记忆召回模式: recent（环境变量 LTM_RECALL_MODE=recent）")
	} else {
		log.Info("长期记忆召回模式: semantic")
	}
	st := shortterm.New(stConf, cfg.Cache)
	lt := longterm.New(ltConf, cfg.DAO.LongTermMemItem)
	sm := skillmem.New(cfg.DAO.SkillMemory)
	if err := sm.Warmup(ctx); err != nil {
		log.Warn("技能记忆预热失败", "err", err)
	}
	h.Memory = memory.NewMemoryGroup(st, lt, sm)
	// 注入 Per-ChatArea 短期记忆配置读取源（cache → DB → 全局默认）
	h.Memory.SetShortTermStore(cfg.DAO.ShortTermMemory)
	// 注入 RAG 客户端（Compact 双写记忆向量；nil=未启用时静默跳过）
	h.Memory.SetRAGClient(cfg.RAG)
	// 注入 Text LLM Provider 动态获取函数（Compact 触发时实时取最新模型）：
	// 必须在 loadProviders 之前调用，启动后 ProviderGroup 才加载完成，
	// 直接赋值 SelectModel 的结果会是 nil，导致 AutoCompact 永不触发。
	h.Memory.SetLLMProviderFn(func() provider.Provider {
		return h.Providers.SelectModel(provider.ModelTypeText)
	})

	h.Prompt = prompt.NewPromptManager(cfg.DAO.Prompt)
	h.Skills = skill.NewSkillEngine()

	// 启动时种子系统锁定提示词（幂等：已存在则同步内容）
	if err := h.Prompt.EnsureSystemPrompt(ctx); err != nil {
		log.Warn("系统锁定提示词种子失败", "err", err)
	}

	// 使用函数 getter 注册工具，支持运行时客户端热更新
	// 注意：getSessionCtx / getCurrentMsg / getRecentMsgs 从 context 中读取 per-message 状态，
	// 避免 HagoCenter 共享字段导致的数据竞争。
	tool.RegisterBuiltinTools(h.Tools, cfg.Adapter,
		func() *sandboxcaller.Client { return h.SandboxClient },
		func() *t2icaller.Client { return h.T2IClient },
		func() provider.Provider { return h.Providers.SelectModel(provider.ModelTypeImage) },
		func(ctx context.Context) string {
			if sc := GetMsgSessionCtx(ctx); sc != nil {
				return sc.SessionCtxStr
			}
			return ""
		},
		func(ctx context.Context) *adapter.MessageEvent {
			if sc := GetMsgSessionCtx(ctx); sc != nil {
				return sc.Msg
			}
			return nil
		},
		func(ctx context.Context, msgType string, targetID int64, limit int) ([]string, error) {
			return h.getRecentMessagesByMsgType(ctx, msgType, targetID, limit)
		},
		func(ctx context.Context, folder string, limit int) (string, error) {
			return h.listImagesForTool(ctx, folder, limit)
		},
		func(ctx context.Context, keyword string, limit int) (string, error) {
			return h.searchImagesForTool(ctx, keyword, limit)
		},
		func(ctx context.Context) (string, error) {
			return h.listStickerTagsForTool(ctx)
		},
		func(ctx context.Context, tag string, page, pageSize int) (string, error) {
			return h.listStickersForTool(ctx, tag, page, pageSize)
		},
		func(ctx context.Context, keyword string, limit int) (string, error) {
			return h.searchStickersForTool(ctx, keyword, limit)
		},
		func(ctx context.Context, keyword, msgType string, targetID int64) (string, error) {
			return h.sendStickerByKeywordForTool(ctx, keyword, msgType, targetID)
		},
		func(ctx context.Context, keyword string, limit int) (string, error) {
			return h.searchKnowledgeForTool(ctx, keyword, limit)
		},
	)

	// 内置工具"仅管理员"标志：seed 默认高危名单到 DB（幂等），并加载运行时权限表
	h.seedBuiltinToolGuard(ctx)

	if err := h.loadProviders(ctx); err != nil {
		return err
	}
	if err := h.loadMCPs(ctx); err != nil {
		return err
	}
	if err := h.loadSkills(ctx); err != nil {
		return err
	}

	h.Concurrency = NewConcurrencyManager(8)
	// 全局并发上限：单群上限 8，但多群同时活跃时 goroutine 数会随群数线性增长，
	// 统一封顶避免 OOM 与 LLM provider 限流（每个 Agent ReAct 循环占用一个全局槽位）。
	h.Concurrency.SetGlobalLimit(globalAgentConcurrency)
	h.CronJobManager = cronjob.New(h.DAO.CronJob, h.CronJobEvents)

	// 构建 Eino ChatModelAgent（替代手写的 ReAct 循环）
	if err := h.buildEinoAgent(ctx); err != nil {
		log.Warn("Eino Agent 构建失败，将回退到旧模式", "err", err)
	}

	return nil
}

func (h *HagoCenter) loadProviders(ctx context.Context) error {
	list, err := h.DAO.Provider.List(ctx, "")
	if err != nil {
		return err
	}
	for _, p := range list {
		if !p.IsActive {
			continue
		}
		pr := provider.NewProvider(provider.ProviderConfigFromModel(&p))
		h.Providers.AddProvider(pr)
	}
	log.Info("Provider 加载完成", "count", len(list))
	return nil
}

func (h *HagoCenter) loadMCPs(ctx context.Context) error {
	list, err := h.DAO.MCPServer.List(ctx)
	if err != nil {
		return err
	}
	for _, srv := range list {
		if !srv.IsActive {
			continue
		}
		headers := make(map[string]string)
		for k, v := range srv.Headers {
			if str, ok := v.(string); ok {
				headers[k] = str
			}
		}
		client := mcp.NewSSEMCPClient(mcp.McpSSEConfig{
			ID:            srv.ID,
			Name:          srv.Name,
			ServerURL:     srv.ServerURL,
			Headers:       headers,
			Timeout:       0,
			RetryCount:    srv.RetryCount,
			ToolFilter:    srv.ToolFilter,
			AutoReconnect: srv.AutoReconnect,
		})
		if err := client.Connect(ctx); err != nil {
			log.Error("MCP 连接失败", "name", srv.Name, "err", err)
			continue
		}
		h.MCP.AddMCP(client)
	}
	log.Info("MCP 加载完成", "count", len(list))
	return nil
}

func (h *HagoCenter) loadSkills(ctx context.Context) error {
	list, err := h.DAO.Skill.List(ctx)
	if err != nil {
		return err
	}
	for _, s := range list {
		h.Skills.AddSkill(&skill.SkillConfig{
			ID:           s.ID,
			Name:         s.Name,
			Description:  s.Description,
			Keywords:     s.Keywords,
			RegexPattern: s.RegexPattern,
			PromptRefs:   s.PromptRefs,
			ToolRefs:     s.ToolRefs,
			McpRefs:      s.McpRefs,
			IsActive:     s.IsActive,
			IsSystem:     s.IsSystem,
			Priority:     s.Priority,
		})
	}
	log.Info("Skill 加载完成", "count", len(list))
	return nil
}

// buildEinoAgent 构建 Eino ChatModelAgent（替代手写的 ReAct 循环）。
// 在 Init 末尾调用，要求 Providers 和 Tools 已就绪。
func (h *HagoCenter) buildEinoAgent(ctx context.Context) error {
	// 1. 选取文本模型并包装为 Eino 适配器
	llm := h.Providers.SelectModel(provider.ModelTypeText)
	if llm == nil {
		return fmt.Errorf("无可用 Text 模型，无法构建 Eino Agent")
	}
	modelAdapter := provider.NewEinoModelAdapter(llm)

	// 2. 将内置工具 + MCP 工具转换为 Eino InvokableTool
	einoInvTools := tool.BuildEinoTools(h.Tools, h.MCP, h.MCP)

	// 类型转换: []tool.InvokableTool → []tool.BaseTool (Eino 要求)
	einoBaseTools := make([]einotool.BaseTool, len(einoInvTools))
	for i, t := range einoInvTools {
		einoBaseTools[i] = t
	}

	// 3. 创建 Agent（Instruction 为空，每条消息由 middleware.BeforeAgent 动态注入）
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "juan-niang-neo",
		Description: "QQ 群聊 AI 助手",
		Model:       modelAdapter,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: einoBaseTools,
			},
		},
		MaxIterations: 20,
		Handlers: []adk.ChatModelAgentMiddleware{
			&JuanNiangMiddleware{h: h},
		},
	})
	if err != nil {
		return fmt.Errorf("创建 Eino ChatModelAgent 失败: %w", err)
	}

	h.EinoAgent = agent
	log.Info("Eino ChatModelAgent 已就绪", "tools", len(einoBaseTools))
	return nil
}

// RebuildEinoAgent 重建 Eino Agent（MCP 热添加/移除后调用，同步工具列表）。
func (h *HagoCenter) RebuildEinoAgent(ctx context.Context) {
	if err := h.buildEinoAgent(ctx); err != nil {
		log.Error("重建 Eino Agent 失败", "err", err)
	} else {
		log.Info("Eino Agent 已重建（工具列表已同步）")
	}
}

// Start 启动 Agent 系统 (事件循环 + CronJob 调度器 + 长期记忆 GC)。
func (h *HagoCenter) Start(ctx context.Context) error {
	go h.runEventLoop(ctx)
	go h.CronJobManager.Run(ctx)
	h.StartLongTermMemoryGC(ctx)
	log.Info("HagoCenter 已启动")
	return nil
}

// getGroupMemberInfoCached 带缓存的群成员信息查询：委托 Adapter 层实现
// （正缓存 10min + 负缓存 60s，见 adapter.Adapter.GetGroupMemberInfoCached）。
func (h *HagoCenter) getGroupMemberInfoCached(groupID, userID int64) (*adapter.GroupMemberInfo, error) {
	if h.Adapter == nil {
		return nil, nil
	}
	return h.Adapter.GetGroupMemberInfoCached(groupID, userID)
}

// seedBuiltinToolGuard 为内置工具幂等创建 ToolConfig 行（首次创建时写入默认
// "仅管理员"标志），并从 DB 加载运行时权限表。
func (h *HagoCenter) seedBuiltinToolGuard(ctx context.Context) {
	if h.DAO == nil {
		return
	}
	for _, t := range h.Tools.List() {
		if !t.IsBuiltin() {
			continue
		}
		if err := h.DAO.ToolConfig.EnsureBuiltin(ctx, t.Name(), t.Description(), adminOnlyToolNames[t.Name()]); err != nil {
			log.Warn("内置工具配置 seed 失败", "tool", t.Name(), "err", err)
		}
	}
	if err := h.loadToolAdminOnly(ctx); err != nil {
		log.Warn("工具管理员名单加载失败", "err", err)
	}
}

// loadToolAdminOnly 从 DB 加载"仅管理员"工具名单。
func (h *HagoCenter) loadToolAdminOnly(ctx context.Context) error {
	m, err := h.DAO.ToolConfig.ListAdminOnly(ctx)
	if err != nil {
		return err
	}
	h.toolAdminOnlyMu.Lock()
	h.toolAdminOnly = m
	h.toolAdminOnlyMu.Unlock()
	return nil
}

// RefreshToolAdminOnly 从 DB 重载"仅管理员"工具名单（Web 配置变更后调用）。
func (h *HagoCenter) RefreshToolAdminOnly(ctx context.Context) {
	if err := h.loadToolAdminOnly(ctx); err != nil {
		log.Error("刷新工具管理员名单失败", "err", err)
		return
	}
	h.toolAdminOnlyMu.RLock()
	defer h.toolAdminOnlyMu.RUnlock()
	log.Info("工具管理员名单已刷新", "count", len(h.toolAdminOnly))
}

// isToolAdminOnly 查询工具是否标记为"仅管理员"。
func (h *HagoCenter) isToolAdminOnly(name string) bool {
	h.toolAdminOnlyMu.RLock()
	defer h.toolAdminOnlyMu.RUnlock()
	return h.toolAdminOnly[name]
}

// Stop 停止 Agent 系统。
func (h *HagoCenter) Stop() {
	// 冲刷统计事件缓冲（Loki+Promtail 通道，独立于主日志）
	if h.Stats != nil {
		h.Stats.Close()
	}
	log.Info("HagoCenter 已停止")
}
