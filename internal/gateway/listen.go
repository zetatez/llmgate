package gateway

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"llmgate/internal/app"
	"llmgate/internal/logbus"
	"llmgate/internal/models"
	"llmgate/internal/router"
	"llmgate/internal/store"
)

// handleDeepgramListenHTTP 代理 Deepgram 预录音频：POST /v1/listen。
func handleDeepgramListenHTTP(a *app.App, bus *logbus.Bus, pen *router.Penalizer) gin.HandlerFunc {
	return func(c *gin.Context) {
		model, ok := resolveRequestModel(c)
		if !ok {
			c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"message": "model does not match session", "type": "invalid_request_error"}})
			return
		}
		if model == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "missing model query parameter", "type": "invalid_request_error"}})
			return
		}
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxAudioBody+1))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "read audio body failed", "type": "invalid_request_error"}})
			return
		}
		if int64(len(body)) > maxAudioBody {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": gin.H{"message": "request body too large", "type": "invalid_request_error"}})
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

		ct := c.GetHeader("Content-Type")
		if ct == "" {
			ct = "application/octet-stream"
		}
		logEntry := &models.RequestLog{UserID: user.ID, DisplayModel: model, CreatedAt: models.Now()}
		start := time.Now()
		res, ferr := rt.ForwardRawCandidates(c.Request.Context(), cands, body, model, "/v1/listen", http.MethodPost, ct, buildPassHeaders(c.Request.Header), false,
			func(upstreamModel string) ([]byte, string, string, error) {
				q := upstreamQuery(c)
				q.Set("model", upstreamModel)
				return body, ct, "/v1/listen?" + q.Encode(), nil
			})
		logEntry.LatencyMS = time.Since(start).Milliseconds()

		if res != nil {
			logEntry.ChannelID = res.Channel.ID
			logEntry.KeyID = res.KeyID
			logEntry.KeyOwnerID = res.KeyOwnerID
			logEntry.UpstreamModel = res.Route.UpstreamModel
			logEntry.Status = successOrError(res.StatusCode)
			if logEntry.Status == "success" {
				var dg struct {
					Metadata struct {
						Duration float64 `json:"duration"`
					} `json:"metadata"`
				}
				dur := 0.0
				if json.Unmarshal(res.Body, &dg) == nil {
					dur = dg.Metadata.Duration
				}
				if dur <= 0 {
					// 上游未返回时长时用 WAV 头估算，避免免费转写。
					dur = wavDuration(body)
				}
				if dur > 0 {
					tokens := int64(math.Ceil(dur * audioTokensPerSecond))
					logEntry.PromptTokens, logEntry.TotalTokens = tokens, tokens
				}
			} else {
				logEntry.ErrorCode = oneLine(string(res.Body), 200)
			}
			finalizeLog(a, bus, logEntry, res.Route.PriceInput, res.Route.PriceOutput)
			if logEntry.Status == "success" {
				_ = store.ApplyUsage(a.DB, user.ID, logEntry.KeyOwnerID, logEntry.PromptTokens, logEntry.CompletionTokens, logEntry.Cost)
			}
			resCT := res.ContentType
			if resCT == "" {
				resCT = "application/json"
			}
			c.Data(res.StatusCode, resCT, res.Body)
			return
		}

		logEntry.Status = "error"
		logEntry.ErrorCode = oneLine(ferr.Error(), 200)
		finalizeLog(a, bus, logEntry, nil, nil)
		code, msg := mapRouteError(ferr, model)
		if retryAfter := retryAfterFor(code); retryAfter != "" {
			c.Header("Retry-After", retryAfter)
		}
		c.JSON(code, gin.H{"error": gin.H{"message": oneLine(msg, 500), "type": "upstream_error"}})
	}
}

// handleDeepgramListen 代理 Deepgram 兼容的实时转写 WebSocket：GET /v1/listen。
// 查询参数（model/language/encoding/...）原样透传，鉴权使用 Deepgram Token 方案。
func handleDeepgramListen(a *app.App, bus *logbus.Bus, pen *router.Penalizer) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !websocket.IsWebSocketUpgrade(c.Request) {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "websocket upgrade required", "type": "invalid_request_error"}})
			return
		}
		model, ok := resolveRequestModel(c)
		if !ok {
			c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"message": "model does not match session", "type": "invalid_request_error"}})
			return
		}
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

		upstream, cand, key, lastErr := dialModelWS(c, rt, cands, model, "/v1/listen", "Token", upstreamQuery(c))
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
		// aggregate=1 时在透传上游消息的同时附加网关累积转写（gateway_transcript / gateway_final）。
		if v := strings.ToLower(strings.TrimSpace(c.Query("aggregate"))); v == "1" || v == "true" || v == "yes" {
			proxyDeepgramAggregated(client, upstream)
		} else {
			proxyWS(client, upstream)
		}
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
