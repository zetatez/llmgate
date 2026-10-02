// OpenAI Responses API (/v1/responses) 兼容层。
// Codex 等客户端走 Responses 协议；llmgate 内部仍用 chat/completions 打上游，
// 此处负责请求/响应的双向翻译，并复用 Router 的故障转移/熔断与 gateway 的计费日志。
package gateway

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"llmgate/internal/app"
	"llmgate/internal/logbus"
	"llmgate/internal/models"
	"llmgate/internal/router"
	"llmgate/internal/store"
)

// responsesBody 是 Responses API 请求体中最常用的字段。
type responsesBody struct {
	Model           string          `json:"model"`
	Instructions    string          `json:"instructions"`
	Input           json.RawMessage `json:"input"`
	MaxOutputTokens int             `json:"max_output_tokens"`
	Stream          bool            `json:"stream"`
}

// responsesError 标准 OpenAI 错误结构（与 chat 网关一致）。
type responsesError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// handleResponses 处理 POST /v1/responses：翻译为 chat 请求，转发，再转回 Responses 格式。
func handleResponses(a *app.App, bus *logbus.Bus, pen *router.Penalizer) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxRequestBody+1))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "read body failed"})
			return
		}
		if len(body) > maxRequestBody {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
			return
		}
		var rb responsesBody
		if err := json.Unmarshal(body, &rb); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "invalid responses request", "type": "invalid_request_error"}})
			return
		}
		if rb.Model == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "missing model", "type": "invalid_request_error"}})
			return
		}

		userAny, _ := c.Get(ctxUserKey)
		user := userAny.(*models.User)
		rt := router.New(a, pen)
		logEntry := &models.RequestLog{
			UserID:       user.ID,
			DisplayModel: rb.Model,
			Stream:       boolToInt(rb.Stream),
			CreatedAt:    models.Now(),
		}
		passHeaders := buildPassHeaders(c.Request.Header)
		// opencode 上游要求 x-opencode-session；客户端(如 Codex)没带时按用户+模型派生稳定值
		if _, ok := passHeaders["x-opencode-session"]; !ok {
			passHeaders["x-opencode-session"] = fmt.Sprintf("llmgate-%d-%s", user.ID, rb.Model)
		}

		// 翻译 Responses 请求 → 内部 chat 请求体
		chatBody, merr := responsesToChat(rb)
		if merr != nil {
			logEntry.Status = "error"
			logEntry.ErrorCode = oneLine(merr.Error(), 200)
			finalizeLog(a, bus, logEntry, nil, nil)
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": merr.Error(), "type": "invalid_request_error"}})
			return
		}

		if rb.Stream {
			responsesStream(c, a, bus, rt, chatBody, rb.Model, logEntry, passHeaders)
			return
		}
		responsesPlain(c, a, bus, rt, chatBody, rb.Model, logEntry, passHeaders)
	}
}

// responsesPlain 非流式：上游 chat 完成 → Responses 对象。
func responsesPlain(c *gin.Context, a *app.App, bus *logbus.Bus, rt *router.Router, chatBody []byte, model string, logEntry *models.RequestLog, passHeaders map[string]string) {
	start := time.Now()
	res, ferr := rt.ForwardChat(c.Request.Context(), chatBody, model, "/v1/chat/completions", passHeaders)
	logEntry.LatencyMS = time.Since(start).Milliseconds()

	if res != nil {
		logEntry.ChannelID = res.Channel.ID
		logEntry.KeyID = res.KeyID
		logEntry.UpstreamModel = res.Route.UpstreamModel
		logEntry.Status = successOrError(res.StatusCode)
		if logEntry.Status == "error" {
			logEntry.ErrorCode = http.StatusText(res.StatusCode)
		}
		ct := res.ContentType
		if ct == "" {
			ct = "application/json"
		}
		if res.StatusCode < 200 || res.StatusCode >= 400 {
			// 上游非 2xx（如协议透传错误）：原样透传，客户端可读到真实原因
			finalizeLog(a, bus, logEntry, res.Route.PriceInput, res.Route.PriceOutput)
			_ = store.TouchChannelKey(a.DB, res.KeyID)
			c.Data(res.StatusCode, ct, res.Body)
			return
		}
		// 2xx：将 chat completion 转成 Responses 对象
		parseUsage(res.Body, logEntry)
		finalizeLog(a, bus, logEntry, res.Route.PriceInput, res.Route.PriceOutput)
		_ = store.TouchChannelKey(a.DB, res.KeyID)
		out := chatToResponse(res.Body, model, res.Route.UpstreamModel, logEntry)
		c.Data(http.StatusOK, "application/json", out)
		return
	}

	logEntry.Status = "error"
	logEntry.ErrorCode = oneLine(ferr.Error(), 200)
	finalizeLog(a, bus, logEntry, nil, nil)
	code, msg := mapRouteError(ferr)
	if ra := retryAfterFor(code); ra != "" {
		c.Header("Retry-After", ra)
	}
	c.JSON(code, gin.H{"error": gin.H{"message": oneLine(msg, 500), "type": "upstream_error"}})
}

// responsesStream 流式：上游 chat SSE → Responses SSE 事件。
func responsesStream(c *gin.Context, a *app.App, bus *logbus.Bus, rt *router.Router, chatBody []byte, model string, logEntry *models.RequestLog, passHeaders map[string]string) {
	start := time.Now()
	sr, ferr := rt.ForwardChatStream(c.Request.Context(), chatBody, model, "/v1/chat/completions", passHeaders)
	logEntry.LatencyMS = time.Since(start).Milliseconds()

	if ferr != nil {
		logEntry.Status = "error"
		logEntry.ErrorCode = oneLine(ferr.Error(), 200)
		finalizeLog(a, bus, logEntry, nil, nil)
		code, msg := mapRouteError(ferr)
		if ra := retryAfterFor(code); ra != "" {
			c.Header("Retry-After", ra)
		}
		c.JSON(code, gin.H{"error": gin.H{"message": oneLine(msg, 500), "type": "upstream_error"}})
		return
	}
	if sr.Passthrough != nil {
		logEntry.Status = successOrError(sr.StatusCode)
		if logEntry.Status == "error" {
			logEntry.ErrorCode = http.StatusText(sr.StatusCode)
		}
		finalizeLog(a, bus, logEntry, sr.Route.PriceInput, sr.Route.PriceOutput)
		if logEntry.Status == "success" {
			_ = store.AddUserQuota(a.DB, logEntry.UserID, logEntry.Cost)
		}
		ct := sr.ContentType
		if ct == "" {
			ct = "application/json"
		}
		c.Data(sr.StatusCode, ct, sr.Passthrough)
		return
	}
	if sr != nil {
		logEntry.ChannelID = sr.Channel.ID
		logEntry.KeyID = sr.KeyID
		logEntry.UpstreamModel = sr.Route.UpstreamModel
	}

	// SSE 输出
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	flusher, _ := c.Writer.(http.Flusher)

	sw := &sseEmitter{w: c.Writer, flusher: flusher}
	respID := "resp_" + randHex(12)

	var usageBuf bytes.Buffer
	logEntry.Status = "success"

	// 开场事件（输出 item 采取 lazy：等收到上游增量后再决定是 reasoning 还是 message）
	sw.emit("response.created", map[string]any{
		"type": "response.created", "response": baseResponse(respID, model, sr.Route.UpstreamModel, statusRunning),
	})
	sw.emit("response.in_progress", map[string]any{
		"type": "response.in_progress", "response": baseResponse(respID, model, sr.Route.UpstreamModel, statusRunning),
	})

	// 负责把上游 chat delta 转成 lazy 的 Responses output item 序列。
	conv := &responsesSSE{sw: sw}

	// 逐帧处理：支持跨 read 边界的部分帧（先攒到完整 \n\n 再解析）
	var frameBuf bytes.Buffer
	feed := func(chunk []byte) {
		if usageBuf.Len() < 1<<20 {
			usageBuf.Write(chunk)
		}
		frameBuf.Write(chunk)
		for {
			idx := bytes.Index(frameBuf.Bytes(), []byte("\n\n"))
			if idx < 0 {
				break
			}
			frame := frameBuf.Next(idx + 2)
			conv.ingestFrame(frame)
		}
	}

	if len(sr.Buffered) > 0 {
		feed(sr.Buffered)
		flush(flusher)
	}

	buf := make([]byte, 32<<10)
	interrupted := false
	for {
		if draining() || c.Request.Context().Err() != nil {
			interrupted = true
			break
		}
		n, rerr := sr.Stream.Read(buf)
		if n > 0 {
			feed(buf[:n])
			flush(flusher)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			if draining() || c.Request.Context().Err() != nil {
				interrupted = true
				break
			}
			logEntry.Status = "error"
			logEntry.ErrorCode = "stream_interrupted"
			sw.emit("response.failed", map[string]any{"type": "response.failed", "response": baseResponse(respID, model, sr.Route.UpstreamModel, statusFailed)})
			break
		}
	}
	_ = sr.Stream.Close()

	// 先解析用量，再发 done/completed，保证 usage 正确
	parseUsageFromSSE(usageBuf.Bytes(), logEntry)

	if interrupted {
		// 客户端断开或服务停机（优雅停机超时强关）：给仍连着的对端一个明确失败终态，
		// 否则 SDK 等不到 completed/failed 会挂起或误判。对已断开的对端，写失败也无害。
		logEntry.Status = "error"
		logEntry.ErrorCode = "stream_interrupted"
		sw.emit("response.failed", map[string]any{
			"type": "response.failed", "response": baseResponse(respID, model, sr.Route.UpstreamModel, statusFailed),
		})
	} else {
		conv.finalize(sr.Route.UpstreamModel)
		// 收尾：done 之后发 completed
		output := conv.buildOutput()
		sw.emit("response.completed", map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"id": respID, "object": "response", "created_at": time.Now().Unix(),
				"status": "completed", "model": upstreamModelifEmpty(sr.Route.UpstreamModel, model),
				"output": output,
				"usage":  usageFromLog(logEntry),
			},
		})
	}
	// 结尾哨兵（标准化）
	_, _ = c.Writer.Write([]byte("data: [DONE]\n\n"))
	if flusher != nil {
		flusher.Flush()
	}

	finalizeLog(a, bus, logEntry, sr.Route.PriceInput, sr.Route.PriceOutput)
	if logEntry.Status == "success" {
		_ = store.AddUserQuota(a.DB, logEntry.UserID, logEntry.Cost)
	}
	_ = store.TouchChannelKey(a.DB, logEntry.KeyID)
}

// responsesSSE 把上游 chat 增量 lazy 转成 Responses 的 output item 序列：
// 先出现 reasoning_content → 建 reasoning item；后出现 content → 建 message item；末尾各自 done。
// 符合 OpenAI Responses 流协议（推理模型必须先发 reasoning item）。
type responsesSSE struct {
	sw       *sseEmitter
	reasonID string
	msgID    string
	reason   strings.Builder
	msg      strings.Builder
}

// ingestFrame 处理一个完整 SSE 帧（含尾部 \n\n）。
func (r *responsesSSE) ingestFrame(frame []byte) {
	line := strings.TrimSpace(string(frame))
	if !strings.HasPrefix(line, "data:") {
		return
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "" || payload == "[DONE]" {
		return
	}
	var chunk struct {
		Choices []struct {
			Delta struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil || len(chunk.Choices) == 0 {
		return
	}
	d := chunk.Choices[0].Delta
	if d.ReasoningContent != "" {
		r.emitReason(d.ReasoningContent)
	}
	if d.Content != "" {
		r.emitText(d.Content)
	}
}

func (r *responsesSSE) emitReason(delta string) {
	if r.reasonID == "" {
		r.reasonID = "reason_" + randHex(10)
		r.sw.emit("response.output_item.added", map[string]any{
			"type": "response.output_item.added", "output_index": 0,
			"item": map[string]any{"id": r.reasonID, "type": "reasoning", "status": "in_progress", "summary": []any{}},
		})
		// 必须先用 summary_part.added 填充 summary[0]，否则后续 summary_text.delta 会因缺内容而报错
		r.sw.emit("response.reasoning_summary_part.added", map[string]any{
			"type": "response.reasoning_summary_part.added", "item_id": r.reasonID, "output_index": 0, "summary_index": 0,
			"part": map[string]any{"type": "summary_text", "text": ""},
		})
	}
	r.reason.WriteString(delta)
	r.sw.emit("response.reasoning_summary_text.delta", map[string]any{
		"type": "response.reasoning_summary_text.delta", "item_id": r.reasonID, "output_index": 0, "summary_index": 0, "delta": delta,
	})
}

func (r *responsesSSE) emitText(delta string) {
	if r.msgID == "" {
		r.msgID = "msg_" + randHex(10)
		idx := r.msgOutputIndex()
		r.sw.emit("response.output_item.added", map[string]any{
			"type": "response.output_item.added", "output_index": idx,
			"item": map[string]any{"id": r.msgID, "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}},
		})
		r.sw.emit("response.content_part.added", map[string]any{
			"type": "response.content_part.added", "item_id": r.msgID, "output_index": idx, "content_index": 0,
			"part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}},
		})
	}
	r.msg.WriteString(delta)
	r.sw.emit("response.output_text.delta", map[string]any{
		"type": "response.output_text.delta", "item_id": r.msgID, "output_index": r.msgOutputIndex(), "content_index": 0, "delta": delta,
	})
}

func (r *responsesSSE) msgOutputIndex() int {
	if r.reasonID != "" {
		return 1
	}
	return 0
}

func (r *responsesSSE) reasonItem() any {
	return map[string]any{
		"id": r.reasonID, "type": "reasoning", "status": "completed",
		"summary": []any{map[string]any{"type": "summary_text", "text": r.reason.String()}},
	}
}

func (r *responsesSSE) msgItem() any {
	return map[string]any{
		"id": r.msgID, "type": "message", "status": "completed", "role": "assistant",
		"content": []any{map[string]any{"type": "output_text", "text": r.msg.String(), "annotations": []any{}}},
	}
}

func (r *responsesSSE) buildOutput() []any {
	if r.reasonID == "" && r.msgID == "" {
		// 没有任何输出（异常）：给一个空消息兜底，避免 Codex 收不到 message
		r.msgID = "msg_" + randHex(10)
	}
	out := make([]any, 0, 2)
	if r.reasonID != "" {
		out = append(out, r.reasonItem())
	}
	if r.msgID != "" {
		out = append(out, r.msgItem())
	}
	return out
}

// finalize 依序发 reasoning / message 两个 output_item.done。
func (r *responsesSSE) finalize(model string) {
	if r.reasonID != "" {
		r.sw.emit("response.output_item.done", map[string]any{
			"type": "response.output_item.done", "output_index": 0, "item": r.reasonItem(),
		})
	}
	if r.msgID != "" {
		r.sw.emit("response.output_item.done", map[string]any{
			"type": "response.output_item.done", "output_index": r.msgOutputIndex(), "item": r.msgItem(),
		})
	}
}

const (
	statusRunning = "in_progress"
	statusFailed  = "failed"
)

func baseResponse(id, displayModel, upstreamModel, status string) map[string]any {
	return map[string]any{
		"id": id, "object": "response", "created_at": time.Now().Unix(),
		"status": status, "model": upstreamModelifEmpty(upstreamModel, displayModel),
		"output": []any{}, "usage": nil,
	}
}

func upstreamModelifEmpty(up, dis string) string {
	if up == "" {
		return dis
	}
	return up
}

// sseEmitter 按 SSE 规范写事件：event: <type>\n data: <json>\n\n
type sseEmitter struct {
	w       io.Writer
	flusher http.Flusher
}

func (s *sseEmitter) emit(evt string, payload any) {
	b, _ := json.Marshal(payload)
	_, _ = s.w.Write([]byte("event: " + evt + "\n"))
	_, _ = s.w.Write([]byte("data: " + string(b) + "\n\n"))
}

func flush(f http.Flusher) {
	if f != nil {
		f.Flush()
	}
}

// emitDelTask 从 chat SSE 文本里提取 delta 增量并转成 Responses 的 output_text.delta 事件。
func emitDelTask(sw *sseEmitter, itemID string, data []byte, accumulated *strings.Builder) {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil || len(chunk.Choices) == 0 {
			continue
		}
		d := chunk.Choices[0].Delta
		if d.Content != "" {
			accumulated.WriteString(d.Content)
			sw.emit("response.output_text.delta", map[string]any{
				"type": "response.output_text.delta", "item_id": itemID, "output_index": 0, "content_index": 0, "delta": d.Content,
			})
		}
		if d.ReasoningContent != "" {
			sw.emit("response.reasoning_summary_text.delta", map[string]any{
				"type": "response.reasoning_summary_text.delta", "item_id": itemID, "output_index": 0, "summary_index": 0, "delta": d.ReasoningContent,
			})
		}
	}
}

// responsesToChat 把 Responses 请求翻译成内部 chat/completions 请求体 JSON。
func responsesToChat(rb responsesBody) ([]byte, error) {
	var messages []map[string]any
	if strings.TrimSpace(rb.Instructions) != "" {
		messages = append(messages, map[string]any{"role": "system", "content": rb.Instructions})
	}

	in, err := parseResponsesInput(rb.Input)
	if err != nil {
		return nil, err
	}
	for _, m := range in {
		messages = append(messages, m)
	}
	if len(messages) == 0 {
		messages = append(messages, map[string]any{"role": "user", "content": ""})
	}

	req := map[string]any{
		"model":    rb.Model,
		"messages": messages,
		"stream":   rb.Stream,
	}
	if rb.MaxOutputTokens > 0 {
		req["max_tokens"] = rb.MaxOutputTokens
	}
	return json.Marshal(req)
}

// parseResponsesInput 把 Responses 的 input（字符串或数组）规整为 chat messages。
func parseResponsesInput(raw json.RawMessage) ([]map[string]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	// 字符串形式：直接用作用户消息
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return []map[string]any{{"role": "user", "content": str}}, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	var out []map[string]any
	for _, it := range items {
		msg, err := responsesItemToChat(it)
		if err != nil {
			return nil, err
		}
		if msg != nil {
			out = append(out, msg)
		}
	}
	return out, nil
}

// responsesItemToChat 规整单个 input item 为 chat {role, content}。
func responsesItemToChat(raw json.RawMessage) (map[string]any, error) {
	var it struct {
		Role    string `json:"role"`
		Type    string `json:"type"`
		Content any    `json:"content"` // string 或 []part
		Text    string `json:"text"`
	}
	if err := json.Unmarshal(raw, &it); err != nil {
		return nil, err
	}
	role := it.Role
	if role == "" {
		// 内容只有 text（如 {type:"message", role,...} 或纯文本）
		role = "user"
	}
	var text string
	switch v := it.Content.(type) {
	case string:
		text = v
	case []any:
		parts := v
		for _, p := range parts {
			if pm, ok := p.(map[string]any); ok {
				switch pm["type"] {
				case "input_text", "output_text", "text":
					if s, ok := pm["text"].(string); ok {
						text += s
					}
				}
			}
		}
	default:
		text = it.Text
	}
	if text == "" {
		text = it.Text
	}
	return map[string]any{"role": role, "content": text}, nil
}

// chatToResponse 把上游 chat completion JSON 转成 Responses 对象 JSON。
func chatToResponse(chat []byte, displayModel, upstreamModel string, l *models.RequestLog) []byte {
	var cc struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Created int64  `json:"created"`
		Choices []struct {
			Index   int `json:"index"`
			Message struct {
				Role      string `json:"role"`
				Content   string `json:"content"`
				Reasoning string `json:"reasoning_content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	var output []any
	_ = json.Unmarshal(chat, &cc)
	for _, ch := range cc.Choices {
		itemID := "msg_" + randHex(10)
		content := []any{map[string]any{
			"type": "output_text", "text": ch.Message.Content, "annotations": []any{},
		}}
		if ch.Message.Reasoning != "" {
			content = append(content, map[string]any{
				"type": "reasoning", "summary": []any{map[string]any{"type": "summary_text", "text": ch.Message.Reasoning}},
			})
		}
		output = append(output, map[string]any{
			"id": itemID, "type": "message", "status": "completed", "role": "assistant",
			"content": content,
		})
	}
	id := cc.ID
	if id == "" {
		id = "resp_" + randHex(12)
	}
	resp := map[string]any{
		"id": id, "object": "response",
		"created_at": cc.Created, "status": "completed", "model": upstreamModel,
		"output": output,
		"usage":  usageFromLog(l),
	}
	out, _ := json.Marshal(resp)
	return out
}

// usageFromLog 从日志统计构建 Responses 用量对象。
func usageFromLog(l *models.RequestLog) map[string]any {
	return map[string]any{
		"input_tokens":  l.PromptTokens,
		"output_tokens": l.CompletionTokens,
		"total_tokens":  l.TotalTokens,
	}
}

// randHex 生成 n 字节随机 hex 数（用于响应/消息 id）。
func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// 极罕见失败：退化为基于时间戳的 id
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}
