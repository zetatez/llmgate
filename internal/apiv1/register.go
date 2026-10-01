// Package apiv1 实现管理端 API /api/admin/*。
package apiv1

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"llmgate/internal/app"
)

// Register 挂载管理端路由，全部走 AdminAuth 中间件。
// syncNow 由 main 注入：手动触发一次模型同步，返回执行摘要。
func Register(admin *gin.RouterGroup, a *app.App, syncNow func() map[string]any) {
	admin.Use(authMiddleware(a))
	admin.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})
	if syncNow != nil {
		admin.POST("/model-sync", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"data": syncNow()})
		})
	}

	RegisterChannelRoutes(admin.Group("/channels"), a)
	RegisterModelRouteRoutes(admin.Group("/model-routes"), a)
	RegisterUserRoutes(admin.Group("/users"), a)
	RegisterLogRoutes(admin.Group("/logs"), a)
	RegisterSettingRoutes(admin.Group("/settings"), a)
}

// authMiddleware 校验 Authorization: Bearer <admin_token>。
func authMiddleware(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerToken(c.GetHeader("Authorization"))
		hash, err := a.GetSetting("admin_token_hash")
		if err != nil || hash == "" || !a.VerifyToken(token, hash) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) > len(prefix) && header[:len(prefix)] == prefix {
		return header[len(prefix):]
	}
	return ""
}
