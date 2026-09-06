package shortterm

import (
	"strings"
	"testing"
)

// TestBuildCompactContentStripsMsgid Compact 摘要前剥离 [msgid:N] 内部标记，
// 避免把展示层标记抄进长期记忆/技能记忆语料污染召回。
func TestBuildCompactContentStripsMsgid(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "[msgid:42][TuF3i(QQ:7)] " + strings.Repeat("长", 120)},
		{Role: "assistant", Content: "收到，马上处理"},
	}
	got := buildCompactContent(msgs)
	if strings.Contains(got, "[msgid:42]") {
		t.Errorf("Compact 内容应剥离 [msgid:N] 标记，实际: %q", got)
	}
	if !strings.Contains(got, "长") {
		t.Errorf("原文正文应保留，实际: %q", got)
	}
}
