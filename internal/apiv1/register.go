// Package apiv1 实现管理端 API /api/admin/*。
package apiv1

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"llmgate/internal/app"
)

// adminSessionCookie 管理端 httpOnly 会话 Cookie 名。
const adminSessionCookie = "llmgate_admin_session"

// Register 挂载管理端路由。
// 会话凭据为 httpOnly Cookie（不再依赖前端 localStorage）：
//   - POST /login  校验 Bearer 令牌并种下 httpOnly 会话 Cookie
//   - POST /logout 清除会话 Cookie
//   - authMiddleware 同时接受 Bearer 头或会话 Cookie
//   - csrfOriginCheck 对非安全方法校验 Origin/Referer 与请求 Host 一致
func Register(admin *gin.RouterGroup, a *app.App, syncNow func() map[string]any) {
	admin.Use(csrfOriginCheck())
	admin.POST("/login", loginHandler(a))
	admin.POST("/logout", logoutHandler())
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

// setAdminSession 种下（或清除，maxAge<0 时）httpOnly 会话 Cookie。
func setAdminSession(c *gin.Context, token string, maxAge int) {
	secure := c.Request.TLS != nil // 直接 TLS 判定；反代 TLS 场景可在边缘层改为总是 true
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(adminSessionCookie, token, maxAge, "/", "", secure, true)
}

// loginHandler 校验管理令牌并建立 httpOnly 会话。
func loginHandler(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerToken(c.GetHeader("Authorization"))
		hash, err := a.GetSetting("admin_token_hash")
		if err != nil || hash == "" || token == "" || !a.VerifyToken(token, hash) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid admin token"})
			return
		}
		setAdminSession(c, token, 30*24*3600)
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"message": "ok"}})
	}
}

// logoutHandler 清除会话 Cookie。
func logoutHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		setAdminSession(c, "", -1)
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"message": "ok"}})
	}
}

// csrfOriginCheck 防 CSRF：非安全方法若带 Origin/Referer 头，其 Host 必须与请求 Host 一致。
// 浏览器跨站请求会带这些头且（SameSite=Lax）不携带 Cookie，双保险防跨站写操作。
func csrfOriginCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		for _, h := range []string{c.GetHeader("Origin"), c.GetHeader("Referer")} {
			if h == "" {
				continue
			}
			u, err := url.Parse(h)
			if err != nil || !strings.EqualFold(u.Host, c.Request.Host) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "cross-origin request blocked"})
				return
			}
		}
		c.Next()
	}
}

// authMiddleware 校验 Authorization: Bearer <admin_token> 或 httpOnly 会话 Cookie。
func authMiddleware(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerToken(c.GetHeader("Authorization"))
		if token == "" {
			token, _ = c.Cookie(adminSessionCookie)
		}
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
