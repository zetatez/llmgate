// Package apiv1 实现管理端 API /api/admin/*。
package apiv1

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"llmgate/internal/app"
	"llmgate/internal/models"
	"llmgate/internal/router"
	"llmgate/internal/store"
)

// adminSessionCookie 管理端 httpOnly 会话 Cookie 名。
const adminSessionCookie = "llmgate_admin_session"

// sessionTTL 会话有效期（秒），默认 30 天；main 启动时经 SetSessionTTL 覆盖。
var sessionTTL = int64(30 * 24 * 3600)

// SetSessionTTL 设置会话有效期（秒）；非正数保持默认。
func SetSessionTTL(seconds int) {
	if seconds > 0 {
		sessionTTL = int64(seconds)
	}
}

// Register 挂载管理端路由。
// 会话凭据为 httpOnly Cookie（服务端会话，可吊销）：
//   - POST /login  校验 Bearer 令牌并建立服务端会话
//   - POST /logout 删除服务端会话并清 Cookie
//   - authMiddleware 同时接受 Bearer 管理令牌（脚本/CLI）或会话 Cookie
//   - csrfOriginCheck 对非安全方法校验 Origin/Referer 与请求 Host 一致
func Register(admin *gin.RouterGroup, a *app.App, syncNow func() map[string]any, pens ...*router.Penalizer) {
	var pen *router.Penalizer
	if len(pens) > 0 {
		pen = pens[0]
	}
	admin.Use(csrfOriginCheck())
	admin.POST("/login", loginHandler(a))
	admin.POST("/logout", logoutHandler(a))
	admin.Use(authMiddleware(a))
	admin.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})
	if syncNow != nil {
		admin.POST("/model-sync", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"data": syncNow()})
		})
	}
	admin.POST("/penalties/reset", resetPenalties(a, pen))

	RegisterChannelRoutes(admin.Group("/channels"), a)
	RegisterModelRouteRoutes(admin.Group("/model-routes"), a)
	RegisterUserRoutes(admin.Group("/users"), a)
	RegisterLogRoutes(admin.Group("/logs"), a)
	RegisterSettingRoutes(admin.Group("/settings"), a)
	RegisterPricingRoutes(admin.Group("/pricing"), a)
	admin.GET("/members", listMembers(a))
	admin.GET("/gateway-endpoints", gatewayEndpoints(a))
}

// resetPenalties 清空内存熔断 + 渠道冷却，便于管理员手工恢复（不删除任何数据）。
func resetPenalties(a *app.App, pen *router.Penalizer) gin.HandlerFunc {
	return func(c *gin.Context) {
		if pen == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "penalizer unavailable"})
			return
		}
		pen.Reset()
		if _, err := a.DB.Exec(`UPDATE channels SET health_state='healthy', cooldown_until=0`); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": "ok"})
	}
}

// setAdminSession 种下（或清除，maxAge<0 时）httpOnly 会话 Cookie。
func setAdminSession(c *gin.Context, token string, maxAge int) {
	secure := isSecureRequest(c) // 直连 TLS 或反代 X-Forwarded-Proto=https
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(adminSessionCookie, token, maxAge, "/", "", secure, true)
}

// clearUserSessionFor 吊销并清除当前浏览器中的用户端会话（切换身份时调用）。
func clearUserSessionFor(a *app.App, c *gin.Context) {
	if sess, err := c.Cookie(userSessionCookie); err == nil && sess != "" {
		_ = store.DeleteUserSession(a.DB, app.HashToken(sess))
	}
	setUserSession(c, "", -1)
}

// clearAdminSessionFor 吊销并清除当前浏览器中的管理端会话（切换身份时调用）。
func clearAdminSessionFor(a *app.App, c *gin.Context) {
	if sess, err := c.Cookie(adminSessionCookie); err == nil && sess != "" {
		_ = store.DeleteAdminSession(a.DB, app.HashToken(sess))
	}
	setAdminSession(c, "", -1)
}

// loginHandler 校验管理令牌并建立服务端会话（带失败限速防爆破）。
// Cookie 只保存随机会话令牌（库内存哈希），不再保存 admin token 本身，登出即可吊销。
func loginHandler(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := "admin|" + remoteKey(c)
		if !authLimiter.allow(key) {
			tooManyLogins(c)
			return
		}
		token := bearerToken(c.GetHeader("Authorization"))
		hash, err := a.GetSetting("admin_token_hash")
		if err != nil || hash == "" || token == "" || !a.VerifyToken(token, hash) {
			authLimiter.fail(key)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid admin token"})
			return
		}
		authLimiter.reset(key)
		sess, err := app.GenSecret(32)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := store.CreateAdminSession(a.DB, app.HashToken(sess), models.Now()+sessionTTL); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		setAdminSession(c, sess, int(sessionTTL))
		// 同一浏览器切换身份：登录 admin 时吊销并清除用户端会话
		clearUserSessionFor(a, c)
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"message": "ok"}})
	}
}

// logoutHandler 删除服务端会话并清除 Cookie。
func logoutHandler(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sess, err := c.Cookie(adminSessionCookie); err == nil && sess != "" {
			_ = store.DeleteAdminSession(a.DB, app.HashToken(sess))
		}
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

// authMiddleware 校验 Authorization: Bearer <admin_token> 或服务端会话 Cookie。
func authMiddleware(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Bearer 管理令牌（脚本/CLI）优先
		if token := bearerToken(c.GetHeader("Authorization")); token != "" {
			hash, err := a.GetSetting("admin_token_hash")
			if err != nil || hash == "" || !a.VerifyToken(token, hash) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
				return
			}
			c.Next()
			return
		}
		// 服务端会话 Cookie
		sess, _ := c.Cookie(adminSessionCookie)
		if sess == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		ok, err := store.AdminSessionValid(a.DB, app.HashToken(sess))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}

// SessionProbe 汇总当前会话身份（供前端路由守卫一次请求判定），始终返回 200。
// role: admin / user / anonymous。
func SessionProbe(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		if sess, err := c.Cookie(adminSessionCookie); err == nil && sess != "" {
			if ok, _ := store.AdminSessionValid(a.DB, app.HashToken(sess)); ok {
				c.JSON(http.StatusOK, gin.H{"role": "admin"})
				return
			}
		}
		if sess, err := c.Cookie(userSessionCookie); err == nil && sess != "" {
			if u, _ := store.GetUserBySessionHash(a.DB, app.HashToken(sess)); u != nil {
				c.JSON(http.StatusOK, gin.H{"role": "user", "user": toPortalUser(u)})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"role": "anonymous"})
	}
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) > len(prefix) && header[:len(prefix)] == prefix {
		return header[len(prefix):]
	}
	return ""
}
