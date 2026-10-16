// 用户端（多用户）API：/api/user/*。
// 与管理员端（/api/admin/*，令牌鉴权）相互独立：用户以「用户名 + 密码」登录，
// 会话为 httpOnly Cookie（随机令牌，库内只存哈希）。
package apiv1

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"llmgate/internal/adapter"
	"llmgate/internal/app"
	"llmgate/internal/models"
	"llmgate/internal/store"
)

// userSessionCookie 用户端 httpOnly 会话 Cookie 名。
const userSessionCookie = "llmgate_user_session"

// ctxUserPortalKey 用户端中间件写入的当前用户（*models.User）。
const ctxUserPortalKey = "llmgate.portal.user"

// HashPassword 生成 bcrypt 密码哈希。
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

// CheckPassword 校验密码是否匹配哈希。
func CheckPassword(hash, password string) bool {
	if hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// RegisterUserPortal 挂载用户端路由。
func RegisterUserPortal(g *gin.RouterGroup, a *app.App) {
	g.Use(csrfOriginCheck())
	g.POST("/login", userLogin(a))
	g.POST("/logout", userLogout(a))

	authed := g.Group("")
	authed.Use(userAuthMiddleware(a))
	authed.GET("/me", userMe(a))
	authed.GET("/overview", userOverview(a))
	authed.PUT("/password", userChangePassword(a))

	// 渠道（套餐）/ 路由 / 共享统计
	RegisterUserResourceRoutes(authed, a)

	authed.GET("/keys", userListKeys(a))
	authed.POST("/keys", userAddKeys(a))
	authed.PUT("/keys/:id", userUpdateKey(a))
	authed.DELETE("/keys/:id", userDeleteKey(a))
	authed.POST("/keys/:id/test", userTestKey(a))

	authed.GET("/token", userGetToken(a))
	authed.PUT("/token", userUpsertToken(a))
	authed.POST("/token/rotate", userRotateToken(a))
	authed.DELETE("/token", userDeleteToken(a))

	authed.GET("/logs", userLogs(a))
	authed.GET("/logs/count", userLogsCount(a))
	authed.POST("/logout-all", userLogoutAll(a))
}

// setUserSession 种下（或清除，maxAge<0 时）用户端 httpOnly 会话 Cookie。
func setUserSession(c *gin.Context, token string, maxAge int) {
	secure := isSecureRequest(c)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(userSessionCookie, token, maxAge, "/", "", secure, true)
}

// userLogin 校验用户名+密码并建立会话（带失败限速防爆破）。
func userLogin(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		var b struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if !decode(c, &b) {
			return
		}
		username := strings.TrimSpace(b.Username)
		key := "user|" + remoteKey(c) + "|" + strings.ToLower(username)
		if !authLimiter.allow(key) {
			tooManyLogins(c)
			return
		}
		u, err := store.GetUserByUsername(a.DB, username)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if u == nil || u.Status != 1 || !CheckPassword(u.PasswordHash, b.Password) {
			authLimiter.fail(key)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
			return
		}
		authLimiter.reset(key)
		token, err := app.GenSecret(32)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := store.CreateUserSession(a.DB, u.ID, app.HashToken(token), models.Now()+sessionTTL); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		_ = store.TouchUserLogin(a.DB, u.ID)
		setUserSession(c, token, int(sessionTTL))
		v := toPortalUser(u)
		v.GatewayPrefix = a.Cfg.GatewayPrefix
		c.JSON(http.StatusOK, gin.H{"data": v})
	}
}

// userLogout 删除会话并清除 Cookie。
func userLogout(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token, err := c.Cookie(userSessionCookie); err == nil && token != "" {
			_ = store.DeleteUserSession(a.DB, app.HashToken(token))
		}
		setUserSession(c, "", -1)
		c.JSON(http.StatusOK, gin.H{"data": "ok"})
	}
}

// userAuthMiddleware 校验用户端会话 Cookie。
func userAuthMiddleware(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, _ := c.Cookie(userSessionCookie)
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		u, err := store.GetUserBySessionHash(a.DB, app.HashToken(token))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		if u == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Set(ctxUserPortalKey, u)
		c.Next()
	}
}

// currentUser 取当前登录用户（中间件已保证非 nil）。
func currentUser(c *gin.Context) *models.User {
	v, _ := c.Get(ctxUserPortalKey)
	u, _ := v.(*models.User)
	return u
}

// portalUserView 用户端展示的自身信息。
type portalUserView struct {
	ID            int64   `json:"id"`
	Username      string  `json:"username"`
	Name          string  `json:"name"`
	QuotaLimit    float64 `json:"quota_limit"`
	QuotaUsed     float64 `json:"quota_used"`
	Status        int     `json:"status"`
	LastLoginAt   int64   `json:"last_login_at"`
	GatewayPrefix string  `json:"gateway_prefix"` // 网关前缀（拼 base URL 用）
}

func toPortalUser(u *models.User) portalUserView {
	return portalUserView{ID: u.ID, Username: u.Username, Name: u.Name,
		QuotaLimit: u.QuotaLimit, QuotaUsed: u.QuotaUsed, Status: u.Status, LastLoginAt: u.LastLoginAt}
}

func userMe(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		v := toPortalUser(currentUser(c))
		v.GatewayPrefix = a.Cfg.GatewayPrefix
		c.JSON(http.StatusOK, gin.H{"data": v})
	}
}

// userChangePassword 用户自助改密码：校验旧密码 → 更新哈希 → 注销其它设备并续签当前会话。
func userChangePassword(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
		// 限速：同一用户 15 分钟内最多 8 次尝试（防爆破当前密码）
		if !opLimit.allow("pwd:"+fmt.Sprint(u.ID), 8, 15*time.Minute) {
			tooManyLogins(c)
			return
		}
		var b struct {
			CurrentPassword string `json:"current_password"`
			NewPassword     string `json:"new_password"`
		}
		if !decode(c, &b) {
			return
		}
		if !CheckPassword(u.PasswordHash, b.CurrentPassword) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "current password is incorrect"})
			return
		}
		if len(b.NewPassword) < 6 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "new password must be at least 6 characters"})
			return
		}
		hash, err := HashPassword(b.NewPassword)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := store.SetUserPassword(a.DB, u.ID, hash); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// 改密后强制其它设备下线；为当前客户端续签一个新会话，避免自己被登出
		_ = store.DeleteUserSessionsForUser(a.DB, u.ID)
		token, err := app.GenSecret(32)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := store.CreateUserSession(a.DB, u.ID, app.HashToken(token), models.Now()+sessionTTL); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		setUserSession(c, token, int(sessionTTL))
		c.JSON(http.StatusOK, gin.H{"data": "ok"})
	}
}

// ---------------- Key ----------------

type userKeyView struct {
	ID          int64  `json:"id"`
	ChannelID   int64  `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	Name        string `json:"name"`
	Source      string `json:"source"`
	Remark      string `json:"remark"`
	Masked      string `json:"masked"`
	KeyTail     string `json:"key_tail"`
	Enabled     int    `json:"enabled"`
	LastUsedAt  int64  `json:"last_used_at"`
	CreatedAt   int64  `json:"created_at"`
}

func channelNameMap(a *app.App) (map[int64]string, error) {
	rows, err := a.DB.Query(`SELECT id, name FROM channels`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[int64]string{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		m[id] = name
	}
	return m, rows.Err()
}

func userListKeys(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
		keys, err := store.ListKeysByUser(a.DB, u.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		names, err := channelNameMap(a)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		out := make([]userKeyView, 0, len(keys))
		for _, k := range keys {
			out = append(out, userKeyView{
				ID: k.ID, ChannelID: k.ChannelID, ChannelName: names[k.ChannelID],
				Name: k.Name, Source: k.Source, Remark: k.Remark,
				Masked: "sk-****", KeyTail: a.KeyTail(k.APIKeyEnc), Enabled: k.Enabled,
				LastUsedAt: k.LastUsedAt, CreatedAt: k.CreatedAt,
			})
		}
		c.JSON(http.StatusOK, gin.H{"data": out})
	}
}

func userAddKeys(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
		var b struct {
			ChannelID int64           `json:"channel_id"`
			Keys      json.RawMessage `json:"keys"`
		}
		if !decode(c, &b) {
			return
		}
		ch, err := store.GetChannel(a.DB, b.ChannelID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if ch == nil || ch.Enabled != 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "channel not found or disabled"})
			return
		}
		entries, err := parseKeyEntries(b.Keys)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if len(entries) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "keys must not be empty"})
			return
		}
		items := make([]store.KeyInput, 0, len(entries))
		for _, e := range entries {
			if strings.TrimSpace(e.Key) == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "key must not be empty"})
				return
			}
			enc, err := a.Secret.Encrypt(strings.TrimSpace(e.Key))
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			items = append(items, store.KeyInput{UserID: u.ID, Name: e.Name, Source: e.Source, Remark: e.Remark, Encrypted: enc})
		}
		if err := store.AddChannelKeys(a.DB, ch.ID, items); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"added": len(items)}})
	}
}

// ownedKey 取当前用户名下的 Key（非本人返回 nil）。
func ownedKey(a *app.App, c *gin.Context, keyID int64) *models.ChannelKey {
	u := currentUser(c)
	k, err := store.GetChannelKey(a.DB, keyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return nil
	}
	if k == nil || k.UserID != u.ID {
		c.JSON(http.StatusNotFound, gin.H{"error": "key not found"})
		return nil
	}
	return k
}

func userUpdateKey(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		cur := ownedKey(a, c, id)
		if cur == nil {
			return
		}
		var b struct {
			Name    *string `json:"name"`
			Source  *string `json:"source"`
			Remark  *string `json:"remark"`
			Key     *string `json:"key"`
			Enabled *int    `json:"enabled"`
		}
		if !decode(c, &b) {
			return
		}
		if b.Name != nil {
			cur.Name = *b.Name
		}
		if b.Source != nil {
			cur.Source = *b.Source
		}
		if b.Remark != nil {
			cur.Remark = *b.Remark
		}
		if b.Enabled != nil {
			cur.Enabled = *b.Enabled
		}
		if err := store.UpdateChannelKey(a.DB, cur); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if b.Key != nil && strings.TrimSpace(*b.Key) != "" {
			enc, err := a.Secret.Encrypt(strings.TrimSpace(*b.Key))
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if err := store.UpdateChannelKeySecret(a.DB, cur.ID, enc); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"data": "ok"})
	}
}

func userDeleteKey(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		if cur := ownedKey(a, c, id); cur == nil {
			return
		}
		if err := store.DeleteChannelKey(a.DB, id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": "deleted"})
	}
}

// userTestKey 用用户自己的 Key 请求所属渠道上游 /v1/models，验证连通性（仅限本人 Key）。
func userTestKey(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		u := currentUser(c)
		// 限速：同一用户每分钟最多 12 次连通性测试（防反复触发上游请求）
		if !opLimit.allow("test:"+fmt.Sprint(u.ID), 12, time.Minute) {
			tooManyLogins(c)
			return
		}
		cur := ownedKey(a, c, id)
		if cur == nil {
			return
		}
		ch, err := store.GetChannel(a.DB, cur.ChannelID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if ch == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
			return
		}
		apiKey, err := a.Secret.Decrypt(cur.APIKeyEnc)
		if err != nil || apiKey == "" {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "key decrypt failed"})
			return
		}
		adpt := adapter.Get(ch.Adapter)
		if adpt == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown adapter: " + ch.Adapter})
			return
		}
		timeout := ch.TimeoutMS
		if timeout <= 0 {
			timeout = 60000
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(timeout)*time.Millisecond)
		defer cancel()
		start := time.Now()
		resp, err := adpt.Do(ctx, adapter.ChannelFrom(ch), apiKey, adapter.TestRequest())
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"data": gin.H{"ok": false, "error": err.Error(), "latency_ms": time.Since(start).Milliseconds()}})
			return
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if resp.StatusCode >= 400 {
			c.JSON(http.StatusOK, gin.H{"data": gin.H{"ok": false, "status": resp.StatusCode, "error": strings.TrimSpace(string(body)), "latency_ms": time.Since(start).Milliseconds()}})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"ok": true, "status": resp.StatusCode, "latency_ms": time.Since(start).Milliseconds()}})
	}
}

// ---------------- 消费令牌（users 表内，一人一 token） ----------------

// tokenPlain 解密令牌明文（供用户在界面上随时复制自己的令牌）。
func tokenPlain(a *app.App, enc string) string {
	p, err := a.Secret.Decrypt(enc)
	if err != nil {
		return ""
	}
	return p
}

// resolveExpiry 依 expires_in_days 计算过期时间：nil=不变(keep)，>0=now+N天，<=0=永不过期。
func resolveExpiry(in *int, keep int64) int64 {
	if in == nil {
		return keep
	}
	if *in > 0 {
		return models.Now() + int64(*in)*86400
	}
	return 0
}

// userGetToken 返回本人的消费令牌。
func userGetToken(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
		c.JSON(http.StatusOK, gin.H{"data": tokenViewOf(a, u, tokenPlain(a, u.TokenEnc))})
	}
}

// userUpsertToken 创建或更新本人令牌（不轮换密钥）：无则创建，有则仅更新过期时间。
func userUpsertToken(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
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
			if err := store.SetUserToken(a.DB, u.ID, app.HashToken(plain), enc, exp); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			u.TokenEnc, u.TokenExpiresAt = enc, exp
			c.JSON(http.StatusOK, gin.H{"data": tokenViewOf(a, u, plain)})
			return
		}
		exp := resolveExpiry(b.ExpiresInDays, u.TokenExpiresAt)
		if err := store.SetUserToken(a.DB, u.ID, u.TokenHash, u.TokenEnc, exp); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		u.TokenExpiresAt = exp
		c.JSON(http.StatusOK, gin.H{"data": tokenViewOf(a, u, tokenPlain(a, u.TokenEnc))})
	}
}

// userRotateToken 轮换本人令牌密钥（保留过期时间），返回新明文。
func userRotateToken(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
		plain, enc, err := a.NewGatewayToken()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := store.SetUserToken(a.DB, u.ID, app.HashToken(plain), enc, u.TokenExpiresAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		u.TokenEnc = enc
		c.JSON(http.StatusOK, gin.H{"data": tokenViewOf(a, u, plain)})
	}
}

// userDeleteToken 吊销本人令牌。
func userDeleteToken(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
		placeholder, err := app.GenSecret(24)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := store.SetUserToken(a.DB, u.ID, app.HashToken("revoked:"+placeholder), "", 0); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": "deleted"})
	}
}

// ---------------- 用量 ----------------

func userLogs(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
		f := store.LogFilter{UserID: u.ID, DisplayModel: c.Query("display_model"), Status: c.Query("status")}
		if v := c.Query("limit"); v != "" {
			fmt.Sscanf(v, "%d", &f.Limit)
		}
		if v := c.Query("offset"); v != "" {
			fmt.Sscanf(v, "%d", &f.Offset)
		}
		list, err := store.QueryLogs(a.DB, f)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": list})
	}
}

// userLogsCount 返回当前用户日志总数（分页用）。
func userLogsCount(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
		f := store.LogFilter{UserID: u.ID, DisplayModel: c.Query("display_model"), Status: c.Query("status")}
		n, err := store.CountLogs(a.DB, f)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": n})
	}
}

// userLogoutAll 注销该用户的所有登录会话（含其它设备），并清除当前 Cookie。
func userLogoutAll(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
		if err := store.DeleteUserSessionsForUser(a.DB, u.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		setUserSession(c, "", -1)
		c.JSON(http.StatusOK, gin.H{"data": "ok"})
	}
}

func userOverview(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
		week, err := store.UserUsageByDay(a.DB, u.ID, 7)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		today, err := store.UserUsageSummary(a.DB, u.ID, dayStartOffset(0))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		total, err := store.UserUsageSummary(a.DB, u.ID, 0)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"week": week, "today": today, "total": total}})
	}
}
