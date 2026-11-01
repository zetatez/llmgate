package gateway

import (
	"database/sql"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"llmgate/internal/app"
	"llmgate/internal/logbus"
	"llmgate/internal/models"
	"llmgate/internal/store"
)

const (
	ctxSessionModelKey = "llmgate.session_model"
	ephemeralTTL       = 10 * time.Minute
)

type ephemeralSession struct {
	userID  int64
	model   string
	kind    string
	expires time.Time
}

var (
	ephMu       sync.Mutex
	ephSessions = map[string]ephemeralSession{}
)

// createEphemeralSession 本地签发短时 WebSocket 会话令牌（ek_...），不依赖上游。
func createEphemeralSession(userID int64, model, kind string) (id, secret string, expires int64, err error) {
	rawID, err := app.GenSecret(12)
	if err != nil {
		return "", "", 0, err
	}
	rawSecret, err := app.GenSecret(32)
	if err != nil {
		return "", "", 0, err
	}
	exp := time.Now().Add(ephemeralTTL)
	now := time.Now()
	ephMu.Lock()
	for k, s := range ephSessions {
		if now.After(s.expires) {
			delete(ephSessions, k)
		}
	}
	secret = "ek_" + rawSecret
	ephSessions[secret] = ephemeralSession{userID: userID, model: model, kind: kind, expires: exp}
	ephMu.Unlock()
	return "sess_" + rawID, secret, exp.Unix(), nil
}

func resolveEphemeralSession(db *sql.DB, token string) (*models.User, string, bool) {
	ephMu.Lock()
	s, ok := ephSessions[token]
	if ok && time.Now().After(s.expires) {
		delete(ephSessions, token)
		ok = false
	}
	ephMu.Unlock()
	if !ok {
		return nil, "", false
	}
	user, err := store.GetUser(db, s.userID)
	if err != nil || user == nil || user.Status != 1 {
		return nil, "", false
	}
	return user, s.model, true
}

// resolveRequestModel 取 query model；若请求携带本地会话令牌，则回落到会话绑定的模型并校验一致性。
func resolveRequestModel(c *gin.Context) (string, bool) {
	model := c.Query("model")
	bound := c.GetString(ctxSessionModelKey)
	if model == "" {
		model = bound
	}
	if bound != "" && c.Query("model") != "" && model != bound {
		return "", false
	}
	return model, true
}

// handleRealtimeSession 本地签发会话（不转发上游），供浏览器/客户端以 ek_ 令牌连接 /v1/realtime 或 /v1/listen。
func handleRealtimeSession(a *app.App, bus *logbus.Bus) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Model            string `json:"model"`
			InputAudioFormat string `json:"input_audio_format"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Model) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "missing valid model field", "type": "invalid_request_error"}})
			return
		}
		userAny, _ := c.Get(ctxUserKey)
		user := userAny.(*models.User)
		kind := "realtime.session"
		if strings.Contains(c.FullPath(), "transcription_sessions") {
			kind = "realtime.transcription_session"
		}
		id, secret, exp, err := createEphemeralSession(user.ID, body.Model, kind)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "type": "internal_error"}})
			return
		}
		data := gin.H{
			"id":            id,
			"object":        kind,
			"model":         body.Model,
			"expires_at":    exp,
			"client_secret": gin.H{"value": secret, "expires_at": exp},
		}
		if body.InputAudioFormat != "" {
			data["input_audio_format"] = body.InputAudioFormat
		}
		bus.Write(&models.RequestLog{
			UserID:       user.ID,
			DisplayModel: body.Model,
			Status:       "success",
			CreatedAt:    models.Now(),
		})
		c.JSON(http.StatusOK, data)
	}
}
