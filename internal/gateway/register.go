// Package gateway 实现 OpenAI 兼容的开放网关 /v1/*。
package gateway

import (
	"encoding/json"
	"io"
	"net/http"
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
func Register(v1 *gin.RouterGroup, a *app.App, bus *logbus.Bus) {
	v1.Use(gatewayAuth(a))
	v1.GET("/models", handleListModels(a))
	v1.POST("/chat/completions", handleForward(a, bus, "/v1/chat/completions"))
	v1.POST("/completions", handleForward(a, bus, "/v1/completions"))
	v1.POST("/embeddings", handleForward(a, bus, "/v1/embeddings"))
}

const ctxUserKey = "llmgate.user"

// gatewayAuth 校验 Bearer sk-xxx 令牌并检查额度。
func gatewayAuth(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerValue(c.GetHeader("Authorization"))
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"message": "缺少 Authorization: Bearer <sk-xxx>", "type": "unauthorized"},
			})
			return
		}
		user, err := store.GetUserByTokenHash(a.DB, app.HashToken(token))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "内部错误", "type": "internal_error"}})
			return
		}
		if user == nil || user.Status != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"message": "令牌无效或已停用", "type": "unauthorized"},
			})
			return
		}
		if user.QuotaLimit > 0 && user.QuotaUsed >= user.QuotaLimit {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": gin.H{"message": "额度已用完", "type": "quota_exceeded"},
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

// handleForward 读取请求体，走路由转发（非流式；stream=true 返回 501）。
func handleForward(a *app.App, bus *logbus.Bus, upstreamPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "read body failed"})
			return
		}
		var meta struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		if err := json.Unmarshal(body, &meta); err != nil || meta.Model == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"message": "缺少合法 model 字段", "type": "invalid_request_error"},
			})
			return
		}
		if meta.Stream {
			c.JSON(http.StatusNotImplemented, gin.H{
				"error": gin.H{"message": "流式响应暂未支持（Phase 2）", "type": "not_implemented"},
			})
			return
		}

		userAny, _ := c.Get(ctxUserKey)
		user := userAny.(*models.User)

		rt := router.New(a)
		start := time.Now()
		res, ferr := rt.ForwardChat(c.Request.Context(), body, meta.Model, upstreamPath)
		latency := time.Since(start).Milliseconds()

		// 记录日志（异步）
		logEntry := &models.RequestLog{
			UserID:       user.ID,
			DisplayModel: meta.Model,
			Stream:       boolToInt(meta.Stream),
			LatencyMS:    latency,
			CreatedAt:    models.Now(),
		}
		if res != nil {
			logEntry.ChannelID = res.Channel.ID
			logEntry.KeyID = res.KeyID
			logEntry.UpstreamModel = res.Route.UpstreamModel
			logEntry.Status = successOrError(res.StatusCode)
			if logEntry.Status == "error" {
				logEntry.ErrorCode = http.StatusText(res.StatusCode)
			}
			parseUsage(res.Body, logEntry)
		} else {
			logEntry.Status = "error"
			logEntry.ErrorCode = "route_failed"
		}
		// 成本估算（读 settings 中 model_pricing）
		pricingStr, _ := a.GetSetting("model_pricing")
		if pricing, perr := quota.ParsePricing(pricingStr); perr == nil {
			logEntry.Cost = quota.EstimateCost(pricing, meta.Model,
				logEntry.PromptTokens, logEntry.CompletionTokens)
		}
		bus.Write(logEntry)
		if logEntry.Status == "success" {
			_ = store.AddUserQuota(a.DB, user.ID, logEntry.Cost)
		}
		_ = store.TouchChannelKey(a.DB, resKeyIDForLog(res))

		if ferr != nil {
			code, msg := mapRouteError(ferr)
			c.JSON(code, gin.H{"error": gin.H{"message": msg, "type": "upstream_error"}})
			return
		}
		c.Data(res.StatusCode, contentType(res), res.Body)
	}
}

// parseUsage 从上游响应体解析 usage，并写回日志字段。
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

func successOrError(code int) string {
	if code >= 200 && code < 400 {
		return "success"
	}
	return "error"
}

func mapRouteError(err error) (int, string) {
	if err == router.ErrNoCandidate {
		return http.StatusNotFound, "模型没有可用路由/渠道"
	}
	return http.StatusBadGateway, err.Error()
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

func resKeyIDForLog(res *router.Result) int64 {
	if res == nil {
		return 0
	}
	return res.KeyID
}
