package groupmgr

import (
	"context"
	"encoding/json"
	"strings"

	"JuanNiang-Neo/internal/adapter"
	"JuanNiang-Neo/internal/core/models"
	"JuanNiang-Neo/internal/metrics"
	"JuanNiang-Neo/internal/otelx"

	"go.opentelemetry.io/otel/attribute"
)

// QQ 群聊推荐卡片：OneBot 11 json 消息段 data 中的 app 标识（计入广告违规）。
var qqCardApps = []string{
	"com.tencent.contact.lua",    // 推荐联系人
	"com.tencent.troopsharecard", // 推荐群聊卡片
}

// stripCQ 剥离 CQ 码（避免命中 json 卡片/图片等富文本 payload）。
func stripCQ(raw string) string {
	return cqCodeRe.ReplaceAllString(raw, " ")
}

// detectGroupCard 检测 QQ 群聊推荐卡片（只在 CQ 段内匹配 app，避免命中段外文本）。
func detectGroupCard(raw string) bool {
	if raw == "" {
		return false
	}
	lower := strings.ToLower(raw)
	pos := 0
	for {
		s := strings.Index(lower[pos:], "[cq:json")
		if s < 0 {
			return false
		}
		s += pos
		e := strings.Index(lower[s:], "]")
		if e < 0 {
			e = len(lower) - s
		}
		segment := lower[s : s+e]
		for _, app := range qqCardApps {
			if strings.Contains(segment, app) {
				return true
			}
		}
		pos = s + e
	}
}

// unescapeCQEntity 还原 CQ 码转义实体（&#44; , / &#91; [ / &#93; ] / &amp; &）。
// &amp; 必须最后还原，避免把用户原文里的 "&amp;#44;" 字面量二次解码。
func unescapeCQEntity(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	s = strings.ReplaceAll(s, "&#44;", ",")
	s = strings.ReplaceAll(s, "&#91;", "[")
	s = strings.ReplaceAll(s, "&#93;", "]")
	return strings.ReplaceAll(s, "&amp;", "&")
}

// cardText 推荐卡片文本化：从 raw 中的 CQ:json 卡片段提取送审文本。
// card-only 消息剥离 CQ 码后为空，若不补文本，RAG 会因空 q 报错降级、
// LLM 送审拿到空 <USER_TEXT> 无法判定（issue #76）。提取 prompt 与 meta 内
// 昵称/描述等文本字段（跳过 URL），JSON 解析失败时退回清洗后的 data 原文，
// 保证对 detectGroupCard 命中的卡片返回非空；无可识别卡片返回空串。
func cardText(raw string) string {
	lower := strings.ToLower(raw)
	pos := 0
	for {
		s := strings.Index(lower[pos:], "[cq:json")
		if s < 0 {
			return ""
		}
		s += pos
		e := strings.Index(lower[s:], "]")
		if e < 0 {
			e = len(lower) - s
		}
		seg, segLower := raw[s:s+e], lower[s:s+e]
		pos = s + e
		isCard := false
		for _, app := range qqCardApps {
			if strings.Contains(segLower, app) {
				isCard = true
				break
			}
		}
		if !isCard {
			continue
		}
		if t := cardJSONText(seg); t != "" {
			return t
		}
	}
}

// cardJSONText 从单个 CQ:json 段提取卡片文本（data= 后的 payload）。
func cardJSONText(seg string) string {
	i := strings.Index(seg, "data=")
	if i < 0 {
		return "[推荐卡片]"
	}
	payload := unescapeCQEntity(seg[i+len("data="):])
	fallback := headText(strings.Join(strings.Fields(payload), " "), 400)
	var obj map[string]any
	if err := json.Unmarshal([]byte(payload), &obj); err != nil || obj == nil {
		if fallback == "" {
			return "[推荐卡片]"
		}
		return "[推荐卡片] " + fallback
	}
	var parts []string
	addPart := func(s string) {
		s = strings.TrimSpace(s)
		// 跳过 URL（jumpUrl/avatar 等对 RAG/LLM 判定是噪声）
		if s == "" || strings.Contains(s, "://") {
			return
		}
		for _, p := range parts {
			if p == s {
				return
			}
		}
		parts = append(parts, s)
	}
	if v, ok := obj["prompt"].(string); ok {
		addPart(v)
	}
	if meta, ok := obj["meta"].(map[string]any); ok {
		for _, mv := range meta {
			cm, ok := mv.(map[string]any)
			if !ok {
				continue
			}
			for _, k := range []string{"nickname", "contact", "desc"} {
				if v, ok := cm[k].(string); ok {
					addPart(v)
				}
			}
		}
	}
	if len(parts) == 0 {
		if fallback == "" {
			return "[推荐卡片]"
		}
		return "[推荐卡片] " + fallback
	}
	return headText("[推荐卡片] "+strings.Join(parts, " "), 600)
}

// detectMessage 群消息检测入口（Phase 0.5）。
// 顺序：违禁言论（不消费）→ 图片刷屏（消费）→ +1 复读（消费）。
func (m *Manager) detectMessage(ctx context.Context, ev adapter.Event, cfg *models.GroupMgrConfig) bool {
	m.detectViolation(ctx, ev, cfg)
	if m.checkImageSpam(ctx, ev, cfg) {
		return true
	}
	if m.checkCopySpam(ctx, ev, cfg) {
		return true
	}
	return false
}

// detectViolation 违禁言论检测：RAG 语义匹配黑名单（第一核实人）→ LLM 统一判定 → 关键词兜底。
// 流程：
//  1. RAG 检索黑名单语录：命中（score ≥ BlackMinScore）→ 按样本类型处罚；
//  2. 未命中 / 未达阈值 / RAG 不可用 → LLM 批量判定（3s 窗口凑批，逐条独立判定）；
//  3. LLM 也不可用 → 关键词兜底（敏感/黑词直罚、灰词放行）。
//
// 返回 true 仅表示"已发起同步处罚"，不影响消费语义（违禁类一律不消费消息）。
func (m *Manager) detectViolation(ctx context.Context, ev adapter.Event, cfg *models.GroupMgrConfig) bool {
	msg := ev.Message
	raw := msg.RawMessage
	text := strings.TrimSpace(stripCQ(raw))
	card := detectGroupCard(raw)
	// 卡片文本化：card-only 消息剥离 CQ 后为空，提取卡片字段作送审文本，
	// 否则 RAG 空 q 报错降级、LLM 送审空 <USER_TEXT> 无法判定（issue #76）
	if text == "" && card {
		text = cardText(raw)
	}
	if text == "" {
		return false
	}
	// 关键词命中仅作最后兜底（RAG+LLM 均不可用时），不参与 RAG 判据
	word, wordCat := m.wordHit(ctx, text)

	// 链路追踪：群管理违禁检测 span（关键词预查结果 + 后续路径）。
	// 必须用新 ctx 继续后续调用：否则 verify_rag/punish/llm.call 等子 span
	// 会与 detect 平级而非嵌套（父 span 提前闭包后子 span 挂到更外层）。
	ctx, span := otelx.Span(ctx, "groupmgr.detect",
		attribute.String("word", word),
		attribute.String("word_cat", wordCat),
		attribute.Bool("card", card),
	)
	defer span.End()

	// 第一核实人：RAG 语义匹配黑名单
	if v := m.verifyByRAG(ctx, text, true); v.ok {
		return m.handleRAGMatch(ctx, ev, cfg, text, card, word, wordCat, v)
	}
	// RAG 不可用 → 转入 LLM 判定路径
	return m.handleRAGUnavailablePath(ctx, ev, cfg, text, card, word, wordCat)
}

// handleRAGUnavailablePath RAG 不可用时的判定路径：先 LLM，LLM 也不可用才走关键词兜底。
func (m *Manager) handleRAGUnavailablePath(ctx context.Context, ev adapter.Event, cfg *models.GroupMgrConfig,
	text string, card bool, word, wordCat string) bool {
	// 无 RAG 命中信息：rc 仅含送审文本 + 关键词预查结果；卡片为硬信号
	// （LLM 异常时按 keyword 路径同语义直罚，不让推荐卡片因 LLM 故障漏网）
	rc := reviewCtx{text: text, word: word, wordCat: wordCat, card: card, highRisk: card, hard: card}
	if m.submitReview(ctx, ev, rc) {
		metrics.GroupMgrDetectionsTotal.WithLabelValues("rag", "review").Inc()
		return true
	}
	// LLM 也不可用 → 关键词兜底（RAG + LLM 均失败）
	return m.handleKeywordPath(ctx, ev, cfg, text, card, word, wordCat)
}

// handleRAGMatch RAG 语义匹配后的分档决策：
//
//	黑名单命中 score ≥ BlackMinScore → 按样本类型直接处罚
//	未命中 / 未达阈值                → LLM 统一判定（批窗口）；LLM 不可用 → 关键词兜底
func (m *Manager) handleRAGMatch(ctx context.Context, ev adapter.Event, cfg *models.GroupMgrConfig,
	text string, card bool, word, wordCat string, v ragVerdict) bool {
	if v.black != nil && v.black.score >= cfg.BlackMinScore {
		category := "ad"
		if v.black.category == "sensitive" {
			category = "sensitive"
		}
		reason := "RAG黑名单语义匹配"
		metrics.GroupMgrDetectionsTotal.WithLabelValues("rag", "punish").Inc()
		// 检索追踪日志：方式=RAG + 命中分数 + 命中语录前 20 字
		log.Info("违禁检测: 方式=RAG", "list", "black", "score", v.black.score, "hit", headText(v.black.text, 20), "user", ev.Message.UserID)
		m.punish(ctx, ev, reason, category, "rag")
		m.phraseHit(ctx, v.black.tag)
		log.Info("RAG 黑名单命中，处罚", "score", v.black.score, "phrase", v.black.text, "user", ev.Message.UserID)
		return true
	}

	// 未命中 / 未达阈值 → LLM 统一判定（批窗口异步，不阻塞主循环）
	if v.black == nil {
		log.Info("违禁检测: 方式=RAG未命中", "list", "none", "score", 0.0, "hit", "", "user", ev.Message.UserID)
	} else {
		log.Info("违禁检测: 方式=RAG未达阈值", "list", "black", "score", v.black.score, "hit", headText(v.black.text, 20), "user", ev.Message.UserID)
	}
	rc := reviewCtx{text: text, word: word, wordCat: wordCat, card: card, highRisk: card, hard: card}
	if v.black != nil {
		rc.ragScore = &v.black.score
		rc.ragPhrase = v.black.text
		rc.ragCategory = v.black.category
	}
	if m.submitReview(ctx, ev, rc) {
		metrics.GroupMgrDetectionsTotal.WithLabelValues("rag", "review").Inc()
		return true
	}
	// LLM 不可用 → 关键词兜底
	metrics.GroupMgrDetectionsTotal.WithLabelValues("rag", "pass").Inc()
	return m.handleKeywordPath(ctx, ev, cfg, text, card, word, wordCat)
}

// handleKeywordPath 关键词兜底路径（仅 RAG 或 LLM 不可用时使用，= 旧插件行为）。
func (m *Manager) handleKeywordPath(ctx context.Context, ev adapter.Event, cfg *models.GroupMgrConfig,
	text string, card bool, word, wordCat string) bool {
	switch {
	case wordCat == "sensitive" || wordCat == "black" || card:
		// 检索追踪日志：方式=关键词兜底（高危词命中）
		log.Info("违禁检测: 方式=关键词", "kind", "high-risk", "word", headText(word, 20), "cat", wordCat, "card", card, "user", ev.Message.UserID)
		// 高危复核；LLM 不可用 → 直接处罚
		kind := "high-risk"
		if card && word == "" {
			kind = "card"
		}
		if m.submitReview(ctx, ev, reviewCtx{
			text: text, word: word, wordCat: wordCat, kind: kind, highRisk: true, hard: true, card: card,
		}) {
			metrics.GroupMgrDetectionsTotal.WithLabelValues("keyword", "review").Inc()
			return true
		}
		metrics.GroupMgrDetectionsTotal.WithLabelValues("keyword", "punish").Inc()
		m.punish(ctx, ev, reasonByWord(word, wordCat, card), categoryByWordOrCard(word, wordCat, card, "ad"), "keyword")
		return true
	case wordCat == "gray":
		// 检索追踪日志：方式=关键词兜底（灰色词命中）
		log.Info("违禁检测: 方式=关键词", "kind", "gray", "word", headText(word, 20), "cat", wordCat, "card", card, "user", ev.Message.UserID)
		// 常规审查；LLM 不可用 → 放行（异步追罚语义）
		if m.submitReview(ctx, ev, reviewCtx{
			text: text, word: word, wordCat: "gray", kind: "gray", highRisk: false, hard: false, card: card,
		}) {
			metrics.GroupMgrDetectionsTotal.WithLabelValues("keyword", "review").Inc()
		} else {
			metrics.GroupMgrDetectionsTotal.WithLabelValues("keyword", "pass").Inc()
		}
		return false
	default:
		metrics.GroupMgrDetectionsTotal.WithLabelValues("keyword", "pass").Inc()
		return false
	}
}

// categoryByWordOrCard 处罚分类：敏感词红线最高优先（即使同时命中卡片/黑词也不得降级为广告）；
// 卡片其次 → ad；黑/灰词 → ad；否则取样本分类（sensitive → sensitive，其余 ad）。
func categoryByWordOrCard(word, wordCat string, card bool, sampleCat string) string {
	if wordCat == "sensitive" {
		return "sensitive"
	}
	if card {
		return "ad"
	}
	switch wordCat {
	case "black", "gray":
		return "ad"
	}
	if sampleCat == "sensitive" {
		return "sensitive"
	}
	return "ad"
}

// reasonByWord 关键词兜底路径的违规理由文案（按命中类别/卡片拼装）。
func reasonByWord(word, wordCat string, card bool) string {
	switch {
	case card:
		return "广告违规：推荐群聊卡片"
	case wordCat == "sensitive":
		return "敏感违规：" + word
	case wordCat == "black":
		return "广告违规(黑名单)：" + word
	case wordCat == "gray":
		return "广告违规(灰色词)：" + word
	default:
		return "违规内容"
	}
}
