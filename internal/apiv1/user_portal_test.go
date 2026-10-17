package apiv1

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"llmgate/internal/app"
	"llmgate/internal/models"
	"llmgate/internal/store"
)

// newPortalRouter 同时挂载管理端与用户端路由。
func newPortalRouter(a *app.App) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Register(r.Group(basePath), a, nil)
	RegisterUserPortal(r.Group("/api/user"), a)
	r.GET("/api/session", SessionProbe(a))
	return r
}

// doCookie 发起带（可选）Cookie 的请求。
func doCookie(r http.Handler, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func adminCreateUser(t *testing.T, r http.Handler, username string) int64 {
	t.Helper()
	body := fmt.Sprintf(`{"username":%q,"password":"secret1","name":%q}`, username, username)
	w := do(r, http.MethodPost, basePath+"/users", body, testAdminToken, "")
	if w.Code != http.StatusOK {
		t.Fatalf("admin create user = %d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	decodeBody(t, w, &resp)
	if resp.Data.ID == 0 {
		t.Fatal("expected non-zero user id")
	}
	return resp.Data.ID
}

func mustChannel(t *testing.T, a *app.App, name string) int64 {
	t.Helper()
	id, err := store.CreateChannel(a.DB, &models.Channel{
		Name: name, BaseURL: "http://upstream", Adapter: "openai", Enabled: 1, HealthState: "healthy", TimeoutMS: 1000,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return id
}

func userLoginCookie(t *testing.T, r http.Handler, username, password string) *http.Cookie {
	t.Helper()
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	w := doCookie(r, http.MethodPost, "/api/user/login", body, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("user login = %d, body=%s", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == userSessionCookie {
			return c
		}
	}
	t.Fatal("expected user session cookie")
	return nil
}

func TestUserPortalLoginAndMe(t *testing.T) {
	a := newApp(t)
	r := newPortalRouter(a)
	adminCreateUser(t, r, "alice")

	// 错误密码 / 不存在用户 → 401
	if w := doCookie(r, http.MethodPost, "/api/user/login", `{"username":"alice","password":"wrong"}`, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("login wrong pwd = %d, want 401", w.Code)
	}
	if w := doCookie(r, http.MethodPost, "/api/user/login", `{"username":"ghost","password":"secret1"}`, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("login ghost = %d, want 401", w.Code)
	}

	// 未登录访问 /me → 401
	if w := doCookie(r, http.MethodGet, "/api/user/me", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("me anonymous = %d, want 401", w.Code)
	}

	cookie := userLoginCookie(t, r, "alice", "secret1")
	w := doCookie(r, http.MethodGet, "/api/user/me", "", cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("me = %d, body=%s", w.Code, w.Body.String())
	}
	var me struct {
		Data portalUserView `json:"data"`
	}
	decodeBody(t, w, &me)
	if me.Data.Username != "alice" || me.Data.GatewayPrefix != "/" {
		t.Fatalf("me payload = %+v", me.Data)
	}

	// 登出清除 cookie
	lo := doCookie(r, http.MethodPost, "/api/user/logout", "", cookie)
	var cleared bool
	for _, c := range lo.Result().Cookies() {
		if c.Name == userSessionCookie && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("logout should clear user session cookie, got %+v", lo.Result().Cookies())
	}
}

func TestUserPortalKeyIsolation(t *testing.T) {
	a := newApp(t)
	r := newPortalRouter(a)
	adminCreateUser(t, r, "alice")
	adminCreateUser(t, r, "bob")
	chID := mustChannel(t, a, "shared")

	aCookie := userLoginCookie(t, r, "alice", "secret1")
	bCookie := userLoginCookie(t, r, "bob", "secret1")

	addKey := func(cookie *http.Cookie, name, key string) {
		body := fmt.Sprintf(`{"channel_id":%d,"keys":[{"name":%q,"key":%q}]}`, chID, name, key)
		if w := doCookie(r, http.MethodPost, "/api/user/keys", body, cookie); w.Code != http.StatusOK {
			t.Fatalf("add key = %d, body=%s", w.Code, w.Body.String())
		}
	}
	addKey(aCookie, "a-key", "sk-aaaa1111")
	addKey(bCookie, "b-key", "sk-bbbb2222")

	type keyList struct {
		Data []userKeyView `json:"data"`
	}
	// A 只能看到自己的 Key
	wa := doCookie(r, http.MethodGet, "/api/user/keys", "", aCookie)
	var ka keyList
	decodeBody(t, wa, &ka)
	if len(ka.Data) != 1 || ka.Data[0].Name != "a-key" || ka.Data[0].ChannelName != "shared" {
		t.Fatalf("alice keys = %+v", ka.Data)
	}
	if !strings.HasSuffix(ka.Data[0].KeyTail, "1111") {
		t.Fatalf("unexpected key tail %q", ka.Data[0].KeyTail)
	}
	aKeyID := ka.Data[0].ID

	// 本人可改自己的 Key（名称/来源/备注/新密钥/启用状态）
	if w := doCookie(r, http.MethodPut, fmt.Sprintf("/api/user/keys/%d", aKeyID), `{"name":"renamed","source":"s","remark":"r","key":"sk-aaaa3333","enabled":1}`, aCookie); w.Code != http.StatusOK {
		t.Fatalf("alice update own key = %d, body=%s", w.Code, w.Body.String())
	}

	wb := doCookie(r, http.MethodGet, "/api/user/keys", "", bCookie)
	var kb keyList
	decodeBody(t, wb, &kb)
	if len(kb.Data) != 1 || kb.Data[0].Name != "b-key" {
		t.Fatalf("bob keys = %+v", kb.Data)
	}
	bKeyID := kb.Data[0].ID

	// A 不能改/删 B 的 Key → 404
	if w := doCookie(r, http.MethodPut, fmt.Sprintf("/api/user/keys/%d", bKeyID), `{"name":"hacked"}`, aCookie); w.Code != http.StatusNotFound {
		t.Fatalf("alice update bob key = %d, want 404", w.Code)
	}
	if w := doCookie(r, http.MethodDelete, fmt.Sprintf("/api/user/keys/%d", bKeyID), "", aCookie); w.Code != http.StatusNotFound {
		t.Fatalf("alice delete bob key = %d, want 404", w.Code)
	}

	// /channels 显示本人 Key 数
	wc := doCookie(r, http.MethodGet, "/api/user/channels", "", aCookie)
	var chs struct {
		Data []userChannelView `json:"data"`
	}
	decodeBody(t, wc, &chs)
	if len(chs.Data) != 1 || chs.Data[0].MyKeys != 1 {
		t.Fatalf("alice channels = %+v", chs.Data)
	}

	// 本人可删除
	if w := doCookie(r, http.MethodDelete, fmt.Sprintf("/api/user/keys/%d", aKeyID), "", aCookie); w.Code != http.StatusOK {
		t.Fatalf("alice delete own key = %d, body=%s", w.Code, w.Body.String())
	}
	wa = doCookie(r, http.MethodGet, "/api/user/keys", "", aCookie)
	decodeBody(t, wa, &ka)
	if len(ka.Data) != 0 {
		t.Fatalf("alice keys after delete = %+v", ka.Data)
	}

	// 空 keys → 400
	if w := doCookie(r, http.MethodPost, "/api/user/keys", fmt.Sprintf(`{"channel_id":%d,"keys":[]}`, chID), aCookie); w.Code != http.StatusBadRequest {
		t.Fatalf("add empty keys = %d, want 400", w.Code)
	}
	// 渠道不存在 → 400
	if w := doCookie(r, http.MethodPost, "/api/user/keys", `{"channel_id":9999,"keys":[{"key":"sk-x"}]}`, aCookie); w.Code != http.StatusBadRequest {
		t.Fatalf("add to missing channel = %d, want 400", w.Code)
	}
}

func TestUserPortalTokens(t *testing.T) {
	a := newApp(t)
	r := newPortalRouter(a)
	adminCreateUser(t, r, "alice")
	adminCreateUser(t, r, "bob")

	aCookie := userLoginCookie(t, r, "alice", "secret1")
	bCookie := userLoginCookie(t, r, "bob", "secret1")

	// 建号即自动创建令牌
	w := doCookie(r, http.MethodGet, "/api/user/token", "", aCookie)
	var gt struct {
		Data tokenView `json:"data"`
	}
	decodeBody(t, w, &gt)
	if !gt.Data.HasToken {
		t.Fatalf("alice should have an auto-created token, got %+v", gt.Data)
	}

	// 创建/更新（PUT）→ 返回明文
	w = doCookie(r, http.MethodPut, "/api/user/token", `{"expires_in_days":7}`, aCookie)
	if w.Code != http.StatusOK {
		t.Fatalf("create token = %d, body=%s", w.Code, w.Body.String())
	}
	var created struct {
		Data tokenView `json:"data"`
	}
	decodeBody(t, w, &created)
	if !created.Data.HasToken || !strings.HasPrefix(created.Data.Token, "sk-") || created.Data.TokenTail != created.Data.Token[len(created.Data.Token)-4:] {
		t.Fatalf("create token payload = %+v", created.Data)
	}
	if created.Data.ExpiresAt == 0 {
		t.Fatal("expiry should be set")
	}

	// 再次 PUT 不轮换密钥
	w = doCookie(r, http.MethodPut, "/api/user/token", `{"expires_in_days":30}`, aCookie)
	var upd struct {
		Data tokenView `json:"data"`
	}
	decodeBody(t, w, &upd)
	if upd.Data.Token != created.Data.Token || upd.Data.ExpiresAt == 0 {
		t.Fatalf("upsert should keep secret: %+v", upd.Data)
	}

	// B 也有独立令牌（互不相同）
	wb := doCookie(r, http.MethodGet, "/api/user/token", "", bCookie)
	decodeBody(t, wb, &gt)
	if !gt.Data.HasToken || gt.Data.TokenTail == created.Data.TokenTail {
		t.Fatalf("bob should have his own token, got %+v", gt.Data)
	}

	// 轮换：密钥变化
	w = doCookie(r, http.MethodPost, "/api/user/token/rotate", "", aCookie)
	var rot struct {
		Data tokenView `json:"data"`
	}
	decodeBody(t, w, &rot)
	if rot.Data.Token == created.Data.Token {
		t.Fatal("rotate should change the secret")
	}

	// B 删除自己的令牌 → 200
	if w := doCookie(r, http.MethodDelete, "/api/user/token", "", bCookie); w.Code != http.StatusOK {
		t.Fatalf("bob delete own (none) = %d", w.Code)
	}
	// A 可吊销
	if w := doCookie(r, http.MethodDelete, "/api/user/token", "", aCookie); w.Code != http.StatusOK {
		t.Fatalf("delete token = %d, body=%s", w.Code, w.Body.String())
	}
	w = doCookie(r, http.MethodGet, "/api/user/token", "", aCookie)
	decodeBody(t, w, &gt)
	if gt.Data.HasToken {
		t.Fatalf("alice token should be gone, got %+v", gt.Data)
	}
}

func TestUserPortalLogsAndOverview(t *testing.T) {
	a := newApp(t)
	r := newPortalRouter(a)
	uid := adminCreateUser(t, r, "alice")
	aCookie := userLoginCookie(t, r, "alice", "secret1")

	if err := store.InsertLogs(a.DB, []*models.RequestLog{
		{UserID: uid, DisplayModel: "m", Status: "success", TotalTokens: 12, PromptTokens: 10, CompletionTokens: 2, Cost: 0.5, CreatedAt: models.Now()},
	}); err != nil {
		t.Fatalf("insert logs: %v", err)
	}

	w := doCookie(r, http.MethodGet, "/api/user/logs?limit=10", "", aCookie)
	if w.Code != http.StatusOK {
		t.Fatalf("logs = %d, body=%s", w.Code, w.Body.String())
	}
	var logs struct {
		Data []*models.RequestLog `json:"data"`
	}
	decodeBody(t, w, &logs)
	if len(logs.Data) != 1 || logs.Data[0].UserID != uid {
		t.Fatalf("logs = %+v", logs.Data)
	}

	wo := doCookie(r, http.MethodGet, "/api/user/overview", "", aCookie)
	if wo.Code != http.StatusOK {
		t.Fatalf("overview = %d, body=%s", wo.Code, wo.Body.String())
	}
	var ov struct {
		Data struct {
			Week  []store.UsagePoint `json:"week"`
			Today store.UsageSummary `json:"today"`
		} `json:"data"`
	}
	decodeBody(t, wo, &ov)
	if ov.Data.Today.TotalRequests != 1 || len(ov.Data.Week) != 7 {
		t.Fatalf("overview payload = %+v", ov.Data)
	}
}

func TestAdminAssignKeyOwner(t *testing.T) {
	a := newApp(t)
	r := newPortalRouter(a)
	uid := adminCreateUser(t, r, "alice")

	// 管理员建渠道 + Key（归属 0 = 全局）
	w := do(r, http.MethodPost, basePath+"/channels", `{"name":"c","base_url":"http://up","keys":[{"name":"k","key":"sk-z"}]}`, testAdminToken, "")
	if w.Code != http.StatusOK {
		t.Fatalf("create channel = %d, body=%s", w.Code, w.Body.String())
	}
	var cid struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	decodeBody(t, w, &cid)

	w = do(r, http.MethodGet, fmt.Sprintf("%s/channels/%d/keys", basePath, cid.Data.ID), "", testAdminToken, "")
	var kl struct {
		Data []keyView `json:"data"`
	}
	decodeBody(t, w, &kl)
	if len(kl.Data) != 1 || kl.Data[0].UserID != 0 {
		t.Fatalf("expected one global key, got %+v", kl.Data)
	}
	keyID := kl.Data[0].ID

	// 指派给 alice
	w = do(r, http.MethodPut, fmt.Sprintf("%s/channels/keys/%d", basePath, keyID), fmt.Sprintf(`{"user_id":%d}`, uid), testAdminToken, "")
	if w.Code != http.StatusOK {
		t.Fatalf("assign owner = %d, body=%s", w.Code, w.Body.String())
	}
	var kv struct {
		Data keyView `json:"data"`
	}
	decodeBody(t, w, &kv)
	if kv.Data.UserID != uid || kv.Data.OwnerName != "alice" {
		t.Fatalf("assign result = %+v", kv.Data)
	}

	// 指派给不存在用户 → 400
	if w := do(r, http.MethodPut, fmt.Sprintf("%s/channels/keys/%d", basePath, keyID), `{"user_id":9999}`, testAdminToken, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("assign missing user = %d, want 400", w.Code)
	}

	// 用户端现在能看到被指派的 Key
	aCookie := userLoginCookie(t, r, "alice", "secret1")
	w = doCookie(r, http.MethodGet, "/api/user/keys", "", aCookie)
	var uk struct {
		Data []userKeyView `json:"data"`
	}
	decodeBody(t, w, &uk)
	if len(uk.Data) != 1 || uk.Data[0].ChannelID != cid.Data.ID {
		t.Fatalf("alice keys after assign = %+v", uk.Data)
	}
}

func TestAdminUserTokenEndpoints(t *testing.T) {
	a := newApp(t)
	r := newPortalRouter(a)
	uid := adminCreateUser(t, r, "alice")
	uid2 := adminCreateUser(t, r, "bob")

	// 改名冲突 → 409
	if w := do(r, http.MethodPut, basePath+"/users/"+fmt.Sprint(uid2), `{"username":"alice"}`, testAdminToken, ""); w.Code != http.StatusConflict {
		t.Fatalf("rename conflict = %d, want 409", w.Code)
	}
	// 改密合法 → 200；username 置空 → 400
	if w := do(r, http.MethodPut, basePath+"/users/"+fmt.Sprint(uid), `{"password":"brandnew"}`, testAdminToken, ""); w.Code != http.StatusOK {
		t.Fatalf("admin update password = %d, body=%s", w.Code, w.Body.String())
	}
	if w := do(r, http.MethodPut, basePath+"/users/"+fmt.Sprint(uid), `{"username":""}`, testAdminToken, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("empty username update = %d, want 400", w.Code)
	}

	// 管理员为用户创建令牌（一人一个，返回明文）
	w := do(r, http.MethodPut, basePath+"/users/"+fmt.Sprint(uid)+"/token", `{"expires_in_days":30}`, testAdminToken, "")
	if w.Code != http.StatusOK {
		t.Fatalf("admin create token = %d, body=%s", w.Code, w.Body.String())
	}
	var cr struct {
		Data tokenView `json:"data"`
	}
	decodeBody(t, w, &cr)
	if !cr.Data.HasToken || !strings.HasPrefix(cr.Data.Token, "sk-") {
		t.Fatalf("admin create token payload = %+v", cr.Data)
	}
	// 隔离：吊销 bob 的令牌后 alice 的不受影响
	if w := do(r, http.MethodDelete, basePath+"/users/"+fmt.Sprint(uid2)+"/token", "", testAdminToken, ""); w.Code != http.StatusOK {
		t.Fatalf("revoke bob token = %d", w.Code)
	}
	w = do(r, http.MethodGet, basePath+"/users/"+fmt.Sprint(uid2)+"/token", "", testAdminToken, "")
	var gt struct {
		Data tokenView `json:"data"`
	}
	decodeBody(t, w, &gt)
	if gt.Data.HasToken {
		t.Fatalf("bob should have no token after revoke, got %+v", gt.Data)
	}
	w = do(r, http.MethodGet, basePath+"/users/"+fmt.Sprint(uid)+"/token", "", testAdminToken, "")
	decodeBody(t, w, &gt)
	if !gt.Data.HasToken {
		t.Fatalf("alice token should be unaffected, got %+v", gt.Data)
	}
	// 不存在用户 → 404
	if w := do(r, http.MethodPut, basePath+"/users/9999/token", `{}`, testAdminToken, ""); w.Code != http.StatusNotFound {
		t.Fatalf("token for missing user = %d, want 404", w.Code)
	}
	// 轮换 → 200，密钥变化
	w = do(r, http.MethodPost, basePath+"/users/"+fmt.Sprint(uid)+"/token/rotate", "", testAdminToken, "")
	var rot struct {
		Data tokenView `json:"data"`
	}
	decodeBody(t, w, &rot)
	if rot.Data.Token == cr.Data.Token {
		t.Fatal("rotate should change the secret")
	}
	// 吊销 → 200
	if w := do(r, http.MethodDelete, basePath+"/users/"+fmt.Sprint(uid)+"/token", "", testAdminToken, ""); w.Code != http.StatusOK {
		t.Fatalf("admin delete token = %d, body=%s", w.Code, w.Body.String())
	}
}

func TestUserChangePassword(t *testing.T) {
	a := newApp(t)
	r := newPortalRouter(a)
	adminCreateUser(t, r, "alice")
	cookie := userLoginCookie(t, r, "alice", "secret1")

	// 旧密码错误 → 400
	if w := doCookie(r, http.MethodPut, "/api/user/password", `{"current_password":"nope","new_password":"brandnew"}`, cookie); w.Code != http.StatusBadRequest {
		t.Fatalf("wrong current = %d, want 400", w.Code)
	}
	// 新密码过短 → 400
	if w := doCookie(r, http.MethodPut, "/api/user/password", `{"current_password":"secret1","new_password":"123"}`, cookie); w.Code != http.StatusBadRequest {
		t.Fatalf("short new = %d, want 400", w.Code)
	}
	// 成功 → 200
	if w := doCookie(r, http.MethodPut, "/api/user/password", `{"current_password":"secret1","new_password":"brandnew"}`, cookie); w.Code != http.StatusOK {
		t.Fatalf("change password = %d, body=%s", w.Code, w.Body.String())
	}
	// 旧密码失效、新密码可用
	if w := doCookie(r, http.MethodPost, "/api/user/login", `{"username":"alice","password":"secret1"}`, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("old password should be rejected = %d", w.Code)
	}
	userLoginCookie(t, r, "alice", "brandnew")
}

func TestUserTestKey(t *testing.T) {
	a := newApp(t)
	r := newPortalRouter(a)
	adminCreateUser(t, r, "alice")
	// 上游不可达的渠道（连接立即被拒）
	chID, err := store.CreateChannel(a.DB, &models.Channel{
		Name: "c", BaseURL: "http://127.0.0.1:1", Adapter: "openai", Enabled: 1, HealthState: "healthy", TimeoutMS: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	cookie := userLoginCookie(t, r, "alice", "secret1")

	if w := doCookie(r, http.MethodPost, "/api/user/keys", fmt.Sprintf(`{"channel_id":%d,"keys":[{"name":"k","key":"sk-abc"}]}`, chID), cookie); w.Code != http.StatusOK {
		t.Fatalf("add key = %d, body=%s", w.Code, w.Body.String())
	}
	w := doCookie(r, http.MethodGet, "/api/user/keys", "", cookie)
	var kl struct {
		Data []userKeyView `json:"data"`
	}
	decodeBody(t, w, &kl)
	keyID := kl.Data[0].ID

	// 上游不可达 → 200 且 ok:false（不把测试失败当成 5xx）
	w = doCookie(r, http.MethodPost, fmt.Sprintf("/api/user/keys/%d/test", keyID), "", cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("test key = %d, body=%s", w.Code, w.Body.String())
	}
	var res struct {
		Data struct {
			Ok    bool   `json:"ok"`
			Error string `json:"error"`
		} `json:"data"`
	}
	decodeBody(t, w, &res)
	if res.Data.Ok {
		t.Fatal("unreachable upstream should report ok=false")
	}

	// 测试不存在的 Key → 404
	if w := doCookie(r, http.MethodPost, "/api/user/keys/99999/test", "", cookie); w.Code != http.StatusNotFound {
		t.Fatalf("test missing key = %d, want 404", w.Code)
	}
}

// TestUserCanTriggerModelSync 普通用户也应能手动触发模型同步。
func TestUserCanTriggerModelSync(t *testing.T) {
	a := newApp(t)
	r := gin.New()
	Register(r.Group(basePath), a, nil)
	RegisterUserPortalWithSync(r.Group("/api/user"), a, func() map[string]any {
		return map[string]any{"created": 3, "skipped": 1}
	})
	adminCreateUser(t, r, "alice")
	cookie := userLoginCookie(t, r, "alice", "secret1")

	w := doCookie(r, http.MethodPost, "/api/user/model-sync", "", cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("user model-sync = %d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Created int `json:"created"`
		} `json:"data"`
	}
	decodeBody(t, w, &resp)
	if resp.Data.Created != 3 {
		t.Fatalf("model-sync result = %+v", resp.Data)
	}
}
