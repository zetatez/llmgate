// 用户端（多用户）API：/api/user/*。
// 与管理员端（/api/admin/*，令牌鉴权）相互独立：用户以「用户名 + 密码」登录，
// 会话为 httpOnly Cookie（随机令牌，库内只存哈希）。
package apiv1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"llmgate/internal/app"
	"llmgate/internal/models"
	"llmgate/internal/store"
)

// userSessionCookie 用户端 httpOnly 会话 Cookie 名。
const userSessionCookie = "llmgate_user_session"

// userSessionTTL 用户会话有效期（秒）。
const userSessionTTL = 30 * 24 * 3600

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
	authed.GET("/channels", userChannels(a))

	authed.GET("/keys", userListKeys(a))
	authed.POST("/keys", userAddKeys(a))
	authed.PUT("/keys/:id", userUpdateKey(a))
	authed.DELETE("/keys/:id", userDeleteKey(a))

	authed.GET("/tokens", userListTokens(a))
	authed.POST("/tokens", userCreateToken(a))
	authed.PUT("/tokens/:id", userUpdateToken(a))
	authed.DELETE("/tokens/:id", userDeleteToken(a))

	authed.GET("/logs", userLogs(a))
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
		if err := store.CreateUserSession(a.DB, u.ID, app.HashToken(token), models.Now()+userSessionTTL); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		_ = store.TouchUserLogin(a.DB, u.ID)
		setUserSession(c, token, userSessionTTL)
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

// ---------------- 渠道 / Key ----------------

type userChannelView struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Adapter string `json:"adapter"`
	BaseURL string `json:"base_url"`
	MyKeys  int    `json:"my_keys"`
}

// userChannels 列出管理员启用中的渠道，并标注当前用户已录入的 Key 数。
func userChannels(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
		rows, err := a.DB.Query(`
			SELECT ch.id, ch.name, ch.adapter, ch.base_url,
				(SELECT COUNT(*) FROM channel_keys ck WHERE ck.channel_id = ch.id AND ck.user_id = ?) AS my_keys
			FROM channels ch WHERE ch.enabled = 1 ORDER BY ch.priority ASC, ch.name ASC`, u.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()
		out := []userChannelView{}
		for rows.Next() {
			var v userChannelView
			if err := rows.Scan(&v.ID, &v.Name, &v.Adapter, &v.BaseURL, &v.MyKeys); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			out = append(out, v)
		}
		c.JSON(http.StatusOK, gin.H{"data": out})
	}
}

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
			c.JSON(http.StatusBadRequest, gin.H{"error": "keys 不能为空"})
			return
		}
		items := make([]store.KeyInput, 0, len(entries))
		for _, e := range entries {
			if strings.TrimSpace(e.Key) == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "key 不能为空"})
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

// ---------------- 消费令牌 ----------------

type userTokenView struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Token      string `json:"token,omitempty"` // 仅创建时返回一次
	TokenTail  string `json:"token_tail"`
	Enabled    int    `json:"enabled"`
	LastUsedAt int64  `json:"last_used_at"`
	CreatedAt  int64  `json:"created_at"`
}

func toUserTokenView(a *app.App, t *models.UserToken, plain string) userTokenView {
	return userTokenView{ID: t.ID, Name: t.Name, Token: plain, TokenTail: a.KeyTail(t.TokenEnc),
		Enabled: t.Enabled, LastUsedAt: t.LastUsedAt, CreatedAt: t.CreatedAt}
}

func userListTokens(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
		list, err := store.ListUserTokens(a.DB, u.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		out := make([]userTokenView, 0, len(list))
		for _, t := range list {
			out = append(out, toUserTokenView(a, t, ""))
		}
		c.JSON(http.StatusOK, gin.H{"data": out})
	}
}

func userCreateToken(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
		var b struct {
			Name string `json:"name"`
		}
		if !decode(c, &b) {
			return
		}
		plain, enc, err := a.NewGatewayToken()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		t := &models.UserToken{UserID: u.ID, Name: strings.TrimSpace(b.Name), TokenHash: app.HashToken(plain), TokenEnc: enc, Enabled: 1}
		id, err := store.CreateUserToken(a.DB, t)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		t.ID = id
		c.JSON(http.StatusOK, gin.H{"data": toUserTokenView(a, t, plain)})
	}
}

func userUpdateToken(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		u := currentUser(c)
		cur, err := store.GetUserToken(a.DB, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if cur == nil || cur.UserID != u.ID {
			c.JSON(http.StatusNotFound, gin.H{"error": "token not found"})
			return
		}
		var b struct {
			Name    *string `json:"name"`
			Enabled *int    `json:"enabled"`
		}
		if !decode(c, &b) {
			return
		}
		if b.Name != nil {
			cur.Name = *b.Name
		}
		if b.Enabled != nil {
			cur.Enabled = *b.Enabled
		}
		if err := store.UpdateUserToken(a.DB, cur); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": toUserTokenView(a, cur, "")})
	}
}

func userDeleteToken(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		u := currentUser(c)
		cur, err := store.GetUserToken(a.DB, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if cur == nil || cur.UserID != u.ID {
			c.JSON(http.StatusNotFound, gin.H{"error": "token not found"})
			return
		}
		if err := store.DeleteUserToken(a.DB, id); err != nil {
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
		list, err := store.QueryLogs(a.DB, f)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": list})
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
