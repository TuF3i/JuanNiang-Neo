package adapter

import (
	"encoding/json"
	"testing"
)

// TestNormalizeReplyIDs 19 位 message_id 无损规范化：普通 json.Unmarshal 会把大整数
// 落入 map[string]any → float64 丢精度，normalizeReplyIDs 从原始 JSON 按字符串
// 读取再转 int64，规避 float64 中间态的精度丢失。
func TestNormalizeReplyIDs(t *testing.T) {
	raw := []byte(`{
		"post_type": "message",
		"message_id": 123,
		"user_id": 7,
		"group_id": 9,
		"message": [
			{"type": "reply", "data": {"id": 1234567890123456789}},
			{"type": "text", "data": {"text": "hi"}}
		],
		"raw_message": "hi"
	}`)
	var msg MessageEvent
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	normalizeReplyIDs(&msg, raw)
	if len(msg.Message) == 0 || msg.Message[0].Type != "reply" {
		t.Fatalf("预期第一条为 reply 段")
	}
	got := msg.Message[0].Data["id"]
	if got != int64(1234567890123456789) {
		t.Fatalf("reply id = %v (%T), want 1234567890123456789", got, got)
	}
}

// TestNormalizeReplyIDsString 字符串形式 id 也归一化为 int64。
func TestNormalizeReplyIDsString(t *testing.T) {
	raw := []byte(`{
		"post_type": "message",
		"message_id": 1,
		"user_id": 7,
		"group_id": 9,
		"message": [{"type": "reply", "data": {"id": "2234567890123456789"}}],
		"raw_message": "x"
	}`)
	var msg MessageEvent
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	normalizeReplyIDs(&msg, raw)
	if got := msg.Message[0].Data["id"]; got != int64(2234567890123456789) {
		t.Fatalf("reply id = %v (%T), want 2234567890123456789", got, got)
	}
}

// TestNormalizeReplyIDsNoReply 无 reply 段不 panic、不改动。
func TestNormalizeReplyIDsNoReply(t *testing.T) {
	raw := []byte(`{"post_type":"message","message":[{"type":"text","data":{"text":"hi"}}]}`)
	var msg MessageEvent
	_ = json.Unmarshal(raw, &msg)
	normalizeReplyIDs(&msg, raw)
	if len(msg.Message) != 1 || msg.Message[0].Type != "text" {
		t.Fatalf("无 reply 段不应被改动")
	}
}
