package models

import (
	"time"

	"gorm.io/gorm"
)

// ---------- 群管理（系统级功能，替代 redrock_group_manager Lua 插件） ----------

// GroupMgrConfig 群管理配置（单行表）。
// 阈值语义：RAG 语义匹配黑名单——命中黑名单语录 score ≥ BlackMinScore 按样本类型直接处罚；
// 未命中/未达阈值送 LLM 统一判定；关键词仅作最后兜底（RAG/LLM 均不可用时）。
// 白名单语录体系已剔除（WhiteMinScore/WhiteGCIntervalDays 列保留兼容存量数据，不再使用）。
type GroupMgrConfig struct {
	ID            uint    `gorm:"primarykey"`
	Enabled       bool    `gorm:"not null;default:false"` // 总开关
	LLMReview     bool    `gorm:"not null;default:true"`  // LLM 审核开关（关闭后仅 RAG 黑名单匹配 + 关键词兜底）
	BlackMinScore float64 `gorm:"not null;default:0.7"`   // 黑名单语录命中阈值（≥ 处罚）
	// 已废弃（白名单语录体系剔除；列保留兼容存量数据）
	WhiteMinScore float64 `gorm:"not null;default:0.75"`
	// 已废弃（新语义改用 BlackMinScore；列保留兼容存量数据）
	HighScore     float64 `gorm:"not null;default:0.75"`
	LowScore      float64 `gorm:"not null;default:0.5"`
	FallbackScore float64 `gorm:"not null;default:0.6"`

	// 图片刷屏检测参数（对齐旧插件 config.yaml）
	ImgSpamWindow    int `gorm:"not null;default:2"`  // 刷屏时间窗口（秒）
	ImgSpamThreshold int `gorm:"not null;default:3"`  // 窗口内触发警告的图片数量
	ImgMuteDuration  int `gorm:"not null;default:60"` // 重复刷屏禁言时长（秒）

	// +1 复读检测参数
	EnableCopyCheck bool `gorm:"not null;default:true"` // 复读检测开关
	CopyThreshold   int  `gorm:"not null;default:3"`    // 多少人连续发相同消息判定为复读

	// 三级惩罚参数
	ViolationMuteSeconds int `gorm:"not null;default:1800"` // 第二次违规禁言时长（秒）

	// ExcludeGroups 排除检测的群 ID 列表（这些群不跑任何检测/惩罚）。
	ExcludeGroups JSONSlice `gorm:"type:jsonb;default:'[]'"`

	// LLM 判定批窗口：窗口内待审消息凑批统一提交 LLM（逐条独立判定），
	// 到点（秒）或队列满（LLMBatchMax 条）先到先提交
	LLMBatchWindow int `gorm:"not null;default:3"`

	// LLM 审核提示词（三套合并为一份，面板只编辑 LLMPrompt；旧三列保留兼容不再使用）。
	LLMPrompt         string `gorm:"type:text"`
	LLMCriteria       string `gorm:"type:text"` // 已废弃（保留兼容）
	LLMGrayPrompt     string `gorm:"type:text"` // 已废弃（保留兼容）
	LLMHighRiskPrompt string `gorm:"type:text"` // 已废弃（保留兼容）

	// WhiteGCIntervalDays 白名单语录 GC 周期（已废弃：白名单语录体系剔除，GC 随之移除；列保留兼容）。
	WhiteGCIntervalDays int `gorm:"not null;default:7"`

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (GroupMgrConfig) TableName() string { return "group_mgr_configs" }

// GroupMgrWord 违规关键词条（黑色/灰色/敏感三分类，Web 可增删/txt 导入）。
// Word 统一小写去重；Source 区分系统种子（go:embed 首次导入）与用户导入。
// RAGSynced 标记该词条是否已同步到 RAG 向量库（同步/导入成功时置 true，删除/新增时置 false）。
// RAGTag 是该词条派生的 RAG-Service tag UUID（ragtag.Word(id)），供面板展示与对账。
// 唯一性：普通唯一索引会阻塞「软删后重建同名」；改为 PG 部分唯一索引
// （WHERE deleted_at IS NULL，见 core.go 迁移）+ DAO 软删行复活双重保障，
// 此处仅保留普通索引用于精确匹配查询。
type GroupMgrWord struct {
	ID        uint   `gorm:"primarykey"`
	Word      string `gorm:"not null;index"`
	Category  string `gorm:"not null;index"` // black / gray / sensitive
	Source    string `gorm:"not null;default:'system'"`
	RAGSynced bool   `gorm:"not null;default:false"` // 是否已同步到 RAG 向量库
	RAGTag    string `gorm:"type:varchar(64)"`       // 派生的 RAG tag UUID（展示用）
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (GroupMgrWord) TableName() string { return "group_mgr_words" }

// GroupMgrSample RAG 违规样本（向量库本体，tag = ragtag.Sample(id)）。
// 来源：seed（词条/关键词导入种子，WordID 关联）、learn（LLM 确认违规自动入库）、import（txt 导入）。
// WordID > 0 表示该样本由对应词条派生：删除词条时同步删除样本 + RAG 向量（对账清理）。
// ListType：黑名单语录（black）命中处罚；白名单语录（white）体系已剔除，
// 存量 white 行保留不使用（检索/同步/展示均跳过）；LastUsedAt 供 GC 清理未使用记录。
type GroupMgrSample struct {
	ID         uint       `gorm:"primarykey"`
	WordID     uint       `gorm:"index;default:0"`                // 关联词条 ID（0 = 非词条派生样本）
	ListType   string     `gorm:"not null;default:'black';index"` // black（white 已废弃，存量保留不使用）
	Text       string     `gorm:"type:text;not null"`
	Category   string     `gorm:"not null;index"` // ad / sensitive（仅 black 语录有意义）
	Source     string     `gorm:"not null;default:'seed'"`
	HitCount   int        `gorm:"not null;default:0"`     // 命中次数（黑=处罚 / 白=放行）
	LastUsedAt *time.Time `gorm:"index"`                  // 最近命中时间（GC 用；NULL=从未命中）
	RAGSynced  bool       `gorm:"not null;default:false"` // 已同步到 RAG 向量库（同步/导入成功置 true）
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  gorm.DeletedAt `gorm:"index"`
}

func (GroupMgrSample) TableName() string { return "group_mgr_samples" }

// GroupMgrViolation 违规记录（群+用户唯一；count = 当前违规等级 1/2/3）。
// Username 记录处罚时该用户的群名片/昵称（面板展示用，可能过期）。
// DetectionPath 记录判定来源：rag / keyword / llm（LLM 确认违规后追罚）。
// LLMReason 记录 LLM 审核返回的 reason（detection_path=llm 时有值，面板可查看）。
type GroupMgrViolation struct {
	ID            uint   `gorm:"primarykey"`
	GroupID       int64  `gorm:"not null;uniqueIndex:idx_gm_viol_g_u"`
	UserID        int64  `gorm:"not null;uniqueIndex:idx_gm_viol_g_u"`
	Count         int    `gorm:"not null;default:1"`
	Username      string `gorm:"type:varchar(128)"` // 处罚时群名片/昵称
	DetectionPath string `gorm:"type:varchar(16)"`  // rag / keyword / llm
	LLMReason     string `gorm:"type:text"`         // LLM 审核返回的 reason
	UpdatedAt     time.Time
}

func (GroupMgrViolation) TableName() string { return "group_mgr_violations" }

// GroupMgrWhitelist 白名单 QQ（不参与任何检测；加入时清违规记录并解禁言）。
type GroupMgrWhitelist struct {
	ID        uint  `gorm:"primarykey"`
	QQ        int64 `gorm:"not null;uniqueIndex"`
	CreatedAt time.Time
}

func (GroupMgrWhitelist) TableName() string { return "group_mgr_whitelist" }

// GroupMgrAdmin 手动管理员 QQ（群角色无法识别时的手动指定）。
type GroupMgrAdmin struct {
	ID        uint  `gorm:"primarykey"`
	QQ        int64 `gorm:"not null;uniqueIndex"`
	CreatedAt time.Time
}

func (GroupMgrAdmin) TableName() string { return "group_mgr_admins" }

// GroupMgrStat 群管理统计/状态 kv（value 为字符串，兼容时间戳列表等非数值态）。
// key 约定："{group_id}:stats:{metric}" 统计计数；"{group_id}:ims:{user}" 图片刷屏时间戳等。
type GroupMgrStat struct {
	Key   string `gorm:"primaryKey;type:varchar(192)"`
	Value string `gorm:"type:text;not null;default:''"`
}

func (GroupMgrStat) TableName() string { return "group_mgr_stats" }
