package apiv1

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"llmgate/internal/app"
	"llmgate/internal/config"
	"llmgate/internal/models"
	"llmgate/internal/store"

	_ "llmgate/internal/adapter/openai" // 注册 openai 适配器供 testChannel 使用
)

const (
	testAdminToken  = "admin-test-token-0001"
	testEncryptSeed = "test-secret-seed-for-llmgate-apiv1-tests-0001"
	basePath        = "/api/admin"
)

// newApp 构造每个用例独立的内存 SQLite + 真实 App。
func newApp(t *testing.T) *app.App {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	a, err := app.New(&config.Config{AdminToken: testAdminToken, EncryptSeed: testEncryptSeed, GatewayPrefix: "/"}, db)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	return a
}

// newRouter 注册全部管理端路由。
func newRouter(a *app.App, syncNow func() map[string]any) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Register(r.Group(basePath), a, syncNow)
	return r
}

// do 发起请求；authToken 非空时以 Bearer 头鉴权；origin 非空时设置 Origin 头。
func do(r http.Handler, method, path, body, authToken, origin string) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode response %q: %v", w.Body.String(), err)
	}
}

func loginAndGetCookie(t *testing.T, r *gin.Engine) *http.Cookie {
	t.Helper()
	w := do(r, http.MethodPost, basePath+"/login", "", testAdminToken, "")
	if w.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 session cookie, got %d", len(cookies))
	}
	return cookies[0]
}

// ---- register.go: 登录 / 鉴权 / CSRF ----

func TestLoginLogoutAndAuth(t *testing.T) {
	a := newApp(t)
	r := newRouter(a, nil)

	// 错误令牌 → 401
	if w := do(r, http.MethodPost, basePath+"/login", "", "wrong-token", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("login(wrong) = %d, want 401", w.Code)
	}
	// 空令牌 → 401
	if w := do(r, http.MethodPost, basePath+"/login", "", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("login(empty) = %d, want 401", w.Code)
	}
	// 正确令牌 → 200
	if w := do(r, http.MethodPost, basePath+"/login", "", testAdminToken, ""); w.Code != http.StatusOK {
		t.Fatalf("login = %d, body=%s", w.Code, w.Body.String())
	}

	// ping 需鉴权
	if w := do(r, http.MethodGet, basePath+"/ping", "", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("ping no-auth = %d, want 401", w.Code)
	}
	if w := do(r, http.MethodGet, basePath+"/ping", "", testAdminToken, ""); w.Code != http.StatusOK {
		t.Fatalf("ping bearer = %d, want 200", w.Code)
	}

	// 会话 cookie 鉴权
	r2 := newRouter(a, nil)
	cookie := loginAndGetCookie(t, r2)
	req := httptest.NewRequest(http.MethodGet, basePath+"/ping", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r2.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ping cookie = %d, want 200", rec.Code)
	}

	// 登出清除 cookie
	lo := do(r2, http.MethodPost, basePath+"/logout", "", testAdminToken, "")
	var cleared bool
	for _, c := range lo.Result().Cookies() {
		if c.Name == adminSessionCookie && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("logout should clear session cookie, got %+v", lo.Result().Cookies())
	}
	if lo.Code != http.StatusOK {
		t.Fatalf("logout status = %d", lo.Code)
	}
}

func TestModelSync(t *testing.T) {
	a := newApp(t)
	called := false
	r := newRouter(a, func() map[string]any { called = true; return map[string]any{"n": 3} })
	w := do(r, http.MethodPost, basePath+"/model-sync", "", testAdminToken, "")
	if w.Code != http.StatusOK || !called {
		t.Fatalf("model-sync = %d (called=%v), body=%s", w.Code, called, w.Body.String())
	}
	var out struct {
		Data map[string]any `json:"data"`
	}
	decodeBody(t, w, &out)
	if out.Data["n"].(float64) != 3 {
		t.Fatalf("model-sync data=%v", out.Data)
	}
}

func TestCSRFOriginCheck(t *testing.T) {
	a := newApp(t)
	r := newRouter(a, nil)

	// POST 携带跨站 Origin → 403（默认请求 Host=example.com）
	if w := do(r, http.MethodPost, basePath+"/login", "", testAdminToken, "http://evil.com"); w.Code != http.StatusForbidden {
		t.Fatalf("csrf(evil origin) = %d, want 403", w.Code)
	}
	// POST 携带同源 Origin → 放行
	if w := do(r, http.MethodPost, basePath+"/login", "", testAdminToken, "http://example.com"); w.Code != http.StatusOK {
		t.Fatalf("csrf(same origin) = %d, want 200", w.Code)
	}
	// Referer 跨站 → 拒绝
	req := httptest.NewRequest(http.MethodPost, basePath+"/login", nil)
	req.Header.Set("Authorization", "Bearer "+testAdminToken)
	req.Header.Set("Referer", "http://evil.com/x")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("csrf(evil referer) = %d, want 403", rec.Code)
	}
	// GET 不受 CSRF 限制
	if w := do(r, http.MethodGet, basePath+"/ping", "", testAdminToken, "http://evil.com"); w.Code != http.StatusOK {
		t.Fatalf("get w/ evil origin = %d, want 200", w.Code)
	}
}

// ---- helpers.go：parseID / decode 直接调用 ----

func TestParseIDAndDecode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/h/:id", func(c *gin.Context) {
		// parseID 失败时已写入 400；这里仅在成功路径补充返回体
		if id, ok := parseID(c, "id"); ok {
			c.JSON(http.StatusOK, gin.H{"id": id, "ok": true})
		}
	})
	r.POST("/d", func(c *gin.Context) {
		var b struct {
			Name string `json:"name"`
		}
		if ok := decode(c, &b); ok {
			c.JSON(http.StatusOK, gin.H{"ok": true, "name": b.Name})
		}
	})

	// parseID 合法 / 非法
	var pr struct {
		ID int64 `json:"id"`
		OK bool  `json:"ok"`
	}
	w := do(r, http.MethodGet, "/h/42", "", "", "")
	decodeBody(t, w, &pr)
	if !pr.OK || pr.ID != 42 {
		t.Fatalf("parseID valid = %+v", pr)
	}
	w = do(r, http.MethodGet, "/h/abc", "", "", "")
	pr = struct {
		ID int64 `json:"id"`
		OK bool  `json:"ok"`
	}{}
	decodeBody(t, w, &pr)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("parseID invalid status=%d want 400", w.Code)
	}

	// decode 合法 / 非法
	var dr struct {
		OK   bool   `json:"ok"`
		Name string `json:"name"`
	}
	w = do(r, http.MethodPost, "/d", `{"name":"x"}`, "", "")
	decodeBody(t, w, &dr)
	if !dr.OK || dr.Name != "x" {
		t.Fatalf("decode valid = %+v", dr)
	}
	w = do(r, http.MethodPost, "/d", `{bad`, "", "")
	dr = struct {
		OK   bool   `json:"ok"`
		Name string `json:"name"`
	}{}
	decodeBody(t, w, &dr)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("decode invalid status=%d want 400", w.Code)
	}
}

// ---- users.go ----

func TestUsersCRUD(t *testing.T) {
	a := newApp(t)
	r := newRouter(a, nil)
	tok := testAdminToken

	w := do(r, http.MethodGet, basePath+"/users", "", tok, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list users = %d", w.Code)
	}
	var lst struct {
		Data []userView `json:"data"`
	}
	decodeBody(t, w, &lst)
	if len(lst.Data) != 0 {
		t.Fatalf("expected empty users, got %d", len(lst.Data))
	}

	// 创建：返回明文 token 一次 + token_tail
	cb := `{"name":"alice","remark":"r1","quota_limit":100,"status":1}`
	w = do(r, http.MethodPost, basePath+"/users", cb, tok, "")
	if w.Code != http.StatusOK {
		t.Fatalf("create user = %d, body=%s", w.Code, w.Body.String())
	}
	var created struct {
		Data userView `json:"data"`
	}
	decodeBody(t, w, &created)
	if created.Data.Token == "" || !strings.HasPrefix(created.Data.Token, "sk-") {
		t.Fatalf("create user should return one-time plain token, got %q", created.Data.Token)
	}
	if created.Data.TokenTail != created.Data.Token[len(created.Data.Token)-4:] {
		t.Fatalf("token_tail mismatch: %s vs %s", created.Data.TokenTail, created.Data.Token)
	}
	if created.Data.Status != 1 {
		t.Fatalf("default status = %d, want 1", created.Data.Status)
	}
	uid := created.Data.ID

	// 创建：name 必填；非法 JSON
	if w := do(r, http.MethodPost, basePath+"/users", `{"name":""}`, tok, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("create empty-name = %d, want 400", w.Code)
	}
	if w := do(r, http.MethodPost, basePath+"/users", `{invalid`, tok, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("create bad body = %d, want 400", w.Code)
	}

	// 更新（部分字段）
	w = do(r, http.MethodPut, basePath+"/users/"+fmt.Sprint(uid), `{"name":"bob","status":0}`, tok, "")
	if w.Code != http.StatusOK {
		t.Fatalf("update user = %d, body=%s", w.Code, w.Body.String())
	}
	var uv struct {
		Data userView `json:"data"`
	}
	decodeBody(t, w, &uv)
	if uv.Data.Name != "bob" || uv.Data.Status != 0 {
		t.Fatalf("update result = %+v", uv.Data)
	}

	if w := do(r, http.MethodPut, basePath+"/users/9999", `{"name":"x"}`, tok, ""); w.Code != http.StatusNotFound {
		t.Fatalf("update missing = %d, want 404", w.Code)
	}
	if w := do(r, http.MethodPut, basePath+"/users/abc", `{"name":"x"}`, tok, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("update bad id = %d, want 400", w.Code)
	}

	// 回显令牌（解密）
	w = do(r, http.MethodGet, basePath+"/users/"+fmt.Sprint(uid)+"/token", "", tok, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get token = %d, body=%s", w.Code, w.Body.String())
	}
	var gtok struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	decodeBody(t, w, &gtok)
	if gtok.Data.Token == "" {
		t.Fatal("get token should return plaintext")
	}

	// 重置令牌
	w = do(r, http.MethodPost, basePath+"/users/"+fmt.Sprint(uid)+"/token", "", tok, "")
	if w.Code != http.StatusOK {
		t.Fatalf("reset token = %d, body=%s", w.Code, w.Body.String())
	}
	decodeBody(t, w, &gtok)
	if !strings.HasPrefix(gtok.Data.Token, "sk-") {
		t.Fatalf("reset token = %q", gtok.Data.Token)
	}

	// 不存在用户的令牌 → 404
	if w := do(r, http.MethodGet, basePath+"/users/9999/token", "", tok, ""); w.Code != http.StatusNotFound {
		t.Fatalf("get token missing = %d, want 404", w.Code)
	}

	// 删除
	if w := do(r, http.MethodDelete, basePath+"/users/"+fmt.Sprint(uid), "", tok, ""); w.Code != http.StatusOK {
		t.Fatalf("delete user = %d, body=%s", w.Code, w.Body.String())
	}
	w = do(r, http.MethodGet, basePath+"/users", "", tok, "")
	decodeBody(t, w, &lst)
	if len(lst.Data) != 0 {
		t.Fatalf("list after delete = %+v", lst.Data)
	}
	// 删除不存在 → store 报错 → 500
	if w := do(r, http.MethodDelete, basePath+"/users/9999", "", tok, ""); w.Code != http.StatusInternalServerError {
		t.Fatalf("delete missing = %d, want 500", w.Code)
	}
}

// ---- channels.go ----

func TestChannelsCRUD(t *testing.T) {
	a := newApp(t)
	r := newRouter(a, nil)
	tok := testAdminToken

	// 创建（含 keys 对象数组 + extra_headers + 默认值）
	cb := `{"name":"ds1","base_url":"https://api.example.com/v1","priority":1,"weight":0,"timeout_ms":0,
		"extra_headers":{"x-header":"v1"},"keys":[{"name":"主号","source":"官网","key":"sk-zzztail"}]}`
	w := do(r, http.MethodPost, basePath+"/channels", cb, tok, "")
	if w.Code != http.StatusOK {
		t.Fatalf("create channel = %d, body=%s", w.Code, w.Body.String())
	}
	var cid struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	decodeBody(t, w, &cid)
	chID := cid.Data.ID

	// get channel：校验默认值
	w = do(r, http.MethodGet, basePath+"/channels/"+fmt.Sprint(chID), "", tok, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get channel = %d", w.Code)
	}
	var ch struct {
		Data models.Channel `json:"data"`
	}
	decodeBody(t, w, &ch)
	if ch.Data.Adapter != "openai" || ch.Data.TimeoutMS != 60000 || ch.Data.Weight != 1 || ch.Data.Enabled != 1 {
		t.Fatalf("channel defaults wrong: %+v", ch.Data)
	}

	// list keys：加密落库 + 回显尾号 + effective_enabled
	w = do(r, http.MethodGet, basePath+"/channels/"+fmt.Sprint(chID)+"/keys", "", tok, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list keys = %d, body=%s", w.Code, w.Body.String())
	}
	var kl struct {
		Data []keyView `json:"data"`
	}
	decodeBody(t, w, &kl)
	if len(kl.Data) != 1 {
		t.Fatalf("expected 1 key, got %d", len(kl.Data))
	}
	if kl.Data[0].KeyTail != "tail" || kl.Data[0].Masked != "sk-****" || kl.Data[0].EffectiveEnabled != 1 {
		t.Fatalf("key view wrong: %+v", kl.Data[0])
	}

	// list channels
	if w := do(r, http.MethodGet, basePath+"/channels", "", tok, ""); w.Code != http.StatusOK {
		t.Fatalf("list channels = %d", w.Code)
	}

	// 更新：部分字段 + weight<=0 归一为 1
	w = do(r, http.MethodPut, basePath+"/channels/"+fmt.Sprint(chID), `{"enabled":1,"weight":-5,"base_url":"https://other.example.com/v1"}`, tok, "")
	if w.Code != http.StatusOK {
		t.Fatalf("update channel = %d, body=%s", w.Code, w.Body.String())
	}
	var uc struct {
		Data models.Channel `json:"data"`
	}
	decodeBody(t, w, &uc)
	if uc.Data.Weight != 1 || uc.Data.Name != "ds1" {
		t.Fatalf("partial update wrong: %+v", uc.Data)
	}

	if w := do(r, http.MethodPut, basePath+"/channels/9999", `{"enabled":0}`, tok, ""); w.Code != http.StatusNotFound {
		t.Fatalf("update missing = %d, want 404", w.Code)
	}
	if w := do(r, http.MethodGet, basePath+"/channels/9999", "", tok, ""); w.Code != http.StatusNotFound {
		t.Fatalf("get missing = %d, want 404", w.Code)
	}

	// 删除成功 + 删除不存在 → 404
	if w := do(r, http.MethodDelete, basePath+"/channels/"+fmt.Sprint(chID), "", tok, ""); w.Code != http.StatusOK {
		t.Fatalf("delete channel = %d, body=%s", w.Code, w.Body.String())
	}
	if w := do(r, http.MethodDelete, basePath+"/channels/9999", "", tok, ""); w.Code != http.StatusNotFound {
		t.Fatalf("delete missing = %d, want 404", w.Code)
	}
}

func TestChannelValidationAndKeys(t *testing.T) {
	a := newApp(t)
	r := newRouter(a, nil)
	tok := testAdminToken

	if w := do(r, http.MethodPost, basePath+"/channels", `{"name":""}`, tok, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("create missing name = %d, want 400", w.Code)
	}
	if w := do(r, http.MethodPost, basePath+"/channels", `{bad`, tok, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("create bad body = %d, want 400", w.Code)
	}
	if w := do(r, http.MethodPost, basePath+"/channels",
		`{"name":"c","base_url":"http://x","keys":[{"key":""}]}`, tok, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("create empty key = %d, want 400", w.Code)
	}
	if w := do(r, http.MethodPost, basePath+"/channels",
		`{"name":"c","base_url":"http://x","keys":"not-array"}`, tok, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("create bad keys = %d, want 400", w.Code)
	}

	// 干净渠道用于测 Key 池
	w := do(r, http.MethodPost, basePath+"/channels", `{"name":"c2","base_url":"http://x"}`, tok, "")
	var cid struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	decodeBody(t, w, &cid)
	chID := cid.Data.ID
	keysPath := basePath + "/channels/" + fmt.Sprint(chID) + "/keys"

	// addKeys 字符串数组形式
	w = do(r, http.MethodPost, keysPath, `{"keys":["sk-aaaa1234","sk-bbbb5678"]}`, tok, "")
	if w.Code != http.StatusOK {
		t.Fatalf("add keys = %d, body=%s", w.Code, w.Body.String())
	}
	var added struct {
		Data struct {
			Added int `json:"added"`
		} `json:"data"`
	}
	decodeBody(t, w, &added)
	if added.Data.Added != 2 {
		t.Fatalf("added = %d, want 2", added.Data.Added)
	}

	w = do(r, http.MethodGet, basePath+"/channels/"+fmt.Sprint(chID)+"/keys", "", tok, "")
	var kl struct {
		Data []keyView `json:"data"`
	}
	decodeBody(t, w, &kl)
	if len(kl.Data) != 2 || kl.Data[0].KeyTail != "1234" || kl.Data[1].KeyTail != "5678" {
		t.Fatalf("key tails wrong: %+v", kl.Data)
	}

	if w := do(r, http.MethodPost, keysPath, `{"keys":[]}`, tok, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("add empty keys = %d, want 400", w.Code)
	}
	if w := do(r, http.MethodPost, keysPath, `{"keys":[{"key":""}]}`, tok, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("add empty key = %d, want 400", w.Code)
	}
	if w := do(r, http.MethodPost, keysPath, `{"keys":"nope"}`, tok, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("add bad keys = %d, want 400", w.Code)
	}

	// updateKey：改名 + 换 key 密文
	keyID := kl.Data[0].ID
	w = do(r, http.MethodPut, basePath+"/channels/keys/"+fmt.Sprint(keyID), `{"name":"main","key":"sk-newtail","enabled":1,"weight":3}`, tok, "")
	if w.Code != http.StatusOK {
		t.Fatalf("update key = %d, body=%s", w.Code, w.Body.String())
	}
	var kv struct {
		Data keyView `json:"data"`
	}
	decodeBody(t, w, &kv)
	if kv.Data.KeyTail != "tail" || kv.Data.Name != "main" || kv.Data.Weight != 3 {
		t.Fatalf("update key result = %+v", kv.Data)
	}

	if w := do(r, http.MethodPut, basePath+"/channels/keys/9999", `{"name":"x"}`, tok, ""); w.Code != http.StatusNotFound {
		t.Fatalf("update missing key = %d, want 404", w.Code)
	}
	if w := do(r, http.MethodPut, basePath+"/channels/keys/abc", `{"name":"x"}`, tok, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("update bad key id = %d, want 400", w.Code)
	}

	if w := do(r, http.MethodDelete, basePath+"/channels/keys/"+fmt.Sprint(keyID), "", tok, ""); w.Code != http.StatusOK {
		t.Fatalf("delete key = %d, body=%s", w.Code, w.Body.String())
	}
	if w := do(r, http.MethodDelete, basePath+"/channels/keys/9999", "", tok, ""); w.Code != http.StatusInternalServerError {
		t.Fatalf("delete missing key = %d, want 500", w.Code)
	}
}

// ---- testChannel（httptest 上游 stub） ----

func TestTestChannel(t *testing.T) {
	a := newApp(t)
	r := newRouter(a, nil)
	tok := testAdminToken

	var gotAuth string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotAuth = req.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"data":[{"id":"deepseek-chat"}]}`)
	}))
	defer up.Close()

	// 上游健康
	chID := createChannelRaw(t, r, tok, up.URL)
	if w := do(r, http.MethodPost, basePath+"/channels/"+fmt.Sprint(chID)+"/keys", `{"keys":["sk-upstreamkey"]}`, tok, ""); w.Code != http.StatusOK {
		t.Fatalf("add key: %d %s", w.Code, w.Body.String())
	}
	w := do(r, http.MethodPost, basePath+"/channels/"+fmt.Sprint(chID)+"/test", "", tok, "")
	if w.Code != http.StatusOK {
		t.Fatalf("test channel = %d, body=%s", w.Code, w.Body.String())
	}
	var tr struct {
		Data struct {
			OK     bool   `json:"ok"`
			Status int    `json:"status"`
			Error  string `json:"error"`
		} `json:"data"`
	}
	decodeBody(t, w, &tr)
	if !tr.Data.OK || tr.Data.Status != http.StatusOK {
		t.Fatalf("test result = %+v", tr.Data)
	}
	if !strings.HasSuffix(gotAuth, "sk-upstreamkey") {
		t.Fatalf("upstream auth = %q", gotAuth)
	}

	// 上游 5xx → ok=false
	upErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "boom")
	}))
	defer upErr.Close()
	chBad := createChannelRaw(t, r, tok, upErr.URL)
	do(r, http.MethodPost, basePath+"/channels/"+fmt.Sprint(chBad)+"/keys", `{"keys":["sk-x"]}`, tok, "")
	w = do(r, http.MethodPost, basePath+"/channels/"+fmt.Sprint(chBad)+"/test", "", tok, "")
	decodeBody(t, w, &tr)
	if tr.Data.OK || tr.Data.Status != http.StatusInternalServerError {
		t.Fatalf("test(500) result = %+v", tr.Data)
	}

	// 渠道不存在 → 404
	if w := do(r, http.MethodPost, basePath+"/channels/9999/test", "", tok, ""); w.Code != http.StatusNotFound {
		t.Fatalf("test missing = %d, want 404", w.Code)
	}

	// 无启用 key → 400
	chNokey := createChannelRaw(t, r, tok, up.URL)
	if w := do(r, http.MethodPost, basePath+"/channels/"+fmt.Sprint(chNokey)+"/test", "", tok, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("test no key = %d, want 400", w.Code)
	}

	// 未知 adapter → 400
	chBadAdp := createChannelRaw(t, r, tok, up.URL)
	do(r, http.MethodPost, basePath+"/channels/"+fmt.Sprint(chBadAdp)+"/keys", `{"keys":["sk-x"]}`, tok, "")
	do(r, http.MethodPut, basePath+"/channels/"+fmt.Sprint(chBadAdp), `{"adapter":"nonexistent"}`, tok, "")
	if w := do(r, http.MethodPost, basePath+"/channels/"+fmt.Sprint(chBadAdp)+"/test", "", tok, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("test bad adapter = %d, want 400", w.Code)
	}
}

func createChannelRaw(t *testing.T, r *gin.Engine, tok, baseURL string) int64 {
	t.Helper()
	w := do(r, http.MethodPost, basePath+"/channels",
		`{"name":"c","base_url":"`+baseURL+`","adapter":"openai","timeout_ms":5000}`, tok, "")
	if w.Code != http.StatusOK {
		t.Fatalf("create channel raw = %d, body=%s", w.Code, w.Body.String())
	}
	var cid struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	decodeBody(t, w, &cid)
	return cid.Data.ID
}

// ---- model_routes.go ----

func TestModelRoutesCRUD(t *testing.T) {
	a := newApp(t)
	r := newRouter(a, nil)
	tok := testAdminToken

	chID := createChannelRaw(t, r, tok, "https://x.example.com/v1")

	cb := fmt.Sprintf(`{"display_name":"m1","channel_id":%d,"upstream_model":"up-m1","priority":2,"weight":-1,"enabled":1,"price_input":0.1,"price_output":0.2}`, chID)
	w := do(r, http.MethodPost, basePath+"/model-routes", cb, tok, "")
	if w.Code != http.StatusOK {
		t.Fatalf("create route = %d, body=%s", w.Code, w.Body.String())
	}
	var rid struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	decodeBody(t, w, &rid)
	routeID := rid.Data.ID

	if w := do(r, http.MethodPost, basePath+"/model-routes", `{"display_name":""}`, tok, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("create route missing = %d, want 400", w.Code)
	}

	w = do(r, http.MethodGet, basePath+"/model-routes", "", tok, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list routes = %d", w.Code)
	}
	var rl struct {
		Data []routeJSON `json:"data"`
	}
	decodeBody(t, w, &rl)
	if len(rl.Data) != 1 || rl.Data[0].Weight != 1 || rl.Data[0].ChannelEnabled != 1 || rl.Data[0].EffectiveEnabled != 1 {
		t.Fatalf("list route wrong: %+v", rl.Data)
	}

	// 更新：归一化 + 部分字段
	w = do(r, http.MethodPut, basePath+"/model-routes/"+fmt.Sprint(routeID), `{"weight":0,"enabled":0,"display_name":"m2"}`, tok, "")
	if w.Code != http.StatusOK {
		t.Fatalf("update route = %d, body=%s", w.Code, w.Body.String())
	}
	var uv struct {
		Data routeJSON `json:"data"`
	}
	decodeBody(t, w, &uv)
	if uv.Data.Weight != 1 || uv.Data.DisplayName != "m2" || uv.Data.Enabled != 0 || uv.Data.EffectiveEnabled != 0 {
		t.Fatalf("update route result = %+v", uv.Data)
	}

	if w := do(r, http.MethodPut, basePath+"/model-routes/9999", `{"enabled":0}`, tok, ""); w.Code != http.StatusNotFound {
		t.Fatalf("update missing route = %d, want 404", w.Code)
	}
	if w := do(r, http.MethodPut, basePath+"/model-routes/abc", `{"enabled":0}`, tok, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("update bad route id = %d, want 400", w.Code)
	}

	if w := do(r, http.MethodDelete, basePath+"/model-routes/"+fmt.Sprint(routeID), "", tok, ""); w.Code != http.StatusOK {
		t.Fatalf("delete route = %d, body=%s", w.Code, w.Body.String())
	}
	if w := do(r, http.MethodDelete, basePath+"/model-routes/9999", "", tok, ""); w.Code != http.StatusNotFound {
		t.Fatalf("delete missing route = %d, want 404", w.Code)
	}
}

// ---- logs.go：查询 / 统计 / 设置 ----

// seedLogs 经 store 插入日志数据，保证 created_at=今天、联表有渠道与用户。
func seedLogs(t *testing.T, a *app.App) {
	t.Helper()
	db := a.DB

	if _, err := db.Exec(`INSERT INTO users (name, remark, token_hash, token_enc, quota_limit, quota_used, status, created_at, updated_at)
		VALUES ('u1','','','',0,0,1,1,1)`); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	var logs []*models.RequestLog
	rows := []struct {
		model, status string
		tokens        int64
		cost          float64
	}{
		{"m-a", "success", 100, 0.5},
		{"m-a", "success", 200, 1.0},
		{"m-b", "error", 50, 0.0},
	}
	for _, x := range rows {
		logs = append(logs, &models.RequestLog{
			UserID: 1, DisplayModel: x.model, UpstreamModel: "up",
			PromptTokens: 10, CompletionTokens: 5, TotalTokens: x.tokens,
			Cost: x.cost, LatencyMS: 20, Status: x.status, CreatedAt: models.Now(),
		})
	}
	if err := store.InsertLogs(db, logs); err != nil {
		t.Fatalf("insert logs: %v", err)
	}

	// 追加一条带 channel_id 的（覆盖 channel 联表统计）
	chID := int64(7)
	if _, err := db.Exec(`INSERT INTO channels (name, base_url, adapter, priority, weight, timeout_ms, enabled, health_state, created_at, updated_at)
		VALUES ('chanA','http://x','openai',0,1,1000,1,'healthy',1,1)`); err != nil {
		t.Fatalf("seed channel: %v", err)
	} else {
		_ = db.QueryRow(`SELECT id FROM channels ORDER BY id DESC LIMIT 1`).Scan(&chID)
	}
	if _, err := db.Exec(`INSERT INTO request_logs (user_id, display_model, channel_id, upstream_model, prompt_tokens, completion_tokens, total_tokens, cost, latency_ms, status, created_at)
		VALUES (1,'m-a',?, 'up', 10, 5, 15, 0.2, 10, 'success', ?)`, chID, models.Now()); err != nil {
		t.Fatalf("seed channel log: %v", err)
	}
}

func TestLogsQueryAndUsage(t *testing.T) {
	a := newApp(t)
	r := newRouter(a, nil)
	tok := testAdminToken
	seedLogs(t, a)

	// 查询日志（带过滤）
	w := do(r, http.MethodGet, basePath+"/logs?display_model=m-a&status=success&limit=50&offset=0", "", tok, "")
	if w.Code != http.StatusOK {
		t.Fatalf("query logs = %d, body=%s", w.Code, w.Body.String())
	}
	var ql struct {
		Data []json.RawMessage `json:"data"`
	}
	decodeBody(t, w, &ql)
	if len(ql.Data) < 1 {
		t.Fatal("query logs returned none")
	}

	// usage / by-user-model（默认 7 天）
	if w := do(r, http.MethodGet, basePath+"/logs/usage/by-user-model", "", tok, ""); w.Code != http.StatusOK {
		t.Fatalf("usage by-user-model = %d, body=%s", w.Code, w.Body.String())
	}
	// days=0（全部）
	if w := do(r, http.MethodGet, basePath+"/logs/usage/by-user-model?days=0", "", tok, ""); w.Code != http.StatusOK {
		t.Fatalf("usage by-user-model days=0 = %d", w.Code)
	}

	// usage / by-model（今天）
	w = do(r, http.MethodGet, basePath+"/logs/usage/by-model", "", tok, "")
	if w.Code != http.StatusOK {
		t.Fatalf("usage by-model = %d, body=%s", w.Code, w.Body.String())
	}
	var bm struct {
		Data struct {
			Date   string            `json:"date"`
			Models []json.RawMessage `json:"models"`
		} `json:"data"`
	}
	decodeBody(t, w, &bm)
	if bm.Data.Date == "" {
		t.Fatal("by-model date empty")
	}
	// by-model 指定 date
	if w := do(r, http.MethodGet, basePath+"/logs/usage/by-model?date="+bm.Data.Date, "", tok, ""); w.Code != http.StatusOK {
		t.Fatalf("by-model specified date = %d", w.Code)
	}
	// 非法 date → 400
	if w := do(r, http.MethodGet, basePath+"/logs/usage/by-model?date=bad", "", tok, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("by-model bad date = %d, want 400", w.Code)
	}

	// usage / dashboard
	w = do(r, http.MethodGet, basePath+"/logs/usage/dashboard", "", tok, "")
	if w.Code != http.StatusOK {
		t.Fatalf("dashboard = %d, body=%s", w.Code, w.Body.String())
	}
	var dbp struct {
		Data dashboardPayload `json:"data"`
	}
	decodeBody(t, w, &dbp)
	if len(dbp.Data.Week) != 7 {
		t.Fatalf("dashboard week len = %d, want 7", len(dbp.Data.Week))
	}
	if dbp.Data.Today.TotalRequests == 0 {
		t.Fatalf("dashboard today should have requests: %+v", dbp.Data.Today)
	}
}

// ---- 纯工具函数直接单测 ----

func TestUtilityHelpers(t *testing.T) {
	// truncateStr：短串原样返回，长串截断加省略号
	if got := truncateStr("abc", 10); got != "abc" {
		t.Fatalf("truncateStr short = %q", got)
	}
	if got := truncateStr("abcdefghij", 5); got != "abcde..." {
		t.Fatalf("truncateStr long = %q", got)
	}

	// normalizeWeight：<=0 → 1，否则原样
	if got := normalizeWeight(0); got != 1 {
		t.Fatalf("normalizeWeight(0) = %d", got)
	}
	if got := normalizeWeight(-7); got != 1 {
		t.Fatalf("normalizeWeight(-7) = %d", got)
	}
	if got := normalizeWeight(4); got != 4 {
		t.Fatalf("normalizeWeight(4) = %d", got)
	}
}

// ---- settings ----

func TestSettings(t *testing.T) {
	a := newApp(t)
	r := newRouter(a, nil)
	tok := testAdminToken

	w := do(r, http.MethodGet, basePath+"/settings", "", tok, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get settings = %d, body=%s", w.Code, w.Body.String())
	}
	var got struct {
		Data []SettingItem `json:"data"`
	}
	decodeBody(t, w, &got)
	m := map[string]string{}
	for _, it := range got.Data {
		m[it.Key] = it.Value
	}
	if m["default_timeout_ms"] != "60000" || m["log_retain_days"] != "30" || m["gateway_prefix"] != "/" {
		t.Fatalf("settings defaults wrong: %+v", m)
	}

	// 更新合法配置
	w = do(r, http.MethodPut, basePath+"/settings", `{"data":[{"key":"default_timeout_ms","value":"90000"},{"key":"log_retain_days","value":"15"}]}`, tok, "")
	if w.Code != http.StatusOK {
		t.Fatalf("update settings = %d, body=%s", w.Code, w.Body.String())
	}
	// 读回确认
	w = do(r, http.MethodGet, basePath+"/settings", "", tok, "")
	decodeBody(t, w, &got)
	for _, it := range got.Data {
		if it.Key == "default_timeout_ms" && it.Value != "90000" {
			t.Fatalf("timeout setting not updated: %+v", it)
		}
	}

	// 非法 key → 400
	if w := do(r, http.MethodPut, basePath+"/settings", `{"data":[{"key":"admin_token_hash","value":"x"}]}`, tok, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("update readonly setting = %d, want 400", w.Code)
	}
	// 非法 body → 400
	if w := do(r, http.MethodPut, basePath+"/settings", `{bad`, tok, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("update settings bad body = %d, want 400", w.Code)
	}
}
