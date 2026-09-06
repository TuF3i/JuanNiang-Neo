package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"JuanNiang-Neo/internal/adapter"
	"JuanNiang-Neo/internal/agent/memory"
	"JuanNiang-Neo/internal/agent/memory/longterm"
	"JuanNiang-Neo/internal/agent/memory/shortterm"
	"JuanNiang-Neo/internal/core/cache"
	"JuanNiang-Neo/internal/core/models"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// partMsg 构造一条走参与窗口的群聊消息：非 @/命令/名字（不进 mustKeep），且非噪音。
func partMsg(id int64, user int64) adapter.Event {
	return adapter.Event{
		PostType: "message",
		Message: &adapter.MessageEvent{
			MessageType: "group",
			MessageID:   id,
			UserID:      user,
			GroupID:     456,
			RawMessage:  "今天天气怎么样大家聊聊",
			Message:     []adapter.Segment{{Type: "text", Data: map[string]any{"text": "今天天气怎么样大家聊聊"}}},
		},
	}
}

// partRS 返回确定性参与参数（关闭随机），便于窗口状态机测试。
func partRS() ReplySettings {
	return ReplySettings{
		QuietGapSeconds:        60,
		ForceCount:             100,
		MaxAgeSeconds:          60,
		WindowMaxMsgs:          20,
		JitterSeconds:          0,
		ForceCountJitter:       0,
		ParticipateProbability: 1.0,
		TypingDelayMaxMs:       0,
	}
}

// waitUntil 带超时轮询等待：runAgent 异步落库在慢机上耗时不定，固定 sleep 会间歇失败；
// 轮询直到 cond 为 true（或到达超时则测试失败）。
func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等待超时（%v）", timeout)
}

// TestParticipationWindowForceCount 插话计数强发：攒够 force_count 条即释放整窗，
// 两条消息都被一次 handleMessage 消费（写 user chat_records）。
func TestParticipationWindowForceCount(t *testing.T) {
	h, db := newDedupTestHago(t)
	ctx := context.Background()
	rs := partRS()
	rs.ForceCount = 2

	h.dispatchToAgent(ctx, partMsg(1, 111), rs)
	h.dispatchToAgent(ctx, partMsg(2, 222), rs)

	waitUntil(t, 5*time.Second, func() bool { return countUserRecords(t, db) == 2 })
}

// TestParticipationWindowQuietRelease 安静释放：窗口消息后无人插话，安静间隔到期触发释放。
func TestParticipationWindowQuietRelease(t *testing.T) {
	h, db := newDedupTestHago(t)
	ctx := context.Background()
	rs := partRS()
	rs.QuietGapSeconds = 1

	h.dispatchToAgent(ctx, partMsg(1, 111), rs)

	waitUntil(t, 5*time.Second, func() bool { return countUserRecords(t, db) == 1 })
}

// TestParticipationWindowMaxAge 最迟必发：窗口创建 max_age 到期后即使无新消息也强制释放。
func TestParticipationWindowMaxAge(t *testing.T) {
	h, db := newDedupTestHago(t)
	ctx := context.Background()
	rs := partRS()
	rs.QuietGapSeconds = 60 // 安静间隔很长，只有 max_age 会触发
	rs.MaxAgeSeconds = 1

	h.dispatchToAgent(ctx, partMsg(1, 111), rs)

	waitUntil(t, 5*time.Second, func() bool { return countUserRecords(t, db) == 1 })
}

// TestParticipationWindowProbabilitySkip 安静释放参与概率：probability=0 时安静路径静默放弃本窗。
func TestParticipationWindowProbabilitySkip(t *testing.T) {
	h, db := newDedupTestHago(t)
	ctx := context.Background()
	rs := partRS()
	rs.QuietGapSeconds = 1
	rs.ParticipateProbability = 0.0

	h.dispatchToAgent(ctx, partMsg(1, 111), rs)

	// 等窗口被释放（概率静默路径不进 handleMessage，不会落 user 记录）再断言
	area, err := h.DAO.ChatArea.GetOrCreate(ctx, models.AreaTypeGroup, 456)
	if err != nil {
		t.Fatalf("get chat area: %v", err)
	}
	waitUntil(t, 5*time.Second, func() bool {
		h.windowMu.Lock()
		defer h.windowMu.Unlock()
		_, exists := h.windows[area.ID]
		return !exists
	})
	if got := countUserRecords(t, db); got != 0 {
		t.Fatalf("参与概率 0 应静默放弃本窗，实际消费 %d", got)
	}
}

// TestParticipationMustKeepDiscardsWindow mustKeep 立即回复并丢弃当前参与窗口：
// 窗口内消息不再释放（等过安静间隔仍不消费），只有 mustKeep 消息被处理。
func TestParticipationMustKeepDiscardsWindow(t *testing.T) {
	h, db := newDedupTestHago(t)
	ctx := context.Background()
	rs := partRS()
	rs.QuietGapSeconds = 1

	h.dispatchToAgent(ctx, partMsg(1, 111), rs) // 进参与窗口
	h.dispatchToAgent(ctx, groupMsg(2), rs)     // 含"机器人"→ mustKeep，丢弃窗口并立即回

	// 先等 mustKeep 立即回复落库，再覆盖安静间隔确认被丢弃的窗口消息不被补发
	waitUntil(t, 5*time.Second, func() bool { return countUserRecords(t, db) == 1 })
	time.Sleep(1600 * time.Millisecond) // 覆盖安静间隔
	if got := countUserRecords(t, db); got != 1 {
		t.Fatalf("mustKeep 应立即回复且丢弃窗口（窗口消息不应被消费），实际消费 %d", got)
	}
}

// TestParticipationMustKeepImmediate mustKeep 私聊/@/名字路径不攒窗，立即处理。
func TestParticipationMustKeepImmediate(t *testing.T) {
	h, db := newDedupTestHago(t)
	ctx := context.Background()
	rs := partRS()

	h.dispatchToAgent(ctx, groupMsg(1), rs) // "你好机器人" → isDefinitelyRelevant

	waitUntil(t, 5*time.Second, func() bool { return countUserRecords(t, db) == 1 })
}

// TestReplySettingsDefaults 参与窗口参数缺省（0）时回退默认值。
func TestReplySettingsDefaults(t *testing.T) {
	var rs ReplySettings
	if got := rs.quietGap(); got != 5*time.Second {
		t.Errorf("quietGap 默认应 5s，实际 %v", got)
	}
	if got := rs.forceCount(); got != 5 {
		t.Errorf("forceCount 默认应 5，实际 %d", got)
	}
	if got := rs.maxAge(); got != 20*time.Second {
		t.Errorf("maxAge 默认应 20s，实际 %v", got)
	}
	if got := rs.windowMaxMsgs(); got != 20 {
		t.Errorf("windowMaxMsgs 默认应 20，实际 %d", got)
	}
}

// TestJitterSecDeterministic 抖动关闭时恒为 0（确定性模式）。
func TestJitterSecDeterministic(t *testing.T) {
	for i := 0; i < 100; i++ {
		if got := jitterSec(0); got != 0 {
			t.Fatalf("jitterSec(0) 应恒为 0，实际 %d", got)
		}
	}
	// 上限内取值
	for i := 0; i < 100; i++ {
		if got := jitterSec(2); got < 0 || got > 2 {
			t.Fatalf("jitterSec(2) 应在 [0,2]，实际 %d", got)
		}
	}
}

// TestParticipationShortSymbolNoise ≤2 字短消息与纯 emoji/符号按产品契约判定为噪音：
// 不进参与窗口、不消费（"哈哈"/"😂😂😂" 属水群刷屏，规则直接丢弃）。
func TestParticipationShortSymbolNoise(t *testing.T) {
	h, db := newDedupTestHago(t)
	ctx := context.Background()
	rs := partRS()
	rs.QuietGapSeconds = 1

	shortMsg := partMsg(1, 111)
	shortMsg.Message.RawMessage = "哈哈"
	shortMsg.Message.Message = []adapter.Segment{{Type: "text", Data: map[string]any{"text": "哈哈"}}}
	symMsg := partMsg(2, 222)
	symMsg.Message.RawMessage = "😂😂😂"
	symMsg.Message.Message = []adapter.Segment{{Type: "text", Data: map[string]any{"text": "😂😂😂"}}}

	h.dispatchToAgent(ctx, shortMsg, rs)
	h.dispatchToAgent(ctx, symMsg, rs)

	time.Sleep(1600 * time.Millisecond)
	if got := countUserRecords(t, db); got != 0 {
		t.Fatalf("≤2 字/纯 emoji 应作为噪音丢弃，实际消费 %d", got)
	}
}

// TestParticipationMeaningfulTextEntersWindow 剥离 CQ 码/URL 后仍含文字的消息不算噪音：
// 覆盖 CJK/字母数字（"666"/"哈哈哈"）及非 ASCII 文字（假名"なにそれ"/谚文"뭐야이거"，
// unicode 字母分类判定），均进参与窗口等待安静释放（而非被规则丢弃）。
func TestParticipationMeaningfulTextEntersWindow(t *testing.T) {
	h, db := newDedupTestHago(t)
	ctx := context.Background()
	rs := partRS()
	rs.QuietGapSeconds = 1

	for i, raw := range []string{"666", "哈哈哈", "なにそれ", "뭐야이거"} {
		msg := partMsg(int64(i+1), int64(111+i))
		msg.Message.RawMessage = raw
		msg.Message.Message = []adapter.Segment{{Type: "text", Data: map[string]any{"text": raw}}}
		h.dispatchToAgent(ctx, msg, rs)
	}

	waitUntil(t, 5*time.Second, func() bool { return countUserRecords(t, db) == 4 })
}

// TestParticipationNoTextNoise 剥离 CQ 码/URL 后无任何文字（纯图片/纯 sticker）仍算噪音，
// 不进入参与窗口。
func TestParticipationNoTextNoise(t *testing.T) {
	h, db := newDedupTestHago(t)
	ctx := context.Background()
	rs := partRS()
	rs.QuietGapSeconds = 1

	imgMsg := partMsg(1, 111)
	imgMsg.Message.RawMessage = "[CQ:image,file=abc.jpg]"
	imgMsg.Message.Message = []adapter.Segment{{Type: "image", Data: map[string]any{"file": "abc.jpg"}}}

	h.dispatchToAgent(ctx, imgMsg, rs)

	time.Sleep(1600 * time.Millisecond)
	if got := countUserRecords(t, db); got != 0 {
		t.Fatalf("无文字纯图应作为噪音丢弃，实际消费 %d", got)
	}
}

// TestParticipationAtOnlyEntersWindow 只有 @（CQ:at，@ 别人无文字）不算噪音：
// @ 是互动信号，进参与窗口等待安静释放（而非被规则丢弃）。
func TestParticipationAtOnlyEntersWindow(t *testing.T) {
	h, db := newDedupTestHago(t)
	ctx := context.Background()
	rs := partRS()
	rs.QuietGapSeconds = 1

	atMsg := partMsg(1, 111)
	atMsg.Message.RawMessage = "[CQ:at,qq=999]"
	atMsg.Message.Message = []adapter.Segment{{Type: "at", Data: map[string]any{"qq": "999"}}}

	h.dispatchToAgent(ctx, atMsg, rs)

	waitUntil(t, 5*time.Second, func() bool { return countUserRecords(t, db) == 1 })
}

// TestParticipationEmptyMessageNoise 完全空消息仍算噪音，不进入参与窗口。
func TestParticipationEmptyMessageNoise(t *testing.T) {
	h, db := newDedupTestHago(t)
	ctx := context.Background()
	rs := partRS()
	rs.QuietGapSeconds = 1

	emptyMsg := partMsg(1, 111)
	emptyMsg.Message.RawMessage = ""
	emptyMsg.Message.Message = nil

	h.dispatchToAgent(ctx, emptyMsg, rs)

	time.Sleep(1600 * time.Millisecond)
	if got := countUserRecords(t, db); got != 0 {
		t.Fatalf("完全空消息应作为噪音丢弃，实际消费 %d", got)
	}
}

// TestParticipationMustKeepWritesShortTermMemory 回归（P0）：mustKeep 路径（私聊/@/插件命令/提及名字）
// 用户消息必须写入短期记忆——否则 LLM 历史上下文只有 assistant 发言、没有用户发言（私聊语境损坏）。
// 用 miniredis 提供真实 Redis 语义 + 真实 MemoryGroup，验证 AddShortTermMessage 真实落库。
func TestParticipationMustKeepWritesShortTermMemory(t *testing.T) {
	mr := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rc.Close() })
	cc := cache.NewCache(rc, "juan-test:")

	h, db := newDedupTestHago(t)
	h.Memory = memory.NewMemoryGroup(
		shortterm.New(shortterm.Config{WindowSize: 100}, cc),
		// RecallModeRecent：避免 SQLite 上走 pg_trgm 语义召回（回退最近，仅验证短期记忆写入）
		longterm.New(longterm.Config{RecallMode: longterm.RecallModeRecent}, h.DAO.LongTermMemItem),
		nil,
	)
	ctx := context.Background()
	rs := partRS()
	// 私聊消息 → 走 mustKeep 立即回（MessageType != group）
	ev := adapter.Event{
		PostType: "message",
		Message: &adapter.MessageEvent{
			MessageType: "private",
			MessageID:   1,
			UserID:      111,
			RawMessage:  "你好机器人",
			Message:     []adapter.Segment{{Type: "text", Data: map[string]any{"text": "你好机器人"}}},
		},
	}
	h.dispatchToAgent(ctx, ev, rs)

	// 等 runAgent goroutine 消费落库（user chat_records 出现即处理完成）
	waitUntil(t, 5*time.Second, func() bool { return countUserRecords(t, db) == 1 })

	// 短期记忆应包含该用户消息
	area, err := h.DAO.ChatArea.GetOrCreate(ctx, models.AreaTypePrivate, 111)
	if err != nil {
		t.Fatalf("get chat area: %v", err)
	}
	msgs, err := h.Memory.GetShortTermMessages(ctx, area.ID)
	if err != nil {
		t.Fatalf("get shortterm messages: %v", err)
	}
	for _, m := range msgs {
		if m.Role == "user" && strings.Contains(m.Content, "你好机器人") {
			return
		}
	}
	t.Fatalf("mustKeep 路径用户消息未写入短期记忆，当前记忆: %+v", msgs)
}

// TestParticipationInflightSerialization 参与窗口在途互斥（双窗口串行化）：上一窗口在
// Agent 处理中（inflight 槽位被占用）时，本窗的 force_count 释放被延后——窗口保留继续攒、
// 不消费；上一窗口处理完（clearInflight + checkWindowRelease）后按释放条件检查再释放。
func TestParticipationInflightSerialization(t *testing.T) {
	h, db := newDedupTestHago(t)
	ctx := context.Background()
	rs := partRS()
	rs.QuietGapSeconds = 60 // 关闭安静释放，只靠 force_count
	rs.ForceCount = 2

	area, err := h.DAO.ChatArea.GetOrCreate(ctx, models.AreaTypeGroup, 456)
	if err != nil {
		t.Fatalf("get chat area: %v", err)
	}

	// 模拟上一参与窗口正在 Agent 处理：先占用在途槽位
	if !h.tryAcquireInflight(area.ID) {
		t.Fatal("应在空闲时成功占用在途槽位")
	}
	defer h.clearInflight(area.ID)

	// 攒 2 条（force_count=2 已满足）：releaseWindow 检测到在途应延后，窗口保留、不消费
	h.dispatchToAgent(ctx, partMsg(1, 111), rs)
	h.dispatchToAgent(ctx, partMsg(2, 222), rs)

	time.Sleep(300 * time.Millisecond) // 给延后释放的 goroutine 一点执行时间
	if got := countUserRecords(t, db); got != 0 {
		t.Fatalf("在途期间本窗不应被释放消费，实际消费 %d", got)
	}
	h.windowMu.Lock()
	_, exists := h.windows[area.ID]
	h.windowMu.Unlock()
	if !exists {
		t.Fatal("在途期间窗口应保留继续攒，实际已被释放")
	}

	// 上一窗口处理完：释放槽位 + 重新检查释放条件 → force_count 已满足，应释放
	h.clearInflight(area.ID)
	h.checkWindowRelease(area.ID)

	waitUntil(t, 5*time.Second, func() bool { return countUserRecords(t, db) == 2 })
}

// TestParticipationInflightDeferredUntilCondition 在途释放延后时，若累积窗口未满足释放
// 条件（如仅 1 条 < force_count），checkWindowRelease 不应释放，窗口继续保留。
func TestParticipationInflightDeferredUntilCondition(t *testing.T) {
	h, db := newDedupTestHago(t)
	ctx := context.Background()
	rs := partRS()
	rs.QuietGapSeconds = 60
	rs.ForceCount = 5

	area, err := h.DAO.ChatArea.GetOrCreate(ctx, models.AreaTypeGroup, 456)
	if err != nil {
		t.Fatalf("get chat area: %v", err)
	}
	if !h.tryAcquireInflight(area.ID) {
		t.Fatal("应在空闲时成功占用在途槽位")
	}
	defer h.clearInflight(area.ID)

	// 仅 1 条，远低于 force_count=5
	h.dispatchToAgent(ctx, partMsg(1, 111), rs)
	time.Sleep(300 * time.Millisecond)

	// 上一窗口处理完，但条件不满足 → 不释放
	h.clearInflight(area.ID)
	h.checkWindowRelease(area.ID)
	time.Sleep(300 * time.Millisecond)
	if got := countUserRecords(t, db); got != 0 {
		t.Fatalf("未满足释放条件不应释放，实际消费 %d", got)
	}
	h.windowMu.Lock()
	_, exists := h.windows[area.ID]
	h.windowMu.Unlock()
	if !exists {
		t.Fatal("未满足释放条件时窗口应保留")
	}
}
