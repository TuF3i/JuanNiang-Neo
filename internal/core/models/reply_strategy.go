package models

import "time"

// ReplyStrategy 回复策略。
// 参与窗口已取代旧 relevance 相关性判断：@/命令/提及名字/私聊必回，
// 其余群聊消息攒窗后整窗一次喂给 Agent，由 LLM 自门控（__NO_REPLY__）决定参与或静默。
// Strategy 字段与旧相关性参数已从配置中移除（DB 旧列残留无害）。
type ReplyStrategy string

const (
	// StrategyRelevance 仅保留供存量迁移 SQL 使用（core.go 收敛历史行），不再参与读取。
	StrategyRelevance ReplyStrategy = "relevance"
)

// ReplyStrategyConfig 系统回复策略配置（单例，DB 中只保留一行）。
// 参与窗口参数：quiet_gap 安静间隔 / force_count 插话计数强发 / max_age 最迟必发 /
// window_max_msgs 窗口消息数上限 / jitter 与 force_count_jitter 释放时机随机抖动 /
// participate_probability 安静释放参与概率 / typing_delay_max_ms 发送前打字延迟。
type ReplyStrategyConfig struct {
	ID                     string    `gorm:"primaryKey;type:uuid" json:"id"`
	BotName                string    `gorm:"default:''" json:"bot_name"`
	StripMarkdown          bool      `gorm:"default:false" json:"strip_markdown"`
	AgentLite              bool      `gorm:"default:false" json:"agent_lite"`
	QuietGapSeconds        int       `gorm:"default:5" json:"quiet_gap_seconds"`
	ForceCount             int       `gorm:"default:5" json:"force_count"`
	MaxAgeSeconds          int       `gorm:"default:20" json:"max_age_seconds"`
	WindowMaxMsgs          int       `gorm:"default:20" json:"window_max_msgs"`
	JitterSeconds          int       `gorm:"default:2" json:"jitter_seconds"`
	ForceCountJitter       int       `gorm:"default:1" json:"force_count_jitter"`
	ParticipateProbability float64   `gorm:"default:0.8" json:"participate_probability"`
	TypingDelayMaxMs       int       `gorm:"default:1500" json:"typing_delay_max_ms"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

func (ReplyStrategyConfig) TableName() string { return "reply_strategy_config" }
