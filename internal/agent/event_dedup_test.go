package agent

import (
	"context"
	"testing"
	"time"

	"JuanNiang-Neo/internal/adapter"
	"JuanNiang-Neo/internal/agent/prompt"
	"JuanNiang-Neo/internal/agent/session"
	"JuanNiang-Neo/internal/agent/skill"
	"JuanNiang-Neo/internal/core"
	"JuanNiang-Neo/internal/core/acl"
	"JuanNiang-Neo/internal/core/dao"
	"JuanNiang-Neo/internal/core/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newDedupTestHago 构造最小 HagoCenter：真实 DAO（sqlite 内存库）+ 空技能引擎。
// Plugin / EinoAgent / Memory / Adapter 均为 nil，handleMessage 在写入
// chat_records（user 角色）后提前返回，故 chat_records 行数 = 消费次数。
func newDedupTestHago(t *testing.T) (*HagoCenter, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// sqlite :memory: 每个连接独立建库：参与窗口/mustKeep 会并发开 goroutine，
	// 必须限制单连接，否则并发连接会看到未迁移的空库（no such table）。
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := core.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	bundle := dao.NewBundle(db)
	h := NewHagoCenter()
	h.DAO = bundle
	h.ACL = acl.NewACL(bundle.ACL)
	h.Session = session.NewSessionManager(bundle.Session, bundle.ChatRecord, bundle.TokenUsageDaily, nil)
	h.Prompt = prompt.NewPromptManager(bundle.Prompt)
	h.Skills = skill.NewSkillEngine()
	return h, db
}

func groupMsg(id int64) adapter.Event {
	return adapter.Event{
		PostType: "message",
		Message: &adapter.MessageEvent{
			MessageType: "group",
			MessageID:   id,
			UserID:      123,
			GroupID:     456,
			// 参与模式必回快路径：文本含"机器人"关键词命中 isDefinitelyRelevant 直接回复，
			// 消息才能无 LLM Provider 地进入 handleMessage（去重测试不依赖参与窗口）。
			RawMessage: "你好机器人",
			Message:    []adapter.Segment{{Type: "text", Data: map[string]any{"text": "你好机器人"}}},
		},
	}
}

func waitBatchConsumed(t *testing.T, db *gorm.DB, want int64) {
	t.Helper()
	// mustKeep 消息异步经 runAgent goroutine 处理（sqlite 写库同步），
	// 固定 sleep 在慢机上不保证覆盖 Session 创建/技能匹配/提示词构建耗时，
	// 改为带超时轮询 chat_records 达到期望条数。
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if countUserRecords(t, db) == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等待 %d 条 user 记录落库超时（实际 %d）", want, countUserRecords(t, db))
}

func countUserRecords(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&models.ChatRecord{}).Where("role = ?", "user").Count(&count).Error; err != nil {
		t.Fatalf("count chat_records: %v", err)
	}
	return count
}

// TestDuplicateMessageIDConsumedTwice 复现"一条消息被重复消费"：
// WS 层将同一条 message_id 投递两次（断线重连重推 / 多连接重复投递），
// 期望消费链路只处理一次。
func TestDuplicateMessageIDConsumedTwice(t *testing.T) {
	h, db := newDedupTestHago(t)
	ctx := context.Background()

	h.processEvent(ctx, groupMsg(10086))
	h.processEvent(ctx, groupMsg(10086)) // 同一条消息再次投递

	waitBatchConsumed(t, db, 1)

	if got := countUserRecords(t, db); got != 1 {
		t.Fatalf("同一条 message_id=10086 被消费了 %d 次，期望 1 次", got)
	}
}

// TestDistinctMessageIDsProcessedOnce 对照组：不同 message_id 各消费一次（防误杀）。
func TestDistinctMessageIDsProcessedOnce(t *testing.T) {
	h, db := newDedupTestHago(t)
	ctx := context.Background()

	h.processEvent(ctx, groupMsg(10086))
	h.processEvent(ctx, groupMsg(10087))

	waitBatchConsumed(t, db, 2)

	if got := countUserRecords(t, db); got != 2 {
		t.Fatalf("两条不同消息应各自消费一次，实际消费 %d 次", got)
	}
}
