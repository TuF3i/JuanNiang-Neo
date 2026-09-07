package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"JuanNiang-Neo/internal/adapter"
	"JuanNiang-Neo/internal/agent/provider"
	"JuanNiang-Neo/internal/agent/tool"
	"JuanNiang-Neo/internal/core/models"
	"JuanNiang-Neo/internal/metrics"
	"JuanNiang-Neo/internal/otelx"

	"go.opentelemetry.io/otel/attribute"
)

// RelevanceCheckResult 相关性检查结果。
type RelevanceCheckResult struct {
	Relevance float64 `json:"relevance"`
	Reason    string  `json:"reason"`
}

// 相关性判断限流（L3.1）：全局并发信号量上限与单次判断超时。
// 并发上限防止热聊时相关性 LLM 请求打爆 provider；超时与信号量等待共享预算，
// 超时/等待超时均按 JudgeFailPolicy 降级。
const (
	relevanceSemLimit     = 4
	relevanceJudgeTimeout = 10 * time.Second
)

// relevanceTimeout 返回相关性判断超时（配置值无效时回退到默认 10s）。
func (rs ReplySettings) relevanceTimeout() time.Duration {
	if rs.RelevanceTimeout <= 0 || rs.RelevanceTimeout > 120*time.Second {
		return relevanceJudgeTimeout
	}
	return rs.RelevanceTimeout
}

// acquireRelevanceSem 获取相关性判断并发令牌（L3.1）。ctx 取消/超时即返回错误。
func (h *HagoCenter) acquireRelevanceSem(ctx context.Context) error {
	if h.relevanceSem == nil {
		return nil
	}
	select {
	case h.relevanceSem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// releaseRelevanceSem 释放相关性判断并发令牌。
func (h *HagoCenter) releaseRelevanceSem() {
	if h.relevanceSem == nil {
		return
	}
	select {
	case <-h.relevanceSem:
	default:
	}
}

// isDefinitelyRelevant 规则快路径（L1）：消息明显指向机器人，无需 LLM 判断直接视为相关。
// 调用方已先处理 @ 自己 / 插件命令；这里覆盖"提及名字/常见称呼"。
func isDefinitelyRelevant(msg *adapter.MessageEvent, rs ReplySettings) bool {
	text := strings.TrimSpace(msg.RawMessage)
	if text == "" {
		return false
	}
	if rs.BotName != "" && strings.Contains(text, rs.BotName) {
		return true
	}
	for _, kw := range []string{"机器人", "bot", "Bot", "BOT"} {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// isDefinitelyIrrelevant 规则快路径（L1）：明显无关的噪音消息，直接丢弃不调 LLM。
// 覆盖：剥离 CQ 码/URL 后无有效文字（纯表情包/纯图片）、过短消息（≤2 字）、
// 纯 emoji/符号（无 CJK/字母数字）且较短。
func isDefinitelyIrrelevant(msg *adapter.MessageEvent) bool {
	text := strings.TrimSpace(msg.RawMessage)
	// 剥离 CQ 码与 URL 后取纯文本
	plain := cqCodeRegexp.ReplaceAllString(text, "")
	plain = urlRegexp.ReplaceAllString(plain, "")
	plain = strings.TrimSpace(plain)

	runes := []rune(plain)
	// 过短（≤2 个有效字符）→ 噪音（"哈哈"、"666"、"1"、纯图片无文字等）
	if len(runes) <= 2 {
		return true
	}
	// 纯 emoji/符号（无 CJK/字母数字）且较短 → 噪音（"😂😂😂"）
	if len(runes) <= 6 && !containsMeaningfulChars(plain) {
		return true
	}
	return false
}

// containsMeaningfulChars 判断文本是否包含有意义字符（ASCII 字母数字或 CJK）。
func containsMeaningfulChars(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return true
		}
		if r >= 0x4E00 && r <= 0x9FFF { // CJK 统一表意文字
			return true
		}
	}
	return false
}

// relevanceBatchEvaluate 对一批候选消息做相关性判断（L2.1 批量合并判断）。
// 候选消息已通过规则快路径（@/命令/提及名字/噪音过滤）。
// 返回 true=批内存在值得回复的内容，false=整批丢弃。
func (h *HagoCenter) relevanceBatchEvaluate(ctx context.Context, events []adapter.Event, rs ReplySettings, areaID string) (result bool) {
	// 链路追踪：相关性批量判断 span（记录候选数/判定结果）
	ctx, span := otelx.Span(ctx, "relevance.check",
		attribute.Int("candidates", len(events)),
		attribute.String("area_id", areaID),
	)
	defer func() {
		span.SetAttributes(attribute.Bool("relevant", result))
		span.End()
	}()
	// 单条候选 → 复用单条判断（带分数阈值比较，更精准）
	if len(events) == 1 {
		msg := events[0].Message
		if msg == nil {
			return false
		}
		recentMsgs, _ := h.getRecentMessages(ctx, areaID, 5) // L3.3: 上下文从 10 条缩至 5 条
		score, reason := h.relevanceAgentEvaluate(ctx, msg, recentMsgs, rs)
		related := score >= rs.RelevanceThreshold
		log.Debug("相关性: 单条候选判断", "score", score, "threshold", rs.RelevanceThreshold, "reason", reason, "related", related)
		result = related
		return
	}

	// 多条候选 → 一次 LLM 调用判断整批
	// L3.1 并发闸门 + 判断超时（信号量等待与 LLM 调用共享超时预算）
	judgeCtx, cancel := context.WithTimeout(ctx, rs.relevanceTimeout())
	defer cancel()
	if err := h.acquireRelevanceSem(judgeCtx); err != nil {
		log.Warn("相关性批量判断: 并发限制等待超时", "err", err)
		score, _ := judgeFailVerdict(rs, "相关性判断并发限制等待超时")
		result = score >= 0.5
		return
	}
	defer h.releaseRelevanceSem()

	botName := rs.BotName
	selfQQ := h.SelfQQ
	if h.Adapter != nil {
		if id := h.Adapter.SelfID(); id != 0 {
			selfQQ = id
		}
	}

	// 构建批量消息列表（带发送者标识；图片消息标注 [图片]）
	var sb strings.Builder
	for i, ev := range events {
		msg := ev.Message
		if msg == nil {
			continue
		}
		text := strings.TrimSpace(msg.RawMessage)
		hasImage := false
		for _, seg := range msg.Message {
			if seg.Type == "image" {
				hasImage = true
				break
			}
		}
		if hasImage {
			if text != "" {
				text += " [图片]"
			} else {
				text = "[图片]"
			}
		}
		if text == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("[%d] %s(%d): %s\n", i+1, msg.Sender.Nickname, msg.UserID, text))
	}
	if sb.Len() == 0 {
		return false
	}

	// 回复规则：用户自定义提示词优先，否则用默认
	rules := rs.RelevancePrompt
	if rules == "" {
		rules = fmt.Sprintf(`- 如果任一消息明确指向你（@你、叫你名字"%s"或"%s"、称呼"机器人"/"bot"、询问你的能力、请求你操作），relevant 为 true
- 如果全部是群友之间的闲聊、互怼、讨论与你无关的话题，relevant 为 false
- 如果包含群管理相关需求（踢人、禁言等），即使没有@你，relevant 也应该为 true
- 如果只是纯表情、无意义内容，relevant 为 false
- 如果群聊处于热聊状态（多人连续发言讨论同一话题），可以适当放宽判断`, h.SelfNickname, botName)
	}

	prompt := fmt.Sprintf(`你是一个群聊相关度判断助手。请判断以下这批群聊消息中是否有值得你（机器人）回复的内容。

你的身份:
- 你的名字: %s
- 你的昵称: %s
- 你的QQ: %d
- 群聊中 at 你的格式: [CQ:at,qq=%d]

回复规则:
%s

消息列表:
%s
请以 JSON 格式回复，只包含 JSON 对象，不要有其他内容:
{"relevant": true 或 false, "reason": "简短原因"}`, botName, h.SelfNickname, selfQQ, selfQQ, rules, sb.String())

	messages := []provider.ChatMessage{
		{Role: "system", Content: "你是一个群聊相关性判断助手。请以 JSON 格式回复。"},
		{Role: "user", Content: prompt},
	}

	req := provider.ChatRequest{
		Messages:    messages,
		Temperature: 0.3,
	}

	llm := h.Providers.SelectModel(provider.ModelTypeText)
	if rs.RelevanceModel != "" {
		if p, ok := h.Providers.GetProvider(rs.RelevanceModel); ok && p.Type() == provider.ModelTypeText {
			llm = p
		}
	}
	if llm == nil {
		log.Warn("相关性批量判断: 无可用 Text 模型，按不相关处理")
		result = false
		return
	}

	resp, err := llm.Chat(judgeCtx, req)
	if err != nil {
		log.Warn("相关性批量判断 LLM 调用失败", "err", err)
		score, _ := judgeFailVerdict(rs, "批量相关性判断 LLM 调用失败")
		result = score >= 0.5
		return
	}
	if resp.TokenUsage > 0 {
		metrics.LLMTokensTotal.WithLabelValues("relevance").Add(float64(resp.TokenUsage))
	}

	content := strings.TrimSpace(resp.Message.Content)
	var parsed struct {
		Relevant bool   `json:"relevant"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		// 容错：正则提取 relevant 字段
		parsed.Relevant = strings.Contains(content, `"relevant": true`) || strings.Contains(content, `"relevant":true`)
		parsed.Reason = "解析失败，按容错提取"
	}

	log.Info("相关性批量判断结果", "count", len(events), "relevant", parsed.Relevant, "reason", parsed.Reason)
	result = parsed.Relevant
	return
}

// relevanceAgentEvaluate 调用 LLM 评估消息相关性。
// 支持自定义提示词（rs.RelevancePrompt）与指定 Text 模型（rs.RelevanceModel）。
// 返回 0-1 之间的相关性分数，以及原因。
func (h *HagoCenter) relevanceAgentEvaluate(ctx context.Context, msg *adapter.MessageEvent, recentMessages []string, rs ReplySettings) (float64, string) {
	// L3.1 并发闸门 + 判断超时（信号量等待与 LLM/Vision 调用共享超时预算）
	judgeCtx, cancel := context.WithTimeout(ctx, rs.relevanceTimeout())
	defer cancel()
	if err := h.acquireRelevanceSem(judgeCtx); err != nil {
		return judgeFailVerdict(rs, "相关性判断并发限制等待超时")
	}
	defer h.releaseRelevanceSem()

	botName := rs.BotName
	// 获取机器人 QQ，优先 Adapter 实时值（防止缓存为 0）
	selfQQ := h.SelfQQ
	if h.Adapter != nil {
		if id := h.Adapter.SelfID(); id != 0 {
			selfQQ = id
		}
	}

	// 检查是否有图片消息但无视觉模型
	hasImage := false
	for _, seg := range msg.Message {
		if seg.Type == "image" {
			hasImage = true
			break
		}
	}
	if hasImage {
		visionModel := h.Providers.SelectModel(provider.ModelTypeImage)
		if visionModel == nil {
			return 0, "消息包含图片但没有配置视觉模型"
		}
		// 有视觉模型，用 Vision 分析
		for _, seg := range msg.Message {
			if seg.Type == "image" {
				url := extractImageURL(seg)
				if url != "" {
					prompt := fmt.Sprintf(`你是一个群聊相关度判断助手。请判断以下由用户发送的图片和文字是否与你（机器人）相关，是否需要你回复。

你的身份:
- 你的昵称: %s
- 你的QQ: %d

当前消息:
- 发送者昵称: %s
- 发送者QQ: %d

回复规则:
- 如果图片/文字明确指向你（@你、叫你名字"%s"、询问你的能力、请求你操作），相关度应该高（>0.7）
- 如果图片/文字是群友之间的闲聊、互怼、讨论与你无关的话题，相关度应该低（<0.3）
- 如果图片/文字是群管理相关需求，相关度应该高
- 如果图片是纯表情包、风景照、美食照等与任务无关的内容，相关度应该低

请以 JSON 格式回复: {"relevance": 0.0-1.0, "reason": "简短原因"}`, h.SelfNickname, selfQQ, msg.Sender.Nickname, msg.UserID, h.SelfNickname)
					// 下载图片字节并调用 Vision（此前误传 nil，模型实际看不到图片）
					imgBytes, err := h.fetchImageBytes(judgeCtx, url, seg)
					if err != nil {
						log.Warn("下载图片失败", "url", url, "err", err)
						return judgeFailVerdict(rs, "图片下载失败")
					}
					resp, err := visionModel.Vision(judgeCtx, imgBytes, prompt) // Vision expects raw image bytes
					if err != nil {
						log.Warn("Vision 调用失败", "err", err)
						return judgeFailVerdict(rs, "Vision 调用失败")
					}
					// 解析 JSON
					var result RelevanceCheckResult
					if err := json.Unmarshal([]byte(resp), &result); err != nil {
						// 尝试从响应中提取 JSON
						result = extractRelevanceJSON(resp)
					}
					return result.Relevance, result.Reason
				}
			}
		}
		return 0, "无法获取图片内容"
	}

	// 文本消息，调用文本模型（支持指定相关性检测专用模型）
	llm := h.Providers.SelectModel(provider.ModelTypeText)
	if rs.RelevanceModel != "" {
		if p, ok := h.Providers.GetProvider(rs.RelevanceModel); ok && p.Type() == provider.ModelTypeText {
			llm = p
		}
	}
	if llm == nil {
		return 0, "无可用 Text 模型"
	}

	// 构建上下文：最近消息 + 当前消息
	contextStr := "最近群聊消息:\n"
	for i, m := range recentMessages {
		contextStr += fmt.Sprintf("[%d] %s\n", i+1, m)
	}
	contextStr += fmt.Sprintf("\n当前消息 (发送者: %s, QQ: %d):\n%s",
		msg.Sender.Nickname, msg.UserID, msg.RawMessage)

	// 回复规则：用户自定义提示词优先，否则使用默认规则
	rules := rs.RelevancePrompt
	if rules == "" {
		rules = fmt.Sprintf(`- 如果消息明确指向你（@你、叫你的名字"%s"或"%s"、称呼"机器人"/"bot"、询问你的能力、请求你操作），相关度应该高（>0.7）
- 如果消息是群友之间的闲聊、互怼、讨论与你无关的话题，相关度应该低（<0.3）
- 如果消息是群管理相关需求（踢人、禁言等），即使没有@你，相关度也应该高
- 如果消息是群友之间互相求助且没有@你，相关度应该低
- 如果消息是纯表情、无意义内容，相关度应该低
- 如果群聊处于热聊状态（多人连续发言讨论同一话题），可以适当提高相关度（0.3-0.5）`, h.SelfNickname, botName)
	}

	prompt := fmt.Sprintf(`你是一个群聊相关度判断助手。请根据以下群聊上下文判断当前消息是否与你（机器人）相关，是否需要你回复。

你的身份:
- 你的名字: %s
- 你的昵称: %s
- 你的QQ: %d
- 群聊中 at 你的格式: [CQ:at,qq=%d]

当前消息:
- 发送者昵称: %s
- 发送者QQ: %d

回复规则:
%s

请以 JSON 格式回复，只包含 JSON 对象，不要有其他内容:
{"relevance": 0.0-1.0, "reason": "简短原因"}

上下文:
%s`, botName, h.SelfNickname, selfQQ, selfQQ, msg.Sender.Nickname, msg.UserID, rules, contextStr)

	messages := []provider.ChatMessage{
		{Role: "system", Content: "你是一个群聊相关性判断助手。请以 JSON 格式回复。"},
		{Role: "user", Content: prompt},
	}

	req := provider.ChatRequest{
		Messages:    messages,
		Temperature: 0.3,
	}

	resp, err := llm.Chat(judgeCtx, req)
	if err != nil {
		log.Warn("相关性检查 LLM 调用失败", "err", err)
		return judgeFailVerdict(rs, "LLM 调用失败")
	}
	if resp.TokenUsage > 0 {
		metrics.LLMTokensTotal.WithLabelValues("relevance").Add(float64(resp.TokenUsage))
	}

	content := strings.TrimSpace(resp.Message.Content)
	var result RelevanceCheckResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		// 尝试从响应中提取 JSON
		result = extractRelevanceJSON(content)
	}

	log.Info("相关性检查结果", "relevance", result.Relevance, "reason", result.Reason, "raw", content)
	return result.Relevance, result.Reason
}

// judgeFailVerdict 相关性判断失败（LLM/Vision 调用出错）时的降级策略。
//   - rs.JudgeFailPolicy == "reply" → 返回 1.0（照常回复，避免机器人"装死"）
//   - 默认（drop）→ 返回 0（不回复）
func judgeFailVerdict(rs ReplySettings, reason string) (float64, string) {
	if rs.JudgeFailPolicy == "reply" {
		return 1.0, reason + "（按配置 reply 照常回复）"
	}
	return 0, reason
}

// extractRelevanceJSON 从 LLM 响应中提取 JSON，处理 reason 中可能包含未转义引号的情况。
func extractRelevanceJSON(content string) RelevanceCheckResult {
	result := RelevanceCheckResult{Relevance: 0, Reason: "无法解析相关性结果"}

	// 1. 用正则提取 relevance 数值
	reRel := regexp.MustCompile(`"relevance"\s*:\s*([\d.]+)`)
	if m := reRel.FindStringSubmatch(content); len(m) >= 2 {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			result.Relevance = v
		}
	}

	// 2. 提取 reason：从 "reason": 之后到最后一个 "} 之前
	//    处理 reason 值中可能包含未转义双引号的情况
	reReason := regexp.MustCompile(`"reason"\s*:\s*"(.+)"\s*}`)
	if m := reReason.FindStringSubmatch(content); len(m) >= 2 {
		result.Reason = m[1]
	}

	return result
}

// extractImageURL 从消息段中提取图片 URL
func extractImageURL(seg adapter.Segment) string {
	if u, ok := seg.Data["url"]; ok {
		if s, ok := u.(string); ok && s != "" {
			return s
		}
	}
	if f, ok := seg.Data["file"]; ok {
		if s, ok := f.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// fetchImageBytes 下载图片消息的字节内容，供 Vision 模型识图。
// http(s) URL 直接下载（复用 tool.DownloadImageBytes：候选解码变体 + 大小上限）；
// file 路径（QQ 本地路径）先经 onebot11 get_image 转为可下载 URL。
func (h *HagoCenter) fetchImageBytes(ctx context.Context, rawURL string, seg adapter.Segment) ([]byte, error) {
	urlStr := strings.TrimSpace(rawURL)
	if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
		if h.Adapter == nil {
			return nil, fmt.Errorf("adapter 未就绪，无法解析图片路径")
		}
		info, err := h.Adapter.GetImage(rawURL)
		if err != nil {
			return nil, fmt.Errorf("get_image 失败: %w", err)
		}
		urlStr = info.URL
		if urlStr == "" {
			urlStr = info.File
		}
	}
	return tool.DownloadImageBytes(ctx, urlStr)
}

// getRecentMessages 获取 ChatArea 中最近 N 条消息（不含当前消息）。
// 同时用于相关性检查和 Agent 工具。
func (h *HagoCenter) getRecentMessages(ctx context.Context, chatAreaID string, limit int) ([]string, error) {
	if h.Memory == nil {
		return nil, nil
	}
	stMsgs, err := h.Memory.GetShortTermMessages(ctx, chatAreaID)
	if err != nil {
		return nil, err
	}
	// 最近 N 条（倒序取）
	if len(stMsgs) > limit {
		stMsgs = stMsgs[len(stMsgs)-limit:]
	}
	var result []string
	for _, m := range stMsgs {
		result = append(result, fmt.Sprintf("[%s] %s", m.Role, m.Content))
	}
	return result, nil
}

// getRecentMessagesByMsgType 根据消息类型和目标 ID 获取最近消息（供工具使用）。
func (h *HagoCenter) getRecentMessagesByMsgType(ctx context.Context, msgType string, targetID int64, limit int) ([]string, error) {
	var chatAreaType models.AreaType
	switch msgType {
	case "private":
		chatAreaType = models.AreaTypePrivate
	case "group":
		chatAreaType = models.AreaTypeGroup
	default:
		return nil, nil
	}
	area, err := h.DAO.ChatArea.GetOrCreate(ctx, chatAreaType, targetID)
	if err != nil {
		return nil, err
	}
	return h.getRecentMessages(ctx, area.ID, limit)
}

// isAtSelf 检查消息是否 @ 了机器人自己。
// SelfID 优先级：事件自带的 self_id（多连接时确定性归属，不依赖 map 遍历顺序）
// > Adapter 实时 SelfID > 启动时缓存的 SelfQQ。
func (h *HagoCenter) isAtSelf(rawMsg string, eventSelfID int64) bool {
	selfQQ := h.SelfQQ
	if eventSelfID != 0 {
		selfQQ = eventSelfID
	}
	if selfQQ == 0 && h.Adapter != nil {
		if id := h.Adapter.SelfID(); id != 0 {
			selfQQ = id
		}
	}
	if selfQQ == 0 {
		return false
	}
	return strings.Contains(rawMsg, fmt.Sprintf("[CQ:at,qq=%d]", selfQQ))
}

// isPluginCommand 检查消息是否是已注册的插件命令（以 / 开头且在命令注册表中存在）。
func (h *HagoCenter) isPluginCommand(rawMsg string) bool {
	if h.PluginEngine == nil {
		return false
	}
	return h.PluginEngine.HasPluginCommand(rawMsg)
}
