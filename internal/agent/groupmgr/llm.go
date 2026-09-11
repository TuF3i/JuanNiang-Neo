package groupmgr

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"JuanNiang-Neo/internal/adapter"
	"JuanNiang-Neo/internal/agent/provider"
	"JuanNiang-Neo/internal/metrics"
	"JuanNiang-Neo/internal/otelx"

	"go.opentelemetry.io/otel/attribute"
)

// LLM 审查参数。
const (
	llmTimeout     = 90 * time.Second // 批量审查超时（批内多条，放宽）
	llmMaxText     = 600              // 单条送入 LLM 的最大长度（字符）
	llmDedupWindow = 600              // 同一条消息 10 分钟内不重复审查（秒）
	llmBatchMax    = 20               // 批队列上限：到点（LLMBatchWindow 秒）或满批先到先提交
	llmQueueSize   = 256              // 审查回调队列容量
	llmPhraseLimit = 2000             // 黑/白语录自学习上限（超出拒绝写入，防 LLM 误判无限扩库）
)

// reviewCtx 一次待审消息的现场上下文（入批窗口）。
type reviewCtx struct {
	text      string   // 送审文本（检测侧传入，非空）
	word      string   // 命中关键词（兜底路径用）
	wordCat   string   // black/gray/sensitive/空
	card      bool     // 推荐卡片硬信号
	kind      string   // gray / high-risk / card（关键词兜底路径用）
	highRisk  bool     // 高危复核（LLM 异常回退直罚）
	hard      bool     // 有硬信号（词/卡片），LLM 异常时的直罚依据
	ragScore  *float64 // RAG 最高分（参考展示）
	ragPhrase string   // RAG 最相似语录（参考展示）
	// ragCategory RAG 命中样本的违规类型（ad/sensitive）：LLM 追罚分类用。
	// RAG 语义命中的类别比关键词预查更可靠（无词命中时 wordCat 为空，
	// 仅靠 wordCat 会把敏感语义内容误判为广告）。
	ragCategory string
}

// reviewItem 批窗口内的一条待审消息。
type reviewItem struct {
	groupID, userID, messageID int64
	admins                     []string
	pk                         string // "群:QQ"
	rc                         reviewCtx
	rawText                    string // 送审原文（学习闭环入库用）
	// senderCard / senderNickname 处罚时记录用户身份（LLM 异步追罚时原始 ev 不在场，
	// 违规记录 Username 依赖群名片/昵称，必须随入批快照保存）
	senderCard     string
	senderNickname string
}

// reviewResult 批量判定结果中单条消息的裁决。
type reviewResult struct {
	Index   int    `json:"index"`   // 对应送审块序号
	Verdict string `json:"verdict"` // black（违规处罚）/ none（放行）
	// Category 违规类型（verdict=black 时输出）：ad（广告引流）/ sensitive（敏感违禁）。
	// 非法/缺失值在裁决应用时兜底 ad；none 时忽略。
	Category string `json:"category"`
	Reason   string `json:"reason"`
}

// reviewBatch LLM 批量判定输出。
type reviewBatch struct {
	Results []reviewResult `json:"results"`
}

// reviewOutcome 批量审查结果（channel 消息，Run 串行消费）。
// results 与 items 通过 Index 关联（缺失的条目标记为未裁决）。
type reviewOutcome struct {
	items   []reviewItem
	results []reviewResult
	content string // LLM 原始输出（日志）
	tokens  int
	err     error
}

// submitReview 提交异步 LLM 审查：入 3s 批窗口（到点或满 LLMBatchMax 条统一提交），
// 批内每条独立判定，不阻塞主循环。返回 true = 已入批（等待批裁决）。
// false = LLM 未启用/无 Provider（调用方按兜底语义处理）。
func (m *Manager) submitReview(ctx context.Context, ev adapter.Event, rc reviewCtx) bool {
	cfg := m.getCfg(ctx)
	if !cfg.LLMReview {
		log.Info("LLM 审核未启用", "user", ev.Message.UserID)
		return false
	}
	p := m.providers.SelectModel(provider.ModelTypeText)
	if p == nil {
		log.Warn("无可用文本模型 Provider，LLM 审核跳过", "user", ev.Message.UserID)
		return false
	}
	msg := ev.Message
	pk := pkOf(msg.GroupID, msg.UserID)

	// 同用户已有在途 → 跳过（调用方按兜底处理）
	m.llmMu.Lock()
	if m.llmPending[pk] {
		m.llmMu.Unlock()
		return false
	}
	// 同消息 10min 去重
	now := time.Now().Unix()
	if msg.MessageID > 0 {
		if ts, ok := m.llmReviewed[msg.MessageID]; ok && now-ts < llmDedupWindow {
			m.llmMu.Unlock()
			return false
		}
		m.llmReviewed[msg.MessageID] = now
		for id, ts := range m.llmReviewed { // 顺带清理过期（终态记录同步清理）
			if now-ts >= llmDedupWindow {
				delete(m.llmReviewed, id)
				delete(m.reviewVerdict, id)
			}
		}
	}
	m.llmPending[pk] = true
	m.llmMu.Unlock()

	// 送审文本：剥离 CQ 码截断；RAG 命中语录文本仅作参考展示
	text := strings.TrimSpace(stripCQ(msg.RawMessage))
	if text == "" {
		text = rc.text
	}
	text = strings.Join(strings.Fields(text), " ")
	if len([]rune(text)) > llmMaxText {
		text = string([]rune(text)[:llmMaxText])
	}

	item := reviewItem{
		groupID: msg.GroupID, userID: msg.UserID, messageID: msg.MessageID,
		admins: ev.Admins, pk: pk, rc: rc, rawText: text,
		senderCard: msg.Sender.Card, senderNickname: msg.Sender.Nickname,
	}

	// 检索追踪日志：方式=LLM 送审（入批窗口，等待批裁决）
	log.Info("违禁检测: 方式=LLM送审", "msg", headText(text, 20), "user", msg.UserID)

	// 入批窗口
	m.llmBatchMu.Lock()
	m.llmBatchItems = append(m.llmBatchItems, item)
	first := len(m.llmBatchItems) == 1
	full := len(m.llmBatchItems) >= llmBatchMax
	m.llmBatchMu.Unlock()

	if first {
		window := 3
		if cfg.LLMBatchWindow > 0 {
			window = cfg.LLMBatchWindow
		}
		time.AfterFunc(time.Duration(window)*time.Second, func() { m.flushBatch(ctx) })
	}
	if full {
		// 满批也异步：此前同步调用 flushBatch 会阻塞消息处理主循环至 LLM 返回（最长 90s）
		go m.flushBatch(ctx)
	}
	return true
}

// flushBatch 取走整批并提交 LLM（仅由 AfterFunc 回调 / go 触发，异步执行不阻塞消息处理；
// 结果经 channel 由 Run 串行消费）。派生调用方 ctx（继承 trace），但不被消息返回取消。
func (m *Manager) flushBatch(ctx context.Context) {
	defer func() {
		// 防御：批处理内 panic 不波及调用方 goroutine（AfterFunc 线程 / submitReview 的 go）
		if r := recover(); r != nil {
			log.Error("flushBatch panic", "recover", r)
		}
	}()
	m.llmBatchMu.Lock()
	if len(m.llmBatchItems) == 0 {
		m.llmBatchMu.Unlock()
		return
	}
	items := m.llmBatchItems
	m.llmBatchItems = nil
	m.llmBatchMu.Unlock()

	p := m.providers.SelectModel(provider.ModelTypeText)
	if p == nil {
		// Provider 中途不可用：整批按失败处理（调用方逐条兜底）
		m.llmResults <- reviewOutcome{items: items, err: errors.New("no provider")}
		return
	}

	// 提示词装配：统一检测提示词（面板 LLMPrompt，默认内置）
	sysPrompt := m.batchSysPrompt(ctx)
	userPrompt := m.batchUserPrompt(items)
	req := provider.ChatRequest{
		Messages: []provider.ChatMessage{
			{Role: "system", Content: sysPrompt},
			{Role: "user", Content: userPrompt},
		},
	}

	// 派生自调用方 ctx（继承 trace/取消信号），批量 LLM 请求超时窗口 llmTimeout
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), llmTimeout)
	defer cancel()
	resp, err := p.Chat(cctx, req)
	out := reviewOutcome{items: items, err: err}
	if resp != nil {
		out.content = resp.Message.Content
		out.tokens = resp.TokenUsage
		if err == nil {
			// LLM 输出可能被 markdown 代码块包裹（```json ... ```），
			// 需提取纯 JSON 再解析，否则 json.Unmarshal 静默失败导致学习闭环失效
			jsonStr := extractJSON(resp.Message.Content)
			var bat reviewBatch
			if jerr := json.Unmarshal([]byte(jsonStr), &bat); jerr == nil {
				out.results = bat.Results
			} else {
				log.Warn("LLM 批量裁决 JSON 解析失败", "err", jerr, "raw_len", len(resp.Message.Content), "raw_head", truncateLog(resp.Message.Content, 200))
			}
		}
	}
	m.llmResults <- out
}

// batchSysPrompt 统一检测提示词（LLMPrompt 面板可编辑，默认内置）。
func (m *Manager) batchSysPrompt(ctx context.Context) string {
	cfg := m.getCfg(ctx)
	if cfg != nil && strings.TrimSpace(cfg.LLMPrompt) != "" {
		return cfg.LLMPrompt
	}
	return llmPhrasePrompt
}

// batchUserPrompt 组装批量送审：每条消息独立 <USER_TEXT> 块 + 序号，逐条判定互不串扰。
// 末尾固定给出角色 + 输出格式契约（代码解析依赖此契约，不依赖外部提示词是否被修改）。
func (m *Manager) batchUserPrompt(items []reviewItem) string {
	var sb strings.Builder
	sb.WriteString("以下是 " + strconv.Itoa(len(items)) + " 条待判定群消息，请逐条输出判定：\n\n")
	for i, it := range items {
		sb.WriteString("<USER_TEXT index=" + strconv.Itoa(i) + ">")
		sb.WriteString(it.rawText)
		sb.WriteString("</USER_TEXT>\n")
	}
	if len(items) > 0 && items[0].rc.ragScore != nil {
		sb.WriteString("\n（提示：部分消息 RAG 语义相似度见各自的 rag_score 字段，仅作参考）")
	}
	sb.WriteString("\n\n你是群聊内容审查员。请严格按以下 JSON 格式逐条输出（每条消息一个结果，index 必须与上方 <USER_TEXT> 的 index 对应）：\n")
	sb.WriteString("{\"results\":[{\"index\":0,\"verdict\":\"black|none\",\"category\":\"ad|sensitive\",\"reason\":\"一句话说明理由\"}]}\n")
	sb.WriteString("verdict 取值：black=违规处罚 / none=放行；verdict=black 时必须给出 category（ad=广告引流 / sensitive=敏感违禁），verdict=none 时 category 输出 \"none\"。只输出 JSON，不要输出任何其它文字。")
	return sb.String()
}

// Run 启动后台循环：串行消费批量审查结果 + 管理员通知队列 pump。
func (m *Manager) Run(ctx context.Context) {
	log.Info("群管理后台循环已启动")
	for {
		select {
		case <-ctx.Done():
			log.Info("群管理后台循环已停止")
			return
		case out := <-m.llmResults:
			m.handleReviewBatch(ctx, out)
		}
	}
}

// handleReviewBatch 处理批量审查结果（Run 内串行执行，天然互斥）。
// 批内每条按 Index 关联裁决，缺失/非法按失败降级（fail-closed：硬信号直罚，否则放行）。
func (m *Manager) handleReviewBatch(ctx context.Context, out reviewOutcome) {
	if out.tokens > 0 {
		metrics.LLMTokensTotal.WithLabelValues("review").Add(float64(out.tokens))
	}
	var verdicts = make(map[int]reviewResult)
	if out.err == nil {
		seen := make(map[int]bool, len(out.results))
		for _, r := range out.results {
			// 校验 Index 位于当前批次范围内且不重复；越界/重复按未裁决处理（fail-closed）
			if r.Index < 0 || r.Index >= len(out.items) || seen[r.Index] {
				log.Warn("LLM 批量裁决索引非法，忽略", "index", r.Index, "batch_size", len(out.items))
				continue
			}
			seen[r.Index] = true
			// 旧提示词/模型漂移可能残留 white 输出：按 none 放行（白名单体系已剔除）
			if r.Verdict == "white" {
				r.Verdict = "none"
			}
			verdicts[r.Index] = r
		}
	}
	for i, it := range out.items {
		res, ok := verdicts[i]
		m.applyVerdict(ctx, it, res, !ok || out.err != nil)
	}
}

// applyVerdict 批内单条裁决应用：处罚 / 放行 / 学习闭环（异步，不阻塞）。
func (m *Manager) applyVerdict(ctx context.Context, it reviewItem, res reviewResult, failed bool) {
	// 审查耗时期间可能已被加入白名单/成为管理员：先复查豁免再写终态。
	// 已豁免用户必须落放行终态（none），否则 ReviewGate 会因 black 丢弃 Agent 回复。
	exempted := m.isWhitelisted(ctx, it.userID) || m.isGroupAdmin(it.userID, it.admins, it.groupID)

	m.llmMu.Lock()
	delete(m.llmPending, it.pk)
	// 审核终态落库（发送前闸门 ReviewGate 查询；TTL 随 llmReviewed 清理）：
	// black=已判违规（Agent 回复应被丢弃）；none=放行；
	// 失败且无硬信号=不记录（按未送审放行）；失败且硬信号直罚=记 black。
	switch {
	case exempted:
		// 已豁免：写放行终态，不落 black（处罚下方同样跳过）
		m.reviewVerdict[it.messageID] = "none"
	case failed || (res.Verdict != "black" && res.Verdict != "none"):
		if it.rc.highRisk && it.rc.hard {
			m.reviewVerdict[it.messageID] = "black"
		}
	case res.Verdict == "black":
		m.reviewVerdict[it.messageID] = "black"
	default:
		m.reviewVerdict[it.messageID] = res.Verdict // none
	}
	m.llmMu.Unlock()

	if exempted {
		return
	}

	ev := adapter.Event{
		PostType: "message",
		Admins:   it.admins,
		Message: &adapter.MessageEvent{
			MessageType: "group",
			GroupID:     it.groupID,
			UserID:      it.userID,
			MessageID:   it.messageID,
			RawMessage:  it.rawText, // 学习闭环兜底用（learnPhraseAsync 第二选择）
		},
	}
	// 违规记录 Username：入批时快照的群名片/昵称（LLM 异步追罚路径原始 ev 不在场）
	ev.Message.Sender.UserID = it.userID
	ev.Message.Sender.Card = it.senderCard
	ev.Message.Sender.Nickname = it.senderNickname
	rc := it.rc

	// LLM 请求失败 / 该条裁决缺失或非法 → fail-closed：硬信号直罚，否则放行
	if failed || (res.Verdict != "black" && res.Verdict != "none") {
		if rc.highRisk && rc.hard {
			// 高危回退直罚（敏感/黑词/卡片）
			metrics.GroupMgrLLMReviewsTotal.WithLabelValues("error").Inc()
			// 检索追踪日志：方式=LLM失败（高危硬信号直罚兜底）
			log.Info("违禁检测: 方式=LLM失败直罚", "msg", headText(it.rawText, 20), "user", it.userID)
			m.punish(ctx, ev, reasonByWord(rc.word, rc.wordCat, rc.card), categoryByWordOrCard(rc.word, rc.wordCat, rc.card, "ad"), "llm")
			return
		}
		if !failed {
			log.Warn("LLM 批量裁决非法，按失败处理", "content", res.Verdict)
		}
		metrics.GroupMgrLLMReviewsTotal.WithLabelValues("error").Inc()
		// 检索追踪日志：方式=LLM失败放行（无硬信号，宁放勿杀）
		log.Info("违禁检测: 方式=LLM失败放行", "msg", headText(it.rawText, 20), "user", it.userID)
		log.Info("LLM 审查失败，放行", "user", it.userID)
		return
	}

	switch res.Verdict {
	case "black":
		metrics.GroupMgrLLMReviewsTotal.WithLabelValues("black").Inc()
		// 处罚/学习入库类型优先级：LLM 判定类型（最直接）> RAG 命中样本类别 > 关键词类别 > ad
		category := violationCategory(res.Category, rc)
		reason := res.Reason
		if reason == "" {
			reason = reasonByWord(rc.word, rc.wordCat, rc.card)
		}
		// 检索追踪日志：方式=LLM + 判定结果 + 消息前 20 字
		log.Info("违禁检测: 方式=LLM", "verdict", "black", "category", category, "msg", headText(it.rawText, 20), "reason", reason, "user", it.userID)
		m.punish(ctx, ev, reason, category, "llm")
		// 学习闭环：异步写入对应类型的黑名单语录（不阻塞）
		m.learnPhraseAsync(ctx, it.rawText, "black", category, ev)
	default: // none
		metrics.GroupMgrLLMReviewsTotal.WithLabelValues("none").Inc()
		log.Info("违禁检测: 方式=LLM", "verdict", "none", "msg", headText(it.rawText, 20), "reason", res.Reason, "user", it.userID)
		log.Info("LLM 判定放行", "user", it.userID, "reason", res.Reason)
	}
}

// violationCategory LLM 判黑的处罚/学习类型：
// LLM 输出 category（ad/sensitive）优先，未输出或非法时回退 RAG 命中样本类别、关键词类别，兜底 ad。
func violationCategory(llmCategory string, rc reviewCtx) string {
	switch llmCategory {
	case "ad", "sensitive":
		return llmCategory
	}
	if rc.ragCategory == "sensitive" || rc.wordCat == "sensitive" {
		return "sensitive"
	}
	return "ad"
}

// learnPhraseAsync 学习闭环：LLM 判黑/白的消息异步写入对应语录（Postgres + RAG）。
// 幂等去重（text 唯一）+ 语录上限（防 LLM 误判无限扩库）；RAG 未配置静默跳过。
func (m *Manager) learnPhraseAsync(ctx context.Context, raw, listType, category string, ev adapter.Event) {
	text := strings.TrimSpace(stripCQ(raw))
	if text == "" {
		text = strings.TrimSpace(stripCQ(ev.Message.RawMessage))
	}
	text = strings.Join(strings.Fields(text), " ")
	if text == "" || len([]rune(text)) > 200 {
		return
	}
	// 上限检查（快速失败，避免无效异步写入；串行区内仍会锁后复查，防并发越过上限）
	if n, err := m.dao.SampleCountByList(ctx, listType); err == nil && n >= llmPhraseLimit {
		log.Warn("语录已达上限，拒绝自学习写入", "list", listType, "limit", llmPhraseLimit)
		return
	}
	go func() {
		// 学习写入串行化：并发双插会绕过幂等去重（Text 无唯一索引）
		m.learnMu.Lock()
		defer m.learnMu.Unlock()
		// 派生自调用方 ctx（继承 trace 值），消息处理返回取消不中断 30s 学习窗口
		lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		// 锁后复查上限：预检在锁外快照，并发裁决可能全部通过；串行区内再校验防少量越过
		if n, err := m.dao.SampleCountByList(lctx, listType); err == nil && n >= llmPhraseLimit {
			log.Warn("语录已达上限，拒绝自学习写入", "list", listType, "limit", llmPhraseLimit)
			return
		}
		if id, err := m.dao.SampleAddPhrase(lctx, text, category, "learn", listType); err != nil {
			log.Warn("学习语录入库失败", "err", err)
			return
		} else if synced, uerr := m.upsertRAGPhrase(lctx, id, text, listType); uerr != nil {
			log.Warn("学习语录写入 RAG 失败", "err", uerr)
			_ = m.dao.SampleMarkRAGSynced(lctx, id, false)
			return
		} else if synced {
			_ = m.dao.SampleMarkRAGSynced(lctx, id, true)
		}
		m.invalidateSampleSet()
		log.Info("学习语录已入库", "list", listType, "text", text)
	}()
}

// extractJSON 从 LLM 输出中提取纯 JSON 文本。
// 处理 markdown 代码块包裹（```json ... ``` 或 ``` ... ```）、前后多余文本。
// DeepSeek/GPT/Claude 等模型即使提示词要求"只输出 JSON"，仍经常用代码块包裹。
func extractJSON(raw string) string {
	s := strings.TrimSpace(raw)
	// 1. 尝试整体解析（最理想情况：纯 JSON）
	if strings.HasPrefix(s, "{") {
		return s
	}
	// 2. 提取 markdown 代码块内容（```json\n...\n``` 或 ```\n...\n```）
	if idx := strings.Index(s, "```"); idx >= 0 {
		after := s[idx+3:]
		// 跳过语言标识（json/JSON 等）
		if nl := strings.IndexByte(after, '\n'); nl >= 0 {
			lang := strings.TrimSpace(after[:nl])
			if lang == "json" || lang == "JSON" || lang == "" {
				after = after[nl+1:]
			}
		}
		// 找结束 ```
		if end := strings.Index(after, "```"); end >= 0 {
			return strings.TrimSpace(after[:end])
		}
		// 没有结束标记，取到末尾
		return strings.TrimSpace(after)
	}
	// 3. 兜底：找第一个 { 到最后一个 }（处理前后有解释性文本的情况）
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}

// truncateLog 截断字符串用于日志展示（防止超长日志刷屏）。
func truncateLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...(truncated)"
}

// headText 截取文本前 n 个字符（rune-safe，中文不会切坏）。
func headText(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// pkOf 群消息在途审查去重键（"群:QQ"）。
func pkOf(groupID, userID int64) string {
	return itoa(groupID) + ":" + itoa(userID)
}

// ReviewGateWait Agent 回复发送前等待审核终态的上限：覆盖批窗口（3s）+ LLM
// 余量；超时按放行处理（撤回兜底仍在）。Agent ReAct 循环通常比审核慢，
// 多数情况零等待。
const ReviewGateWait = 5 * time.Second

// ReviewGate 发送前审核闸门（Agent 回复发送前调用）。
// 返回 blocked：消息已被判定违规（已处罚），Agent 回复不应发送；
// pending：审核仍在途（批窗口/LLM 判断中），调用方可用 WaitReview 等待。
// 私聊 / 未送审 / 无记录一律放行（LLMReview 关闭、去重命中、重启丢失等）。
func (m *Manager) ReviewGate(ctx context.Context, groupID, userID, messageID int64) (blocked, pending bool) {
	if messageID <= 0 || groupID <= 0 {
		return false, false
	}
	m.llmMu.Lock()
	defer m.llmMu.Unlock()
	if v, ok := m.reviewVerdict[messageID]; ok {
		return v == "black", false
	}
	// 在途审查（批窗口/LLM 判断中）
	if m.llmPending[pkOf(groupID, userID)] {
		return false, true
	}
	return false, false
}

// WaitReview 等待审核终态（轮询 ReviewGate，短间隔），最多等 timeout。
// 返回 blocked=true 应丢弃回复；false 表示放行（含超时/ctx 取消/未送审）。
func (m *Manager) WaitReview(ctx context.Context, groupID, userID, messageID int64, timeout time.Duration) (blocked bool) {
	// 链路追踪：审核闸门 span（发送前等待审核终态，记录等待耗时与结果）
	start := time.Now()
	ctx, span := otelx.Span(ctx, "groupmgr.review_gate",
		attribute.Int64("group_id", groupID),
		attribute.Int64("message_id", messageID),
	)
	defer func() {
		span.SetAttributes(
			attribute.Bool("blocked", blocked),
			attribute.Int64("wait_ms", time.Since(start).Milliseconds()),
		)
		span.End()
	}()
	deadline := time.Now().Add(timeout)
	for {
		blocked, pending := m.ReviewGate(ctx, groupID, userID, messageID)
		if blocked || !pending {
			return blocked
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(50 * time.Millisecond):
		}
	}
}
