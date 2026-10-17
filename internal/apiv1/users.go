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

	g.GET("/:id/token", adminGetUserToken(a))
	g.PUT("/:id/token", adminUpsertUserToken(a))
	g.POST("/:id/token/rotate", adminRotateUserToken(a))
	g.DELETE("/:id/token", adminDeleteUserToken(a))
	g.GET("/:id/overview", adminUserOverview(a))
}

type userView struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	Remark         string  `json:"remark"`
	Username       string  `json:"username"`
	QuotaLimit     float64 `json:"quota_limit"`
	QuotaUsed      float64 `json:"quota_used"`
	Status         int     `json:"status"`
	HasToken       bool    `json:"has_token"`
	TokenTail      string  `json:"token_tail"`
	Token          string  `json:"token,omitempty"` // 仅创建时返回一次的明文
	TokenExpiresAt int64   `json:"token_expires_at"`
	TokenLastUsed  int64   `json:"token_last_used_at"`
	// 共享池账本
	CallsSpent      int64   `json:"calls_spent"`
	CallsEarned     int64   `json:"calls_earned"`
	CallsBalance    int64   `json:"calls_balance"`
	InSpent         int64   `json:"in_spent"`
	OutSpent        int64   `json:"out_spent"`
	InServed        int64   `json:"in_served"`
	OutServed       int64   `json:"out_served"`
	CreditSpent     float64 `json:"credit_spent"`
	CreditEarned    float64 `json:"credit_earned"`
	CreditBalance   float64 `json:"credit_balance"`
	DebtFloorCalls  int64   `json:"debt_floor_calls"`
	DebtFloorCredit float64 `json:"debt_floor_credit"`
	LastLoginAt     int64   `json:"last_login_at"`
	CreatedAt       int64   `json:"created_at"`
}

func toUserView(a *app.App, u *models.User) userView {
	return userView{
		ID: u.ID, Name: u.Name, Remark: u.Remark, Username: u.Username,
		QuotaLimit: u.QuotaLimit, QuotaUsed: u.QuotaUsed, Status: u.Status,
		HasToken: u.TokenEnc != "", TokenTail: a.KeyTail(u.TokenEnc), TokenExpiresAt: u.TokenExpiresAt, TokenLastUsed: u.TokenLastUsed,
		CallsSpent: u.CallsSpent, CallsEarned: u.CallsEarned, CallsBalance: u.CallsEarned - u.CallsSpent + u.BaseCalls,
		InSpent: u.InSpent, OutSpent: u.OutSpent, InServed: u.InServed, OutServed: u.OutServed,
		CreditSpent: u.CreditSpent, CreditEarned: u.CreditEarned, CreditBalance: u.CreditEarned - u.CreditSpent + u.BaseCredit,
		DebtFloorCalls: u.DebtFloorCalls, DebtFloorCredit: u.DebtFloorCredit,
		LastLoginAt: u.LastLoginAt, CreatedAt: u.CreatedAt,
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
			Name            string   `json:"name"`
			Remark          string   `json:"remark"`
			Username        string   `json:"username"`
			Password        string   `json:"password"`
			QuotaLimit      float64  `json:"quota_limit"`
			Status          *int     `json:"status"`
			DebtFloorCalls  *int64   `json:"debt_floor_calls"`
			DebtFloorCredit *float64 `json:"debt_floor_credit"`
		}
		if !decode(c, &b) {
			return
		}
		username := strings.ToLower(strings.TrimSpace(b.Username))
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
		// 遗留字段 token_hash 为 NOT NULL UNIQUE：先放占位哈希，随后写入真实 API_KEY 哈希
		placeholder, err := app.GenSecret(24)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		u := &models.User{
			Name: b.Name, Remark: b.Remark, Username: username, PasswordHash: pwHash,
			TokenHash: app.HashToken("placeholder:" + placeholder), QuotaLimit: b.QuotaLimit, Status: status,
			DebtFloorCalls: store.DefaultDebtFloorCalls, DebtFloorCredit: store.DefaultDebtFloorCredit,
		}
		if b.DebtFloorCalls != nil {
			u.DebtFloorCalls = *b.DebtFloorCalls
		}
		if b.DebtFloorCredit != nil {
			u.DebtFloorCredit = *b.DebtFloorCredit
		}
		id, err := store.CreateUser(a.DB, u)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		u.ID = id
		// 建号即生成消费 API_KEY，管理员可一次性复制转交
		plain, enc, terr := a.NewGatewayToken()
		if terr == nil {
			if err := store.SetUserToken(a.DB, id, app.HashToken(plain), enc, 0); err == nil {
				u.TokenEnc = enc
			} else {
				plain = ""
			}
		} else {
			plain = ""
		}
		if got, err := store.GetUser(a.DB, id); err == nil && got != nil {
			u = got
		}
		v := toUserView(a, u)
		v.Token = plain // 仅创建时返回一次
		c.JSON(http.StatusOK, gin.H{"data": v})
	}
}

func updateUser(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		var b struct {
			Name            *string  `json:"name"`
			Remark          *string  `json:"remark"`
			Username        *string  `json:"username"`
			Password        *string  `json:"password"`
			QuotaLimit      *float64 `json:"quota_limit"`
			Status          *int     `json:"status"`
			DebtFloorCalls  *int64   `json:"debt_floor_calls"`
			DebtFloorCredit *float64 `json:"debt_floor_credit"`
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
		if b.DebtFloorCalls != nil {
			cur.DebtFloorCalls = *b.DebtFloorCalls
		}
		if b.DebtFloorCredit != nil {
			cur.DebtFloorCredit = *b.DebtFloorCredit
		}
		newUsername := strings.ToLower(strings.TrimSpace(cur.Username))
		if b.Username != nil {
			newUsername = strings.ToLower(strings.TrimSpace(*b.Username))
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
		today, _ := store.UserUsageSummary(a.DB, id, dayStartOffset(0))
		total, _ := store.UserUsageSummary(a.DB, id, 0)
		byModel, _ := store.UserUsageByModel(a.DB, id, dayStartOffset(29), 0)
		c.JSON(http.StatusOK, gin.H{"data": gin.H{
			"user":     toUserView(a, u),
			"keys":     keyViews,
			"token":    tokenViewOf(a, u, tokenPlain(a, u.TokenEnc)),
			"today":    today,
			"total":    total,
			"by_model": byModel,
		}})
	}
}

// ---- 令牌视图（users 表内，一人一 token） ----

// tokenView 是用户唯一消费令牌的对外视图。
type tokenView struct {
	Token      string `json:"token,omitempty"` // 明文（可随时回显）
	TokenTail  string `json:"token_tail"`
	HasToken   bool   `json:"has_token"`
	ExpiresAt  int64  `json:"expires_at"`
	LastUsedAt int64  `json:"last_used_at"`
}

func tokenViewOf(a *app.App, u *models.User, plain string) tokenView {
	return tokenView{Token: plain, TokenTail: a.KeyTail(u.TokenEnc), HasToken: u.TokenEnc != "", ExpiresAt: u.TokenExpiresAt, LastUsedAt: u.TokenLastUsed}
}

func adminGetUserToken(a *app.App) gin.HandlerFunc {
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
		c.JSON(http.StatusOK, gin.H{"data": tokenViewOf(a, u, tokenPlain(a, u.TokenEnc))})
	}
}

// adminUpsertUserToken 创建或更新某用户令牌（不轮换密钥）。
func adminUpsertUserToken(a *app.App) gin.HandlerFunc {
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
		var b struct {
			ExpiresInDays *int `json:"expires_in_days"`
		}
		if !decode(c, &b) {
			return
		}
		if u.TokenEnc == "" {
			plain, enc, err := a.NewGatewayToken()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			exp := resolveExpiry(b.ExpiresInDays, 0)
			if err := store.SetUserToken(a.DB, id, app.HashToken(plain), enc, exp); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			u.TokenEnc, u.TokenExpiresAt = enc, exp
			c.JSON(http.StatusOK, gin.H{"data": tokenViewOf(a, u, plain)})
			return
		}
		exp := resolveExpiry(b.ExpiresInDays, u.TokenExpiresAt)
		if err := store.SetUserToken(a.DB, id, u.TokenHash, u.TokenEnc, exp); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		u.TokenExpiresAt = exp
		c.JSON(http.StatusOK, gin.H{"data": tokenViewOf(a, u, tokenPlain(a, u.TokenEnc))})
	}
}

// adminRotateUserToken 轮换某用户令牌密钥，返回新明文。
func adminRotateUserToken(a *app.App) gin.HandlerFunc {
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
		plain, enc, err := a.NewGatewayToken()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := store.SetUserToken(a.DB, id, app.HashToken(plain), enc, u.TokenExpiresAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		u.TokenEnc = enc
		c.JSON(http.StatusOK, gin.H{"data": tokenViewOf(a, u, plain)})
	}
}

func adminDeleteUserToken(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		if u, err := store.GetUser(a.DB, id); err != nil || u == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		placeholder, err := app.GenSecret(24)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := store.SetUserToken(a.DB, id, app.HashToken("revoked:"+placeholder), "", 0); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": "deleted"})
	}
}
