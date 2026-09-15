package models

import "time"

// ---------- 加群审核（AI 攒批审核入群请求，Web 面板人工兜底） ----------
//
// 数据流：加群请求事件 → GroupJoinRequest 落库（表内存在行 = 待审）→ 攒批缓冲
// （满 BatchSize 条或首条起算 FlushSeconds 秒）→ LLM 一批审核 / 面板人工审核 →
// 调 set_group_add_request 执行 → 删请求行 + 落 GroupJoinReview 终态记录。
// 执行失败（flag 过期等）保留请求行，面板仍可见可重试。

// GroupJoinRequest 待审加群请求。审核完成即删行并写入 GroupJoinReview，
// 无独立 status 字段（行存在即 pending）。
// Flag 是 OneBot 加群请求凭证，执行时必须原样回传（平台有时效，过期执行会报错）。
type GroupJoinRequest struct {
	ID        uint      `gorm:"primarykey"`
	GroupID   int64     `gorm:"not null;index"`
	UserID    int64     `gorm:"not null;index"`
	Username  string    `gorm:"type:varchar(128)"` // 申请时昵称（请求事件不含，预留展示）
	Comment   string    `gorm:"type:varchar(512)"` // 申请留言
	Flag      string    `gorm:"type:varchar(256);not null"`
	SubType   string    `gorm:"type:varchar(16);not null;default:'add'"`
	CreatedAt time.Time
}

func (GroupJoinRequest) TableName() string { return "group_join_requests" }

// GroupJoinReview 加群审核记录（AI 批量审核与人工操作统一落这里，面板"审核记录"Tab）。
// RequestID 关联原始请求（请求行删除后仍可追溯）；Reviewer 区分 ai / manual；
// Reason 为 AI 判定理由 / 人工备注，执行失败时附加失败说明。
type GroupJoinReview struct {
	ID         uint      `gorm:"primarykey"`
	RequestID  uint      `gorm:"index;default:0"`
	GroupID    int64     `gorm:"not null;index"`
	UserID     int64     `gorm:"not null;index"`
	Username   string    `gorm:"type:varchar(128)"`
	Comment    string    `gorm:"type:varchar(512)"`
	Verdict    string    `gorm:"type:varchar(16);not null"` // approve / reject
	Reviewer   string    `gorm:"type:varchar(16);not null"` // ai / manual
	Reason     string    `gorm:"type:text"`
	ReviewedAt time.Time `gorm:"index"`
}

func (GroupJoinReview) TableName() string { return "group_join_reviews" }

// GroupJoinReviewConfig 加群审核配置（单行表，强制 ID=1，仿 GroupMgrConfig）。
// EnabledGroups 审核生效群（AI 审核只接管这些群的 add 请求）；Prompts 每群提示词
// （键为群号十进制字符串，空串 = 使用内置默认提示词）；BatchSize 同群缓冲攒满条数
// 立即送审；FlushSeconds 首条入队起算的触发窗口（秒），到点未满也整批送审。
type GroupJoinReviewConfig struct {
	ID            uint       `gorm:"primarykey"`
	EnabledGroups Int64Slice `gorm:"type:jsonb;default:'[]'"`
	Prompts       StringMap  `gorm:"type:jsonb;default:'{}'"`
	BatchSize     int        `gorm:"not null;default:5"`
	FlushSeconds  int        `gorm:"not null;default:60"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (GroupJoinReviewConfig) TableName() string { return "group_join_review_configs" }
