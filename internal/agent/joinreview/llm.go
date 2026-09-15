package joinreview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"JuanNiang-Neo/internal/agent/provider"
	"JuanNiang-Neo/internal/core/models"
)

// LLM 审核参数。
const (
	llmTimeout    = 60 * time.Second // 单批审核超时
	llmMaxComment = 300              // 单条留言送入 LLM 的最大长度（字符）
)

// defaultSysPrompt 内置默认审核提示词（面板可按群覆盖/追加）。
const defaultSysPrompt = `你是 QQ 群的入群审核员，负责逐条判定入群申请是否通过。
判定依据：申请留言的内容与意图，是否含广告、引流、营销、兼职、贷款等推广信息或其他违规内容。
口径：明确的推广/引流/违规申请一律拒绝；正常交流诉求一律通过；拿不准的拒绝并说明原因。
拒绝理由会作为拒绝说明发送给申请者，必须简短、友好、不带攻击性。`

// reviewResult 批量判定结果中单条申请的裁决。
type reviewResult struct {
	Index   int    `json:"index"`   // 对应送审块序号
	Verdict string `json:"verdict"` // approve（通过）/ reject（拒绝）
	Reason  string `json:"reason"`  // 一句话理由（拒绝时发送给申请者）
}

// reviewBatch LLM 批量判定输出。
type reviewBatch struct {
	Results []reviewResult `json:"results"`
}

// reviewResults 按 index 取裁决（供 flushGroup 逐条查找）。
type reviewResults map[int]reviewResult

// reviewBatch 整批送 LLM 逐条判定，返回 index → 裁决（仅含有效裁决；
// 索引越界 / verdict 非法 / 重复输出的条目剔除，由调用方按"留待人工"处理）。
func (m *Manager) reviewBatch(ctx context.Context, cfg *models.GroupJoinReviewConfig, groupID int64, items []*models.GroupJoinRequest) (reviewResults, error) {
	p := m.providers.SelectModel(provider.ModelTypeText)
	if p == nil {
		return nil, errors.New("无可用文本模型 Provider")
	}

	req := provider.ChatRequest{
		Messages: []provider.ChatMessage{
			{Role: "system", Content: sysPrompt(cfg, groupID)},
			{Role: "user", Content: batchUserPrompt(items)},
		},
	}
	// 派生自调用方 ctx（继承 trace 值），脱离事件处理的取消信号
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), llmTimeout)
	defer cancel()
	resp, err := p.Chat(cctx, req)
	if err != nil {
		return nil, err
	}
	if resp == nil || strings.TrimSpace(resp.Message.Content) == "" {
		return nil, errors.New("LLM 空响应")
	}

	// LLM 输出可能被 markdown 代码块包裹，需提取纯 JSON 再解析
	jsonStr := extractJSON(resp.Message.Content)
	var bat reviewBatch
	if jerr := json.Unmarshal([]byte(jsonStr), &bat); jerr != nil {
		return nil, fmt.Errorf("LLM 输出 JSON 解析失败: %w", jerr)
	}

	out := make(reviewResults, len(items))
	seen := make(map[int]bool, len(items))
	for _, r := range bat.Results {
		if r.Index < 0 || r.Index >= len(items) || seen[r.Index] {
			log.Warn("加群审核裁决索引非法，忽略", "index", r.Index, "batch_size", len(items))
			continue
		}
		if r.Verdict != "approve" && r.Verdict != "reject" {
			log.Warn("加群审核裁决 verdict 非法，忽略", "index", r.Index, "verdict", r.Verdict)
			continue
		}
		if strings.TrimSpace(r.Reason) == "" {
			r.Reason = "AI 判定" + map[string]string{"approve": "通过", "reject": "拒绝"}[r.Verdict]
		}
		seen[r.Index] = true
		out[r.Index] = r
	}
	return out, nil
}

// sysPrompt 审核系统提示词 = 内置默认 + 该群自定义提示词（管理员面板设置，优先级更高）。
func sysPrompt(cfg *models.GroupJoinReviewConfig, groupID int64) string {
	var sb strings.Builder
	sb.WriteString(defaultSysPrompt)
	if cfg != nil && len(cfg.Prompts) > 0 {
		if p := strings.TrimSpace(cfg.Prompts[groupPromptKey(groupID)]); p != "" {
			sb.WriteString("\n\n本群补充审核口径（管理员设置，与默认规则冲突时以本段为准）：\n")
			sb.WriteString(p)
		}
	}
	return sb.String()
}

// batchUserPrompt 组装批量送审：每条申请独立 <JOIN_REQUEST> 块 + 序号，逐条判定互不串扰。
// 末尾固定给出输出格式契约（代码解析依赖此契约，不依赖提示词是否被修改）。
func batchUserPrompt(items []*models.GroupJoinRequest) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "以下是本群 %d 条待审核的入群申请，请逐条判定：\n\n", len(items))
	for i, it := range items {
		comment := strings.TrimSpace(it.Comment)
		if comment == "" {
			comment = "（无留言）"
		}
		if r := []rune(comment); len(r) > llmMaxComment {
			comment = string(r[:llmMaxComment])
		}
		fmt.Fprintf(&sb, "<JOIN_REQUEST index=%d>\nQQ: %d\n留言: %s\n</JOIN_REQUEST>\n", i, it.UserID, comment)
	}
	sb.WriteString("\n请严格按以下 JSON 格式逐条输出判定结果（index 必须与上方 <JOIN_REQUEST> 的 index 对应）：\n")
	sb.WriteString(`{"results":[{"index":0,"verdict":"approve|reject","reason":"一句话理由"}` + "]}\n")
	sb.WriteString("verdict 取值：approve=通过 / reject=拒绝；reason 为一句话理由（拒绝时会发送给申请者，请友好）。只输出 JSON，不要输出任何其它文字。")
	return sb.String()
}

// extractJSON 从 LLM 输出中提取纯 JSON 文本。
// 处理 markdown 代码块包裹（```json ... ```）、前后多余解释性文本（与 groupmgr 同款容错）。
func extractJSON(raw string) string {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "{") {
		return s
	}
	if idx := strings.Index(s, "```"); idx >= 0 {
		after := s[idx+3:]
		if nl := strings.IndexByte(after, '\n'); nl >= 0 {
			lang := strings.TrimSpace(after[:nl])
			if lang == "json" || lang == "JSON" || lang == "" {
				after = after[nl+1:]
			}
		}
		if end := strings.Index(after, "```"); end >= 0 {
			return strings.TrimSpace(after[:end])
		}
		return strings.TrimSpace(after)
	}
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}
