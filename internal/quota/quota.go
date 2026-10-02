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

// EstimateCost 估算一次请求成本，按全局定价（model_pricing，键=对外模型名）。
func EstimateCost(pricing map[string]ModelPrice, model string, promptTokens, completionTokens int64) float64 {
	p, ok := pricing[model]
	if !ok {
		return 0
	}
	return float64(promptTokens)/1e6*p.Input + float64(completionTokens)/1e6*p.Output
}

// EstimateCostRoute 估算一次请求成本：路由自定义单价（priceIn/priceOut 非 nil）优先，
// 否则回退全局定价（model_pricing 按对外模型名）。都没有则 0。
func EstimateCostRoute(pricing map[string]ModelPrice, priceIn, priceOut *float64, model string, promptTokens, completionTokens int64) float64 {
	var input, output float64
	switch {
	case priceIn != nil && priceOut != nil:
		input, output = *priceIn, *priceOut
	case priceIn != nil:
		input = *priceIn
	case priceOut != nil:
		output = *priceOut
	default:
		p, ok := pricing[model]
		if !ok {
			return 0
		}
		input, output = p.Input, p.Output
	}
	return float64(promptTokens)/1e6*input + float64(completionTokens)/1e6*output
}
