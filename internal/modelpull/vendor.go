package modelpull

import "strings"

// vendorRules 模型名前缀 → 厂商命名空间。顺序匹配，靠前优先。
var vendorRules = []struct {
	prefix string
	vendor string
}{
	{"deepseek", "deepseek"},
	{"gpt", "openai"}, {"o3", "openai"}, {"o4", "openai"}, {"o1", "openai"},
	{"text-embedding", "openai"}, {"chatgpt", "openai"},
	{"claude", "anthropic"},
	{"gemini", "google"}, {"gemma", "google"},
	{"kimi", "moonshotai"},
	{"glm", "z-ai"},
	{"qwen", "qwen"},
	{"minimax", "minimax"},
	{"millennium", "minimax"},
	{"mimo", "xiaomi"},
	{"grok", "x-ai"},
	{"llama", "meta"}, {"muse", "meta"},
	{"doubao", "bytedance"}, {"seed", "bytedance"},
	{"kling", "klingai"},
	{"flux", "bfl"},
	{"wan", "alibaba"},
	{"hy3", "tencent"}, {"hy4", "tencent"}, {"hunyuan", "tencent"},
	{"ernie", "baidu"},
	{"step", "stepfun"},
	{"longcat", "meituan"},
	{"ling", "inclusionai"}, {"ring", "inclusionai"}, {"llada", "inclusionai"}, {"ming", "inclusionai"},
}

// InferVendor 从模型名推断厂商命名空间；模型名已带 "/" 或无法推断时返回空串。
func InferVendor(model string) string {
	if strings.Contains(model, "/") {
		return ""
	}
	low := strings.ToLower(model)
	for _, r := range vendorRules {
		if strings.HasPrefix(low, r.prefix) {
			return r.vendor
		}
	}
	return ""
}

// NamespacedName 返回带厂商前缀的名字（如 deepseek-v4-flash → deepseek/deepseek-v4-flash）；无法推断时原样返回。
func NamespacedName(model string) string {
	if v := InferVendor(model); v != "" {
		return v + "/" + model
	}
	return model
}
