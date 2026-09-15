package agent

import (
	"fmt"
	"strings"
	"testing"

	"JuanNiang-Neo/internal/adapter"
)

// histMsg 构造历史消息。
func histMsg(id int64, card, nickname, raw string) adapter.MessageEvent {
	m := adapter.MessageEvent{
		MessageType: "group",
		MessageID:   id,
		UserID:      id * 10,
		GroupID:     10001,
		RawMessage:  raw,
	}
	m.Sender.Card = card
	m.Sender.Nickname = nickname
	return m
}

// 正常路径：过滤锚点/空消息，取锚点前最近 N 条，升序拼接并带发言人前缀。
func TestFormatRecentContext(t *testing.T) {
	msgs := []adapter.MessageEvent{
		histMsg(101, "", "阿伟", "早"),
		histMsg(102, "小明", "", "在吗"),
		histMsg(103, "", "阿伟", ""),           // 空消息应过滤
		histMsg(104, "", "阿伟", "[CQ:image]"), // 含内容保留
		histMsg(105, "小明", "", "看完回复我"),      // 锚点本身应过滤
	}
	got := formatRecentContext(msgs, 105)
	if got == "" {
		t.Fatal("expected non-empty context")
	}
	if !strings.HasPrefix(got, "以下是该消息之前最近的聊天记录") {
		t.Errorf("missing header: %q", got)
	}
	// 锚点 105 与空消息 103 被过滤；101~104 升序保留
	if strings.Contains(got, "看完回复我") {
		t.Error("anchor message itself must be filtered out")
	}
	if !strings.Contains(got, "[阿伟(QQ:1010) 在群10001] 早") {
		t.Errorf("missing speaker-prefixed line: %q", got)
	}
	if !strings.Contains(got, "[小明(QQ:1020) 在群10001] 在吗") {
		t.Errorf("missing card-prefixed line: %q", got)
	}
	idx早 := strings.Index(got, "早")
	idx在吗 := strings.Index(got, "在吗")
	idx图片 := strings.Index(got, "[CQ:image]")
	if !(idx早 < idx在吗 && idx在吗 < idx图片) {
		t.Errorf("lines should be in ascending order: %q", got)
	}
}

// 超过上限时只保留锚点之前最近的 recentContextLimit 条。
func TestFormatRecentContextTailLimit(t *testing.T) {
	msgs := make([]adapter.MessageEvent, 0, 8)
	for i := 1; i <= 8; i++ {
		msgs = append(msgs, histMsg(int64(i), "", "u", fmt.Sprintf("msg%d", i)))
	}
	got := formatRecentContext(msgs, 999) // 锚点不在返回内
	if strings.Contains(got, "msg1") || strings.Contains(got, "msg2") || strings.Contains(got, "msg3") {
		t.Errorf("older messages beyond limit should be dropped: %q", got)
	}
	for _, want := range []string{"msg4", "msg5", "msg6", "msg7", "msg8"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing recent message %s: %q", want, got)
		}
	}
}

// 全部被过滤（锚点唯一/全空）时返回空串。
func TestFormatRecentContextEmpty(t *testing.T) {
	if got := formatRecentContext([]adapter.MessageEvent{histMsg(5, "", "u", "only anchor")}, 5); got != "" {
		t.Errorf("anchor-only history should yield empty, got %q", got)
	}
	if got := formatRecentContext(nil, 5); got != "" {
		t.Errorf("nil history should yield empty, got %q", got)
	}
}
