// Package metrics Prometheus 监控指标：/metrics 端点 + 全模块挂点。
//
// 设计约束：
//   - 标签只保留低基数字段（message_type/provider ID/类别等），
//     禁止 group_id/user_id 等高基数标签（时间序列爆炸）；
//   - Counter/Histogram 在事件发生点原子埋点（热路径 <1μs）；
//   - Gauge 由 runtimeCollector 在 scrape 时从注入的回调实时读取，
//     DB 类数据由调用方用 CachedInt 包一层 TTL 缓存（scrape 不打 DB）。
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// registry 独立注册表（不污染 prometheus 默认注册表，测试可重建）。
var registry = prometheus.NewRegistry()

func init() {
	// Go 运行时指标（goroutine/内存/GC）+ 进程指标（CPU/文件句柄）
	registry.MustRegister(collectors.NewGoCollector())
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
}

// MustRegister 注册自定义 collector（panic 视为程序错误）。
func MustRegister(c prometheus.Collector) { registry.MustRegister(c) }

// Register 注册 collector，已存在/冲突返回错误（插件指标等动态注册场景用，
// 避免 MustRegister panic 导致插件加载失败）。
func Register(c prometheus.Collector) error { return registry.Register(c) }

// Gatherer 返回注册表（/metrics handler 与测试读取）。
func Gatherer() prometheus.Gatherer { return registry }

// ---------- 消息事件流 ----------

var (
	// EventsTotal 收到的事件数（post_type: message/notice/request/cronjob/webhook）。
	EventsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "juanniang_events_total",
		Help: "OneBot11 事件到达总数",
	}, []string{"post_type"})

	// MessagesTotal 群/私聊消息数。
	MessagesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "juanniang_messages_total",
		Help: "消息总数",
	}, []string{"message_type"})

	// DedupDroppedTotal 幂等去重丢弃数（WS 断线重连重复推送监控）。
	DedupDroppedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "juanniang_message_dedup_dropped_total",
		Help: "消息幂等去重丢弃总数",
	})

	// BlockedTotal 黑名单拦截数。
	BlockedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "juanniang_message_blocked_total",
		Help: "消息被拦截总数",
	}, []string{"reason"})

	// DroppedTotal 消息被丢弃数（reason: irrelevant 相关性丢弃 / silenced 群聊静默 / flood 刷屏降级）。
	DroppedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "juanniang_message_dropped_total",
		Help: "消息被丢弃总数",
	}, []string{"reason"})

	// ChatRepliesTotal Agent 回复发送数（message_type 维度；每群细分统计走 Loki，
	// 遵守「禁止 group_id/user_id 高基数标签」约束）。
	ChatRepliesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "juanniang_chat_replies_total",
		Help: "Agent 回复发送总数",
	}, []string{"message_type"})

	// ChatStatsDroppedTotal 群统计事件被丢弃数（Loki 通道队列满/写失败；主流程不受影响）。
	ChatStatsDroppedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "juanniang_chat_stats_dropped_total",
		Help: "群消息/回复统计事件丢弃总数",
	}, []string{"direction"})
)

// ---------- Agent 循环与并发 ----------

var (
	// AgentLoopsTotal ReAct 循环完成结果（outcome: ok/error/timeout）。
	AgentLoopsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "juanniang_agent_loops_total",
		Help: "Agent ReAct 循环完成总数",
	}, []string{"outcome"})

	// AgentLoopDuration 单轮 ReAct 循环耗时（1s~5min 长尾，桶偏大）。
	AgentLoopDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "juanniang_agent_loop_duration_seconds",
		Help:    "Agent ReAct 循环耗时",
		Buckets: []float64{1, 2, 5, 10, 20, 30, 60, 120, 180, 300},
	})

	// ConcurrencyWaitsTotal 全局并发令牌等待次数（result: acquired 拿到令牌 / timeout 等待超时直派发）。
	ConcurrencyWaitsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "juanniang_agent_concurrency_waits_total",
		Help: "Agent 并发令牌等待总数",
	}, []string{"result"})

	// ConcurrencyWaitDuration 令牌等待耗时。
	ConcurrencyWaitDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "juanniang_agent_concurrency_wait_seconds",
		Help:    "Agent 并发令牌等待耗时",
		Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30},
	})
)

// ---------- LLM 用量 ----------

var (
	// LLMRequestsTotal LLM 调用次数（provider: Provider ID 低基数；result: ok/error）。
	LLMRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "juanniang_llm_requests_total",
		Help: "LLM 调用总数",
	}, []string{"provider", "result"})

	// LLMTokensTotal Token 消耗按用途（phase: agent 对话循环 / review 群管理审核 / relevance 相关性判断）。
	LLMTokensTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "juanniang_llm_tokens_total",
		Help: "LLM Token 消耗总数",
	}, []string{"phase"})

	// LLMLatency LLM 响应延迟。
	LLMLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "juanniang_llm_latency_seconds",
		Help:    "LLM 调用耗时",
		Buckets: prometheus.DefBuckets,
	})
)

// ---------- 群管理 ----------

var (
	// GroupMgrViolationsTotal 三级惩罚动作（category: ad/sensitive；action: warn/mute/kick）。
	GroupMgrViolationsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "juanniang_groupmgr_violations_total",
		Help: "群管理处罚动作总数",
	}, []string{"category", "action"})

	// GroupMgrDetectionsTotal 违禁判定流水（path: rag/keyword；verdict: punish/review/pass）。
	GroupMgrDetectionsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "juanniang_groupmgr_detections_total",
		Help: "群管理违禁判定流水总数",
	}, []string{"path", "verdict"})

	// GroupMgrRAGScore RAG 核实分数分布（调阈值依据）。
	GroupMgrRAGScore = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "juanniang_groupmgr_rag_score",
		Help:    "群管理 RAG 语义核实分数分布",
		Buckets: []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.75, 0.8, 0.9, 1.0},
	})

	// GroupMgrLLMReviewsTotal LLM 审核结果（result: black/white/none/error）。
	// error = LLM 请求失败/裁决非法（无硬信号放行，有硬信号 fail-closed 直罚）。
	GroupMgrLLMReviewsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "juanniang_groupmgr_llm_reviews_total",
		Help: "群管理 LLM 审核结果总数",
	}, []string{"result"})

	// GroupMgrSpamTotal 刷屏/复读触发（type: image/copy）。
	GroupMgrSpamTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "juanniang_groupmgr_spam_total",
		Help: "群管理刷屏/复读触发总数",
	}, []string{"type"})
)

// ---------- 外部服务 ----------

var (
	// RAGSearchLatency RAG 检索耗时。
	RAGSearchLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "juanniang_rag_search_latency_seconds",
		Help:    "RAG 语义检索耗时",
		Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1},
	})

	// RAGSearchErrorsTotal RAG 检索失败数（降级监控）。
	RAGSearchErrorsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "juanniang_rag_search_errors_total",
		Help: "RAG 语义检索失败总数",
	})
)

// ---------- 插件 ----------

var (
	// PluginHookErrorsTotal 插件回调错误（plugin: 目录名；hook: on_message/on_notice/...）。
	PluginHookErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "juanniang_plugin_hook_errors_total",
		Help: "插件回调错误总数",
	}, []string{"plugin", "hook"})

	// PluginHookDuration 插件回调耗时（on_message 高频路径监控）。
	PluginHookDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "juanniang_plugin_hook_duration_seconds",
		Help:    "插件回调耗时",
		Buckets: prometheus.DefBuckets,
	}, []string{"plugin", "hook"})
)

// ---------- HTTP API ----------

var (
	// HTTPRequestsTotal API 请求数（path 为 Hertz 路由模板，如 /api/v1/providers/:id）。
	HTTPRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "juanniang_http_requests_total",
		Help: "HTTP API 请求总数",
	}, []string{"method", "path", "status"})

	// HTTPRequestDuration API 请求耗时。
	HTTPRequestDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "juanniang_http_request_duration_seconds",
		Help:    "HTTP API 请求耗时",
		Buckets: prometheus.DefBuckets,
	})
)

func init() {
	for _, c := range []prometheus.Collector{
		EventsTotal, MessagesTotal, DedupDroppedTotal, BlockedTotal, DroppedTotal,
		AgentLoopsTotal, AgentLoopDuration, ConcurrencyWaitsTotal, ConcurrencyWaitDuration,
		LLMRequestsTotal, LLMTokensTotal, LLMLatency,
		GroupMgrViolationsTotal, GroupMgrDetectionsTotal, GroupMgrRAGScore,
		GroupMgrLLMReviewsTotal, GroupMgrSpamTotal,
		RAGSearchLatency, RAGSearchErrorsTotal,
		PluginHookErrorsTotal, PluginHookDuration,
		HTTPRequestsTotal, HTTPRequestDuration,
		ChatRepliesTotal, ChatStatsDroppedTotal,
		&runtimeCollector{},
	} {
		MustRegister(c)
	}
}

// Handler /metrics 端点 handler。
func Handler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}
