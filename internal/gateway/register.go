// Package gateway 实现 OpenAI 兼容的开放网关 /v1/*。
package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"llmgate/internal/app"
	"llmgate/internal/logbus"
	"llmgate/internal/models"
	"llmgate/internal/quota"
	"llmgate/internal/router"
	"llmgate/internal/store"
)

// Register 挂载网关路由，全部请求走用户令牌鉴权。
// 注意：gate 组路径由调用方传入（cfg.GatewayPrefix + "v1"）。
func Register(v1 *gin.RouterGroup, a *app.App, bus *logbus.Bus, pen *router.Penalizer) {
	v1.Use(gatewayAuth(a))
	v1.GET("/models", handleListModels(a))
	v1.POST("/chat/completions", handleForward(a, bus, pen, "/v1/chat/completions"))
	v1.POST("/completions", handleForward(a, bus, pen, "/v1/completions"))
	v1.POST("/embeddings", handleForward(a, bus, pen, "/v1/embeddings"))
	v1.POST("/responses", handleResponses(a, bus, pen))
}

const ctxUserKey = "llmgate.user"

// gatewayAuth 校验 Bearer sk-xxx 令牌并检查额度。
func gatewayAuth(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerValue(c.GetHeader("Authorization"))
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"message": "missing Authorization: Bearer <sk-xxx>", "type": "unauthorized"},
			})
			return
		}
		user, err := store.GetUserByTokenHash(a.DB, app.HashToken(token))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "internal error", "type": "internal_error"}})
			return
		}
		if user == nil || user.Status != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"message": "invalid or disabled token", "type": "unauthorized"},
			})
			return
		}
		if user.QuotaLimit > 0 && user.QuotaUsed >= user.QuotaLimit {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": gin.H{"message": "quota exceeded", "type": "quota_exceeded"},
			})
			return
		}
		c.Set(ctxUserKey, user)
		c.Next()
	}
}

func bearerValue(h string) string {
	const p = "Bearer "
	if len(h) > len(p) && h[:len(p)] == p {
		return h[len(p):]
	}
	return ""
}

// passThroughDefault 常见客户端/上游会要求透传的会话头白名单（小写比较）。
var passThroughDefault = map[string]bool{
	"x-opencode-session":  true,
	"x-codex-session":     true,
	"x-api-key":           false, // 禁用：避免覆盖我们的 Bearer 鉴权
	"openai-organization": true,
	"openai-project":      true,
	"openai-request-id":   true,
	"x-request-id":        true,
	"x-conversation-id":   true,
}

// buildPassHeaders 从客户端请求头中提取需要透传给上游的头（内置白名单），大小写不敏感。
func buildPassHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for name := range h {
		key := strings.ToLower(name)
		if passThroughDefault[key] {
			if v := h.Get(name); v != "" {
				out[key] = v
			}
		}
	}
	return out
}

func handleListModels(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := a.DB.Query(`SELECT DISTINCT display_name FROM model_routes WHERE enabled = 1`)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query models failed"})
			return
		}
		defer rows.Close()
		data := []gin.H{}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "scan models failed"})
				return
			}
			data = append(data, gin.H{"id": name, "object": "model", "owned_by": "llmgate"})
		}
		c.JSON(http.StatusOK, gin.H{"object": "list", "data": data})
	}
}

// maxRequestBody OpenAI 网关请求体上限（防异常大请求占内存）。
const maxRequestBody = 32 << 20 // 32MB

// handleForward 读取请求体，按 stream 分流：非流式转发或 SSE 流式代理。
func handleForward(a *app.App, bus *logbus.Bus, pen *router.Penalizer, upstreamPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxRequestBody+1))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "read body failed"})
			return
		}
		if len(body) > maxRequestBody {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{
				"error": gin.H{"message": "request body too large", "type": "invalid_request_error"},
			})
			return
		}
		var meta struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		if err := json.Unmarshal(body, &meta); err != nil || meta.Model == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"message": "missing valid model field", "type": "invalid_request_error"},
			})
			return
		}

		userAny, _ := c.Get(ctxUserKey)
		user := userAny.(*models.User)
		rt := router.New(a, pen)

		logEntry := &models.RequestLog{
			UserID:       user.ID,
			DisplayModel: meta.Model,
			Stream:       boolToInt(meta.Stream),
			CreatedAt:    models.Now(),
		}

		// 需要透传的上游会话头（opencode/codex/OpenAI 系客户端白名单）
		passHeaders := buildPassHeaders(c.Request.Header)

		// opencode.ai 要求请求带稳定的 x-opencode-session（用于会话路由/缓存）；
		// 客户端没带时按「用户+模型」派生稳定值自动附带（渠道配置的额外头可覆盖）
		if _, ok := passHeaders["x-opencode-session"]; !ok {
			passHeaders["x-opencode-session"] = fmt.Sprintf("llmgate-%d-%s", user.ID, meta.Model)
		}

		if meta.Stream {
			handleStreamRoute(c, a, bus, rt, body, meta.Model, upstreamPath, logEntry, passHeaders)
			return
		}
		handlePlainRoute(c, a, bus, rt, body, meta.Model, upstreamPath, logEntry, passHeaders)
	}
}

func finalizeLog(a *app.App, bus *logbus.Bus, logEntry *models.RequestLog, priceIn, priceOut *float64) {
	pricingStr, _ := a.GetSetting("model_pricing")
	if pricing, perr := quota.ParsePricing(pricingStr); perr == nil {
		// 成本计算：路由自定义单价优先，否则回退全局 model_pricing（按对外模型名）
		logEntry.Cost = quota.EstimateCostRoute(pricing, priceIn, priceOut, logEntry.DisplayModel, logEntry.PromptTokens, logEntry.CompletionTokens)
	}
	bus.Write(logEntry)
}

// oneLine 把多行文本压成单行（用于日志/错误记录，避免换行破坏展示）。
func oneLine(s string, max int) string {
	if max <= 0 {
		max = 200
	}
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		s = s[:max] + "..."
	}
	return s
}

// handlePlainRoute 非流式转发。
func handlePlainRoute(c *gin.Context, a *app.App, bus *logbus.Bus, rt *router.Router, body []byte, model, upstreamPath string, logEntry *models.RequestLog, passHeaders map[string]string) {
	start := time.Now()
	res, ferr := rt.ForwardChat(c.Request.Context(), body, model, upstreamPath, passHeaders)
	logEntry.LatencyMS = time.Since(start).Milliseconds()

	if res != nil {
		logEntry.ChannelID = res.Channel.ID
		logEntry.KeyID = res.KeyID
		logEntry.UpstreamModel = res.Route.UpstreamModel
		logEntry.Status = successOrError(res.StatusCode)
		if logEntry.Status == "error" {
			logEntry.ErrorCode = http.StatusText(res.StatusCode)
		}
		parseUsage(res.Body, logEntry)
		finalizeLog(a, bus, logEntry, res.Route.PriceInput, res.Route.PriceOutput)
		if logEntry.Status == "success" {
			_ = store.AddUserQuota(a.DB, logEntry.UserID, logEntry.Cost)
		}
		_ = store.TouchChannelKey(a.DB, res.KeyID)
		c.Data(res.StatusCode, contentType(res), res.Body)
		return
	}
	logEntry.Status = "error"
	logEntry.ErrorCode = oneLine(ferr.Error(), 200) // 单行：渠道 + 原因（如 channel opencode: upstream 403 ...）
	finalizeLog(a, bus, logEntry, nil, nil)
	code, msg := mapRouteError(ferr)
	if retryAfter := retryAfterFor(code); retryAfter != "" {
		c.Header("Retry-After", retryAfter)
	}
	c.JSON(code, gin.H{"error": gin.H{"message": oneLine(msg, 500), "type": "upstream_error"}})
}

// retryAfterFor 返回指定错误码应带上的 Retry-After 秒数；空串表示不重试提示。
func retryAfterFor(code int) string {
	if code == http.StatusServiceUnavailable {
		return "5"
	}
	return ""
}

// handleStreamRoute SSE 流式代理：上游首个 data 事件前可透明故障转移（连接即死不外露）；
// 首事件交付后绑定渠道；流中断时补发标准 error 事件。
func handleStreamRoute(c *gin.Context, a *app.App, bus *logbus.Bus, rt *router.Router, body []byte, model, upstreamPath string, logEntry *models.RequestLog, passHeaders map[string]string) {
	start := time.Now()
	sr, ferr := rt.ForwardChatStream(c.Request.Context(), body, model, upstreamPath, passHeaders)
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

	if sr != nil {
		logEntry.ChannelID = sr.Channel.ID
		logEntry.KeyID = sr.KeyID
		logEntry.UpstreamModel = sr.Route.UpstreamModel
	}
	if sr.Passthrough != nil {
		// 上游非 2xx 透传（原始错误体，非 SSE）
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

	// SSE 代理：首事件已在路由层缓冲确认（上游稳定），先写给客户端再继续转发
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	flusher, _ := c.Writer.(http.Flusher)

	logEntry.Status = "success"
	var usageBuf bytes.Buffer
	if len(sr.Buffered) > 0 {
		_, _ = c.Writer.Write(sr.Buffered)
		if usageBuf.Len() < 1<<20 {
			usageBuf.Write(sr.Buffered)
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	buf := make([]byte, 32<<10)
	clientGone := false
	for {
		// 客户端已断开连接：立即停止读上游，避免在渠道上继续白烧额度/占用连接。
		if c.Request.Context().Err() != nil {
			clientGone = true
			break
		}
		n, rerr := sr.Stream.Read(buf)
		if n > 0 {
			if _, werr := c.Writer.Write(buf[:n]); werr != nil {
				// 写给客户端失败（对端断开/重置）：同样立即停，别再拖上游。
				clientGone = true
				break
			}
			if usageBuf.Len() < 1<<20 {
				usageBuf.Write(buf[:n])
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			if c.Request.Context().Err() != nil {
				clientGone = true
				break
			}
			// 上游流中断（客户仍在）：给客户端明确信号；已消耗的 token 照常计费。
			logEntry.Status = "error"
			logEntry.ErrorCode = "stream_interrupted"
			_, _ = c.Writer.Write([]byte("data: {\"error\":{\"message\":\"upstream stream interrupted\",\"type\":\"stream_error\"}}\n\n"))
			_, _ = c.Writer.Write([]byte("data: [DONE]\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
			break
		}
	}
	if clientGone {
		log.Printf("gateway: sse client disconnected early for user %d (%s)", logEntry.UserID, logEntry.DisplayModel)
	}
	_ = sr.Stream.Close()
	parseUsageFromSSE(usageBuf.Bytes(), logEntry)
	finalizeLog(a, bus, logEntry, sr.Route.PriceInput, sr.Route.PriceOutput)
	if logEntry.Status == "success" {
		_ = store.AddUserQuota(a.DB, logEntry.UserID, logEntry.Cost)
	}
	_ = store.TouchChannelKey(a.DB, logEntry.KeyID)
}

// parseUsage 从（非流式）上游响应体解析 usage。
func parseUsage(body []byte, l *models.RequestLog) {
	var up struct {
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			TotalTokens      int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &up); err == nil && up.Usage.TotalTokens > 0 {
		l.PromptTokens = up.Usage.PromptTokens
		l.CompletionTokens = up.Usage.CompletionTokens
		l.TotalTokens = up.Usage.TotalTokens
	}
}

// parseUsageFromSSE 从 SSE 文本中提取最后一个包含 usage 的 data 块（需客户端开启 stream_options.include_usage）。
func parseUsageFromSSE(buf []byte, l *models.RequestLog) {
	for _, line := range strings.Split(string(buf), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") || !strings.Contains(line, "usage") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var u struct {
			Usage struct {
				PromptTokens     int64 `json:"prompt_tokens"`
				CompletionTokens int64 `json:"completion_tokens"`
				TotalTokens      int64 `json:"total_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &u); err == nil && u.Usage.TotalTokens > 0 {
			l.PromptTokens = u.Usage.PromptTokens
			l.CompletionTokens = u.Usage.CompletionTokens
			l.TotalTokens = u.Usage.TotalTokens
		}
	}
}

func successOrError(code int) string {
	if code >= 200 && code < 400 {
		return "success"
	}
	return "error"
}

func mapRouteError(err error) (int, string) {
	switch {
	case errors.Is(err, router.ErrNoCandidate):
		return http.StatusNotFound, "no available channel for this model"
	case errors.Is(err, router.ErrChannelBusy):
		return http.StatusServiceUnavailable, "all channels are temporarily unavailable, please retry shortly"
	case errors.Is(err, router.ErrNetworkExhausted):
		return http.StatusServiceUnavailable, "all channels failed due to network errors, please retry shortly"
	default:
		return http.StatusBadGateway, err.Error()
	}
}

func contentType(res *router.Result) string {
	if res.ContentType != "" {
		return res.ContentType
	}
	return "application/json"
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
