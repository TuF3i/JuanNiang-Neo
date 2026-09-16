package adapter

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// --- 消息段构建器 ---

func Text(text string) Segment {
	return Segment{Type: "text", Data: map[string]any{"text": text}}
}

// Image 图片。file 可以是 URL、base64 或本地路径。
func Image(file string) Segment {
	return Segment{Type: "image", Data: map[string]any{"file": file}}
}

// Sticker 表情段。与普通图片的区别是 subType=1（OneBot11 用它区分表情与图片）。
// file 传图床表情引用（stk://<短UUID>），发送层会自动解析为 base64。
func Sticker(file string) Segment {
	return Segment{Type: "image", Data: map[string]any{"file": file, "subType": 1}}
}

// FileMsg 文件 (群文件上传)。
func FileMsg(file string) Segment {
	return Segment{Type: "file", Data: map[string]any{"file": file}}
}

// Face QQ 表情。id 为表情编号。
func Face(id int) Segment {
	return Segment{Type: "face", Data: map[string]any{"id": strconv.Itoa(id)}}
}

// At @某人。qq 为 QQ 号, "all" 表示全体。
func At(qq string) Segment {
	return Segment{Type: "at", Data: map[string]any{"qq": qq}}
}

func AtAll() Segment { return At("all") }

// Record 语音。
func Record(file string) Segment {
	return Segment{Type: "record", Data: map[string]any{"file": file}}
}

// Video 视频。
func Video(file string) Segment {
	return Segment{Type: "video", Data: map[string]any{"file": file}}
}

// Reply 回复消息。id 为被回复的消息 ID。
func Reply(id string) Segment {
	return Segment{Type: "reply", Data: map[string]any{"id": id}}
}

// R 回复消息, int64 版本。
func ReplyInt64(id int64) Segment {
	return Reply(strconv.FormatInt(id, 10))
}

// BuildMessage 混合构建消息段数组。
// 参数可以是 string (→Text) 或 Segment。
//
//	BuildMessage(At("123456"), " hello ", Image("https://..."))
func BuildMessage(parts ...any) []Segment {
	segments := make([]Segment, 0, len(parts))
	for _, p := range parts {
		switch v := p.(type) {
		case string:
			segments = append(segments, Text(v))
		case Segment:
			segments = append(segments, v)
		case []Segment:
			segments = append(segments, v...)
		default:
			segments = append(segments, Text(fmt.Sprint(v)))
		}
	}
	return segments
}

// --- CQ 码解析 ---

var cqCodeRe = regexp.MustCompile(`\[CQ:(\w+)((?:,[^,\]]+=.*?)*)\]`)

// cqCodeNormalizeRe 匹配 CQ 码前缀，容忍 [ 后 / CQ 后 / 冒号后的空白瑕疵。
// LLM 偶尔生成 "[ CQ:face,id=66]" 这类格式，OneBot 无法解析会原样显示在消息里。
var cqCodeNormalizeRe = regexp.MustCompile(`\[\s*CQ\s*:\s*(\w+)`)

// face 幻觉码兜底（LLM 偶尔漏写 CQ: 前缀，或改用冒号/空格分隔）：
//
//	"[face,id=123]" / "[face id=123]" → "[CQ:face,id=123]"
//	"[face:123]"                      → "[CQ:face,id=123]"
//
// 非数字 id 的畸形码（如 "[face:微笑]"）无法发送，直接剔除避免原文露出。
var (
	faceColonRe = regexp.MustCompile(`(?i)\[\s*face\s*:\s*(\d+)\s*\]`)
	faceArgsRe  = regexp.MustCompile(`(?i)\[\s*face\s*[, ]\s*id\s*=\s*(\d+)\s*((?:,\s*[\w-]+\s*=[^,\]]*)*)\s*\]`)
	faceJunkRe  = regexp.MustCompile(`(?i)\[\s*face(?:[\s:,][^\]]*)?\]`)
)

// NormalizeCQCodes 修复 CQ 码格式瑕疵，使其可被 cqCodeRe 正确解析：
//
//	"[ CQ:face,id=66]"   → "[CQ:face,id=66]"
//	"[CQ : image,file=url]" → "[CQ:image,file=url]"
//	"[face,id=123]" / "[face:123]" / "[face id=123]" → "[CQ:face,id=123]"（幻觉兜底）
//
// 参数部分（,key=value]）原样保留。
func NormalizeCQCodes(s string) string {
	if s == "" {
		return s
	}
	// 空白瑕疵修复
	if strings.Contains(s, "CQ") {
		s = cqCodeNormalizeRe.ReplaceAllString(s, "[CQ:$1")
	}
	// face 幻觉码兜底：先改写可识别的数字 id 变体，再剔除剩余畸形码。
	// 正则要求 [ 后紧跟 face，标准 [CQ:face,...] 不会被误改。
	if strings.Contains(s, "face") || strings.Contains(s, "Face") || strings.Contains(s, "FACE") {
		s = faceColonRe.ReplaceAllString(s, "[CQ:face,id=$1]")
		s = faceArgsRe.ReplaceAllString(s, "[CQ:face,id=${1}${2}]")
		s = faceJunkRe.ReplaceAllString(s, "")
	}
	return s
}

// ParseCQCodes 解析字符串中的 CQ 码, 返回消息段数组。
func ParseCQCodes(raw string) []Segment {
	if raw == "" {
		return nil
	}

	var segments []Segment
	lastIdx := 0

	for _, match := range cqCodeRe.FindAllStringSubmatchIndex(raw, -1) {
		if match[0] > lastIdx {
			text := raw[lastIdx:match[0]]
			if text != "" {
				segments = append(segments, Text(text))
			}
		}

		cqType := raw[match[2]:match[3]]
		argsStr := raw[match[4]:match[5]]
		seg := Segment{Type: cqType, Data: make(map[string]any)}
		for k, v := range parseCQArgs(argsStr) {
			seg.Data[k] = v
		}
		segments = append(segments, seg)

		lastIdx = match[1]
	}

	if lastIdx < len(raw) {
		text := raw[lastIdx:]
		if text != "" {
			segments = append(segments, Text(text))
		}
	}

	return segments
}

func HasCQCode(s string) bool { return cqCodeRe.MatchString(s) }

// --- 消息构建器 (流式 API) ---

// MessageBuilder 提供链式消息构建, 可直接传给 SendGroupMsg / SendPrivateMsg。
type MessageBuilder struct {
	segments []Segment
}

// NewMsg 创建一个空的消息构建器。
//
//	msg := adapter.NewMsg().Text("Hello ").At("123").Image("https://...")
//	p.SendGroupMsg(groupID, msg)
func NewMsg() *MessageBuilder {
	return &MessageBuilder{}
}

func (b *MessageBuilder) Text(s string) *MessageBuilder {
	b.segments = append(b.segments, Text(s))
	return b
}

func (b *MessageBuilder) Image(file string) *MessageBuilder {
	b.segments = append(b.segments, Image(file))
	return b
}

func (b *MessageBuilder) File(file string) *MessageBuilder {
	b.segments = append(b.segments, FileMsg(file))
	return b
}

func (b *MessageBuilder) Face(id int) *MessageBuilder {
	b.segments = append(b.segments, Face(id))
	return b
}

func (b *MessageBuilder) At(qq string) *MessageBuilder {
	b.segments = append(b.segments, At(qq))
	return b
}

func (b *MessageBuilder) AtAll() *MessageBuilder {
	b.segments = append(b.segments, AtAll())
	return b
}

func (b *MessageBuilder) Record(file string) *MessageBuilder {
	b.segments = append(b.segments, Record(file))
	return b
}

func (b *MessageBuilder) Video(file string) *MessageBuilder {
	b.segments = append(b.segments, Video(file))
	return b
}

func (b *MessageBuilder) Reply(id string) *MessageBuilder {
	b.segments = append(b.segments, Reply(id))
	return b
}

func (b *MessageBuilder) ReplyInt64(id int64) *MessageBuilder {
	b.segments = append(b.segments, Reply(strconv.FormatInt(id, 10)))
	return b
}

// Seg 追加任意 Segment。
func (b *MessageBuilder) Seg(seg Segment) *MessageBuilder {
	b.segments = append(b.segments, seg)
	return b
}

// Build 返回构建完成的消息段数组。
func (b *MessageBuilder) Build() []Segment {
	return b.segments
}

func parseCQArgs(s string) map[string]string {
	args := make(map[string]string)
	s = strings.TrimPrefix(s, ",")
	for _, part := range strings.Split(s, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			args[strings.TrimSpace(kv[0])] = kv[1]
		}
	}
	return args
}
