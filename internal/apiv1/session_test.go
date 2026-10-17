package apiv1

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"llmgate/internal/models"
	"llmgate/internal/store"
)

func adminLoginCookie(t *testing.T, r http.Handler) *http.Cookie {
	t.Helper()
	w := do(r, http.MethodPost, basePath+"/login", "", testAdminToken, "")
	if w.Code != http.StatusOK {
		t.Fatalf("admin login = %d, body=%s", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == adminSessionCookie {
			return c
		}
	}
	t.Fatal("expected admin session cookie")
	return nil
}

func TestSessionProbe(t *testing.T) {
	a := newApp(t)
	r := newPortalRouter(a)
	adminCreateUser(t, r, "alice")

	role := func(cookie *http.Cookie) string {
		w := doCookie(r, http.MethodGet, "/api/session", "", cookie)
		if w.Code != http.StatusOK {
			t.Fatalf("session probe = %d", w.Code)
		}
		var sp struct {
			Role string `json:"role"`
		}
		decodeBody(t, w, &sp)
		return sp.Role
	}

	if got := role(nil); got != "anonymous" {
		t.Fatalf("anonymous role = %q", got)
	}
	if got := role(userLoginCookie(t, r, "alice", "secret1")); got != "user" {
		t.Fatalf("user role = %q", got)
	}
	if got := role(adminLoginCookie(t, r)); got != "admin" {
		t.Fatalf("admin role = %q", got)
	}
}

func TestAdminLogoutRevokesSession(t *testing.T) {
	a := newApp(t)
	r := newPortalRouter(a)
	cookie := adminLoginCookie(t, r)

	if w := doCookie(r, http.MethodGet, basePath+"/ping", "", cookie); w.Code != http.StatusOK {
		t.Fatalf("ping with session = %d", w.Code)
	}
	if w := doCookie(r, http.MethodPost, basePath+"/logout", "", cookie); w.Code != http.StatusOK {
		t.Fatalf("logout = %d", w.Code)
	}
	// 旧 Cookie 已被服务端吊销
	if w := doCookie(r, http.MethodGet, basePath+"/ping", "", cookie); w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session ping = %d, want 401", w.Code)
	}
}

func TestUserLogoutAll(t *testing.T) {
	a := newApp(t)
	r := newPortalRouter(a)
	adminCreateUser(t, r, "alice")
	c1 := userLoginCookie(t, r, "alice", "secret1")
	c2 := userLoginCookie(t, r, "alice", "secret1")

	if w := doCookie(r, http.MethodGet, "/api/user/me", "", c2); w.Code != http.StatusOK {
		t.Fatalf("session 2 me = %d", w.Code)
	}
	if w := doCookie(r, http.MethodPost, "/api/user/logout-all", "", c1); w.Code != http.StatusOK {
		t.Fatalf("logout-all = %d, body=%s", w.Code, w.Body.String())
	}
	// 两个会话都应失效
	if w := doCookie(r, http.MethodGet, "/api/user/me", "", c2); w.Code != http.StatusUnauthorized {
		t.Fatalf("session 2 after logout-all = %d, want 401", w.Code)
	}
	if w := doCookie(r, http.MethodGet, "/api/user/me", "", c1); w.Code != http.StatusUnauthorized {
		t.Fatalf("session 1 after logout-all = %d, want 401", w.Code)
	}
}

func TestAdminLogsCountAndUserOverview(t *testing.T) {
	a := newApp(t)
	r := newPortalRouter(a)
	uid := adminCreateUser(t, r, "alice")
	if err := store.InsertLogs(a.DB, []*models.RequestLog{
		{UserID: uid, DisplayModel: "m", Status: "success", TotalTokens: 5, CreatedAt: models.Now()},
	}); err != nil {
		t.Fatal(err)
	}
	ac := adminLoginCookie(t, r)

	// 日志计数
	w := doCookie(r, http.MethodGet, basePath+"/logs/count", "", ac)
	if w.Code != http.StatusOK {
		t.Fatalf("logs count = %d", w.Code)
	}
	var cnt struct {
		Data int64 `json:"data"`
	}
	decodeBody(t, w, &cnt)
	if cnt.Data != 1 {
		t.Fatalf("logs count = %d, want 1", cnt.Data)
	}

	// 用户聚合视图
	w = doCookie(r, http.MethodGet, fmt.Sprintf("%s/users/%d/overview", basePath, uid), "", ac)
	if w.Code != http.StatusOK {
		t.Fatalf("user overview = %d, body=%s", w.Code, w.Body.String())
	}
	var ov struct {
		Data struct {
			User struct {
				Username string `json:"username"`
			} `json:"user"`
			Total struct {
				TotalRequests int64 `json:"total_requests"`
			} `json:"total"`
		} `json:"data"`
	}
	decodeBody(t, w, &ov)
	if ov.Data.User.Username != "alice" || ov.Data.Total.TotalRequests != 1 {
		t.Fatalf("user overview payload = %+v", ov.Data)
	}

	// 不存在用户 → 404
	if w := doCookie(r, http.MethodGet, basePath+"/users/9999/overview", "", ac); w.Code != http.StatusNotFound {
		t.Fatalf("missing user overview = %d, want 404", w.Code)
	}
}

// TestLoginSwitchesRole 同一浏览器登录另一类身份时，应吊销并清除旧身份会话，
// 避免 /api/session 优先级导致页面被带到错误的一端。
func TestLoginSwitchesRole(t *testing.T) {
	a := newApp(t)
	r := newPortalRouter(a)
	adminCreateUser(t, r, "alice")

	adminCookie := adminLoginCookie(t, r)

	// 模拟浏览器已持有 admin Cookie，再提交用户登录
	body := `{"username":"alice","password":"secret1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/user/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("user login = %d, body=%s", w.Code, w.Body.String())
	}
	var userCookie *http.Cookie
	for _, ck := range w.Result().Cookies() {
		if ck.Name == userSessionCookie {
			userCookie = ck
		}
	}
	if userCookie == nil {
		t.Fatal("expected user session cookie")
	}

	// 浏览器同时携带两个 Cookie：admin 会话已被吊销，应返回 user
	req = httptest.NewRequest(http.MethodGet, "/api/session", nil)
	req.AddCookie(adminCookie)
	req.AddCookie(userCookie)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var sp struct {
		Role string `json:"role"`
	}
	decodeBody(t, w, &sp)
	if sp.Role != "user" {
		t.Fatalf("role after user login = %q, want user", sp.Role)
	}

	// 反向：已持有 user Cookie 时登录 admin，应吊销用户会话
	req = httptest.NewRequest(http.MethodPost, "/api/admin/login", nil)
	req.Header.Set("Authorization", "Bearer "+testAdminToken)
	req.AddCookie(userCookie)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin login = %d, body=%s", w.Code, w.Body.String())
	}
	var adminCookie2 *http.Cookie
	for _, ck := range w.Result().Cookies() {
		if ck.Name == adminSessionCookie {
			adminCookie2 = ck
		}
	}
	if adminCookie2 == nil {
		t.Fatal("expected admin session cookie")
	}
	req = httptest.NewRequest(http.MethodGet, "/api/session", nil)
	req.AddCookie(userCookie)
	req.AddCookie(adminCookie2)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	decodeBody(t, w, &sp)
	if sp.Role != "admin" {
		t.Fatalf("role after admin login = %q, want admin", sp.Role)
	}
}
