package apiv1

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"llmgate/internal/app"
	"llmgate/internal/models"
	"llmgate/internal/store"
)

// RegisterUserRoutes 挂载用户账号管理（管理员）。
func RegisterUserRoutes(g *gin.RouterGroup, a *app.App) {
	g.GET("", listUsers(a))
	g.POST("", createUser(a))
	g.PUT("/:id", updateUser(a))
	g.DELETE("/:id", deleteUser(a))

	g.GET("/:id/tokens", adminListUserTokens(a))
	g.POST("/:id/tokens", adminCreateUserToken(a))
	g.DELETE("/:id/tokens/:tokenID", adminDeleteUserToken(a))
	g.GET("/:id/overview", adminUserOverview(a))
}

type userView struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Remark      string  `json:"remark"`
	Username    string  `json:"username"`
	QuotaLimit  float64 `json:"quota_limit"`
	QuotaUsed   float64 `json:"quota_used"`
	Status      int     `json:"status"`
	TokenCount  int     `json:"token_count"`
	LastLoginAt int64   `json:"last_login_at"`
	CreatedAt   int64   `json:"created_at"`
}

func toUserView(a *app.App, u *models.User) userView {
	n := 0
	if list, err := store.ListUserTokens(a.DB, u.ID); err == nil {
		n = len(list)
	}
	return userView{
		ID: u.ID, Name: u.Name, Remark: u.Remark, Username: u.Username,
		QuotaLimit: u.QuotaLimit, QuotaUsed: u.QuotaUsed, Status: u.Status,
		TokenCount: n, LastLoginAt: u.LastLoginAt, CreatedAt: u.CreatedAt,
	}
}

func listUsers(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		list, err := store.ListUsers(a.DB)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		out := make([]userView, 0, len(list))
		for _, u := range list {
			out = append(out, toUserView(a, u))
		}
		c.JSON(http.StatusOK, gin.H{"data": out})
	}
}

func createUser(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		var b struct {
			Name       string  `json:"name"`
			Remark     string  `json:"remark"`
			Username   string  `json:"username"`
			Password   string  `json:"password"`
			QuotaLimit float64 `json:"quota_limit"`
			Status     *int    `json:"status"`
		}
		if !decode(c, &b) {
			return
		}
		username := strings.TrimSpace(b.Username)
		if username == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "username is required"})
			return
		}
		if len(b.Password) < 6 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "password must be at least 6 characters"})
			return
		}
		if b.Name == "" {
			b.Name = username
		}
		if existing, err := store.GetUserByUsername(a.DB, username); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		} else if existing != nil {
			c.JSON(http.StatusConflict, gin.H{"error": "username already exists"})
			return
		}
		pwHash, err := HashPassword(b.Password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		status := 1
		if b.Status != nil {
			status = *b.Status
		}
		// 遗留字段 token_hash 为 NOT NULL UNIQUE：放一个不可用的占位哈希，鉴权已改用 user_tokens
		placeholder, err := app.GenSecret(24)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		u := &models.User{
			Name: b.Name, Remark: b.Remark, Username: username, PasswordHash: pwHash,
			TokenHash: app.HashToken("placeholder:" + placeholder), QuotaLimit: b.QuotaLimit, Status: status,
		}
		id, err := store.CreateUser(a.DB, u)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		u.ID = id
		if got, err := store.GetUser(a.DB, id); err == nil && got != nil {
			u = got
		}
		c.JSON(http.StatusOK, gin.H{"data": toUserView(a, u)})
	}
}

func updateUser(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		var b struct {
			Name       *string  `json:"name"`
			Remark     *string  `json:"remark"`
			Username   *string  `json:"username"`
			Password   *string  `json:"password"`
			QuotaLimit *float64 `json:"quota_limit"`
			Status     *int     `json:"status"`
		}
		if !decode(c, &b) {
			return
		}
		cur, err := store.GetUser(a.DB, id)
		if err != nil || cur == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		if b.Name != nil {
			cur.Name = *b.Name
		}
		if b.Remark != nil {
			cur.Remark = *b.Remark
		}
		if b.QuotaLimit != nil {
			cur.QuotaLimit = *b.QuotaLimit
		}
		newUsername := strings.TrimSpace(cur.Username)
		if b.Username != nil {
			newUsername = strings.TrimSpace(*b.Username)
			if newUsername == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "username is required"})
				return
			}
			if newUsername != cur.Username {
				if existing, err := store.GetUserByUsername(a.DB, newUsername); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				} else if existing != nil && existing.ID != cur.ID {
					c.JSON(http.StatusConflict, gin.H{"error": "username already exists"})
					return
				}
			}
		}
		cur.Username = newUsername
		pwChanged := false
		if b.Password != nil && *b.Password != "" {
			if len(*b.Password) < 6 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "password must be at least 6 characters"})
				return
			}
			hash, err := HashPassword(*b.Password)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			cur.PasswordHash = hash
			pwChanged = true
		}
		disabled := false
		if b.Status != nil {
			if *b.Status != 1 && cur.Status == 1 {
				disabled = true
			}
			cur.Status = *b.Status
		}
		if err := store.UpdateUser(a.DB, cur); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// 改密或禁用后强制该用户下线
		if pwChanged || disabled {
			_ = store.DeleteUserSessionsForUser(a.DB, cur.ID)
		}
		c.JSON(http.StatusOK, gin.H{"data": toUserView(a, cur)})
	}
}

func deleteUser(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		// 归属该用户的渠道 Key 一并移除（避免遗留无主数据）
		if _, err := a.DB.Exec(`DELETE FROM channel_keys WHERE user_id=?`, id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := store.DeleteUser(a.DB, id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": "deleted"})
	}
}

// adminUserOverview 管理员查看某用户的聚合信息：Key、令牌、用量。
func adminUserOverview(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		u, err := store.GetUser(a.DB, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if u == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		names, err := channelNameMap(a)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		keys, err := store.ListKeysByUser(a.DB, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		keyViews := make([]userKeyView, 0, len(keys))
		for _, k := range keys {
			keyViews = append(keyViews, userKeyView{
				ID: k.ID, ChannelID: k.ChannelID, ChannelName: names[k.ChannelID],
				Name: k.Name, Source: k.Source, Remark: k.Remark,
				Masked: "sk-****", KeyTail: a.KeyTail(k.APIKeyEnc), Enabled: k.Enabled,
				LastUsedAt: k.LastUsedAt, CreatedAt: k.CreatedAt,
			})
		}
		tokens, err := store.ListUserTokens(a.DB, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		tokenViews := make([]adminTokenView, 0, len(tokens))
		for _, t := range tokens {
			tokenViews = append(tokenViews, adminTokenView{ID: t.ID, UserID: t.UserID, Name: t.Name,
				TokenTail: a.KeyTail(t.TokenEnc), Enabled: t.Enabled, ExpiresAt: t.ExpiresAt, LastUsedAt: t.LastUsedAt, CreatedAt: t.CreatedAt})
		}
		today, _ := store.UserUsageSummary(a.DB, id, dayStartOffset(0))
		total, _ := store.UserUsageSummary(a.DB, id, 0)
		byModel, _ := store.UserUsageByModel(a.DB, id, dayStartOffset(29), 0)
		c.JSON(http.StatusOK, gin.H{"data": gin.H{
			"user":     toUserView(a, u),
			"keys":     keyViews,
			"tokens":   tokenViews,
			"today":    today,
			"total":    total,
			"by_model": byModel,
		}})
	}
}

// ---- 管理员管理某用户的消费令牌 ----

type adminTokenView struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"user_id"`
	Name       string `json:"name"`
	Token      string `json:"token,omitempty"` // 仅创建时返回一次
	TokenTail  string `json:"token_tail"`
	Enabled    int    `json:"enabled"`
	ExpiresAt  int64  `json:"expires_at"`
	LastUsedAt int64  `json:"last_used_at"`
	CreatedAt  int64  `json:"created_at"`
}

func adminListUserTokens(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		if u, err := store.GetUser(a.DB, id); err != nil || u == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		list, err := store.ListUserTokens(a.DB, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		out := make([]adminTokenView, 0, len(list))
		for _, t := range list {
			out = append(out, adminTokenView{ID: t.ID, UserID: t.UserID, Name: t.Name,
				TokenTail: a.KeyTail(t.TokenEnc), Enabled: t.Enabled, ExpiresAt: t.ExpiresAt, LastUsedAt: t.LastUsedAt, CreatedAt: t.CreatedAt})
		}
		c.JSON(http.StatusOK, gin.H{"data": out})
	}
}

func adminCreateUserToken(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		if u, err := store.GetUser(a.DB, id); err != nil || u == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		var b struct {
			Name          string `json:"name"`
			ExpiresInDays *int   `json:"expires_in_days"`
		}
		if !decode(c, &b) {
			return
		}
		expiresAt := int64(0)
		if b.ExpiresInDays != nil && *b.ExpiresInDays > 0 {
			expiresAt = models.Now() + int64(*b.ExpiresInDays)*86400
		}
		plain, enc, err := a.NewGatewayToken()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		t := &models.UserToken{UserID: id, Name: strings.TrimSpace(b.Name), TokenHash: app.HashToken(plain), TokenEnc: enc, Enabled: 1, ExpiresAt: expiresAt}
		tid, err := store.CreateUserToken(a.DB, t)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		t.ID = tid
		c.JSON(http.StatusOK, gin.H{"data": adminTokenView{ID: t.ID, UserID: t.UserID, Name: t.Name,
			Token: plain, TokenTail: a.KeyTail(enc), Enabled: 1, ExpiresAt: expiresAt, CreatedAt: t.CreatedAt}})
	}
}

func adminDeleteUserToken(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := parseID(c, "id")
		if !ok {
			return
		}
		tokenID, ok := parseID(c, "tokenID")
		if !ok {
			return
		}
		t, err := store.GetUserToken(a.DB, tokenID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if t == nil || t.UserID != userID {
			c.JSON(http.StatusNotFound, gin.H{"error": "token not found"})
			return
		}
		if err := store.DeleteUserToken(a.DB, tokenID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": "deleted"})
	}
}
