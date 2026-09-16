package groupmgr

import (
	"encoding/json"
	"testing"
)

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string // 期望提取出的 JSON 文本
	}{
		{
			name:  "纯JSON无包裹",
			input: `{"results":[{"index":0,"verdict":"black","reason":"广告"}]}`,
			want:  `{"results":[{"index":0,"verdict":"black","reason":"广告"}]}`,
		},
		{
			name:  "markdown json 代码块",
			input: "```json\n{\"results\":[{\"index\":0,\"verdict\":\"black\",\"reason\":\"广告\"}]}\n```",
			want:  `{"results":[{"index":0,"verdict":"black","reason":"广告"}]}`,
		},
		{
			name:  "markdown 无语言标识代码块",
			input: "```\n{\"results\":[{\"index\":0,\"verdict\":\"none\",\"category\":\"none\",\"reason\":\"正常\"}]}\n```",
			want:  `{"results":[{"index":0,"verdict":"none","category":"none","reason":"正常"}]}`,
		},
		{
			name:  "前后有解释文本",
			input: "根据判定标准，结果如下：\n{\"results\":[{\"index\":0,\"verdict\":\"none\",\"reason\":\"模糊\"}]}\n以上为判定结果。",
			want:  `{"results":[{"index":0,"verdict":"none","reason":"模糊"}]}`,
		},
		{
			name:  "代码块前后有文本",
			input: "判定完成：\n```json\n{\"results\":[{\"index\":0,\"verdict\":\"black\",\"reason\":\"办卡\"}]}\n```\n请参考。",
			want:  `{"results":[{"index":0,"verdict":"black","reason":"办卡"}]}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractJSON(tc.input)
			// 验证提取出的文本能被 json.Unmarshal 解析
			var bat reviewBatch
			if err := json.Unmarshal([]byte(got), &bat); err != nil {
				t.Fatalf("提取结果无法解析为 JSON: %v\n提取内容: %s", err, got)
			}
			if len(bat.Results) != 1 {
				t.Fatalf("应解析出 1 条结果，got %d", len(bat.Results))
			}
			if bat.Results[0].Verdict != "black" && bat.Results[0].Verdict != "none" {
				t.Fatalf("verdict 非法: %q", bat.Results[0].Verdict)
			}
		})
	}
}

func TestExtractJSONEmpty(t *testing.T) {
	if got := extractJSON(""); got != "" {
		t.Fatalf("空输入应返回空，got %q", got)
	}
	if got := extractJSON("   "); got != "" {
		t.Fatalf("纯空白应返回空，got %q", got)
	}
}
