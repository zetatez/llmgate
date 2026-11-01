package gateway

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/websocket"

	"llmgate/internal/app"
	"llmgate/internal/logbus"
	"llmgate/internal/models"
	"llmgate/internal/router"
	"llmgate/internal/store"
)

// realtimeURL 把渠道 BaseURL 转成 WebSocket 地址，并把 model 换成上游模型名。
func realtimeURL(baseURL, upstreamModel string, q url.Values) (string, error) {
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
	u.Path = basePath + "/v1/realtime"

	out := url.Values{}
	for k, vs := range q {
		out[k] = append([]string(nil), vs...)
	}
	out.Set("model", upstreamModel)
	u.RawQuery = out.Encode()
	return u.String(), nil
}

// handleRealtime 处理 GET /v1/realtime（WebSocket 双向代理）。
// 计费：连接时长 × audioTokensPerSecond，按模型 token 单价结算；同时计 1 次调用。
func handleRealtime(a *app.App, bus *logbus.Bus, pen *router.Penalizer) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
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

		excluded := map[int64]bool{}
		failedKeys := map[int64]bool{}
		var upstream *websocket.Conn
		var cand *router.Candidate
		var key *models.ChannelKey
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
			upURL, uerr := realtimeURL(cd.Channel.BaseURL, cd.Route.UpstreamModel, c.Request.URL.Query())
			if uerr != nil {
				lastErr = uerr
				excluded[cd.Channel.ID] = true
				continue
			}
			cfg, cerr := websocket.NewConfig(upURL, "http://localhost")
			if cerr != nil {
				lastErr = cerr
				excluded[cd.Channel.ID] = true
				continue
			}
			cfg.Header.Set("Authorization", "Bearer "+apiKey)
			var extra map[string]string
			if cd.Channel.ExtraHeaders != "" {
				_ = json.Unmarshal([]byte(cd.Channel.ExtraHeaders), &extra)
			}
			for hk, hv := range extra {
				cfg.Header.Set(hk, hv)
			}
			conn, derr := websocket.DialConfig(cfg)
			if derr != nil {
				lastErr = derr
				excluded[cd.Channel.ID] = true
				continue
			}
			upstream, cand, key = conn, cd, k
			break
		}

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

		websocket.Server{
			Handler: func(client *websocket.Conn) {
				start := time.Now()
				defer client.Close()
				done := make(chan struct{})
				var once sync.Once
				finish := func() {
					once.Do(func() {
						_ = upstream.Close()
						_ = client.Close()
						close(done)
					})
				}
				go func() {
					_, _ = io.Copy(upstream, client)
					finish()
				}()
				go func() {
					_, _ = io.Copy(client, upstream)
					finish()
				}()
				<-done

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
			},
			// 已通过 gatewayAuth 校验 Bearer，跳过 WebSocket Origin 检查。
			Handshake: func(_ *websocket.Config, _ *http.Request) error { return nil },
		}.ServeHTTP(c.Writer, c.Request)
	}
}
