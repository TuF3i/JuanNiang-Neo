package adapter

import (
	"fmt"
	"strings"
	"testing"
)

func TestNormalizeCQCodes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"无CQ码原样", "你好世界", "你好世界"},
		{"标准CQ码不变", "看这个 [CQ:face,id=66] 表情", "看这个 [CQ:face,id=66] 表情"},
		{"左括号后空格", "看这个 [ CQ:face,id=66] 表情", "看这个 [CQ:face,id=66] 表情"},
		{"CQ后空格", "[CQ : face,id=66]", "[CQ:face,id=66]"},
		{"冒号后空格", "[CQ: face,id=66]", "[CQ:face,id=66]"},
		{"混合多处", "[ CQ:at,qq=123] 你好 [ CQ:image,file=http://x/y.jpg]", "[CQ:at,qq=123] 你好 [CQ:image,file=http://x/y.jpg]"},
		// face 幻觉码兜底
		{"face逗号幻觉", "来个表情[face,id=123]", "来个表情[CQ:face,id=123]"},
		{"face冒号幻觉", "[face:123]来个表情", "[CQ:face,id=123]来个表情"},
		{"face空格幻觉", "[face id=123]", "[CQ:face,id=123]"},
		{"face大小写不敏感", "[Face:123]", "[CQ:face,id=123]"},
		{"face带额外参数", "[face,id=123,sub_type=3]", "[CQ:face,id=123,sub_type=3]"},
		{"face非数字剔除", "看[face:微笑]这个", "看这个"},
		{"face空标签剔除", "[face] 你好", " 你好"},
		{"标准face码不受影响", "[CQ:face,id=66]", "[CQ:face,id=66]"},
		{"facebook不误伤", "上[facebook]了", "上[facebook]了"},
	}
	for _, c := range cases {
		if got := NormalizeCQCodes(c.in); got != c.want {
			t.Errorf("%s: NormalizeCQCodes(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// TestParseCQCodesWithNormalize 验证带空格瑕疵的 CQ 码经规范化后能被正确解析为消息段。
func TestParseCQCodesWithNormalize(t *testing.T) {
	raw := "[ CQ:at,qq=1483915073] 看嘛 [ CQ:face,id=66]"
	segments := ParseCQCodes(NormalizeCQCodes(raw))
	if len(segments) != 3 {
		t.Fatalf("期望 3 个消息段, got %d: %+v", len(segments), segments)
	}
	if segments[0].Type != "at" || segments[0].Data["qq"] != "1483915073" {
		t.Errorf("第一个段应为 at, got %+v", segments[0])
	}
	if segments[2].Type != "face" || segments[2].Data["id"] != "66" {
		t.Errorf("第三个段应为 face id=66, got %+v", segments[2])
	}
}

// TestNormalizeMessageWithCQ 验证 normalizeMessage 对带瑕疵 CQ 码的字符串消息能产出消息段。
func TestNormalizeMessageWithCQ(t *testing.T) {
	out := normalizeMessage("[ CQ:face,id=66]")
	segs, ok := out.([]Segment)
	if !ok {
		t.Fatalf("期望 []Segment, got %T: %+v", out, out)
	}
	if len(segs) != 1 || segs[0].Type != "face" || segs[0].Data["id"] != "66" {
		t.Errorf("期望 face 消息段, got %+v", segs)
	}
}

// TestParseCQCodesWithHallucinatedFace 验证幻觉 face 码经兜底后能正确解析为表情消息段。
func TestParseCQCodesWithHallucinatedFace(t *testing.T) {
	raw := "看这个[face,id=123]和[face:66]"
	segs := ParseCQCodes(NormalizeCQCodes(raw))
	if len(segs) != 4 {
		t.Fatalf("期望 4 个消息段, got %d: %+v", len(segs), segments2str(segs))
	}
	if segs[1].Type != "face" || segs[1].Data["id"] != "123" {
		t.Errorf("第二个段应为 face id=123, got %+v", segs[1])
	}
	if segs[3].Type != "face" || segs[3].Data["id"] != "66" {
		t.Errorf("第四个段应为 face id=66, got %+v", segs[3])
	}
}

// segments2str 测试辅助：消息段数组转可读字符串。
func segments2str(segs []Segment) string {
	parts := make([]string, 0, len(segs))
	for _, s := range segs {
		parts = append(parts, s.Type+":"+fmt.Sprint(s.Data))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
