package dto

import (
	"encoding/json"
	"fmt"
	"strconv"

	"JuanNiang-Neo/internal/core/models"
)

// FlexFloat32 支持 JSON 字符串和数字两种格式的反序列化。
// 前端 <input type="number"> 在某些情况下会将数字值序列化为 JSON 字符串。
type FlexFloat32 float32

func (f *FlexFloat32) UnmarshalJSON(data []byte) error {
	// 尝试作为数字解析
	var v float32
	if err := json.Unmarshal(data, &v); err == nil {
		*f = FlexFloat32(v)
		return nil
	}
	// 尝试作为字符串解析
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		v64, err := strconv.ParseFloat(s, 32)
		if err != nil {
			return fmt.Errorf("FlexFloat32: 无法解析字符串 %q: %w", s, err)
		}
		*f = FlexFloat32(v64)
		return nil
	}
	return fmt.Errorf("FlexFloat32: 无法解析 %s", string(data))
}

func (f FlexFloat32) MarshalJSON() ([]byte, error) {
	return json.Marshal(float32(f))
}

type ChangePasswordReq struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

type LoginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type AddAdminQQReq struct {
	QQ string `json:"qq"`
}

type UpdateAdapterConfigReq struct {
	Addr           string   `json:"addr"`
	Port           int      `json:"port"`
	Token          string   `json:"token"`
	AdminQQNumbers []string `json:"admin_qq_numbers"`
	Enabled        bool     `json:"enabled"`
}

type AddProviderReq struct {
	Name              string           `json:"name"`
	Type              models.ModelType `json:"type"`
	Endpoint          string           `json:"endpoint"`
	Token             string           `json:"token"`
	Model             string           `json:"model"`
	Temperature       FlexFloat32      `json:"temperature"`
	IsActive          bool             `json:"isActive"`
	EnableThinking    bool             `json:"enable_thinking"` // 模型思考开关（旧字段）
	APIMode           string           `json:"api_mode"`        // 协议模式：chat_completions/anthropic_messages/openai_responses/gemini_native
	ThinkingEffort    string           `json:"thinking_effort"` // off/low/medium/high
	ThinkingBudget    int              `json:"thinking_budget"`
	MaxTokens         int              `json:"max_tokens"`
	TopP              *FlexFloat32     `json:"top_p"`
	TopK              *int             `json:"top_k"`
	FrequencyPenalty  *FlexFloat32     `json:"frequency_penalty"`
	PresencePenalty   *FlexFloat32     `json:"presence_penalty"`
	RepetitionPenalty *FlexFloat32     `json:"repetition_penalty"`
	ProviderKey       string           `json:"provider_key"`
	AuthHeader        string           `json:"auth_header"`
	URLMode           string           `json:"url_mode"`
}

// UpdateProviderReq 与 AddProviderReq 结构完全一致（覆盖更新），用类型别名复用避免重复维护。
type UpdateProviderReq = AddProviderReq

type ToggleProviderReq struct {
	IsActive bool `json:"is_active"`
}

// ---------- MCP ----------

type AddMCPServerReq struct {
	Name          string           `json:"name"`
	ServerURL     string           `json:"server_url"`
	Headers       models.JSONMap   `json:"headers"`
	Timeout       int              `json:"timeout"`
	RetryCount    int              `json:"retry_count"`
	ToolFilter    models.JSONSlice `json:"tool_filter"`
	AutoReconnect bool             `json:"auto_reconnect"`
	IsActive      bool             `json:"is_active"`
}

// UpdateMCPServerReq 与 AddMCPServerReq 结构完全一致（覆盖更新），用类型别名复用。
type UpdateMCPServerReq = AddMCPServerReq

type ToggleMCPServerReq struct {
	IsActive bool `json:"is_active"`
}

// ---------- Skill ----------

type AddSkillReq struct {
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
}

// UpdateSkillReq 与 AddSkillReq 结构完全一致（覆盖更新），用类型别名复用。
type UpdateSkillReq = AddSkillReq

// ---------- Prompt ----------

type AddPromptReq struct {
	Name     string            `json:"name"`
	Content  string            `json:"content"`
	Type     models.PromptType `json:"type"`
	IsActive bool              `json:"is_active"`
}

// UpdatePromptReq 与 AddPromptReq 结构完全一致（覆盖更新），用类型别名复用。
type UpdatePromptReq = AddPromptReq

type TogglePromptReq struct {
	IsActive bool `json:"is_active"`
}

// ---------- Tool ----------

type ToggleToolReq struct {
	IsActive bool `json:"is_active"`
}

// UpdateToolAdminOnlyReq 更新工具"仅管理员"标志。
type UpdateToolAdminOnlyReq struct {
	AdminOnly bool `json:"admin_only"` // true=仅管理员可调用
}

// ---------- Knowledge ----------

// AddKnowledgeReq 新增知识库条目。
type AddKnowledgeReq struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

// UpdateKnowledgeReq 编辑知识库条目。
type UpdateKnowledgeReq struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

// ---------- 图床 ----------

// UpdateImageReq 编辑图床图片（重命名/移动虚拟文件夹）。
type UpdateImageReq struct {
	Name   string `json:"name"`
	Folder string `json:"folder"` // 虚拟文件夹路径，/ 或 /<name>
}

// CreateImageFolderReq 创建图床虚拟文件夹。
type CreateImageFolderReq struct {
	Name string `json:"name"`
}

// ---------- 表情包库 ----------

// CreateStickerReq 新建表情（引用图床图片）。
type CreateStickerReq struct {
	ImageID string   `json:"image_id"` // 图床图片长 UUID（必填）
	Name    string   `json:"name"`     // 表情名称（必填）
	Desc    string   `json:"desc"`     // 简介（可选，支持模糊匹配）
	Tags    []string `json:"tags"`     // 标签名数组（可选）
}

// UpdateStickerReq 编辑表情。
type UpdateStickerReq struct {
	Name string   `json:"name"`
	Desc string   `json:"desc"`
	Tags []string `json:"tags"`
}

// CreateStickerTagReq 创建表情标签。
type CreateStickerTagReq struct {
	Name string `json:"name"`
}

// ---------- 摸鱼人日历 ----------

// UpdateFishCalendarConfigReq 更新摸鱼日历配置。
type UpdateFishCalendarConfigReq struct {
	Enabled      bool     `json:"enabled"`       // 总开关
	CronExpr     string   `json:"cron_expr"`     // 发送时间（6 字段秒级 cron）
	TargetGroups []string `json:"target_groups"` // 目标群号列表
}

// SetFishCalendarAffairReq 设置某天群务（content 为空则清除）。
type SetFishCalendarAffairReq struct {
	Date    string `json:"date"`    // YYYY-MM-DD
	Content string `json:"content"` // 当天群务内容
}

// ---------- 定时消息 ----------

// ScheduledSegmentReq 消息块内的消息段。
type ScheduledSegmentReq struct {
	Type    string `json:"type"`             // text / image / face
	Source  string `json:"source,omitempty"` // image 段：t2i / url / imgstore
	Content string `json:"content"`          // 文字 / 图片内容 / CQ 码
}

// ScheduledBlockReq 编排块（积木式）：message 消息块 / delay 延时块。
type ScheduledBlockReq struct {
	Type         string                `json:"type"`                    // message / delay
	Segments     []ScheduledSegmentReq `json:"segments,omitempty"`      // message 块的段（一条消息）
	DelaySeconds int                   `json:"delay_seconds,omitempty"` // delay 块的秒数
}

// AddScheduledMessageReq 新建定时消息任务。
type AddScheduledMessageReq struct {
	Name       string              `json:"name"`
	Enabled    bool                `json:"enabled"`
	CronExpr   string              `json:"cron_expr"` // 触发器
	TargetType string              `json:"target_type"`
	TargetID   int64               `json:"target_id"`
	Blocks     []ScheduledBlockReq `json:"blocks"`
}

// UpdateScheduledMessageReq 编辑定时消息任务。
type UpdateScheduledMessageReq struct {
	Name       string              `json:"name"`
	Enabled    bool                `json:"enabled"`
	CronExpr   string              `json:"cron_expr"`
	TargetType string              `json:"target_type"`
	TargetID   int64               `json:"target_id"`
	Blocks     []ScheduledBlockReq `json:"blocks"`
}

// ---------- Plugin ----------

type TogglePluginReq struct {
	IsActive bool `json:"is_active"`
}

// ---------- ACL ----------

type AddACLRuleReq struct {
	ChatAreaID string               `json:"chat_area_id"`
	Scope      models.ACLScope      `json:"scope"`
	Permission models.ACLPermission `json:"permission"`
	TargetType models.ACLTargetType `json:"target_type"`
	UserIDs    models.JSONSlice     `json:"user_ids"`
	ToolIDs    models.JSONSlice     `json:"tool_ids"`
	MCPIDs     models.JSONSlice     `json:"mcp_ids"`
}

// ---------- Memory ----------

type UpdateShortTermMemoryReq struct {
	WindowSize  int  `json:"window_size"`
	AutoCompact bool `json:"auto_compact"`
}

type UpdateLongTermMemoryReq struct {
	HotAreaSize  int `json:"hot_area_size"`
	HotMemoryTTL int `json:"hot_memory_ttl"`
	// GCIntervalDays 记忆 GC 周期（天）：0 表示不修改（保持原值）
	GCIntervalDays int `json:"gc_interval_days"`
}

// ---------- T2I ----------

type UpdateT2IConfigReq struct {
	BaseURL       string `json:"base_url"`
	Timeout       int    `json:"timeout"`
	IsActive      bool   `json:"is_active"`
	SelectedStyle string `json:"selected_style"`
}

// UpdateRAGConfigReq RAG 向量检索服务配置更新请求。
type UpdateRAGConfigReq struct {
	BaseURL  string `json:"base_url"`
	Timeout  int    `json:"timeout"`
	IsActive bool   `json:"is_active"`
}

// ---------- Sandbox ----------

type UpdateSandboxConfigReq struct {
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Timeout  int    `json:"timeout"`
	IsActive bool   `json:"is_active"`
}

// ---------- Webhook ----------

type UpdateWebhookConfigReq struct {
	Addr    string `json:"addr"`
	Port    int    `json:"port"`
	Token   string `json:"token"`
	Enabled bool   `json:"enabled"`
}

// ---------- CronJob ----------

type AddCronJobReq struct {
	Name        string   `json:"name"`
	CronExpr    string   `json:"cron_expr"`
	Message     string   `json:"message"`
	MessageType string   `json:"message_type"`
	TargetID    int64    `json:"target_id"`
	IsActive    bool     `json:"is_active"`
	PluginIDs   []string `json:"plugin_ids"` // 触发时调用的插件目录名列表
	Payload     string   `json:"payload"`    // JSON 字符串：传给 on_timer_call 的 payload
}

type UpdateCronJobReq struct {
	Name        string   `json:"name"`
	CronExpr    string   `json:"cron_expr"`
	Message     string   `json:"message"`
	MessageType string   `json:"message_type"`
	TargetID    int64    `json:"target_id"`
	IsActive    bool     `json:"is_active"`
	PluginIDs   []string `json:"plugin_ids"` // 触发时调用的插件目录名列表
	Payload     string   `json:"payload"`    // JSON 字符串：传给 on_timer_call 的 payload
}

type ToggleCronJobReq struct {
	IsActive bool `json:"is_active"`
}

// ---------- 回复策略 ----------
// 回复策略已收敛为仅 relevance：请求不再接受 strategy 字段，
// 以下均为相关性判断的参数配置。

type UpdateReplyStrategyReq struct {
	RelevanceThreshold float64 `json:"relevance_threshold"`
	BotName            string  `json:"bot_name"`
	StripMarkdown      bool    `json:"strip_markdown"`
	AgentLite          bool    `json:"agent_lite"`
	RelevancePrompt    string  `json:"relevance_prompt"`  // 相关性检测自定义提示词（空则用默认）
	RelevanceModel     string  `json:"relevance_model"`   // 相关性检测使用的 Text Provider ID（空则用默认）
	RelevanceTimeout   int     `json:"relevance_timeout"` // 相关性检测超时（秒），0=默认 10s
	JudgeFailPolicy    string  `json:"judge_fail_policy"` // 判断失败策略: drop=不回复（默认）, reply=照常回复
}

// ---------- 群管理 ----------

// UpdateGroupMgrConfigReq 更新群管理配置（黑/白阈值/批窗口/排除群/统一提示词/检测参数）。
type UpdateGroupMgrConfigReq struct {
	Enabled              bool     `json:"enabled"`
	LLMReview            bool     `json:"llm_review"`
	BlackMinScore        float64  `json:"black_min_score"`
	WhiteMinScore        float64  `json:"white_min_score"`
	LLMBatchWindow       int      `json:"llm_batch_window"`
	ImgSpamWindow        int      `json:"img_spam_window"`
	ImgSpamThreshold     int      `json:"img_spam_threshold"`
	ImgMuteDuration      int      `json:"img_mute_duration"`
	EnableCopyCheck      bool     `json:"enable_copy_check"`
	CopyThreshold        int      `json:"copy_threshold"`
	ViolationMuteSeconds int      `json:"violation_mute_seconds"`
	ExcludeGroups        []string `json:"exclude_groups"`
	LLMPrompt            string   `json:"llm_prompt"` // 统一检测提示词
	LLMCriteria          string   `json:"llm_criteria"`
	LLMGrayPrompt        string   `json:"llm_gray_prompt"`
	LLMHighRiskPrompt    string   `json:"llm_high_risk_prompt"`

	// WhiteGCIntervalDays 白名单语录 GC 周期（天），默认 7
	WhiteGCIntervalDays int `json:"white_gc_interval_days"`
}

// AddGroupMgrPhraseReq 新增违禁语录（黑/白名单）。
type AddGroupMgrPhraseReq struct {
	Text     string `json:"text"`
	Category string `json:"category"`  // black 语录：ad / sensitive；white 语录忽略
	ListType string `json:"list_type"` // black / white
}

// AddGroupMgrWordReq 新增词条（已废弃：关键词仅作兜底不可修改，保留兼容）。
type AddGroupMgrWordReq struct {
	Word     string `json:"word"`
	Category string `json:"category"` // black / gray / sensitive
}

// UpdateGroupMgrQQListReq 更新白名单/管理员列表（全量覆盖）。
type UpdateGroupMgrQQListReq struct {
	QQList []int64 `json:"qq_list"`
}

// TestGroupMgrReq 链路测试。
type TestGroupMgrReq struct {
	Text string `json:"text"`
}
