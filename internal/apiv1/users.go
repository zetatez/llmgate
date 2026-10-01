package apiv1

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"llmgate/internal/app"
	"llmgate/internal/models"
	"llmgate/internal/store"
)

// RegisterUserRoutes 挂载用户管理。
func RegisterUserRoutes(g *gin.RouterGroup, a *app.App) {
	g.GET("", listUsers(a))
	g.POST("", createUser(a))
	g.PUT("/:id", updateUser(a))
	g.DELETE("/:id", deleteUser(a))
	g.GET("/:id/token", getUserToken(a))
	g.POST("/:id/token", resetUserToken(a))
}

type userView struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Remark     string  `json:"remark"`
	Token      string  `json:"token"`      // 仅创建/重置时返回一次
	TokenTail  string  `json:"token_tail"` // 令牌末4位（辨认）
	QuotaLimit float64 `json:"quota_limit"`
	QuotaUsed  float64 `json:"quota_used"`
	Status     int     `json:"status"`
	CreatedAt  int64   `json:"created_at"`
}

func toUserView(a *app.App, u *models.User, rawToken string) userView {
	v := userView{
		ID: u.ID, Name: u.Name, Remark: u.Remark, Token: rawToken,
		QuotaLimit: u.QuotaLimit, QuotaUsed: u.QuotaUsed, Status: u.Status, CreatedAt: u.CreatedAt,
	}
	v.TokenTail = a.TokenTail(u)
	return v
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
			out = append(out, toUserView(a, u, ""))
		}
		c.JSON(http.StatusOK, gin.H{"data": out})
	}
}

func createUser(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		var b struct {
			Name       string  `json:"name"`
			Remark     string  `json:"remark"`
			QuotaLimit float64 `json:"quota_limit"`
			Status     *int    `json:"status"`
		}
		if !decode(c, &b) {
			return
		}
		if b.Name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name 必填"})
			return
		}
		token, err := app.GenSecret(24) // 48 hex chars
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		plain := "sk-" + token
		enc, err := a.Secret.Encrypt(plain)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		status := 1
		if b.Status != nil {
			status = *b.Status
		}
		u := &models.User{
			Name: b.Name, Remark: b.Remark, QuotaLimit: b.QuotaLimit,
			TokenHash: app.HashToken(plain), TokenEnc: enc, Status: status,
		}
		id, err := store.CreateUser(a.DB, u)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		u.ID = id
		c.JSON(http.StatusOK, gin.H{"data": toUserView(a, u, plain)})
	}
}

func updateUser(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		var b struct {
			Name       string   `json:"name"`
			Remark     string   `json:"remark"`
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
		if b.Name != "" {
			cur.Name = b.Name
		}
		cur.Remark = b.Remark
		if b.QuotaLimit != nil {
			cur.QuotaLimit = *b.QuotaLimit
		}
		if b.Status != nil {
			cur.Status = *b.Status
		}
		if err := store.UpdateUser(a.DB, cur); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": toUserView(a, cur, "")})
	}
}

func deleteUser(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		if err := store.DeleteUser(a.DB, id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": "deleted"})
	}
}

func resetUserToken(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		token, err := app.GenSecret(24)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		plain := "sk-" + token
		enc, err := a.Secret.Encrypt(plain)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := store.SetUserToken(a.DB, id, app.HashToken(plain), enc); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"token": plain}})
	}
}

// getUserToken 回显用户令牌（AES 解密），供管理员随时复制。
// 旧格式用户已由迁移清理，不存在无 token_enc 的用户。
func getUserToken(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		u, err := store.GetUser(a.DB, id)
		if err != nil || u == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		plain, err := a.Secret.Decrypt(u.TokenEnc)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "令牌解密失败：加密种子可能已更换"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"token": plain}})
	}
}
