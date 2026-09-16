package dao

import (
	"crypto/rand"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// isDupKeyErr 判断是否为唯一约束/主键冲突错误（Postgres 23505 / SQLite / 通用 duplicate 文案）。
func isDupKeyErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "unique index")
}

// ---------- DAO Bundle ----------

// Bundle 汇聚所有 DAO，方便注入。
type Bundle struct {
	// DB 为底层 *gorm.DB 句柄，供 Service 层开启事务（配合各 DAO 的 WithTx）。
	DB              *gorm.DB
	User            *UserDAO
	Provider        *ProviderDAO
	MCPServer       *MCPServerDAO
	Skill           *SkillDAO
	ToolConfig      *ToolConfigDAO
	Prompt          *PromptDAO
	ChatArea        *ChatAreaDAO
	Session         *SessionDAO
	ShortTermMemory *ShortTermMemoryDAO
	LongTermMemory  *LongTermMemoryDAO
	LongTermMemItem *LongTermMemoryItemDAO
	BackgroundTask  *BackgroundTaskDAO
	ChatRecord      *ChatRecordDAO
	Plugin          *PluginDAO
	ACL             *ACLDAO
	Onebot11Adapter *Onebot11AdapterDao
	Sandbox         *SandboxConfigDAO
	T2I             *T2IConfigDAO
	RAG             *RAGConfigDAO
	Webhook         *WebhookConfigDAO
	CronJob         *CronJobDAO
	ReplyStrategy   *ReplyStrategyDAO
	SkillMemory     *SkillMemoryDAO
	TokenUsageDaily *TokenUsageDailyDAO
	Knowledge       *KnowledgeDAO
	Image           *ImageDAO
	Sticker         *StickerDAO
	FishCalendar    *FishCalendarDAO
	ScheduledMsg    *ScheduledMessageDAO
	GroupMgr        *GroupMgrDAO
	JoinReview      *JoinReviewDAO
}

func NewBundle(db *gorm.DB) *Bundle {
	return &Bundle{
		DB:              db,
		User:            NewUserDAO(db),
		Provider:        NewProviderDAO(db),
		MCPServer:       NewMCPServerDAO(db),
		Skill:           NewSkillDAO(db),
		ToolConfig:      NewToolConfigDAO(db),
		Prompt:          NewPromptDAO(db),
		ChatArea:        NewChatAreaDAO(db),
		Session:         NewSessionDAO(db),
		ShortTermMemory: NewShortTermMemoryDAO(db),
		LongTermMemory:  NewLongTermMemoryDAO(db),
		LongTermMemItem: NewLongTermMemoryItemDAO(db),
		BackgroundTask:  NewBackgroundTaskDAO(db),
		ChatRecord:      NewChatRecordDAO(db),
		Plugin:          NewPluginDAO(db),
		ACL:             NewACLDAO(db),
		Onebot11Adapter: NewOnebot11AdapterDao(db),
		Sandbox:         NewSandboxConfigDAO(db),
		T2I:             NewT2IConfigDAO(db),
		RAG:             NewRAGConfigDAO(db),
		Webhook:         NewWebhookConfigDAO(db),
		CronJob:         NewCronJobDAO(db),
		ReplyStrategy:   NewReplyStrategyDAO(db),
		SkillMemory:     NewSkillMemoryDAO(db),
		TokenUsageDaily: NewTokenUsageDailyDAO(db),
		Knowledge:       NewKnowledgeDAO(db),
		Image:           NewImageDAO(db),
		Sticker:         NewStickerDAO(db),
		FishCalendar:    NewFishCalendarDAO(db),
		ScheduledMsg:    NewScheduledMessageDAO(db),
		GroupMgr:        NewGroupMgrDAO(db),
		JoinReview:      NewJoinReviewDAO(db),
	}
}
