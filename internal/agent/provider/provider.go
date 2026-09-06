package provider

import (
	"JuanNiang-Neo/internal/logging"
	"JuanNiang-Neo/internal/metrics"
	"JuanNiang-Neo/internal/otelx"
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var log = logging.NewModule("provider")

// openAIProvider 实现多协议 LLM API 客户端：按 cfg.APIMode 分派
// chat_completions / anthropic_messages / openai_responses / gemini_native。
type openAIProvider struct {
	cfg ProviderConfig
	cli *http.Client
}

func NewProvider(cfg ProviderConfig) Provider {
	return &openAIProvider{
		cfg: cfg,
		cli: &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *openAIProvider) ID() string      { return p.cfg.ID }
func (p *openAIProvider) Name() string    { return p.cfg.Name }
func (p *openAIProvider) Type() ModelType { return p.cfg.Type }
func (p *openAIProvider) Model() string   { return p.cfg.Model }

// APIMode 返回归一化协议模式。
func (p *openAIProvider) APIMode() APIMode { return p.cfg.apiMode() }

// ---------- Chat ----------

func (p *openAIProvider) Chat(ctx context.Context, req ChatRequest) (resp *ChatResponse, err error) {
	start := time.Now()
	// 链路追踪：LLM 调用 span（Agent 循环/群管理审核共用统一入口）
	_, span := otelx.Span(ctx, "llm.call",
		attribute.String("provider", p.ID()),
		attribute.String("model", p.Model()),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else if resp != nil && resp.TokenUsage > 0 {
			span.SetAttributes(attribute.Int("tokens", resp.TokenUsage))
		}
		span.End()
	}()

	body, err := p.buildRequest(req, false)
	if err != nil {
		metrics.LLMRequestsTotal.WithLabelValues(p.ID(), "error").Inc()
		metrics.LLMLatency.Observe(time.Since(start).Seconds())
		return nil, err
	}
	respBody, err := p.doRequest(ctx, p.endpointURL(), body)
	if err != nil {
		metrics.LLMRequestsTotal.WithLabelValues(p.ID(), "error").Inc()
		metrics.LLMLatency.Observe(time.Since(start).Seconds())
		return nil, err
	}
	resp, err = p.parseResponse(respBody)
	if err != nil {
		metrics.LLMRequestsTotal.WithLabelValues(p.ID(), "error").Inc()
	} else {
		metrics.LLMRequestsTotal.WithLabelValues(p.ID(), "ok").Inc()
	}
	metrics.LLMLatency.Observe(time.Since(start).Seconds())
	return resp, err
}

func (p *openAIProvider) ChatStream(ctx context.Context, req ChatRequest) (<-chan ChatStreamChunk, error) {
	body, err := p.buildRequest(req, true)
	if err != nil {
		return nil, err
	}
	return p.streamRequest(ctx, p.endpointURL(), body)
}

// ---------- Vision ----------

func (p *openAIProvider) Vision(ctx context.Context, imageData []byte, prompt string) (string, error) {
	imgB64 := base64.StdEncoding.EncodeToString(imageData)
	body, err := p.buildVisionRequest(imgB64, prompt)
	if err != nil {
		return "", err
	}
	respBody, err := p.doRequest(ctx, p.endpointURL(), body)
	if err != nil {
		return "", err
	}
	chatResp, err := p.parseResponse(respBody)
	if err != nil {
		return "", err
	}
	return chatResp.Message.Content, nil
}

// ---------- URL 规则（§5.3，解决 Issue #22） ----------

// apiSuffixes 协议已知完整端点后缀：URL 以其结尾 → 视为完整端点，不再追加。
var apiSuffixes = map[APIMode][]string{
	APIModeChatCompletions:   {"/chat/completions"},
	APIModeAnthropicMessages: {"/messages"},
	APIModeOpenAIResponses:   {"/responses"},
	APIModeGeminiNative:      {":generatecontent"},
}

// endpointURL 返回目标完整端点：base URL + 协议后缀自动拼接，或识别完整 URL、
// 或 url_mode=exact 原样使用。
func (p *openAIProvider) endpointURL() string {
	if p.cfg.urlMode() == "exact" {
		return p.cfg.Endpoint // 完全自定义：原样使用，不追加不识别
	}
	e := strings.TrimRight(p.cfg.Endpoint, "/")
	if e == "" {
		return ""
	}
	for _, s := range apiSuffixes[p.APIMode()] {
		if strings.HasSuffix(strings.ToLower(e), s) {
			return e // 用户已提供完整端点
		}
	}
	switch p.APIMode() {
	case APIModeGeminiNative:
		return e + "/models/" + url.PathEscape(p.cfg.Model) + ":generateContent"
	case APIModeAnthropicMessages:
		return e + "/messages"
	case APIModeOpenAIResponses:
		return e + "/responses"
	default:
		return e + "/chat/completions"
	}
}

// ---------- 请求构造（§5.2） ----------

func (p *openAIProvider) buildRequest(req ChatRequest, stream bool) ([]byte, error) {
	switch p.APIMode() {
	case APIModeAnthropicMessages:
		return p.buildAnthropic(req, stream)
	case APIModeOpenAIResponses:
		return p.buildResponses(req, stream)
	case APIModeGeminiNative:
		return p.buildGemini(req, stream)
	default:
		return p.buildChatCompletions(req, stream)
	}
}

// buildChatCompletions OpenAI Chat Completions 协议。
func (p *openAIProvider) buildChatCompletions(req ChatRequest, stream bool) ([]byte, error) {
	messages := make([]map[string]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		msg := map[string]any{"role": m.Role}
		if m.Content != "" {
			msg["content"] = m.Content
		}
		if len(m.ToolCalls) > 0 {
			toolCalls := make([]map[string]any, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				toolCalls = append(toolCalls, map[string]any{
					"id":   tc.ID,
					"type": tc.Type,
					"function": map[string]any{
						"name":      tc.Function.Name,
						"arguments": string(tc.Function.Arguments),
					},
				})
			}
			msg["tool_calls"] = toolCalls
		}
		if m.ToolCallID != "" {
			msg["tool_call_id"] = m.ToolCallID
		}
		if m.Name != "" {
			msg["name"] = m.Name
		}
		messages = append(messages, msg)
	}

	body := map[string]any{
		"model":    p.cfg.Model,
		"messages": messages,
	}
	if temp := p.resolveTemperature(req); temp > 0 {
		body["temperature"] = temp
	}
	if mt := p.resolveMaxTokens(req, 0); mt > 0 {
		body["max_tokens"] = mt
	}
	if p.cfg.TopP != nil {
		body["top_p"] = *p.cfg.TopP
	}
	if p.cfg.TopK != nil {
		body["top_k"] = *p.cfg.TopK
	}
	if p.cfg.FrequencyPenalty != nil {
		body["frequency_penalty"] = *p.cfg.FrequencyPenalty
	}
	if p.cfg.PresencePenalty != nil {
		body["presence_penalty"] = *p.cfg.PresencePenalty
	}
	if p.cfg.RepetitionPenalty != nil {
		body["repetition_penalty"] = *p.cfg.RepetitionPenalty
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        t.Function.Name,
					"description": t.Function.Description,
					"parameters":  json.RawMessage(t.Function.Parameters),
				},
			})
		}
		body["tools"] = tools
	}
	if stream {
		body["stream"] = true
	}
	p.applyThinking(body, APIModeChatCompletions)
	return json.Marshal(body)
}

// buildAnthropic Anthropic Messages 协议：system 独立字段、max_tokens 必填、tool_use/tool_result 结构。
func (p *openAIProvider) buildAnthropic(req ChatRequest, stream bool) ([]byte, error) {
	var system string
	msgs := make([]map[string]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		if m.Role == "system" {
			if system != "" {
				system += "\n"
			}
			system += m.Content
			continue
		}
		if m.Role == "tool" && m.ToolCallID != "" {
			msgs = append(msgs, map[string]any{
				"role": "user",
				"content": []map[string]any{
					{"type": "tool_result", "tool_use_id": m.ToolCallID, "content": m.Content},
				},
			})
			continue
		}
		role := "user"
		if m.Role == "assistant" {
			role = "assistant"
		}
		content := make([]map[string]any, 0, 1+len(m.ToolCalls))
		if m.Content != "" {
			content = append(content, map[string]any{"type": "text", "text": m.Content})
		}
		for _, tc := range m.ToolCalls {
			content = append(content, map[string]any{
				"type":  "tool_use",
				"id":    tc.ID,
				"name":  tc.Function.Name,
				"input": json.RawMessage(tc.Function.Arguments),
			})
		}
		msgs = append(msgs, map[string]any{"role": role, "content": content})
	}

	body := map[string]any{
		"model":      p.cfg.Model,
		"max_tokens": p.resolveMaxTokens(req, 4096),
		"messages":   msgs,
	}
	if system != "" {
		body["system"] = system
	}
	if temp := p.resolveTemperature(req); temp > 0 {
		body["temperature"] = temp
	}
	if p.cfg.TopP != nil {
		body["top_p"] = *p.cfg.TopP
	}
	if p.cfg.TopK != nil {
		body["top_k"] = *p.cfg.TopK
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"name":         t.Function.Name,
				"description":  t.Function.Description,
				"input_schema": json.RawMessage(t.Function.Parameters),
			})
		}
		body["tools"] = tools
	}
	if stream {
		body["stream"] = true
	}
	p.applyThinking(body, APIModeAnthropicMessages)
	return json.Marshal(body)
}

// buildResponses OpenAI Responses 协议：input 数组（非 messages）。
// system 消息不放入 input（responses 协议不允许 role=system），合并到 instructions 字段。
func (p *openAIProvider) buildResponses(req ChatRequest, stream bool) ([]byte, error) {
	var instructions string
	input := make([]map[string]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		if m.Role == "system" {
			if instructions != "" {
				instructions += "\n"
			}
			instructions += m.Content
			continue
		}
		if m.ToolCallID != "" {
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": m.ToolCallID,
				"output":  m.Content,
			})
			continue
		}
		item := map[string]any{"role": m.Role, "content": m.Content}
		if len(m.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				calls = append(calls, map[string]any{
					"type":      "function_call",
					"id":        tc.ID,
					"call_id":   tc.ID,
					"name":      tc.Function.Name,
					"arguments": string(tc.Function.Arguments),
				})
			}
			item["content"] = calls
		}
		input = append(input, item)
	}

	body := map[string]any{"model": p.cfg.Model, "input": input}
	if instructions != "" {
		body["instructions"] = instructions
	}
	if mt := p.resolveMaxTokens(req, 0); mt > 0 {
		body["max_output_tokens"] = mt
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"type":        "function",
				"name":        t.Function.Name,
				"description": t.Function.Description,
				"parameters":  json.RawMessage(t.Function.Parameters),
			})
		}
		body["tools"] = tools
	}
	if stream {
		body["stream"] = true
	}
	p.applyThinking(body, APIModeOpenAIResponses)
	return json.Marshal(body)
}

// buildGemini Gemini Generative Language 协议：contents[] / generationConfig / functionDeclarations。
func (p *openAIProvider) buildGemini(req ChatRequest, stream bool) ([]byte, error) {
	var systemParts []map[string]any
	contents := make([]map[string]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		if m.Role == "system" {
			systemParts = append(systemParts, map[string]any{"text": m.Content})
			continue
		}
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		parts := make([]map[string]any, 0, 1+len(m.ToolCalls))
		if m.Content != "" {
			parts = append(parts, map[string]any{"text": m.Content})
		}
		for _, tc := range m.ToolCalls {
			parts = append(parts, map[string]any{
				"functionCall": map[string]any{
					"name": tc.Function.Name,
					"args": json.RawMessage(tc.Function.Arguments),
				},
			})
		}
		if m.ToolCallID != "" {
			fnName := m.Name
			if fnName == "" {
				fnName = m.ToolCallID
			}
			parts = append(parts, map[string]any{
				"functionResponse": map[string]any{
					"name":     fnName,
					"response": map[string]any{"result": m.Content},
				},
			})
		}
		contents = append(contents, map[string]any{"role": role, "parts": parts})
	}

	body := map[string]any{"contents": contents}
	if len(systemParts) > 0 {
		body["systemInstruction"] = map[string]any{"parts": systemParts}
	}
	genCfg := map[string]any{}
	if temp := p.resolveTemperature(req); temp > 0 {
		genCfg["temperature"] = temp
	}
	if p.cfg.TopP != nil {
		genCfg["topP"] = *p.cfg.TopP
	}
	if p.cfg.TopK != nil {
		genCfg["topK"] = *p.cfg.TopK
	}
	if p.cfg.RepetitionPenalty != nil {
		genCfg["repetitionPenalty"] = *p.cfg.RepetitionPenalty
	}
	if mt := p.resolveMaxTokens(req, 8192); mt > 0 {
		genCfg["maxOutputTokens"] = mt
	}
	if len(genCfg) > 0 {
		body["generationConfig"] = genCfg
	}
	if len(req.Tools) > 0 {
		fds := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			params := json.RawMessage(t.Function.Parameters)
			if len(params) == 0 {
				params = json.RawMessage(`{}`)
			}
			fds = append(fds, map[string]any{
				"name":        t.Function.Name,
				"description": t.Function.Description,
				"parameters":  params,
			})
		}
		body["tools"] = []map[string]any{{"functionDeclarations": fds}}
	}
	if stream {
		body["stream"] = true
	}
	p.applyThinking(body, APIModeGeminiNative)
	return json.Marshal(body)
}

// ---------- 多模态请求构造（§5.6） ----------

func (p *openAIProvider) buildVisionRequest(imgB64, prompt string) ([]byte, error) {
	imgURL := "data:image/jpeg;base64," + imgB64
	switch p.APIMode() {
	case APIModeAnthropicMessages:
		body := map[string]any{
			"model":      p.cfg.Model,
			"max_tokens": p.resolveMaxTokens(ChatRequest{}, 4096),
			"messages": []map[string]any{
				{
					"role": "user",
					"content": []map[string]any{
						{"type": "text", "text": prompt},
						{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/jpeg", "data": imgB64}},
					},
				},
			},
		}
		return json.Marshal(body)
	case APIModeGeminiNative:
		body := map[string]any{
			"contents": []map[string]any{
				{
					"role": "user",
					"parts": []map[string]any{
						{"text": prompt},
						{"inlineData": map[string]any{"mimeType": "image/jpeg", "data": imgB64}},
					},
				},
			},
		}
		return json.Marshal(body)
	case APIModeOpenAIResponses:
		body := map[string]any{
			"model": p.cfg.Model,
			"input": []map[string]any{
				{
					"role": "user",
					"content": []map[string]any{
						{"type": "input_text", "text": prompt},
						{"type": "input_image", "image_url": imgURL, "detail": "auto"},
					},
				},
			},
		}
		return json.Marshal(body)
	default: // chat_completions
		body := map[string]any{
			"model": p.cfg.Model,
			"messages": []map[string]any{
				{
					"role": "user",
					"content": []map[string]any{
						{"type": "text", "text": prompt},
						{"type": "image_url", "image_url": map[string]string{"url": imgURL}},
					},
				},
			},
		}
		return json.Marshal(body)
	}
}

// ---------- 响应解析（§5.2） ----------

func (p *openAIProvider) parseResponse(body []byte) (*ChatResponse, error) {
	switch p.APIMode() {
	case APIModeAnthropicMessages:
		return p.parseAnthropic(body)
	case APIModeOpenAIResponses:
		return p.parseResponses(body)
	case APIModeGeminiNative:
		return p.parseGemini(body)
	default:
		return p.parseChatCompletions(body)
	}
}

func (p *openAIProvider) parseAnthropic(body []byte) (*ChatResponse, error) {
	var raw struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		StopReason string `json:"stop_reason"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("provider parseAnthropic: %w", err)
	}
	var text strings.Builder
	for _, b := range raw.Content {
		if b.Type == "text" {
			text.WriteString(b.Text)
		}
	}
	return &ChatResponse{
		Message:      ChatMessage{Role: "assistant", Content: text.String()},
		TokenUsage:   raw.Usage.InputTokens + raw.Usage.OutputTokens,
		FinishReason: raw.StopReason,
	}, nil
}

func (p *openAIProvider) parseResponses(body []byte) (*ChatResponse, error) {
	var raw struct {
		Output []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("provider parseResponses: %w", err)
	}
	var text strings.Builder
	for _, o := range raw.Output {
		for _, c := range o.Content {
			if c.Text != "" {
				text.WriteString(c.Text)
			}
		}
	}
	return &ChatResponse{
		Message:      ChatMessage{Role: "assistant", Content: text.String()},
		TokenUsage:   raw.Usage.TotalTokens,
		FinishReason: raw.Status,
	}, nil
}

func (p *openAIProvider) parseGemini(body []byte) (*ChatResponse, error) {
	var raw struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		UsageMetadata struct {
			TotalTokenCount int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("provider parseGemini: %w", err)
	}
	if len(raw.Candidates) == 0 {
		return nil, fmt.Errorf("provider parseGemini: 空响应")
	}
	c := raw.Candidates[0]
	var text strings.Builder
	for _, part := range c.Content.Parts {
		text.WriteString(part.Text)
	}
	return &ChatResponse{
		Message:      ChatMessage{Role: "assistant", Content: text.String()},
		TokenUsage:   raw.UsageMetadata.TotalTokenCount,
		FinishReason: c.FinishReason,
	}, nil
}

func (p *openAIProvider) parseChatCompletions(body []byte) (*ChatResponse, error) {
	var raw rawChatResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("provider parse: %w", err)
	}
	if len(raw.Choices) == 0 {
		return nil, fmt.Errorf("provider: 空响应")
	}

	choice := raw.Choices[0]
	msg := ChatMessage{
		Role:       choice.Message.Role,
		Content:    choice.Message.Content,
		ToolCallID: choice.Message.ToolCallID,
	}

	for _, tc := range choice.Message.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: ToolCallFunc{
				Name:      tc.Function.Name,
				Arguments: json.RawMessage(tc.Function.Arguments),
			},
		})
	}

	return &ChatResponse{
		Message:      msg,
		TokenUsage:   raw.Usage.TotalTokens,
		FinishReason: choice.FinishReason,
	}, nil
}

// ---------- HTTP 请求 ----------

func (p *openAIProvider) doRequest(ctx context.Context, url string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("provider request: %w", err)
	}
	p.setHeaders(req)

	resp, err := p.cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("provider do: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("provider read: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("provider %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// setHeaders 按 AuthHeader / 协议默认设置认证头。
func (p *openAIProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	// 浏览器风格 UA：部分 provider（opencode 等走 Cloudflare）按 User-Agent
	// 拦截 Go 默认客户端（Go-http-client/1.1 → 403 error code: 1010）。
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	auth := p.cfg.AuthHeader
	if auth == "" {
		switch p.APIMode() {
		case APIModeAnthropicMessages:
			auth = "x-api-key"
		case APIModeGeminiNative:
			auth = "x-goog-api-key"
		default:
			auth = "bearer"
		}
	}
	switch strings.ToLower(auth) {
	case "x-api-key":
		req.Header.Set("x-api-key", p.cfg.Token)
		if p.APIMode() == APIModeAnthropicMessages {
			req.Header.Set("anthropic-version", "2023-06-01")
		}
	case "x-goog-api-key":
		req.Header.Set("x-goog-api-key", p.cfg.Token)
	case "api-key":
		req.Header.Set("api-key", p.cfg.Token)
	default: // bearer
		req.Header.Set("Authorization", "Bearer "+p.cfg.Token)
	}
}

// ---------- 流式 ----------

func (p *openAIProvider) streamRequest(ctx context.Context, url string, body []byte) (<-chan ChatStreamChunk, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	p.setHeaders(httpReq)

	resp, err := p.cli.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("provider chat stream: %w", err)
	}

	ch := make(chan ChatStreamChunk, 32)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			log.Error("provider stream 非200", "status", resp.StatusCode, "body", string(body))
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return
			}
			for _, out := range p.parseStreamEvent(data) {
				select {
				case ch <- out:
				case <-ctx.Done():
					return
				}
			}
		}
		if err := scanner.Err(); err != nil {
			log.Error("provider stream 读取中断", "err", err)
		}
	}()

	return ch, nil
}

func (p *openAIProvider) parseStreamEvent(data string) []ChatStreamChunk {
	switch p.APIMode() {
	case APIModeAnthropicMessages:
		return p.parseAnthropicStreamEvent(data)
	case APIModeGeminiNative:
		return p.parseGeminiStreamEvent(data)
	case APIModeOpenAIResponses:
		return p.parseResponsesStreamEvent(data)
	default:
		return p.parseChatStreamEvent(data)
	}
}

func (p *openAIProvider) parseChatStreamEvent(data string) []ChatStreamChunk {
	var chunk rawStreamChunk
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		log.Error("provider stream 解析失败", "err", err)
		return nil
	}
	if len(chunk.Choices) == 0 {
		return nil
	}
	choice := chunk.Choices[0]
	out := ChatStreamChunk{
		Content:      choice.Delta.Content,
		FinishReason: choice.FinishReason,
	}
	for _, tc := range choice.Delta.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: ToolCallFunc{
				Name:      tc.Function.Name,
				Arguments: json.RawMessage(tc.Function.Arguments),
			},
		})
	}
	return []ChatStreamChunk{out}
}

func (p *openAIProvider) parseAnthropicStreamEvent(data string) []ChatStreamChunk {
	var ev struct {
		Type  string `json:"type"`
		Delta struct {
			Type       string `json:"type"`
			Text       string `json:"text"`
			StopReason string `json:"stop_reason"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		return nil
	}
	switch ev.Type {
	case "content_block_delta":
		return []ChatStreamChunk{{Content: ev.Delta.Text}}
	case "message_delta":
		return []ChatStreamChunk{{FinishReason: ev.Delta.StopReason}}
	}
	return nil
}

func (p *openAIProvider) parseGeminiStreamEvent(data string) []ChatStreamChunk {
	var ev struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		return nil
	}
	if len(ev.Candidates) == 0 {
		return nil
	}
	c := ev.Candidates[0]
	var text strings.Builder
	for _, pt := range c.Content.Parts {
		text.WriteString(pt.Text)
	}
	out := ChatStreamChunk{Content: text.String()}
	if c.FinishReason != "" {
		out.FinishReason = c.FinishReason
	}
	return []ChatStreamChunk{out}
}

func (p *openAIProvider) parseResponsesStreamEvent(data string) []ChatStreamChunk {
	var ev struct {
		Type  string `json:"type"`
		Delta string `json:"delta"`
	}
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		return nil
	}
	if ev.Type == "response.output_text.delta" {
		return []ChatStreamChunk{{Content: ev.Delta}}
	}
	return nil
}

// ---------- thinking 适配（§5.5） ----------

// adaptiveModelRe 匹配 Anthropic 新代模型（使用 adaptive thinking，且不支持 extended thinking 的型号）。
var adaptiveModelRe = regexp.MustCompile(`(?i)(opus-5|sonnet-5|fable-5|4-7|4-8|m3)`)

func (p *openAIProvider) applyThinking(body map[string]any, mode APIMode) {
	switch mode {
	case APIModeAnthropicMessages:
		p.applyThinkingAnthropic(body)
	case APIModeGeminiNative:
		p.applyThinkingGemini(body)
	case APIModeOpenAIResponses:
		p.applyThinkingResponses(body)
	default:
		p.applyThinkingChat(body)
	}
}

// thinkingEffort 归一化档位：off/low/medium/high。EnableThinking 旧字段映射为 medium。
func (p *openAIProvider) thinkingEffort() string {
	if p.cfg.ThinkingEffort != "" {
		return p.cfg.ThinkingEffort
	}
	if p.cfg.EnableThinking {
		return "medium"
	}
	return "off"
}

// providerKey 判定厂商分组：优先 ProviderKey，否则按 Name 关键词匹配。
func (p *openAIProvider) providerKey() string {
	if p.cfg.ProviderKey != "" {
		return normalizeProviderKey(p.cfg.ProviderKey)
	}
	n := strings.ToLower(p.cfg.Name)
	for _, k := range []string{"deepseek", "kimi", "moonshot", "zhipu", "zai", "bigmodel", "alibaba", "dashscope", "qwen", "siliconflow", "stepfun", "minimax", "tencent", "hunyuan"} {
		if strings.Contains(n, k) {
			return k
		}
	}
	return ""
}

func (p *openAIProvider) applyThinkingChat(body map[string]any) {
	effort := p.thinkingEffort()
	key := p.providerKey()
	if effort == "off" {
		switch key {
		case "deepseek", "kimi", "moonshot", "zhipu", "zai", "bigmodel":
			body["thinking"] = map[string]any{"type": "disabled"}
		case "alibaba", "dashscope", "qwen", "siliconflow":
			body["enable_thinking"] = false
		}
		return
	}
	switch key {
	case "deepseek":
		body["thinking"] = map[string]any{"type": "enabled"}
		if strings.Contains(strings.ToLower(p.cfg.Model), "deepseek-v4") {
			body["reasoning_effort"] = effort
		}
	case "kimi", "moonshot":
		body["thinking"] = map[string]any{"type": "enabled"}
		body["reasoning_effort"] = effort
	case "zhipu", "zai", "bigmodel":
		body["thinking"] = map[string]any{"type": "enabled"}
		if strings.Contains(strings.ToLower(p.cfg.Model), "glm-5.2") {
			body["reasoning_effort"] = effort
		}
	case "alibaba", "dashscope", "qwen", "siliconflow":
		body["enable_thinking"] = true
	case "stepfun":
		body["reasoning_effort"] = effort
	case "minimax":
		body["reasoning_split"] = true
	default:
		body["reasoning_effort"] = effort
	}
}

func (p *openAIProvider) applyThinkingAnthropic(body map[string]any) {
	if p.thinkingEffort() == "off" {
		return
	}
	if adaptiveModelRe.MatchString(p.cfg.Model) {
		body["thinking"] = map[string]any{"type": "adaptive"}
		return
	}
	budget := p.cfg.ThinkingBudget
	if budget <= 0 {
		budget = 8000
	}
	body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget}
}

func (p *openAIProvider) applyThinkingGemini(body map[string]any) {
	effort := p.thinkingEffort()
	tc := map[string]any{"includeThoughts": effort != "off"}
	if effort != "off" {
		if strings.Contains(strings.ToLower(p.cfg.Model), "gemini-3") {
			level := effort
			if level != "low" && level != "medium" && level != "high" {
				level = "high"
			}
			tc["thinkingLevel"] = level
		} else {
			budget := p.cfg.ThinkingBudget
			if budget <= 0 {
				budget = 8192
			}
			tc["thinkingBudget"] = budget
		}
	}
	body["thinkingConfig"] = tc
}

func (p *openAIProvider) applyThinkingResponses(body map[string]any) {
	if p.thinkingEffort() == "off" {
		return
	}
	body["reasoning"] = map[string]any{"effort": p.thinkingEffort(), "summary": "auto"}
}

// ---------- 采样参数优先级 ----------

// resolveMaxTokens 优先级：req.MaxTokens > cfg.MaxTokens > 协议默认。
func (p *openAIProvider) resolveMaxTokens(req ChatRequest, def int) int {
	if req.MaxTokens > 0 {
		return req.MaxTokens
	}
	if p.cfg.MaxTokens > 0 {
		return p.cfg.MaxTokens
	}
	return def
}

// resolveTemperature 优先级：req.Temperature > cfg.Temperature。
func (p *openAIProvider) resolveTemperature(req ChatRequest) float32 {
	if req.Temperature > 0 {
		return req.Temperature
	}
	return p.cfg.Temperature
}

// ---------- 底层 JSON 类型 ----------

type rawChatResponse struct {
	Choices []struct {
		Message struct {
			Role       string        `json:"role"`
			Content    string        `json:"content"`
			ToolCallID string        `json:"tool_call_id"`
			ToolCalls  []rawToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

type rawToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type rawStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string        `json:"content"`
			ToolCalls []rawToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}
