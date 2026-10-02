package adapter

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"llmgate/internal/models"
)

// ChannelFrom 从渠道模型构建适配器所需的最小渠道信息。
func ChannelFrom(ch *models.Channel) BaseChannel {
	return BaseChannel{BaseURL: ch.BaseURL, TimeoutMS: ch.TimeoutMS}
}

// TestRequest 构造一个连通性测试请求（GET 上游 /v1/models）。
func TestRequest() *Request {
	return &Request{
		Method:  http.MethodGet,
		Path:    "/v1/models",
		Headers: map[string]string{},
	}
}

// HealthRequest 构造探活请求。model 非空时对 /v1/chat/completions 发最小编码请求——
// 真实反映聊天通道健康（models 列表可用但 chat 挂掉时，只有 chat 探活才能发现）。
// model 为空则回退 GET /v1/models（如渠道尚未同步路由）。
func HealthRequest(model string) *Request {
	if model != "" {
		body := `{"model":"` + escapeJSON(model) + `","messages":[{"role":"user","content":"ping"}],"max_tokens":1,"stream":false}`
		return &Request{
			Method:  http.MethodPost,
			Path:    "/v1/chat/completions",
			Body:    bytes.NewReader([]byte(body)),
			Headers: map[string]string{"Content-Type": "application/json"},
		}
	}
	return TestRequest()
}

// escapeJSON 转义模型名中的引号/反斜杠，保证嵌入 JSON 字符串合法。
func escapeJSON(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return r.Replace(s)
}

// ParseModels 解析标准 OpenAI /v1/models 响应，返回模型 id 列表。
func ParseModels(body []byte) ([]string, error) {
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(resp.Data))
	for _, m := range resp.Data {
		if m.ID != "" {
			out = append(out, m.ID)
		}
	}
	return out, nil
}
