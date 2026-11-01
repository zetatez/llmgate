package gateway

import (
	"math"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"llmgate/internal/app"
	"llmgate/internal/logbus"
	"llmgate/internal/models"
	"llmgate/internal/router"
	"llmgate/internal/store"
)

// handleDeepgramListen 代理 Deepgram 兼容的实时转写 WebSocket：GET /v1/listen。
// 查询参数（model/language/encoding/...）原样透传，鉴权使用 Deepgram Token 方案。
func handleDeepgramListen(a *app.App, bus *logbus.Bus, pen *router.Penalizer) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !websocket.IsWebSocketUpgrade(c.Request) {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "websocket upgrade required", "type": "invalid_request_error"}})
			return
		}
		model := c.Query("model")
		if model == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "missing model query parameter", "type": "invalid_request_error"}})
			return
		}
		userAny, _ := c.Get(ctxUserKey)
		user := userAny.(*models.User)
		rt := router.New(a, pen).SetUser(user.ID)
		all, err := rt.Candidates(c.Request.Context(), model)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "type": "internal_error"}})
			return
		}
		cands := make([]router.Candidate, 0, len(all))
		for _, cd := range all {
			if cd.Channel.Adapter == "deepgram" {
				cands = append(cands, cd)
			}
		}
		if len(cands) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "no deepgram channel for model " + model, "type": "upstream_error"}})
			return
		}

		upstream, cand, key, lastErr := dialModelWS(c, rt, cands, model, "/v1/listen", "Token", c.Request.URL.Query())
		if upstream == nil {
			code, msg := mapRouteError(router.ErrChannelBusy, model)
			if lastErr != nil {
				msg = lastErr.Error()
				code = http.StatusBadGateway
			}
			c.JSON(code, gin.H{"error": gin.H{"message": oneLine(msg, 500), "type": "upstream_error"}})
			return
		}
		defer upstream.Close()

		client, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		defer client.Close()

		start := time.Now()
		proxyWS(client, upstream)
		dur := time.Since(start)
		tokens := int64(math.Ceil(dur.Seconds() * audioTokensPerSecond))
		le := &models.RequestLog{
			UserID:        user.ID,
			DisplayModel:  model,
			ChannelID:     cand.Channel.ID,
			KeyID:         key.ID,
			KeyOwnerID:    key.UserID,
			UpstreamModel: cand.Route.UpstreamModel,
			PromptTokens:  tokens,
			TotalTokens:   tokens,
			Status:        "success",
			Stream:        1,
			LatencyMS:     dur.Milliseconds(),
			CreatedAt:     models.Now(),
		}
		finalizeLog(a, bus, le, cand.Route.PriceInput, cand.Route.PriceOutput)
		_ = store.ApplyUsage(a.DB, user.ID, key.UserID, tokens, 0, le.Cost)
	}
}
