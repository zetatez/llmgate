package adapter

import (
	"encoding/json"
	"net/http"

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
