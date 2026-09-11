package groupmgr

import (
	"context"
	"strconv"
	"strings"
)

// TestReport 链路测试报告（Web 面板粘贴文本 → 判定流水）。
type TestReport struct {
	Text        string  `json:"text"`
	Card        bool    `json:"card"`         // 推荐卡片命中
	Word        string  `json:"word"`         // 命中关键词（仅兜底路径展示）
	WordCat     string  `json:"word_cat"`     // black/gray/sensitive/空
	RAGOK       bool    `json:"rag_ok"`       // RAG 路径是否可用
	BlackScore  float64 `json:"black_score"`  // 黑名单语录最高分
	BlackPhrase string  `json:"black_phrase"` // 黑名单最相似语录
	WhiteScore  float64 `json:"white_score"`  // 白名单语录最高分
	WhitePhrase string  `json:"white_phrase"` // 白名单最相似语录
	Verdict     string  `json:"verdict"`      // 最终判定：punish / review / pass
	Reason      string  `json:"reason"`       // 判定说明
}

// TestViolation 链路测试：不处罚、不写库，仅返回判定流水（供 Web 面板诊断）。
func (m *Manager) TestViolation(ctx context.Context, text string) *TestReport {
	cfg := m.getCfg(ctx)
	text = strings.TrimSpace(text)
	rep := &TestReport{Text: text, RAGOK: false}

	card := detectGroupCard(text)
	rep.Card = card
	// 卡片文本化：card-only 输入剥离 CQ 后为空，与 detectViolation 主链路保持一致
	query := strings.TrimSpace(stripCQ(text))
	if query == "" && card {
		query = cardText(text)
	}
	rep.Word, rep.WordCat = m.wordHit(ctx, query)

	// observe=false：链路测试不观测生产指标（RAGSearchLatency/GroupMgrRAGScore/RAGSearchErrorsTotal）
	if v := m.verifyByRAG(ctx, query, false); v.ok {
		rep.RAGOK = true
		if v.black != nil {
			rep.BlackScore = v.black.score
			rep.BlackPhrase = v.black.text
		}
		if v.white != nil {
			rep.WhiteScore = v.white.score
			rep.WhitePhrase = v.white.text
		}
		// 黑名单命中（≥ BlackMinScore）→ 处罚
		if v.black != nil && v.black.score >= cfg.BlackMinScore {
			rep.Verdict = "punish"
			rep.Reason = "RAG 黑名单命中（分数 " + fscore(v.black.score) + " ≥ " + fscore(cfg.BlackMinScore) + "）→ 直接处罚"
			return rep
		}
		// 白名单命中（≥ WhiteMinScore）→ 放行
		if v.white != nil && v.white.score >= cfg.WhiteMinScore {
			rep.Verdict = "pass"
			rep.Reason = "RAG 白名单命中（分数 " + fscore(v.white.score) + " ≥ " + fscore(cfg.WhiteMinScore) + "）→ 放行"
			return rep
		}
		// 均未达到阈值 → LLM 统一判定（批窗口）
		rep.Verdict = "review"
		rep.Reason = "未命中黑白名单 → LLM 统一判定（3s 批窗口，逐条独立）"
		return rep
	}

	// RAG 不可用 → 先 LLM 判定；LLM 也不可用 → 关键词兜底（仅两者均失败）
	switch {
	case rep.WordCat == "sensitive" || rep.WordCat == "black" || card:
		rep.Verdict = "review"
		rep.Reason = "RAG 不可用 → LLM 统一判定；LLM 也不可用时关键词兜底（高危复核，LLM 挂则直罚）"
	case rep.WordCat == "gray":
		rep.Verdict = "review"
		rep.Reason = "RAG 不可用 → LLM 统一判定；LLM 也不可用时灰色词放行"
	default:
		rep.Verdict = "pass"
		rep.Reason = "RAG 不可用且无关键词命中 → 放行"
	}
	return rep
}

// fscore 分数格式化（两位小数，避免 0.6999999 显示）。
func fscore(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}
