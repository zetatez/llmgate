package gateway

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/url"
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

// realtimeURL 把渠道 BaseURL 转成 WebSocket 地址，并把 model 换成上游模型名。
func realtimeURL(baseURL, upstreamModel string, q url.Values) (string, error) {
	return wsEndpointURL(baseURL, "/v1/realtime", upstreamModel, q)
}

func wsEndpointURL(baseURL, endpoint, upstreamModel string, q url.Values) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http", "":
		u.Scheme = "ws"
	}
	basePath := strings.TrimSuffix(u.Path, "/")
	basePath = strings.TrimSuffix(basePath, "/v1")
	u.Path = basePath + endpoint

	out := url.Values{}
	for k, vs := range q {
		out[k] = append([]string(nil), vs...)
	}
	if upstreamModel != "" {
		out.Set("model", upstreamModel)
	}
	u.RawQuery = out.Encode()
	return u.String(), nil
}

// handleRealtime 处理 GET /v1/realtime（OpenAI Realtime WebSocket 代理）。
func handleRealtime(a *app.App, bus *logbus.Bus, pen *router.Penalizer) gin.HandlerFunc {
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
		cands, err := rt.Candidates(c.Request.Context(), model)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "type": "internal_error"}})
			return
		}
		if len(cands) == 0 {
			code, msg := mapRouteError(router.ErrNoCandidate, model)
			c.JSON(code, gin.H{"error": gin.H{"message": msg, "type": "upstream_error"}})
			return
		}

		upstream, cand, key, lastErr := dialModelWS(c, rt, cands, model, "/v1/realtime", "", c.Request.URL.Query())
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

// dialModelWS 在候选中选路并建立上游 WebSocket；authScheme 为空时用 Bearer，否则用指定方案（如 Token）。
func dialModelWS(c *gin.Context, rt *router.Router, cands []router.Candidate, model, endpoint, authScheme string, q url.Values) (*websocket.Conn, *router.Candidate, *models.ChannelKey, error) {
	excluded := map[int64]bool{}
	failedKeys := map[int64]bool{}
	var lastErr error
	for i := 0; i < 5; i++ {
		cd, k, apiKey, serr := rt.Select(cands, model, excluded, failedKeys)
		if cd == nil {
			break
		}
		if serr != nil {
			lastErr = serr
			excluded[cd.Channel.ID] = true
			continue
		}
		upURL, uerr := wsEndpointURL(cd.Channel.BaseURL, endpoint, cd.Route.UpstreamModel, q)
		if uerr != nil {
			lastErr = uerr
			excluded[cd.Channel.ID] = true
			continue
		}
		header := http.Header{}
		if authScheme == "" {
			header.Set("Authorization", "Bearer "+apiKey)
		} else {
			header.Set("Authorization", authScheme+" "+apiKey)
		}
		var extra map[string]string
		if cd.Channel.ExtraHeaders != "" {
			_ = json.Unmarshal([]byte(cd.Channel.ExtraHeaders), &extra)
		}
		for hk, hv := range extra {
			header.Set(hk, hv)
		}
		conn, resp, derr := newWSDialer().DialContext(c.Request.Context(), upURL, header)
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		if derr != nil {
			lastErr = derr
			excluded[cd.Channel.ID] = true
			continue
		}
		return conn, cd, k, nil
	}
	return nil, nil, nil, lastErr
}

// handleRealtimeSession 代理 POST /v1/realtime/sessions 与 /v1/realtime/transcription_sessions。
// 会话创建不按调用计费，仅记录审计日志。
func handleRealtimeSession(a *app.App, bus *logbus.Bus, pen *router.Penalizer, upstreamPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxRequestBody+1))
		if err != nil || len(body) > maxRequestBody {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "invalid request body", "type": "invalid_request_error"}})
			return
		}
		var meta struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(body, &meta); err != nil || meta.Model == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "missing valid model field", "type": "invalid_request_error"}})
			return
		}
		userAny, _ := c.Get(ctxUserKey)
		user := userAny.(*models.User)
		rt := router.New(a, pen).SetUser(user.ID)
		logEntry := &models.RequestLog{UserID: user.ID, DisplayModel: meta.Model, CreatedAt: models.Now()}
		start := time.Now()
		res, ferr := rt.ForwardChat(c.Request.Context(), body, meta.Model, upstreamPath, buildPassHeaders(c.Request.Header))
		logEntry.LatencyMS = time.Since(start).Milliseconds()
		if res != nil {
			logEntry.ChannelID = res.Channel.ID
			logEntry.KeyID = res.KeyID
			logEntry.KeyOwnerID = res.KeyOwnerID
			logEntry.UpstreamModel = res.Route.UpstreamModel
			logEntry.Status = successOrError(res.StatusCode)
			if logEntry.Status != "success" {
				logEntry.ErrorCode = oneLine(string(res.Body), 200)
			}
			bus.Write(logEntry)
			resCT := res.ContentType
			if resCT == "" {
				resCT = "application/json"
			}
			c.Data(res.StatusCode, resCT, res.Body)
			return
		}
		logEntry.Status = "error"
		logEntry.ErrorCode = oneLine(ferr.Error(), 200)
		bus.Write(logEntry)
		code, msg := mapRouteError(ferr, meta.Model)
		if retryAfter := retryAfterFor(code); retryAfter != "" {
			c.Header("Retry-After", retryAfter)
		}
		c.JSON(code, gin.H{"error": gin.H{"message": oneLine(msg, 500), "type": "upstream_error"}})
	}
}
