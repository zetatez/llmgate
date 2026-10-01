// Package quota 提供成本估算与额度计算。
package quota

import "encoding/json"

// ModelPrice 某模型的单价（美元 / 百万 tokens）。
type ModelPrice struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

// ParsePricing 解析 settings 中存量的 model_pricing JSON。
// 格式: {"deepseek-chat": {"input": 0.14, "output": 0.28}, ...}
func ParsePricing(s string) (map[string]ModelPrice, error) {
	if s == "" {
		return map[string]ModelPrice{}, nil
	}
	m := map[string]ModelPrice{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, err
	}
	return m, nil
}

// EstimateCost 估算一次请求成本。模型无单价配置时返回 0。
func EstimateCost(pricing map[string]ModelPrice, model string, promptTokens, completionTokens int64) float64 {
	p, ok := pricing[model]
	if !ok {
		return 0
	}
	return float64(promptTokens)/1e6*p.Input + float64(completionTokens)/1e6*p.Output
}
