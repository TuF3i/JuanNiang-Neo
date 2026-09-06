package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"JuanNiang-Neo/internal/adapter"
	"JuanNiang-Neo/internal/agent/memory"
	"JuanNiang-Neo/internal/agent/memory/longterm"
	"JuanNiang-Neo/internal/agent/memory/shortterm"
	"JuanNiang-Neo/internal/core/cache"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// quoteSender 构造带昵称的发送者字段。
func quoteSender(card, nickname string) struct {
	UserID   int64  `json:"user_id"`
	Nickname string `json:"nickname"`
	Sex      string `json:"sex"`
	Age      int    `json:"age"`
	Card     string `json:"card"`
} {
	return struct {
		UserID   int64  `json:"user_id"`
		Nickname string `json:"nickname"`
		Sex      string `json:"sex"`
		Age      int    `json:"age"`
		Card     string `json:"card"`
	}{Card: card, Nickname: nickname}
}

// TestPlainRuneLen 验证纯文本字数：剥离 CQ 码 / URL / [msgid:N]，rune 计数。
func TestPlainRuneLen(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"普通中文", "你好世界", 4},
		{"剥离 CQ 码", "看图[CQ:image,file=https://a.com/b.png]后的文字", 6},
		{"剥离 URL", "看这个 https://multimedia.nt.qq.com.cn/x/y?z=1 然后", 7},
		{"剥离 msgid", "[msgid:123]长消息正文", 5},
		{"CQ+URL+msgid 混合", "[msgid:9][CQ:face,id=14]https://a.com/x 正文", 3},
		{"空串", "", 0},
	}
	for _, c := range cases {
		if got := plainRuneLen(c.in); got != c.want {
			t.Errorf("%s: plainRuneLen(%q) = %d, want %d", c.name, c.in, got, c.want)
		}
	}
}

// TestMemoryMsgLongGetsMsgid 验证 memoryMsg 的长消息标记规则：
// >阈值（纯文本>100）且带真实 message_id → 前缀 [msgid:N]；≤阈值 / 空 / 无 ID 不加。
func TestMemoryMsgLongGetsMsgid(t *testing.T) {
	base := func(id int64, raw string) *adapter.MessageEvent {
		return &adapter.MessageEvent{
			MessageType: "group",
			MessageID:   id,
			UserID:      7,
			GroupID:     9,
			RawMessage:  raw,
			Sender:      quoteSender("TuF3i", "nick"),
		}
	}
	speaker := "[TuF3i(QQ:7) 在群9] "

	long := strings.Repeat("长", 101)
	if got := memoryMsg(base(42, long)); !strings.HasPrefix(got, "[msgid:42]") {
		t.Errorf(">100 字长消息应带 [msgid:42]，实际: %q", got)
	} else if !strings.Contains(got, speaker) {
		t.Errorf("长消息仍应带发言人标识，实际: %q", got)
	}

	// 恰好 100 字（阈值不含）→ 不加标记
	if got := memoryMsg(base(43, strings.Repeat("短", 100))); strings.Contains(got, "[msgid:43]") {
		t.Errorf("恰好 100 字不应带 [msgid:43]，实际: %q", got)
	}

	// 空消息 → ""
	if got := memoryMsg(base(44, "   ")); got != "" {
		t.Errorf("空消息应返回空串，实际: %q", got)
	}

	// 无 message_id（cronjob/webhook 注入）→ 即使长也不带标记
	if got := memoryMsg(base(0, long)); strings.Contains(got, "[msgid:") {
		t.Errorf("无 message_id 不应带 [msgid:]，实际: %q", got)
	}

	// 长消息但纯文本其实被 URL 撑长 → 剥离 URL 后 ≤ 阈值，不加标记
	urlLong := "看这个链接 https://multimedia.nt.qq.com.cn/very/long/path?x=" + strings.Repeat("a", 200)
	if got := memoryMsg(base(45, urlLong)); strings.Contains(got, "[msgid:45]") {
		t.Errorf("URL 撑长不计入纯文本，不应带 [msgid:45]，实际: %q", got)
	}
}

// newQuoteTestHago 构造带真实短期记忆（miniredis）的 HagoCenter，供 enrichQuote 反查。
func newQuoteTestHago(t *testing.T) (*HagoCenter, *memory.MemoryGroup) {
	t.Helper()
	mr := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rc.Close() })
	cc := cache.NewCache(rc, "juan-test:")
	mg := memory.NewMemoryGroup(
		shortterm.New(shortterm.Config{WindowSize: 100}, cc),
		longterm.New(longterm.Config{RecallMode: longterm.RecallModeRecent}, nil),
		nil,
	)
	return &HagoCenter{Memory: mg}, mg
}

// quoteMsg 构造一条带 reply 段的用户消息。
func quoteMsg(id int64, replyData map[string]any) *adapter.MessageEvent {
	segs := []adapter.Segment{{Type: "text", Data: map[string]any{"text": "我回复一下"}}}
	if replyData != nil {
		segs = append([]adapter.Segment{{Type: "reply", Data: replyData}}, segs...)
	}
	return &adapter.MessageEvent{
		MessageType: "group",
		MessageID:   id,
		UserID:      7,
		GroupID:     9,
		RawMessage:  "我回复一下",
		Message:     segs,
		Sender:      quoteSender("Bob", "nick"),
	}
}

// TestEnrichQuoteShortInMemory 短引用（记忆内且无 [msgid:N] 标记）→ 注入引用原文。
func TestEnrichQuoteShortInMemory(t *testing.T) {
	h, mg := newQuoteTestHago(t)
	ctx := context.Background()
	areaID := "area-short"
	if err := mg.AddShortTermMessage(ctx, areaID, shortterm.ChatMessage{
		Role: "user", MsgID: "123", Content: "[Alice(QQ:11)] 你好呀",
	}); err != nil {
		t.Fatalf("add shortterm: %v", err)
	}
	m := quoteMsg(999, map[string]any{"id": "123"})
	got, _ := h.enrichQuote(ctx, "[CQ:reply,id=123] 我回复一下", m, areaID, nil, nil)
	if !strings.Contains(got, "【引用原文】") || !strings.Contains(got, "[Alice(QQ:11)] 你好呀") {
		t.Errorf("短引用应注入引用原文，实际: %q", got)
	}
}

// TestEnrichQuoteLongInMemoryPassIDOnly 长引用且在被引用消息在记忆窗口（带 [msgid:N]）
// → 只传 messageid，不注入原文（历史 [msgid:N] 可关联）。
func TestEnrichQuoteLongInMemoryPassIDOnly(t *testing.T) {
	h, mg := newQuoteTestHago(t)
	ctx := context.Background()
	areaID := "area-long"
	longContent := "[msgid:456][Bob(QQ:22)] " + strings.Repeat("长", 120)
	if err := mg.AddShortTermMessage(ctx, areaID, shortterm.ChatMessage{
		Role: "user", MsgID: "456", Content: longContent,
	}); err != nil {
		t.Fatalf("add shortterm: %v", err)
	}
	mu := "[CQ:reply,id=456] 这条太长我只引用一下"
	m := quoteMsg(999, map[string]any{"id": "456"})
	if got, _ := h.enrichQuote(ctx, mu, m, areaID, nil, nil); got != mu {
		t.Errorf("长引用在记忆窗口应只传 messageid（不注入），实际: %q", got)
	}
	if strings.Contains(mu, "【引用原文】") {
		t.Errorf("原始 mu 不应含引用标记，构造错误")
	}
}

// TestEnrichQuoteLongNotInMemoryInject 长引用但不在记忆窗口 → 回退内嵌内容注入引用原文。
func TestEnrichQuoteLongNotInMemoryInject(t *testing.T) {
	h, _ := newQuoteTestHago(t)
	ctx := context.Background()
	areaID := "area-empty"
	embedded := "一条已经不在记忆窗口里的被引用长消息"
	m := quoteMsg(999, map[string]any{"id": "789", "content": embedded})
	got, _ := h.enrichQuote(ctx, "[CQ:reply,id=789] 回复", m, areaID, nil, nil)
	if !strings.Contains(got, "【引用原文】") || !strings.Contains(got, embedded) {
		t.Errorf("长引用不在记忆应回退注入内嵌内容，实际: %q", got)
	}
}

// TestEnrichQuoteNoIDEmbeddedInject 无 reply id 但有内嵌内容 → 回退注入引用原文。
func TestEnrichQuoteNoIDEmbeddedInject(t *testing.T) {
	h, _ := newQuoteTestHago(t)
	ctx := context.Background()
	embedded := "内嵌引用内容（无 id）"
	m := quoteMsg(999, map[string]any{"content": embedded})
	got, _ := h.enrichQuote(ctx, "[CQ:reply] 回复", m, "area-no-id", nil, nil)
	if !strings.Contains(got, "【引用原文】") || !strings.Contains(got, embedded) {
		t.Errorf("无 id 有内嵌内容应注入引用原文，实际: %q", got)
	}
}

// TestEnrichQuoteIDNoContentKeepCQ 有 reply id 但拿不到内容（记忆未命中且无内嵌）
// → 保持 CQ 码原样（现状）。
func TestEnrichQuoteIDNoContentKeepCQ(t *testing.T) {
	h, _ := newQuoteTestHago(t)
	ctx := context.Background()
	mu := "[CQ:reply,id=99999] 这条引用查不到"
	m := quoteMsg(1000, map[string]any{"id": "99999"})
	if got, _ := h.enrichQuote(ctx, mu, m, "area-unknown", nil, nil); got != mu {
		t.Errorf("有 id 无内容应保持 CQ 码原样，实际: %q", got)
	}
}

// TestEnrichQuoteNoReplySegment 无 reply 段 → 原样返回。
func TestEnrichQuoteNoReplySegment(t *testing.T) {
	h, _ := newQuoteTestHago(t)
	ctx := context.Background()
	m := quoteMsg(1001, nil)
	mu := "没有引用的普通消息"
	if got, _ := h.enrichQuote(ctx, mu, m, "area-none", nil, nil); got != mu {
		t.Errorf("无 reply 段应原样返回，实际: %q", got)
	}
}

// TestReplySegmentID 验证 reply 段 id 类型兼容（string/int64/float64）。
func TestReplySegmentID(t *testing.T) {
	for _, c := range []struct {
		name string
		data map[string]any
		want int64
	}{
		{"string", map[string]any{"id": "123"}, 123},
		{"int64", map[string]any{"id": int64(456)}, 456},
		{"int", map[string]any{"id": 789}, 789},
		{"float64", map[string]any{"id": float64(1011)}, 1011},
		{"string 19 位", map[string]any{"id": "1234567890123456789"}, 1234567890123456789},
		{"json.Number 19 位", map[string]any{"id": json.Number("1234567890123456789")}, 1234567890123456789},
		{"无 id", map[string]any{"content": "x"}, 0},
		{"无 reply 段", nil, 0},
	} {
		m := quoteMsg(1, c.data)
		if got := replySegmentID(m); got != c.want {
			t.Errorf("%s: replySegmentID = %d, want %d", c.name, got, c.want)
		}
	}
}

// TestEnrichQuoteMsgidForgery 内容任意位置的 [msgid:N] 不作长消息判定：
// 用户正文含字面 [msgid:999] 的短消息被引用时应注入原文而非只传 id。
func TestEnrichQuoteMsgidForgery(t *testing.T) {
	h, mg := newQuoteTestHago(t)
	ctx := context.Background()
	areaID := "area-forge"
	if err := mg.AddShortTermMessage(ctx, areaID, shortterm.ChatMessage{
		Role: "user", MsgID: "123", Content: "前面聊过 [msgid:999] 这个标记但这是正文",
	}); err != nil {
		t.Fatalf("add shortterm: %v", err)
	}
	m := quoteMsg(888, map[string]any{"id": "123"})
	got, result := h.enrichQuote(ctx, "[CQ:reply,id=123] 回复", m, areaID, nil, nil)
	if result != quoteEnrichShortInjected {
		t.Errorf("伪造标记不应判定为长消息，result = %s, want short_injected", result)
	}
	if !strings.Contains(got, "【引用原文】") {
		t.Errorf("应注入原文，实际: %q", got)
	}
}

// TestEnrichQuoteNameQQNotTrusted name/qq 字段不当作引用原文：只有 name/qq 时
// 走无内嵌路径（保持 CQ 码原样），避免 LLM 把 QQ 号当原文。
func TestEnrichQuoteNameQQNotTrusted(t *testing.T) {
	h, _ := newQuoteTestHago(t)
	ctx := context.Background()
	mu := "[CQ:reply,id=777] 回复"
	m := quoteMsg(999, map[string]any{"id": "777", "name": "Alice", "qq": "123456"})
	got, result := h.enrichQuote(ctx, mu, m, "area-nq", nil, nil)
	if result != quoteEnrichKeptCQ {
		t.Errorf("name/qq 不应作为内嵌内容注入，result = %s, want kept_cq", result)
	}
	if got != mu {
		t.Errorf("应保持 CQ 码原样，实际: %q", got)
	}
}

// TestEnrichQuoteDupInBatch 同批互引：被引用短消息已在当前轮作为独立 userMsg 注入，
// 富化应跳过再次注入原文，避免当前轮重复占用 token。
func TestEnrichQuoteDupInBatch(t *testing.T) {
	h, mg := newQuoteTestHago(t)
	ctx := context.Background()
	areaID := "area-batch"
	// A（MsgID=500，短消息）已在当前批 → 同时存在于记忆与 batchIDs
	if err := mg.AddShortTermMessage(ctx, areaID, shortterm.ChatMessage{
		Role: "user", MsgID: "500", Content: "[Alice(QQ:11)] 你好呀",
	}); err != nil {
		t.Fatalf("add shortterm: %v", err)
	}
	batchIDs := map[int64]struct{}{500: {}}
	m := quoteMsg(501, map[string]any{"id": "500"})
	got, result := h.enrichQuote(ctx, "[CQ:reply,id=500] 回复", m, areaID, nil, batchIDs)
	if result != quoteEnrichDupInBatch {
		t.Errorf("同批互引应跳过注入，result = %s, want dup_in_batch", result)
	}
	if strings.Contains(got, "【引用原文】") {
		t.Errorf("同批互引不应再注入原文，实际: %q", got)
	}
}

// TestReplySegmentEmbeddedOnlyContent 只信任 content：name/qq 不作为引用原文。
func TestReplySegmentEmbeddedOnlyContent(t *testing.T) {
	m := quoteMsg(1, map[string]any{"name": "Alice", "qq": "123456"})
	if got := replySegmentEmbedded(m); got != "" {
		t.Errorf("仅 name/qq 不应返回内容，实际: %q", got)
	}
	m2 := quoteMsg(1, map[string]any{"content": "真实原文", "name": "Alice"})
	if got := replySegmentEmbedded(m2); got != "真实原文" {
		t.Errorf("有 content 应返回 content，实际: %q", got)
	}
}

// TestQuoteWrapStripsReplyCQ 注入原文前剥离 reply CQ 码，避免两种形态并存。
func TestQuoteWrapStripsReplyCQ(t *testing.T) {
	got := quoteWrap("[CQ:reply,id=123] 回复正文", "被引用原文")
	if strings.Contains(got, "[CQ:reply") {
		t.Errorf("注入原文应剥离 reply CQ 码，实际: %q", got)
	}
	if !strings.HasPrefix(got, "回复正文【引用原文】被引用原文") {
		t.Errorf("剥离后应保留正文+引用原文，实际: %q", got)
	}
}

// TestQuoteWrapKeepsMsgidAndSpeaker 长引用消息（带 [msgid:N] 与发言人前缀）注入原文时
// 只剥离 reply CQ 码，保留 msgid 标记与发言人前缀。
func TestQuoteWrapKeepsMsgidAndSpeaker(t *testing.T) {
	mu := "[msgid:555][TuF3i(QQ:7) 在群9] [CQ:reply,id=123] 长正文"
	got := quoteWrap(mu, "被引用原文")
	if strings.Contains(got, "[CQ:reply") {
		t.Errorf("注入原文应剥离 reply CQ 码，实际: %q", got)
	}
	if !strings.HasPrefix(got, "[msgid:555][TuF3i(QQ:7) 在群9] 长正文【引用原文】被引用原文") {
		t.Errorf("应保留 msgid/发言人前缀，实际: %q", got)
	}
}

// TestEnrichQuoteMemoryMsgFormat 覆盖 memoryMsg（发言人前缀）处理后再富化的真实路径：
// 富化注入原文时应剥离首个 reply CQ 码，同时保留发言人前缀与注入原文。
func TestEnrichQuoteMemoryMsgFormat(t *testing.T) {
	h, mg := newQuoteTestHago(t)
	ctx := context.Background()
	areaID := "area-memfmt"
	if err := mg.AddShortTermMessage(ctx, areaID, shortterm.ChatMessage{
		Role: "user", MsgID: "123", Content: "[Alice(QQ:11)] 被引用短消息",
	}); err != nil {
		t.Fatalf("add shortterm: %v", err)
	}
	m := &adapter.MessageEvent{
		MessageType: "group",
		MessageID:   999,
		UserID:      7,
		GroupID:     9,
		RawMessage:  "[CQ:reply,id=123] 我回复一下",
		Message: []adapter.Segment{
			{Type: "reply", Data: map[string]any{"id": "123"}},
			{Type: "text", Data: map[string]any{"text": "我回复一下"}},
		},
		Sender: quoteSender("TuF3i", "nick"),
	}
	mu := memoryMsg(m)
	if !strings.HasPrefix(mu, "[TuF3i(QQ:7) 在群9]") {
		t.Fatalf("memoryMsg 应带发言人前缀，实际: %q", mu)
	}
	got, result := h.enrichQuote(ctx, mu, m, areaID, nil, nil)
	if result != quoteEnrichShortInjected {
		t.Errorf("短引用应注入原文，result = %s, want short_injected", result)
	}
	if strings.Contains(got, "[CQ:reply") {
		t.Errorf("富化后不应保留 reply CQ 码，实际: %q", got)
	}
	if !strings.Contains(got, "[TuF3i(QQ:7) 在群9]") {
		t.Errorf("应保留发言人前缀，实际: %q", got)
	}
	if !strings.Contains(got, "【引用原文】[Alice(QQ:11)] 被引用短消息") {
		t.Errorf("应注入被引用原文，实际: %q", got)
	}
}

// TestStripMsgidMarkers 剥离 [msgid:N] 标记（检索 query / Compact 摘要用）。
func TestStripMsgidMarkers(t *testing.T) {
	if got := stripMsgidMarkers("[msgid:123]长消息正文"); got != "长消息正文" {
		t.Errorf("应剥离标记，实际: %q", got)
	}
	if got := stripMsgidMarkers("普通消息"); got != "普通消息" {
		t.Errorf("无标记应原样，实际: %q", got)
	}
	if got := stripMsgidMarkers("多标记 [msgid:1] 与 [msgid:2] 文本"); got != "多标记  与  文本" {
		t.Errorf("应剥离全部标记，实际: %q", got)
	}
}
