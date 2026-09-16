package groupmgr

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"JuanNiang-Neo/internal/agent/provider"
)

// adCardRaw 实际广告现场（issue #76）：群名片推荐卡片，剥离 CQ 码后文本为空，
// 此前 RAG 空 q 报错降级、LLM 送审空 <USER_TEXT> 无法判定，广告卡片漏网。
const adCardRaw = `[CQ:json,data={"app":"com.tencent.contact.lua"&#44;"prompt":"群名片: 学府超市特惠版ʳ"&#44;"bizsrc":"qun.share"&#44;"meta":{"contact":{"avatar":"https://p.qlogo.cn/gh/1032574526/1032574526_1/100"&#44;"contact":"商品1折起，每天抢免单！"&#44;"jumpUrl":"mqqapi://card/show_pslcard?authSig=m6oSvQY7IeQJGMkB1YwCLElHSBifANHzoE6bMiDIe978eI8USuRCgCTK3E4NGPNJ&amp;card_type=group&amp;uin=1032574526"&#44;"nickname":"学府超市特惠版ʳ"&#44;"tag":"群名片"&#44;"tagIcon":""&#44;"pcJumpUrl":"tencent://groupwpa/?jump_from=&amp;uin=1032574526&amp;subcmd=all"&#44;"avatarType":""}}&#44;"config":{"autosize":0&#44;"collect":0&#44;"ctime":1789096691&#44;"forward":1&#44;"height":225&#44;"reply":0&#44;"round":1&#44;"token":"652639a459f130fdccff28b00804f50b"&#44;"type":"normal"&#44;"width":526}&#44;"view":"contact"&#44;"ver":"0.0.0.1"}]`

// TestCardText 卡片文本化：提取 prompt/昵称/描述，跳过 URL；解析失败退回原文。
func TestCardText(t *testing.T) {
	got := cardText(adCardRaw)
	for _, want := range []string{"[推荐卡片]", "群名片: 学府超市特惠版ʳ", "学府超市特惠版ʳ", "商品1折起，每天抢免单！"} {
		if !strings.Contains(got, want) {
			t.Errorf("卡片文本应含 %q，got %q", want, got)
		}
	}
	for _, skip := range []string{"mqqapi", "tencent://", "https://", "652639a"} {
		if strings.Contains(got, skip) {
			t.Errorf("卡片文本不应含 URL/token 片段 %q，got %q", skip, got)
		}
	}

	// troopsharecard（推荐群聊卡片）：转义逗号还原后可提取 prompt
	troop := `[CQ:json,data={"app":"com.tencent.troopsharecard"&#44;"prompt":"推荐群聊【学习资料群】"}]`
	if got := cardText(troop); !strings.Contains(got, "推荐群聊【学习资料群】") {
		t.Errorf("troopsharecard 应提取 prompt，got %q", got)
	}

	// JSON 截断解析失败 → 退回清洗后的 data 原文，仍非空可送审
	broken := `[CQ:json,data={"app":"com.tencent.contact.lua"&#44;"prompt":"被截断的卡片`
	if got := cardText(broken); !strings.Contains(got, "[推荐卡片]") || !strings.Contains(got, "被截断的卡片") {
		t.Errorf("解析失败应退回原文，got %q", got)
	}

	// 非推荐卡片 / 纯文本 → 空串
	other := `[CQ:json,data={"app":"com.tencent.miniapp"&#44;"prompt":"normal share"}]`
	if got := cardText(other); got != "" {
		t.Errorf("非推荐卡片应返回空串，got %q", got)
	}
	if got := cardText("今天天气不错"); got != "" {
		t.Errorf("纯文本应返回空串，got %q", got)
	}
}

// TestVerifyByRAGEmptyQuery 空 query 不送检索（此前发出空 q 请求必然 400，
// 日志出现「RAG 检索失败，降级」噪声并走降级路径）。
func TestVerifyByRAGEmptyQuery(t *testing.T) {
	m, _ := newTestManager(t, nil) // mock RAG 可用
	if v := m.verifyByRAG(context.Background(), "  ", true); v.ok {
		t.Fatal("空 query 不应送检索，应按 RAG 不可用降级")
	}
}

// captureLLM 捕获送入 LLM 的 user prompt（验证 card-only 文本化后 LLM 可见）。
type captureLLM struct {
	mockLLMProvider
	prompt string
}

func (c *captureLLM) Chat(ctx context.Context, req provider.ChatRequest) (*provider.ChatResponse, error) {
	c.prompt = req.Messages[len(req.Messages)-1].Content
	return c.mockLLMProvider.Chat(ctx, req)
}

// TestDetectViolationCardOnlyReviewText 回归 issue #76：card-only 推荐卡片消息
// 必须文本化后送审——入批 rawText 非空、LLM user prompt 含卡片内容（此前均为空导致漏检）。
func TestDetectViolationCardOnlyReviewText(t *testing.T) {
	m, gmdao := newTestManager(t, nil) // 无 RAG → 与现场一致走 RAG 不可用路径
	ctx := context.Background()
	cfg, _ := gmdao.GetConfig(ctx)
	cfg.LLMReview = true
	_ = gmdao.UpdateConfig(ctx, cfg)
	cap := &captureLLM{}
	m.providers.AddProvider(cap)

	ev := groupEv(100, 200, adCardRaw)
	ev.Message.MessageID = 3001
	if !m.detectViolation(ctx, ev, m.getCfg(ctx)) {
		t.Fatal("card-only 推荐卡片应入批送审")
	}

	// 入批快照：rawText 为文本化卡片内容，卡片硬信号齐备
	m.llmBatchMu.Lock()
	items := append([]reviewItem(nil), m.llmBatchItems...)
	m.llmBatchMu.Unlock()
	if len(items) != 1 {
		t.Fatalf("批队列应 1 条，got %d", len(items))
	}
	it := items[0]
	for _, want := range []string{"[推荐卡片]", "学府超市特惠版", "商品1折起"} {
		if !strings.Contains(it.rawText, want) {
			t.Errorf("送审文本应含 %q，got %q", want, it.rawText)
		}
	}
	if strings.Contains(it.rawText, "mqqapi") {
		t.Errorf("送审文本不应含 jumpUrl，got %q", it.rawText)
	}
	if !it.rc.card || !it.rc.highRisk || !it.rc.hard {
		t.Errorf("卡片应为硬信号（card/highRisk/hard），got %+v", it.rc)
	}

	// 提交批 → LLM 收到的 user prompt 必须非空且含卡片内容
	m.flushBatch(ctx)
	select {
	case out := <-m.llmResults:
		if out.err != nil {
			t.Fatalf("批裁决不应报错，got %v", out.err)
		}
		m.handleReviewBatch(ctx, out)
	case <-time.After(5 * time.Second):
		t.Fatal("批裁决未到达 llmResults")
	}
	if strings.Contains(cap.prompt, "<USER_TEXT index=0></USER_TEXT>") {
		t.Errorf("LLM user prompt 不应出现空 <USER_TEXT> 块，got %q", headText(cap.prompt, 200))
	}
	if !strings.Contains(cap.prompt, "[推荐卡片]") {
		t.Errorf("LLM user prompt 应含卡片文本化内容，got %q", headText(cap.prompt, 200))
	}
}

// failLLMProvider Chat 固定失败的 Provider（验证卡片硬信号 fail-closed）。
type failLLMProvider struct{}

func (failLLMProvider) ID() string               { return "fail-llm" }
func (failLLMProvider) Name() string             { return "Fail LLM" }
func (failLLMProvider) Type() provider.ModelType { return provider.ModelTypeText }
func (failLLMProvider) Model() string            { return "fail-model" }
func (failLLMProvider) Chat(_ context.Context, _ provider.ChatRequest) (*provider.ChatResponse, error) {
	return nil, errors.New("mock provider down")
}
func (failLLMProvider) ChatStream(_ context.Context, _ provider.ChatRequest) (<-chan provider.ChatStreamChunk, error) {
	ch := make(chan provider.ChatStreamChunk)
	close(ch)
	return ch, nil
}
func (failLLMProvider) Vision(_ context.Context, _ []byte, _ string) (string, error) {
	return "", nil
}

// TestCardOnlyLLMFailPunish RAG 不可用路径中卡片为硬信号：LLM 故障时 fail-closed
// 直罚（与关键词兜底路径对卡片的处理一致），不再因 LLM 故障漏放推荐卡片。
func TestCardOnlyLLMFailPunish(t *testing.T) {
	m, gmdao := newTestManager(t, nil)
	ctx := context.Background()
	cfg, _ := gmdao.GetConfig(ctx)
	cfg.LLMReview = true
	_ = gmdao.UpdateConfig(ctx, cfg)
	m.providers.AddProvider(failLLMProvider{})

	ev := groupEv(100, 200, adCardRaw)
	ev.Message.MessageID = 3002
	if !m.detectViolation(ctx, ev, m.getCfg(ctx)) {
		t.Fatal("card-only 推荐卡片应入批送审")
	}
	m.flushBatch(ctx)
	select {
	case out := <-m.llmResults:
		m.handleReviewBatch(ctx, out)
	case <-time.After(5 * time.Second):
		t.Fatal("批结果未到达 llmResults")
	}
	if c, _ := gmdao.ViolationGet(ctx, 100, 200); c < 1 {
		t.Fatal("LLM 故障时推荐卡片应按硬信号直罚")
	}
	if blocked, _ := m.ReviewGate(ctx, 100, 200, 3002); !blocked {
		t.Fatal("LLM 故障直罚后 ReviewGate 应视为 blocked")
	}
}

// TestViolationCardOnlyPanel 面板链路测试对 card-only 输入与主链路行为一致（review 而非 pass）。
func TestViolationCardOnlyPanel(t *testing.T) {
	m, _ := newTestManager(t, nil)
	rep := m.TestViolation(context.Background(), adCardRaw)
	if !rep.Card {
		t.Fatal("应识别推荐卡片")
	}
	if rep.Verdict != "review" {
		t.Fatalf("card-only 推荐卡片应 review（送 LLM 判定），got %s (%s)", rep.Verdict, rep.Reason)
	}
}
