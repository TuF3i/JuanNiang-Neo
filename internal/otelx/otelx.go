// Package otelx 轻量 OpenTelemetry 封装：事件链路追踪（Grafana Tempo）。
//
// 设计约束（与项目"配置在磁盘上、降级优先"风格一致）：
//   - 全部由环境变量驱动，OTEL_EXPORTER_OTLP_ENDPOINT 未配置时自动 no-op
//     （不创建 exporter/processor，零开销、零故障影响，机器人照常运行）
//   - 每条事件一个 trace（根 span process_event），下游各阶段为子 span，
//     span 树即"单条事件处理全流程"，可在 Grafana Tempo 查看瀑布图
//   - 消息内容默认截断 100 字符记录（OTEL_TRACE_CAPTURE_CONTENT 可关）
package otelx

import (
	"context"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const (
	// EnvEndpoint OTLP 上报地址（host:port 或 http://host:port；空 = 禁用）。
	EnvEndpoint = "OTEL_EXPORTER_OTLP_ENDPOINT"
	// EnvServiceName 服务名（Tempo 里按 service.name 过滤）。
	EnvServiceName = "OTEL_SERVICE_NAME"
	// EnvSampleRatio 采样率（0~1，默认 1.0 全量；热聊群量大可调低）。
	EnvSampleRatio = "OTEL_TRACE_SAMPLE_RATIO"
	// EnvCaptureContent 是否在根 span 记录消息内容（截断 100 字符）。
	EnvCaptureContent = "OTEL_TRACE_CAPTURE_CONTENT"
)

// DefaultServiceName 默认服务名。
const DefaultServiceName = "juan-niang-neo"

// tracer 全局 Tracer：未初始化/禁用时为 no-op tracer（调用安全）。
var tracer = otel.Tracer(DefaultServiceName)

// captureContent 是否在根 span 记录消息内容（截断版）。
var captureContent bool

// Init 初始化 TracerProvider。endpoint 为空 → no-op。
// 返回 shutdown 函数（进程退出时调用，冲刷缓冲 span）。
func Init(serviceName, endpoint string, sampleRatio float64, capture bool) func(context.Context) error {
	captureContent = capture
	if serviceName == "" {
		serviceName = DefaultServiceName
	}
	if endpoint == "" {
		slog.Info("链路追踪未配置 OTLP endpoint，保持 no-op（设 " + EnvEndpoint + " 启用）")
		tracer = otel.Tracer(serviceName)
		return func(context.Context) error { return nil }
	}

	// 兼容带 scheme 的地址（http://host:port / https://host:port）
	addr := strings.TrimPrefix(strings.TrimPrefix(endpoint, "http://"), "https://")
	opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(addr)}
	if strings.HasPrefix(endpoint, "http://") {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	exporter, err := otlptracehttp.New(context.Background(), opts...)
	if err != nil {
		slog.Warn("OTel exporter 创建失败，链路追踪降级 no-op", "err", err)
		tracer = otel.Tracer(serviceName)
		return func(context.Context) error { return nil }
	}

	if sampleRatio < 0 {
		sampleRatio = 0
	}
	if sampleRatio > 1 {
		sampleRatio = 1
	}
	res := resource.NewWithAttributes("", attribute.String("service.name", serviceName))
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampleRatio))),
	)
	otel.SetTracerProvider(tp)
	tracer = tp.Tracer(serviceName)
	slog.Info("链路追踪已启用", "endpoint", endpoint, "sample_ratio", sampleRatio, "capture_content", captureContent)
	return tp.Shutdown
}

// Span 创建子 span（name + 属性），返回派生 ctx 与结束函数。
// 未启用链路追踪时返回 no-op span（零开销）。
func Span(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return tracer.Start(ctx, name, trace.WithAttributes(attrs...))
}

// NewRootSpan 创建独立根 span（不继承父级 trace）：用于参与窗口释放等
// 定时器/异步路径——父级 process_event span 早已结束，若仍挂在其下会形成
// 瀑布图虚假间隙；新根让释放链路可独立聚合、单独查询。
func NewRootSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return tracer.Start(ctx, name, trace.WithAttributes(attrs...), trace.WithNewRoot())
}

// CaptureContent 是否记录消息内容（根 span 属性用）。
func CaptureContent() bool { return captureContent }

// MessageContentAttr 构造消息内容属性：截断 rune-safe 前 n 字符（中文不切坏）。
func MessageContentAttr(text string, n int) attribute.KeyValue {
	r := []rune(text)
	if len(r) > n {
		text = string(r[:n]) + "…"
	}
	return attribute.String("message_content", text)
}
