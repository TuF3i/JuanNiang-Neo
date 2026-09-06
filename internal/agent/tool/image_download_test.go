package tool

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"JuanNiang-Neo/internal/adapter"
)

// TestImageURLCandidates 候选链覆盖：原始 → HTML 实体解码 → 百分号解码变体。
// 群聊识图（vision）等工具共用该候选逻辑，任一成功即下载成功。
func TestImageURLCandidates(t *testing.T) {
	raw := "https://multimedia.nt.qq.com.cn/download?appid=1407&amp;rkey=CAESM"
	got := ImageURLCandidates(raw)
	want := []string{
		raw,
		"https://multimedia.nt.qq.com.cn/download?appid=1407&rkey=CAESM",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("实体解码候选错误\n got: %v\nwant: %v", got, want)
	}

	// 百分号编码（LLM 复刻变形）：%26 应产生 QueryUnescape 变体
	// （去重后：原始 + 解码，共 2 个候选）
	encoded := "https://multimedia.nt.qq.com.cn/download?appid=1407%26rkey=x"
	got = ImageURLCandidates(encoded)
	if len(got) != 2 {
		t.Fatalf("百分号编码候选数量错误: %d (%v)", len(got), got)
	}
	if got[1] != "https://multimedia.nt.qq.com.cn/download?appid=1407&rkey=x" {
		t.Fatalf("QueryUnescape 变体缺失: %v", got)
	}
	// 原始 URL 必须始终是第一个候选（最优路径优先）
	if got[0] != encoded {
		t.Fatalf("原始 URL 应为首个候选: %v", got)
	}
}

// TestImageURLCandidatesNoChange 无实体/无编码时不应产生重复候选。
func TestImageURLCandidatesNoChange(t *testing.T) {
	raw := "https://example.com/a.png"
	got := ImageURLCandidates(raw)
	if len(got) != 1 || got[0] != raw {
		t.Fatalf("无变形 URL 应只有原始候选: %v", got)
	}
}

// TestBlockedIP SSRF 防护范围：loopback/私网/链路本地/组播/未指定/CGNAT 拒绝，
// 公网地址放行。
func TestBlockedIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.1.1",
		"169.254.1.1", "0.0.0.0", "100.64.1.1", "224.0.0.1",
		"::1", "fe80::1", "fc00::1", "ff02::1",
	}
	for _, s := range blocked {
		if !blockedIP(net.ParseIP(s)) {
			t.Errorf("%s 应被 SSRF 防护拒绝", s)
		}
	}
	allowed := []string{"8.8.8.8", "1.1.1.1", "2400:3200::1"}
	for _, s := range allowed {
		if blockedIP(net.ParseIP(s)) {
			t.Errorf("%s 不应被 SSRF 防护拒绝", s)
		}
	}
}

// TestDialRestrictedRejectsLoopback 受限拨号必须拒绝回环地址（SSRF 关键路径）。
func TestDialRestrictedRejectsLoopback(t *testing.T) {
	ctx := context.Background()
	if _, err := dialRestricted(ctx, "tcp", "127.0.0.1:8080"); err == nil {
		t.Fatal("回环地址应被拒绝")
	}
	if _, err := dialRestricted(ctx, "tcp", "[::1]:8080"); err == nil {
		t.Fatal("IPv6 回环地址应被拒绝")
	}
}

// TestDownloadImageBytesRejectsLocalServer 完整路径：httpDownload 经受限客户端
// 下载本地服务必须失败（SSRF 拒绝），防止攻击者借 vision 工具探测内部网络。
func TestDownloadImageBytesRejectsLocalServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("internal data"))
	}))
	defer srv.Close()

	_, err := DownloadImageBytes(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("下载本地服务应被 SSRF 防护拒绝")
	}
	if !strings.Contains(err.Error(), "拒绝访问非公网地址") {
		t.Fatalf("应返回 SSRF 拒绝错误, got: %v", err)
	}
}

// ---------- 静默内容过滤（__NO_REPLY__ 防泄漏） ----------

func TestIsSilenceToolContent(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"纯 NO_REPLY 标记", "__NO_REPLY__", true},
		{"NO_REPLY 带前后缀", "我判断不回复 __NO_REPLY__", true},
		{"静默短语", "保持静默", true},
		{"静默短语带标点", "不回复", true},
		{"做空气", "做空气", true},
		{"正常回复", "你好呀", false},
		{"长文本不被误判", "这是一段超过十五个字的正常回复内容，需要正常发送", false},
		{"空串", "", false},
		{"回复含 NO_REPLY 字样但语义不同", "我说的是__NO_REPLY__这个词本身的意思", true}, // 保守：含标记即吞
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isSilenceToolContent(c.in); got != c.want {
				t.Errorf("isSilenceToolContent(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestDeferredFlushSkipsSilence(t *testing.T) {
	fakeSent = map[string]bool{}
	q := NewDeferredSendQueue()
	q.Add(DeferredSend{MessageType: "group", TargetID: 123, Message: "__NO_REPLY__", Delivery: true})
	q.Add(DeferredSend{MessageType: "group", TargetID: 123, Message: "正常消息", Delivery: true})

	sent := q.Flush(context.Background(), &fakeAdapter{})
	// Flush 只返回实际投递的条目，静默内容不出现在返回值中
	if len(sent) != 1 {
		t.Fatalf("Flush 返回 %d 条（应只含实际投递条目），want 1", len(sent))
	}
	if sent[0].Text() != "正常消息" {
		t.Errorf("返回条目应为正常消息，got %q", sent[0].Text())
	}
	// 验证正常消息仍被发送、静默消息被跳过（由 fakeAdapter 记录）
	if !fakeSent["正常消息"] {
		t.Error("正常消息未被发送")
	}
	if fakeSent["__NO_REPLY__"] {
		t.Error("静默内容 __NO_REPLY__ 不应被发送")
	}
}

func TestDeferredFlushSkipsSilenceSegments(t *testing.T) {
	fakeSent = map[string]bool{}
	q := NewDeferredSendQueue()
	// 消息段数组中的 text 段携带静默标记：同样被跳过
	q.Add(DeferredSend{
		MessageType: "group", TargetID: 123, Delivery: true,
		Message: []adapter.Segment{
			{Type: "text", Data: map[string]any{"text": "__NO_REPLY__"}},
			{Type: "image", Data: map[string]any{"file": "x"}},
		},
	})
	q.Add(DeferredSend{MessageType: "group", TargetID: 123, Message: "正常消息", Delivery: true})

	sent := q.Flush(context.Background(), &fakeAdapter{})
	if len(sent) != 1 || sent[0].Text() != "正常消息" {
		t.Fatalf("消息段数组静默项应被过滤，实际返回 %v", sent)
	}
	if fakeSent["__NO_REPLY__"] {
		t.Error("静默消息段不应被发送")
	}
}

func TestDeliveredToSkipsSilence(t *testing.T) {
	q := NewDeferredSendQueue()
	q.Add(DeferredSend{MessageType: "group", TargetID: 123, Message: "__NO_REPLY__", Delivery: true})
	// 静默项不抑制最终回复
	if q.DeliveredTo("group", 123) {
		t.Error("静默内容不应视为已投递")
	}
	q.Add(DeferredSend{MessageType: "group", TargetID: 123, Message: "正常消息", Delivery: true})
	if !q.DeliveredTo("group", 123) {
		t.Error("正常交付消息应视为已投递")
	}
	// 无效目标不抑制最终回复
	q2 := NewDeferredSendQueue()
	q2.Add(DeferredSend{MessageType: "group", TargetID: 0, Message: "正常消息", Delivery: true})
	if q2.DeliveredTo("group", 0) {
		t.Error("无效目标不应视为已投递")
	}
}

// TestSendMsgToolSilenceSegmentArray direct-send 路径（无 DeferredSendQueue）：
// 消息段数组中 text 段携带静默标记时同样被拦截，不发送且不报错。
func TestSendMsgToolSilenceSegmentArray(t *testing.T) {
	fakeSent = map[string]bool{}
	cur := &adapter.MessageEvent{MessageType: "group", GroupID: 456}
	getCurrentMsg := func(ctx context.Context) *adapter.MessageEvent { return cur }
	tool := send_group_msg(&fakeAdapter{}, getCurrentMsg)

	args := json.RawMessage(`{"group_id":456,"message":[{"type":"text","data":{"text":"__NO_REPLY__"}}]}`)
	out, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("静默段数组不应报错: %v", err)
	}
	if !strings.Contains(out, "静默") {
		t.Errorf("应返回静默提示，got %q", out)
	}
	for k := range fakeSent {
		if strings.Contains(k, "__NO_REPLY__") {
			t.Errorf("静默内容不应被发送，实际发送了 %q", k)
		}
	}

	// 对照：正常段数组仍正常入队/发送
	fakeSent = map[string]bool{}
	args = json.RawMessage(`{"group_id":456,"message":[{"type":"text","data":{"text":"正常段数组消息"}}]}`)
	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("正常段数组不应报错: %v", err)
	}
	// 无 DeferredSendQueue 时 direct-send：fakeAdapter 应记录到发送
	if !fakeSent["正常段数组消息"] {
		t.Error("正常段数组消息未被发送")
	}
}

// fakeAdapter 记录发送内容的最小 AdapterProvider 实现。
var fakeSent = map[string]bool{}

type fakeAdapter struct{}

func (f *fakeAdapter) SendPrivateMsg(userID int64, message any) (int64, error) {
	if text := messagePlainText(message); text != "" {
		fakeSent[text] = true
	}
	return 1, nil
}
func (f *fakeAdapter) SendGroupMsg(groupID int64, message any) (int64, error) {
	if text := messagePlainText(message); text != "" {
		fakeSent[text] = true
	}
	return 1, nil
}
func (f *fakeAdapter) DeleteMsg(messageID int64) error                        { return nil }
func (f *fakeAdapter) GetGroupInfo(groupID int64) (*adapter.GroupInfo, error) { return nil, nil }
func (f *fakeAdapter) GetGroupMemberList(groupID int64) ([]adapter.GroupMemberInfo, error) {
	return nil, nil
}
func (f *fakeAdapter) KickGroupMember(groupID, userID int64, rejectAdd bool) error        { return nil }
func (f *fakeAdapter) BanGroupMember(groupID, userID int64, duration int) error           { return nil }
func (f *fakeAdapter) SetGroupWholeBan(groupID int64, enable bool) error                  { return nil }
func (f *fakeAdapter) SetGroupCard(groupID, userID int64, card string) error              { return nil }
func (f *fakeAdapter) HandleFriendRequest(flag string, approve bool, remark string) error { return nil }
func (f *fakeAdapter) HandleGroupRequest(flag, subType string, approve bool, reason string) error {
	return nil
}
