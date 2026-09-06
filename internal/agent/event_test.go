package agent

import (
	"strings"
	"testing"

	einoschema "github.com/cloudwego/eino/schema"
)

// TestSplitMessagesCQCodeNotSplit 验证 CQ 码不会被切到两条消息里。
// 带 query 参数的图片 URL 含 "?"（断句符），若 CQ 码不保护会被从中间切开。
func TestSplitMessagesCQCodeNotSplit(t *testing.T) {
	content := "看图 [CQ:image,file=https://example.com/img.jpg?x=1&y=2]。再看看这张 [CQ:face,id=14]。最后一句话到这里结束。"
	parts := splitMessages(content)

	for i, p := range parts {
		if strings.Contains(p, "[CQ:") && !strings.Contains(p, "]") {
			t.Fatalf("段 %d 包含不完整的 CQ 码（被切开）: %q", i, p)
		}
	}

	// 拼接回去应保留所有 CQ 码完整
	joined := strings.Join(parts, "")
	for _, cq := range []string{"[CQ:image,file=https://example.com/img.jpg?x=1&y=2]", "[CQ:face,id=14]"} {
		if !strings.Contains(joined, cq) {
			t.Fatalf("CQ 码在拆分后丢失: %s\nparts: %v", cq, parts)
		}
	}
}

// TestSplitMessagesLongTextWithCQ 长文本中 CQ 码在断句处附近，拆分后 CQ 码仍完整。
func TestSplitMessagesLongTextWithCQ(t *testing.T) {
	content := "第一句话讲了一些内容，第二句也有点长需要被拆分处理看看效果如何。第三句带图[CQ:image,file=https://example.com/a.png?size=1]第四句继续说事情。第五句结束啦！"
	parts := splitMessages(content)

	if len(parts) == 0 {
		t.Fatal("拆分结果为空")
	}
	joined := strings.Join(parts, "")
	if !strings.Contains(joined, "[CQ:image,file=https://example.com/a.png?size=1]") {
		t.Fatalf("CQ 码在拆分后不完整\nparts: %v", parts)
	}
	for _, p := range parts {
		if strings.Contains(p, "[CQ:") && !strings.Contains(p, "]") {
			t.Fatalf("段包含被切开的 CQ 码: %q", p)
		}
	}
}

// TestSplitMessagesShortNoSplit 短内容（≤60 有效字）不拆分。
func TestSplitMessagesShortNoSplit(t *testing.T) {
	content := "你好[CQ:face,id=14]。今天天气不错！"
	parts := splitMessages(content)
	if len(parts) != 1 {
		t.Fatalf("短内容应不拆分，实际 %d 段: %v", len(parts), parts)
	}
}

// TestSplitMessagesEmojiStaysWithPunctuation 断句符后紧跟的 emoji 应留在前一段，
// 不被切到下一条消息开头（如 "明天见！😊 拜拜" 的 😊 应跟 "明天见！"）。
func TestSplitMessagesEmojiStaysWithPunctuation(t *testing.T) {
	content := "明天见！😊 拜拜～再见！👋 明天继续聊。这是一段用于凑够六十个有效字符的长文本内容，确保能够触发分段拆分逻辑，让我们看看 emoji 是否会被错误地切到下一条消息的开头位置去。"
	parts := splitMessages(content)
	if len(parts) < 2 {
		t.Fatalf("长内容应拆分为多段: %v", parts)
	}
	// 第一段应包含 "！😊"（emoji 跟断句符一起）
	if !strings.Contains(parts[0], "！😊") {
		t.Fatalf("emoji 应留在断句符所在段: %v", parts)
	}
	// emoji 不应出现在任何段的开头（下一条消息以 emoji 开头即视为被切）
	for i, p := range parts {
		trimmed := strings.TrimLeft(p, " \t\n")
		if m := emojiPrefixRe.FindString(trimmed); m != "" {
			t.Fatalf("段 %d 以 emoji 开头（被切到下一条消息）: %q", i, p)
		}
	}
}

// TestSplitMessagesEmojiSequence 断句符后紧跟的 emoji 序列（多个/ZWJ）整体归前段。
func TestSplitMessagesEmojiSequence(t *testing.T) {
	content := "太棒了！🎉🎉 下次再约。"
	parts := splitMessages(content)
	if !strings.Contains(parts[0], "！🎉🎉") {
		t.Fatalf("emoji 序列应留在前一段: %v", parts)
	}
}

// TestSplitMessagesNewlineAsSplitPoint 换行也是拆分点：长内容按行拆分为多条消息。
func TestSplitMessagesNewlineAsSplitPoint(t *testing.T) {
	content := strings.Repeat("第一行内容比较长一些。", 5) + "\n" + strings.Repeat("第二行内容也比较长。", 5)
	parts := splitMessages(content)
	if len(parts) < 2 {
		t.Fatalf("长内容应按换行拆分为多段: %v", parts)
	}
	// 每段内不应残留换行（换行已作为拆分点被消费）
	for i, p := range parts {
		if strings.Contains(p, "\n") {
			t.Fatalf("段 %d 内不应包含换行: %q", i, p)
		}
	}
}

// TestSplitMessagesNewlineNoEmojiMerge 换行后的 emoji 属于下一行，不归入前段。
func TestSplitMessagesNewlineNoEmojiMerge(t *testing.T) {
	content := strings.Repeat("前一行内容比较长需要拆分。", 4) + "\n😊 后一行内容。"
	parts := splitMessages(content)
	// 换行处的 emoji 应留在后一段（前段不以 emoji 结尾归并）
	joined := strings.Join(parts, "")
	if !strings.Contains(joined, "😊") {
		t.Fatalf("emoji 不应丢失: %v", parts)
	}
}

func TestMergeLeadingSystemMsgs(t *testing.T) {
	sys := func(c string) *einoschema.Message { return &einoschema.Message{Role: einoschema.System, Content: c} }
	usr := func(c string) *einoschema.Message { return &einoschema.Message{Role: einoschema.User, Content: c} }

	t.Run("三条 system 合并为一条", func(t *testing.T) {
		in := []*einoschema.Message{sys("A"), sys("B"), sys("C"), usr("hi")}
		out := mergeLeadingSystemMsgs(in)
		if len(out) != 2 {
			t.Fatalf("len = %d, want 2", len(out))
		}
		if out[0].Role != einoschema.System || out[0].Content != "A\n\nB\n\nC" {
			t.Errorf("out[0] = %+v, want 合并后的 system", out[0])
		}
		if out[1].Content != "hi" {
			t.Errorf("out[1].Content = %q, want hi", out[1].Content)
		}
	})

	t.Run("单条 system 不变", func(t *testing.T) {
		in := []*einoschema.Message{sys("A"), usr("hi")}
		out := mergeLeadingSystemMsgs(in)
		if len(out) != 2 || out[0].Content != "A" {
			t.Errorf("单条 system 不应被合并: %+v", out)
		}
	})

	t.Run("开头无 system 不变", func(t *testing.T) {
		in := []*einoschema.Message{usr("hi")}
		out := mergeLeadingSystemMsgs(in)
		if len(out) != 1 || out[0].Content != "hi" {
			t.Errorf("无 system 不应变化: %+v", out)
		}
	})
}
