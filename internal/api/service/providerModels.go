package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"JuanNiang-Neo/internal/api/dto"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// 模型列表拉取参数。
const (
	providerModelsTimeout  = 15 * time.Second
	providerModelsMaxBody  = 2 << 20 // 响应体上限 2MB（模型列表可能很长，防滥用）
	providerModelsEndpoint = "/models"
)

// ListProviderModels 代理拉取厂商模型列表（GET {endpoint}/models）。
// 浏览器直连厂商 API 受 CORS 限制，由后端代理转发；与 TestProvider 相同的
// 信任模型（JWT 管理面板、用户提供 endpoint/token 的探测类操作）。
// 上游失败不报 500：信封 OK + {ok:false, message}，前端按"获取失败可手动输入"降级。
func (s *Service) ListProviderModels(ctx context.Context, c *app.RequestContext) {
	var data dto.ListProviderModelsReq
	if err := c.BindJSON(&data); err != nil {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.BindJSONErr, dto.ErrorDetail{ErrorDetail: err.Error()}))
		return
	}
	endpoint := strings.TrimRight(strings.TrimSpace(data.Endpoint), "/")
	fail := func(msg string) {
		c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.ProviderModelsResp{Ok: false, Message: msg, Models: []string{}}))
	}
	if endpoint == "" {
		fail("请先填写 API 地址")
		return
	}

	url := endpoint + providerModelsEndpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		fail("构造请求失败: " + err.Error())
		return
	}
	// 认证头：bearer（默认）/ x-api-key / api-key；Gemini Native 用 query 参数 key
	if key := strings.TrimSpace(data.Token); key != "" {
		if strings.TrimSpace(data.APIMode) == "gemini_native" {
			req.URL.RawQuery = "key=" + key
		} else {
			switch strings.ToLower(strings.TrimSpace(data.AuthHeader)) {
			case "x-api-key":
				req.Header.Set("x-api-key", key)
				req.Header.Set("anthropic-version", "2023-06-01")
			case "api-key":
				req.Header.Set("api-key", key)
			default:
				req.Header.Set("Authorization", "Bearer "+key)
			}
		}
	}

	client := &http.Client{Timeout: providerModelsTimeout}
	resp, err := client.Do(req)
	if err != nil {
		fail("请求厂商 API 失败: " + err.Error())
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, providerModelsMaxBody))
	if err != nil {
		fail("读取响应失败: " + err.Error())
		return
	}
	if resp.StatusCode != http.StatusOK {
		fail(fmt.Sprintf("厂商 API 返回 HTTP %d: %s", resp.StatusCode, truncateModelResp(string(body))))
		return
	}

	models := parseProviderModels(body)
	if len(models) == 0 {
		fail("厂商 API 未返回模型列表")
		return
	}
	c.JSON(consts.StatusOK, dto.GenFinalResponse(dto.OK, dto.ProviderModelsResp{Ok: true, Message: fmt.Sprintf("已获取 %d 个模型", len(models)), Models: models}))
}

// parseProviderModels 解析厂商模型列表响应，兼容三种常见形态：
// OpenAI/Anthropic 风格 {"data":[{"id":"gpt-4o"},...]}、
// Gemini 风格 {"models":[{"name":"models/gemini-xx"},...]}、纯字符串数组 ["m1","m2"]。
func parseProviderModels(body []byte) []string {
	var wrapped struct {
		Data   []json.RawMessage `json:"data"`
		Models []json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil && (len(wrapped.Data) > 0 || len(wrapped.Models) > 0) {
		return modelIDsFromRaw(append(wrapped.Data, wrapped.Models...))
	}
	var strArr []string
	if err := json.Unmarshal(body, &strArr); err == nil {
		return cleanModelIDs(strArr)
	}
	return nil
}

// modelIDsFromRaw 从元素列表提取模型 ID（对象取 id/name，字符串直接收集）。
func modelIDsFromRaw(items []json.RawMessage) []string {
	out := make([]string, 0, len(items))
	for _, raw := range items {
		var o struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &o); err == nil {
			if id := strings.TrimSpace(o.ID); id != "" {
				out = append(out, id)
				continue
			}
			if name := strings.TrimSpace(o.Name); name != "" {
				out = append(out, name)
			}
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err == nil && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return dedupModelIDs(out)
}

func cleanModelIDs(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return dedupModelIDs(out)
}

func dedupModelIDs(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// truncateModelResp 截断上游错误响应用于提示展示。
func truncateModelResp(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
