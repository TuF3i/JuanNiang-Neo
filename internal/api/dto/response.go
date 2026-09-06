package dto

import (
	"JuanNiang-Neo/internal/adapter"
	"JuanNiang-Neo/internal/core/models"
	"time"
)

var (
	OK                      = Response{Status: 0, Info: "OK"}
	ServerInternalErr       = Response{Status: 50000, Info: "服务器内部错误"}
	BindJSONErr             = Response{Status: 40001, Info: "参数格式错误"}
	UserOrPasswordWrong     = Response{Status: 40002, Info: "用户名或密码错误"}
	GenTokenFail            = Response{Status: 40003, Info: "token 生成失败"}
	UserNotExists           = Response{Status: 40004, Info: "用户不存在"}
	OriginPasswordWrong     = Response{Status: 40005, Info: "原密码错误"}
	UpdatePasswordFail      = Response{Status: 40006, Info: "密码更新失败"}
	InvalidQQNumber         = Response{Status: 40007, Info: "无效的 QQ 号"}
	AdapterNotReady         = Response{Status: 40008, Info: "adapter 未初始化"}
	ProviderNotExist        = Response{Status: 40009, Info: "provider 不存在"}
	MCPNotExist             = Response{Status: 40010, Info: "MCP 服务器不存在"}
	SessionNotExist         = Response{Status: 40011, Info: "Session 不存在"}
	EmptyFileToUpload       = Response{Status: 40012, Info: "缺少上传文件"}
	TempFileCreateFail      = Response{Status: 40013, Info: "临时文件创建失败"}
	WriteFileFail           = Response{Status: 40014, Info: "文件写入失败"}
	InvalidZipFile          = Response{Status: 40015, Info: "无效的 ZIP 文件"}
	InvalidACLID            = Response{Status: 40016, Info: "无效的 ACL ID"}
	UpdateAdapterConfigFail = Response{Status: 40017, Info: "onebot11 适配器配置更新失败"}
	SkillNotExist           = Response{Status: 40018, Info: "Skill 不存在"}
	PromptNotExist          = Response{Status: 40019, Info: "Prompt 不存在"}
	ToolNotExist            = Response{Status: 40020, Info: "Tool 不存在"}
	PluginNotExist          = Response{Status: 40021, Info: "Plugin 不存在"}
	PluginLoadFail          = Response{Status: 40022, Info: "插件加载失败"}
	ChatAreaNotFound        = Response{Status: 40023, Info: "ChatArea 不存在"}
	MemoryConfigNotFound    = Response{Status: 40024, Info: "Memory 配置不存在"}
	AdapterConfigNotFound   = Response{Status: 40025, Info: "adapter 配置不存在"}
	T2IConfigNotFound       = Response{Status: 40026, Info: "T2I 配置不存在"}
	SandboxConfigNotFound   = Response{Status: 40027, Info: "Sandbox 配置不存在"}
	RAGConfigNotFound       = Response{Status: 40050, Info: "RAG 配置不存在"}
	PluginIsSystem          = Response{Status: 40028, Info: "系统插件不允许删除或停用"}
	PromptIsSystem          = Response{Status: 40029, Info: "系统提示词不允许修改或删除"}
	ToolIsBuiltin           = Response{Status: 40030, Info: "内置工具运行时常驻, 不支持启停"}
	InvalidPluginName       = Response{Status: 40045, Info: "插件名不合法（仅允许字母/数字/下划线/连字符）"}
	PluginPackageUnsafe     = Response{Status: 40046, Info: "插件包包含非法路径（疑似 zip-slip 攻击）"}
	TextProviderRequired    = Response{Status: 40048, Info: "至少保留一个启用的 Text 模型，无法停用、删除或变更"}
	KnowledgeContentEmpty   = Response{Status: 40033, Info: "知识内容不能为空"}
	ImageTooLarge           = Response{Status: 40034, Info: "图片大小不能超过 1.5MB"}
	ImageTypeNotAllowed     = Response{Status: 40035, Info: "不支持的图片格式（仅支持 jpg/png/gif/webp）"}
	ImageNotExist           = Response{Status: 40036, Info: "图片不存在"}
	ImageFolderExist        = Response{Status: 40037, Info: "文件夹已存在"}
	ImageFolderNotExist     = Response{Status: 40038, Info: "文件夹不存在"}
	StickerNotExist         = Response{Status: 40039, Info: "表情不存在"}
	StickerTagExist         = Response{Status: 40040, Info: "标签已存在"}
	StickerTagNotExist      = Response{Status: 40041, Info: "标签不存在"}
	StickerImageExist       = Response{Status: 40042, Info: "该图床图片已被其他表情引用"}
	FishCalConfigNotExist   = Response{Status: 40043, Info: "摸鱼日历配置不存在"}
	ScheduledMsgNotExist    = Response{Status: 40044, Info: "定时消息任务不存在"}
	T2IStyleInvalid         = Response{Status: 40049, Info: "T2I 渲染风格无效（仅允许空、random 或风格库中定义的风格名）"}
	StickerTagSystem        = Response{Status: 40047, Info: "系统内置标签不可删除"}
	WordImportTooLarge      = Response{Status: 40051, Info: "词库导入文件过大（≤1MB）或行数超限（≤20000）"}
)

type TokenResp struct {
	Token string `json:"token"`
}

type ErrorDetail struct {
	ErrorDetail string `json:"error_detail"`
}

type AdapterStatus struct {
	Running    bool                 `json:"running"`
	ListenAddr string               `json:"listen_addr"`
	SelfID     int64                `json:"self_id"`
	ConnCount  int                  `json:"conn_count"`
	ConnIDs    []int64              `json:"conn_ids"`
	Conns      []adapter.ConnDetail `json:"conns"`
}

type AdapterConfig struct {
	Addr           string   `json:"addr"`
	Port           int      `json:"port"`
	Token          string   `json:"token"`
	AdminQQNumbers []string `json:"admin_qq_numbers"`
	Enabled        bool     `json:"enabled"`
}

type ProviderResp struct {
	ID                string           `json:"id"`
	CreatedAt         time.Time        `json:"created_at"`
	Name              string           `json:"name"`
	Type              models.ModelType `json:"type"`
	Endpoint          string           `json:"endpoint"`
	Token             string           `json:"token"`
	Model             string           `json:"model"`
	Temperature       float32          `json:"temperature"`
	IsActive          bool             `json:"is_active"`
	EnableThinking    bool             `json:"enable_thinking"` // 模型思考开关（旧字段）
	APIMode           string           `json:"api_mode"`        // 协议模式
	ThinkingEffort    string           `json:"thinking_effort"`
	ThinkingBudget    int              `json:"thinking_budget"`
	MaxTokens         int              `json:"max_tokens"`
	TopP              *float32         `json:"top_p"`
	TopK              *int             `json:"top_k"`
	FrequencyPenalty  *float32         `json:"frequency_penalty"`
	PresencePenalty   *float32         `json:"presence_penalty"`
	RepetitionPenalty *float32         `json:"repetition_penalty"`
	ProviderKey       string           `json:"provider_key"`
	AuthHeader        string           `json:"auth_header"`
	URLMode           string           `json:"url_mode"`
}

// ProviderPresetResp 国产厂商协议能力预设（前端渲染协议下拉）。
type ProviderPresetResp struct {
	Key       string                 `json:"key"`
	Name      string                 `json:"name"`
	Protocols []ProviderProtocolResp `json:"protocols"`
}

// ProviderProtocolResp 单个协议能力（前端渲染协议下拉）。
type ProviderProtocolResp struct {
	APIMode    string `json:"api_mode"`
	BaseURL    string `json:"base_url"`
	AuthHeader string `json:"auth_header"`
	Note       string `json:"note,omitempty"`
}

// TestProviderResp 连接测试结果。
type TestProviderResp struct {
	Ok      bool   `json:"ok"`
	Message string `json:"message"` // 成功=模型回复；失败=错误详情
}

type MCPServerResp struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	ServerURL     string           `json:"server_url"`
	Headers       models.JSONMap   `json:"headers"`
	Timeout       int              `json:"timeout"`
	RetryCount    int              `json:"retry_count"`
	ToolFilter    models.JSONSlice `json:"tool_filter"`
	AutoReconnect bool             `json:"auto_reconnect"`
	IsActive      bool             `json:"is_active"`
	CreatedAt     time.Time        `json:"created_at"`
}

type SkillResp struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Description  string           `json:"description"`
	Keywords     models.JSONSlice `json:"keywords"`
	RegexPattern string           `json:"regex_pattern"`
	PromptRefs   models.JSONSlice `json:"prompt_refs"`
	ToolRefs     models.JSONSlice `json:"tool_refs"`
	McpRefs      models.JSONSlice `json:"mcp_refs"`
	IsActive     bool             `json:"is_active"`
	IsSystem     bool             `json:"is_system"`
	Priority     int              `json:"priority"`
	CreatedAt    time.Time        `json:"created_at"`
}

type PromptResp struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Content   string            `json:"content"`
	Type      models.PromptType `json:"type"`
	IsActive  bool              `json:"is_active"`
	IsSystem  bool              `json:"is_system"`
	CreatedAt time.Time         `json:"created_at"`
}

type ToolConfigResp struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  models.JSONMap `json:"parameters"`
	Timeout     int            `json:"timeout"`
	IsActive    bool           `json:"is_active"`
	IsBuiltin   bool           `json:"is_builtin"`
	AdminOnly   bool           `json:"admin_only"` // 仅管理员可调用
	CreatedAt   time.Time      `json:"created_at"`
}

// KnowledgeResp 知识库条目。
type KnowledgeResp struct {
	ID            string           `json:"id"`
	Title         string           `json:"title"`
	Content       string           `json:"content"`
	Keywords      models.JSONSlice `json:"keywords"`
	KeywordStatus string           `json:"keyword_status"` // pending / ready / failed
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

type PluginResp struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Version   string         `json:"version"`
	Path      string         `json:"path"`
	Config    models.JSONMap `json:"config"`
	IsActive  bool           `json:"is_active"`
	CreatedAt time.Time      `json:"created_at"`
}

type ACLRuleResp struct {
	ID         int64                `json:"id"`
	ChatAreaID string               `json:"chat_area_id"`
	Scope      models.ACLScope      `json:"scope"`
	Permission models.ACLPermission `json:"permission"`
	TargetType models.ACLTargetType `json:"target_type"`
	UserIDs    models.JSONSlice     `json:"user_ids"`
	ToolIDs    models.JSONSlice     `json:"tool_ids"`
	MCPIDs     models.JSONSlice     `json:"mcp_ids"`
	CreatedAt  time.Time            `json:"created_at"`
}

type SessionResp struct {
	ID         string         `json:"id"`
	ChatAreaID string         `json:"chat_area_id"`
	Model      string         `json:"model"`
	TokenUsage int64          `json:"token_usage"`
	MetaData   models.JSONMap `json:"meta_data"`
	CreatedAt  time.Time      `json:"created_at"`
}

type ShortTermMemoryResp struct {
	ID          string    `json:"id"`
	ChatAreaID  string    `json:"chat_area_id"`
	WindowSize  int       `json:"window_size"`
	AutoCompact bool      `json:"auto_compact"`
	CreatedAt   time.Time `json:"created_at"`
}

type LongTermMemoryResp struct {
	ID             string    `json:"id"`
	ChatAreaID     string    `json:"chat_area_id"`
	HotAreaSize    int       `json:"hot_area_size"`
	HotMemoryTTL   int       `json:"hot_memory_ttl"`
	GCIntervalDays int       `json:"gc_interval_days"` // 记忆 GC 周期（天），默认 7
	CreatedAt      time.Time `json:"created_at"`
}

type ChatAreaResp struct {
	ID        string          `json:"id"`
	AreaType  models.AreaType `json:"area_type"`
	TargetID  int64           `json:"target_id"`
	CreatedAt time.Time       `json:"created_at"`
}

type ChatRecordResp struct {
	ID         int64          `json:"id"`
	ChatAreaID string         `json:"chat_area_id"`
	UserID     int64          `json:"user_id"`
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	TokenCount int            `json:"token_count"`
	ToolCalls  models.JSONMap `json:"tool_calls"`
	CreatedAt  time.Time      `json:"created_at"`
}

type OverviewResp struct {
	ChatAreaCount     int64  `json:"chat_area_count"`
	MCPCount          int64  `json:"mcp_count"`
	AdapterCount      int64  `json:"adapter_count"`
	PluginCount       int64  `json:"plugin_count"`
	ProviderCount     int    `json:"provider_count"`
	SkillCount        int    `json:"skill_count"`
	SessionCount      int    `json:"session_count"`
	TotalTokenUsage   int64  `json:"total_token_usage"`
	CPUCount          int    `json:"cpu_count"`            // 逻辑 CPU 核数
	GoroutineNum      int    `json:"goroutine_num"`        // 当前 goroutine 数
	MemAllocBytes     uint64 `json:"mem_alloc_bytes"`      // 堆已分配 (活跃对象)
	MemSysBytes       uint64 `json:"mem_sys_bytes"`        // 从 OS 获取的内存总量
	MemHeapInUseBytes uint64 `json:"mem_heap_inuse_bytes"` // 堆中正在使用
	GoVersion         string `json:"go_version"`           // Go 版本

	// Adapter 运行状态
	AdapterRunning bool `json:"adapter_running"` // OneBot11 反向 WS 是否运行中

	// T2I / Sandbox 状态
	T2IActive      bool `json:"t2i_active"`      // 客户端已加载
	T2IHealthy     bool `json:"t2i_healthy"`     // HealthCheck 通过
	SandboxActive  bool `json:"sandbox_active"`  // 客户端已加载
	SandboxHealthy bool `json:"sandbox_healthy"` // HealthCheck 通过
	RAGActive      bool `json:"rag_active"`      // 客户端已加载
	RAGHealthy     bool `json:"rag_healthy"`     // HealthCheck 通过
}

// DailyTokenUsageResp 单日 Token 用量（折线图数据点）。
type DailyTokenUsageResp struct {
	Date       string `json:"date"`
	TokenCount int64  `json:"token_count"`
}

type ChatRecordListResp struct {
	Total int64            `json:"total"`
	List  []ChatRecordResp `json:"list"`
}

// ImageResp 图床图片元数据。
type ImageResp struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Folder    string    `json:"folder"` // 虚拟文件夹路径，/ 表示根
	MimeType  string    `json:"mime_type"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ImageFolderResp 图床虚拟文件夹。
type ImageFolderResp struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// ImageListResp 图床图片分页列表。
type ImageListResp struct {
	Total int64       `json:"total"`
	List  []ImageResp `json:"list"`
}

// StickerResp 表情包（引用图床图片，ID 为发送用的短 UUID）。
type StickerResp struct {
	ID        string           `json:"id"`
	ImageID   string           `json:"image_id"`
	Name      string           `json:"name"`
	Desc      string           `json:"desc"`
	Tags      models.JSONSlice `json:"tags"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

// StickerTagResp 表情标签。
type StickerTagResp struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// StickerListResp 表情分页列表。
type StickerListResp struct {
	Total int64         `json:"total"`
	List  []StickerResp `json:"list"`
}

// FishCalendarConfigResp 摸鱼人日历配置。
type FishCalendarConfigResp struct {
	Enabled      bool       `json:"enabled"`
	CronExpr     string     `json:"cron_expr"`
	TargetGroups []string   `json:"target_groups"`
	LastRunAt    *time.Time `json:"last_run_at"`
	LastError    string     `json:"last_error"`
}

// FishCalendarAffairResp 某天的群务。
type FishCalendarAffairResp struct {
	Date    string `json:"date"`
	Content string `json:"content"`
}

// ScheduledSegmentResp 消息块内的消息段。
type ScheduledSegmentResp struct {
	Type    string `json:"type"`
	Source  string `json:"source,omitempty"`
	Content string `json:"content"`
}

// ScheduledBlockResp 编排块。
type ScheduledBlockResp struct {
	Type         string                 `json:"type"`
	Segments     []ScheduledSegmentResp `json:"segments,omitempty"`
	DelaySeconds int                    `json:"delay_seconds,omitempty"`
}

// ScheduledMessageResp 定时消息任务。
type ScheduledMessageResp struct {
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Enabled    bool                 `json:"enabled"`
	CronExpr   string               `json:"cron_expr"`
	TargetType string               `json:"target_type"`
	TargetID   int64                `json:"target_id"`
	Blocks     []ScheduledBlockResp `json:"blocks"`
	LastRunAt  *time.Time           `json:"last_run_at"`
	LastError  string               `json:"last_error"`
	CreatedAt  time.Time            `json:"created_at"`
	UpdatedAt  time.Time            `json:"updated_at"`
}

// ScheduledMessageListResp 定时消息分页列表。
type ScheduledMessageListResp struct {
	Total int64                  `json:"total"`
	List  []ScheduledMessageResp `json:"list"`
}

type PluginUploadResp struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type T2IConfigResp struct {
	BaseURL       string `json:"base_url"`
	Timeout       int    `json:"timeout"`
	IsActive      bool   `json:"is_active"`
	Healthy       bool   `json:"healthy"`
	SelectedStyle string `json:"selected_style"`
}

// RAGConfigResp RAG 向量检索服务配置响应。
type RAGConfigResp struct {
	BaseURL  string `json:"base_url"`
	Timeout  int    `json:"timeout"`
	IsActive bool   `json:"is_active"`
	Healthy  bool   `json:"healthy"`
}

type SandboxConfigResp struct {
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Timeout  int    `json:"timeout"`
	IsActive bool   `json:"is_active"`
	Healthy  bool   `json:"healthy"`
}

type WebhookConfigResp struct {
	Addr    string `json:"addr"`
	Port    int    `json:"port"`
	Token   string `json:"token"`
	Enabled bool   `json:"enabled"`
	Running bool   `json:"running"`
}

// ---------- Logs ----------

// LogEntryResp 对应 internal/logging.Entry 的前端响应结构。
type LogEntryResp struct {
	Time    time.Time      `json:"time"`
	Level   string         `json:"level"`
	Module  string         `json:"module,omitempty"`
	Message string         `json:"message"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Stack   string         `json:"stack,omitempty"`
}

// ---------- Agent 活跃循环 ----------

// AgentLoopResp 当前活跃的 Agent ReAct 循环（监控展示）。
type AgentLoopResp struct {
	ID          string    `json:"id"`
	ChatAreaID  string    `json:"chat_area_id"`
	MessageType string    `json:"message_type"` // private / group
	TargetID    int64     `json:"target_id"`    // 私聊: user_id; 群聊: group_id
	UserID      int64     `json:"user_id"`
	UserMsg     string    `json:"user_msg"`
	CurrentTool string    `json:"current_tool"` // 当前正在执行的工具；空表示思考/生成中
	StartedAt   time.Time `json:"started_at"`
}

// ---------- CronJob ----------

type CronJobResp struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	CronExpr    string     `json:"cron_expr"`
	Message     string     `json:"message"`
	MessageType string     `json:"message_type"`
	TargetID    int64      `json:"target_id"`
	IsActive    bool       `json:"is_active"`
	PluginIDs   []string   `json:"plugin_ids"` // 触发时调用的插件目录名列表
	Payload     string     `json:"payload"`    // JSON 字符串：传给 on_timer_call 的 payload
	LastRunAt   *time.Time `json:"last_run_at"`
	LastError   string     `json:"last_error"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// ---------- 回复策略 ----------

type ReplyStrategyResp struct {
	BotName                string  `json:"bot_name"`
	StripMarkdown          bool    `json:"strip_markdown"`
	AgentLite              bool    `json:"agent_lite"`
	QuietGapSeconds        int     `json:"quiet_gap_seconds"`
	ForceCount             int     `json:"force_count"`
	MaxAgeSeconds          int     `json:"max_age_seconds"`
	WindowMaxMsgs          int     `json:"window_max_msgs"`
	JitterSeconds          int     `json:"jitter_seconds"`
	ForceCountJitter       int     `json:"force_count_jitter"`
	ParticipateProbability float64 `json:"participate_probability"`
	TypingDelayMaxMs       int     `json:"typing_delay_max_ms"`
}

// ---------- 群管理 ----------

// GroupMgrConfigResp 群管理配置。
type GroupMgrConfigResp struct {
	Enabled              bool     `json:"enabled"`
	LLMReview            bool     `json:"llm_review"`
	BlackMinScore        float64  `json:"black_min_score"`  // 黑名单语录命中阈值
	WhiteMinScore        float64  `json:"white_min_score"`  // 白名单语录命中阈值
	LLMBatchWindow       int      `json:"llm_batch_window"` // LLM 判定批窗口（秒）
	ImgSpamWindow        int      `json:"img_spam_window"`
	ImgSpamThreshold     int      `json:"img_spam_threshold"`
	ImgMuteDuration      int      `json:"img_mute_duration"`
	EnableCopyCheck      bool     `json:"enable_copy_check"`
	CopyThreshold        int      `json:"copy_threshold"`
	ViolationMuteSeconds int      `json:"violation_mute_seconds"`
	ExcludeGroups        []string `json:"exclude_groups"`
	LLMPrompt            string   `json:"llm_prompt"`   // 统一检测提示词
	LLMCriteria          string   `json:"llm_criteria"` // 已废弃（兼容）
	LLMGrayPrompt        string   `json:"llm_gray_prompt"`
	LLMHighRiskPrompt    string   `json:"llm_high_risk_prompt"`

	// WhiteGCIntervalDays 白名单语录 GC 周期（天），默认 7
	WhiteGCIntervalDays int `json:"white_gc_interval_days"`
}

// GroupMgrWordResp 词条。
type GroupMgrWordResp struct {
	ID        uint   `json:"id"`
	Word      string `json:"word"`
	Category  string `json:"category"`
	Source    string `json:"source"`
	RAGSynced bool   `json:"rag_synced"` // 是否已同步到 RAG 向量库
	RAGTag    string `json:"rag_tag"`    // 派生 RAG tag UUID
}

// GroupMgrSampleResp 语录（样本）。
type GroupMgrSampleResp struct {
	ID         uint    `json:"id"`
	WordID     uint    `json:"word_id"`   // 关联词条 ID（0=非词条派生样本）
	ListType   string  `json:"list_type"` // black / white
	Text       string  `json:"text"`
	Category   string  `json:"category"`
	Source     string  `json:"source"`
	HitCount   int     `json:"hit_count"`
	RAGSynced  bool    `json:"rag_synced"`   // 已同步到 RAG 向量库
	RAGTag     string  `json:"rag_tag"`      // 派生 RAG tag UUID（面板展示/对账用）
	LastUsedAt *string `json:"last_used_at"` // 最近命中时间（可空）
	CreatedAt  string  `json:"created_at"`
}

// GroupMgrViolationResp 违规记录。
type GroupMgrViolationResp struct {
	ID            uint   `json:"id"`
	GroupID       int64  `json:"group_id"`
	UserID        int64  `json:"user_id"`
	Username      string `json:"username"`       // 处罚时群名片/昵称
	Count         int    `json:"count"`          // 当前违规等级
	DetectionPath string `json:"detection_path"` // rag / keyword / llm
	LLMReason     string `json:"llm_reason"`     // LLM 审核返回的 reason
}

// GroupMgrQQListResp 白名单/管理员列表。
type GroupMgrQQListResp struct {
	QQList []int64 `json:"qq_list"`
}

// GroupMgrStatsResp 统计。
type GroupMgrStatsResp struct {
	GroupID   int64  `json:"group_id"`
	Date      string `json:"date"`
	JoinToday int64  `json:"join_today"`
	Warns     int64  `json:"warns"`
	Mutes     int64  `json:"mutes"`
	CopyWarns int64  `json:"copy_warns"`
	Ad        int64  `json:"ad"`
	Sensitive int64  `json:"sensitive"`
	Kicks     int64  `json:"kicks"`
}

// GroupMgrSyncResp 同步向量库结果。
type GroupMgrSyncResp struct {
	Total  int `json:"total"`
	Failed int `json:"failed"`
}
