package agent

import (
	"JuanNiang-Neo/internal/adapter"
	"strings"
	"sync"
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

// TestSplitMessagesMultiLineListStaysWhole 换行不是拆分点：无句读的多行列表
// （如部门介绍）整体保持在一条消息内，不再被 60 字贪心合并从中间切开。
func TestSplitMessagesMultiLineListStaysWhole(t *testing.T) {
	content := "红岩网校是重邮团委旗下唯一互联网开发运营的学生组织，七大部门喵：\n" +
		"产品策划及运营部：产品设计蓝图\n" +
		"视觉设计部：用色彩渲染世界\n" +
		"前端研发部：网站小程序幕后画师\n" +
		"后端研发部：网校最可靠保障\n" +
		"移动开发部：APP无限可能\n" +
		"运维安全部（SRE）：系统稳定守护者\n" +
		"AI部：主攻AI应用落地想了解详情发 /redrock 喵～"
	parts := splitMessages(content)
	if len(parts) != 1 || parts[0] != content {
		t.Fatalf("无句读的多行列表应整体一条消息，实际 %d 段: %v", len(parts), parts)
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

// TestSplitMessagesForceMergeJoinsWithNewline 超 maxSegs 触发尾部强制合并时，
// 被合并的两段以换行连接，不再无分隔符直拼（避免不同语义内容粘连，如代码后粘上尾缀话术）。
func TestSplitMessagesForceMergeJoinsWithNewline(t *testing.T) {
	// 4 个 50 字长句（各带句号）：贪心合并每段已 >60 字无法两两并段，
	// 得到 4 段，硬限 3 段触发尾部合并
	sentence := func(r rune) string { return strings.Repeat(string(r), 50) + "。" }
	content := sentence('甲') + sentence('乙') + sentence('丙') + sentence('丁')
	parts := splitMessages(content)
	if len(parts) != 3 {
		t.Fatalf("应硬限合并为 3 段，实际 %d 段: %q", len(parts), parts)
	}
	last := parts[len(parts)-1]
	if !strings.Contains(last, "\n") {
		t.Errorf("强制合并的两段应以换行连接: %q", last)
	}
	if !strings.Contains(last, "丙") || !strings.Contains(last, "丁") {
		t.Errorf("被合并段应包含最后两句: %q", last)
	}
}

// TestSplitMessagesBlankLineForceMergeJoinsWithNewline 空行强分段超 3 段时，
// 尾部合并同样以换行连接。
func TestSplitMessagesBlankLineForceMergeJoinsWithNewline(t *testing.T) {
	content := "第一段。\n\n第二段。\n\n第三段。\n\n第四段。\n\n第五段。"
	parts := splitMessages(content)
	if len(parts) != 3 {
		t.Fatalf("应硬限合并为 3 段，实际 %d 段: %q", len(parts), parts)
	}
	last := parts[len(parts)-1]
	if !strings.Contains(last, "第三段。\n第四段。\n第五段。") {
		t.Errorf("尾部合并应以换行连接各段: %q", last)
	}
}

// TestParseCQCodeUnescape agent 侧 CQ 码解析同样还原标准转义实体（与 adapter 侧一致）。
func TestParseCQCodeUnescape(t *testing.T) {
	seg := parseCQCode("[CQ:music,type=custom,title=晴天&#44;Live,audio=https://e.com/a.mp3]")
	if seg.Type != "music" {
		t.Fatalf("类型应为 music, got %s", seg.Type)
	}
	if seg.Data["title"] != "晴天,Live" {
		t.Errorf("title 应反转义逗号, got %q", seg.Data["title"])
	}
	if seg.Data["audio"] != "https://e.com/a.mp3" {
		t.Errorf("audio 不应变, got %q", seg.Data["audio"])
	}
}

// TestGroupEventsByUser 验证按 UserID 分组：不同用户分开、同一用户按原顺序合并。
func TestGroupEventsByUser(t *testing.T) {
	ev := func(uid int64) adapter.Event {
		return adapter.Event{Message: &adapter.MessageEvent{UserID: uid}}
	}
	events := []adapter.Event{ev(1), ev(2), ev(1), ev(3), ev(2)}
	groups := groupEventsByUser(events)
	if len(groups) != 3 {
		t.Fatalf("应分成 3 组（用户 1/2/3），实际 %d 组", len(groups))
	}
	for _, g := range groups {
		uid := g[0].Message.UserID
		for _, e := range g {
			if e.Message.UserID != uid {
				t.Fatalf("组内混入了其他用户的消息: %v", g)
			}
		}
	}
	// 用户 1 的两条消息应按原顺序在同一组
	for _, g := range groups {
		if g[0].Message.UserID == 1 && len(g) != 2 {
			t.Fatalf("用户 1 的两条消息应合并为一组: %v", g)
		}
	}
	// nil Message 的事件被丢弃
	nilEvs := append(events, adapter.Event{})
	if got := groupEventsByUser(nilEvs); len(got) != 3 {
		t.Fatalf("nil Message 应被丢弃，仍为 3 组，实际 %d 组", len(got))
	}
}

// TestOrderedReplierOrder 并行处理完成后按 index 顺序投递（乱序到达也按序执行）。
func TestOrderedReplierOrder(t *testing.T) {
	r := newOrderedReplier()
	var mu sync.Mutex
	var got []int
	fn := func(i int) func() {
		return func() {
			mu.Lock()
			defer mu.Unlock()
			got = append(got, i)
		}
	}

	// 模拟并行完成：index 乱序到达（2 先完成，然后 0、1）
	r.Enqueue(2, fn(2))
	r.Enqueue(0, fn(0))
	r.Enqueue(1, fn(1))

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("应执行 3 个动作，实际 %d: %v", len(got), got)
	}
	for i, v := range got {
		if v != i {
			t.Fatalf("执行顺序应为 0,1,2，实际 %v", got)
		}
	}
}

// TestOrderedReplierSequential 顺序到达时依次立即执行，不缓存。
func TestOrderedReplierSequential(t *testing.T) {
	r := newOrderedReplier()
	var got []int
	r.Enqueue(0, func() { got = append(got, 0) })
	r.Enqueue(1, func() { got = append(got, 1) })
	r.Enqueue(2, func() { got = append(got, 2) })
	if len(got) != 3 || got[0] != 0 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("顺序到达应依次执行: %v", got)
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
