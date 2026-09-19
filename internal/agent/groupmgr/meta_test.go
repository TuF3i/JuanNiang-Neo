package groupmgr

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestPunishRecordsMeta 处罚记录现场信息：用户名/判定来源/LLM reason。
func TestPunishRecordsMeta(t *testing.T) {
	m, gmdao := newTestManager(t, nil)
	ctx := context.Background()
	ev := groupEv(100, 200, "广告")
	ev.Message.Sender.Card = "张三"
	ev.Message.Sender.Nickname = "nick张三"

	// RAG 路径处罚
	m.punish(ctx, ev, "RAG语义核实(校园卡)", "ad", "rag")
	list, err := gmdao.ViolationList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("应 1 条违规记录，got %d", len(list))
	}
	v := list[0]
	if v.Username != "张三" {
		t.Errorf("用户名应为群名片 张三，got %q", v.Username)
	}
	if v.DetectionPath != "rag" {
		t.Errorf("判定来源应为 rag，got %q", v.DetectionPath)
	}

	// LLM 路径覆盖（reason 为 LLM 输出）
	m.punish(ctx, ev, "明确广告引流：低价流量卡", "ad", "llm")
	list, _ = gmdao.ViolationList(ctx)
	v = list[0]
	if v.Count != 2 {
		t.Errorf("第 2 次违规 count = %d", v.Count)
	}
	if v.DetectionPath != "llm" || !strings.Contains(v.LLMReason, "广告引流") {
		t.Errorf("LLM 路径应记录 reason，path=%q reason=%q", v.DetectionPath, v.LLMReason)
	}
}

// TestPunishLLMBatchKeepsUsername LLM 批量追罚（异步路径）处罚记录用户名不应丢失：
// applyVerdict 重建 ev 时使用入批快照的群名片/昵称（回归：曾因缺 Sender 导致 Username 为空）。
func TestPunishLLMBatchKeepsUsername(t *testing.T) {
	m, gmdao := newTestManager(t, nil)
	ctx := context.Background()

	// 构造一条带群名片的消息（模拟 RAG 未命中送审后的入批快照）
	ev := groupEv(100, 200, "低价流量卡办理")
	ev.Message.Sender.Card = "李四"
	ev.Message.Sender.Nickname = "nick李四"

	// 直接构造批结果：black 判定 + 快照用户名，走 handleReviewBatch 完整链路
	item := reviewItem{
		groupID: 100, userID: 200, messageID: ev.Message.MessageID,
		admins: ev.Admins, pk: "100:200",
		rawText:    "低价流量卡办理",
		senderCard: "李四", senderNickname: "nick李四",
	}
	m.handleReviewBatch(ctx, reviewOutcome{
		items:   []reviewItem{item},
		results: []reviewResult{{Index: 0, Verdict: "black", Reason: "广告引流"}},
	})

	list, err := gmdao.ViolationList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("应 1 条违规记录，got %d", len(list))
	}
	v := list[0]
	if v.Username != "李四" {
		t.Errorf("LLM 追罚用户名应为快照群名片 李四，got %q", v.Username)
	}
	if v.DetectionPath != "llm" {
		t.Errorf("判定来源应为 llm，got %q", v.DetectionPath)
	}
	// 学习闭环应把该消息写入黑名单语录（LLM 判 black；异步 goroutine，轮询等待）
	deadline := time.Now().Add(3 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		samples, _ := gmdao.SampleListByList(ctx, "black")
		for _, s := range samples {
			if strings.Contains(s.Text, "流量卡") {
				found = true
				break
			}
		}
		if found {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !found {
		t.Error("LLM 判 black 后应写入黑名单语录（学习闭环）")
	}
}

// TestSyncAdminsFromAdapter 从 Adapter.Admins 合并管理员（去重）。
func TestSyncAdminsFromAdapter(t *testing.T) {
	m, gmdao := newTestManager(t, nil)
	ctx := context.Background()
	_ = gmdao.AdminAdd(ctx, 100) // 已有管理员

	added, err := m.SyncAdminsFromAdapter(ctx, []string{"100", "200", "abc", "300"})
	if err != nil {
		t.Fatal(err)
	}
	if added != 2 { // 200、300 新增；100 已存在；abc 非法跳过
		t.Fatalf("应新增 2 个管理员，got %d", added)
	}
	list, _ := gmdao.AdminList(ctx)
	if len(list) != 3 {
		t.Fatalf("管理员总数应 3，got %d", len(list))
	}
	if !m.IsCommandAdmin(0, 200, nil) {
		t.Error("同步后 200 应视为管理员")
	}
}

// TestPunishMarksReviewVerdict 直罚（RAG/关键词/刷屏/复读）也必须登记审核终态：
// 否则 ReviewGate 查不到 black，处罚后针对该消息的 Agent 回复仍会发出
// （线上事故：RAG 处罚 warn 已执行，review_gate blocked=false，回复照发）。
func TestPunishMarksReviewVerdict(t *testing.T) {
	m, _ := newTestManager(t, nil)
	ctx := context.Background()
	ev := groupEv(619463411, 2706880859, "别给卷娘玩亖了")

	m.punish(ctx, ev, "RAG语义核实(敏感内容)", "sensitive", "rag")

	blocked, pending := m.ReviewGate(ctx, 619463411, 2706880859, 2706880859)
	if !blocked || pending {
		t.Fatalf("处罚后应 blocked=true pending=false, got blocked=%v pending=%v", blocked, pending)
	}

	// 顺带登记了送审去重（10 分钟后与终态一并过期）
	if _, ok := m.llmReviewed[2706880859]; !ok {
		t.Fatal("处罚应顺带登记送审去重")
	}
}
