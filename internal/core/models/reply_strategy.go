package models

import "time"

// ReplyStrategy 回复策略。
// 当前仅保留按相关性回复（relevance）一种策略：@/命令/提及名字必回，
// 其余消息由 LLM 按相关性分数决定是否回复（规则快路径 + 批量判断 + 缓存降级）。
type ReplyStrategy string

const (
	StrategyRelevance ReplyStrategy = "relevance" // 按相关性回复（唯一策略）
)

// ReplyStrategyConfig 系统回复策略配置（单例，DB 中只保留一行）。
type ReplyStrategyConfig struct {
	ID                 string        `gorm:"primaryKey;type:uuid" json:"id"`
	Strategy           ReplyStrategy `gorm:"not null;default:'relevance'" json:"strategy"`
	RelevanceThreshold float64       `gorm:"default:0.5" json:"relevance_threshold"`
	BotName            string        `gorm:"default:''" json:"bot_name"`
	StripMarkdown      bool          `gorm:"default:false" json:"strip_markdown"`          // 是否去除 Agent 消息中的 Markdown 格式
	AgentLite          bool          `gorm:"default:false" json:"agent_lite"`              // AgentLite 模式：不调用工具/MCP，无 Agent 循环
	RelevancePrompt    string        `gorm:"type:text;default:''" json:"relevance_prompt"` // 相关性检测自定义提示词（空则用默认）
	RelevanceModel     string        `gorm:"default:''" json:"relevance_model"`            // 相关性检测使用的 Text Provider ID（空则用默认）
	RelevanceTimeout   int           `gorm:"default:10" json:"relevance_timeout"`          // 相关性检测超时（秒），含信号量等待与 LLM 调用总预算
	JudgeFailPolicy    string        `gorm:"default:'drop'" json:"judge_fail_policy"`      // 相关性判断失败时的处理: drop=不回复（默认）, reply=照常回复
	CreatedAt          time.Time     `json:"created_at"`
	UpdatedAt          time.Time     `json:"updated_at"`
}

func (ReplyStrategyConfig) TableName() string { return "reply_strategy_config" }
