package apiv1

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLoginLimiterUnit(t *testing.T) {
	now := time.Now()
	l := newLoginLimiter()
	l.now = func() time.Time { return now }

	if !l.allow("k") {
		t.Fatal("fresh key should be allowed")
	}
	for i := 0; i < loginFailThreshold; i++ {
		l.fail("k")
	}
	if l.allow("k") {
		t.Fatal("key should be locked after reaching threshold")
	}
	// 其它 key 不受影响
	if !l.allow("other") {
		t.Fatal("other key should not be locked")
	}
	// 锁定期过后恢复
	now = now.Add(loginLockout + time.Second)
	if !l.allow("k") {
		t.Fatal("key should be allowed after lockout expires")
	}
	// reset 生效
	l.fail("k")
	l.reset("k")
	if !l.allow("k") {
		t.Fatal("reset should clear the counter")
	}
}

// doRemote 发起来自指定 RemoteAddr 的请求（隔离限速键，避免污染其它用例）。
func doRemote(r http.Handler, method, path, body, token, remote string) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if remote != "" {
		req.RemoteAddr = remote
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAdminLoginRateLimited(t *testing.T) {
	a := newApp(t)
	r := newRouter(a, nil)
	const ip = "198.51.100.11:2222"

	for i := 0; i < loginFailThreshold; i++ {
		if w := doRemote(r, http.MethodPost, basePath+"/login", "", "wrong-token", ip); w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401", i, w.Code)
		}
	}
	// 达到阈值后：即使令牌正确也应被限速拒绝
	if w := doRemote(r, http.MethodPost, basePath+"/login", "", testAdminToken, ip); w.Code != http.StatusTooManyRequests {
		t.Fatalf("locked admin login = %d, want 429", w.Code)
	}
	if w := doRemote(r, http.MethodPost, basePath+"/login", "", "wrong-token", ip); w.Code != http.StatusTooManyRequests {
		t.Fatalf("locked admin login (wrong) = %d, want 429", w.Code)
	}
	// 其它来源不受影响
	if w := doRemote(r, http.MethodPost, basePath+"/login", "", testAdminToken, "198.51.100.12:2222"); w.Code != http.StatusOK {
		t.Fatalf("other ip admin login = %d, want 200", w.Code)
	}
}

func TestUserLoginRateLimitedPerUsername(t *testing.T) {
	a := newApp(t)
	r := newPortalRouter(a)
	const ip = "198.51.100.21:3333"

	for i := 0; i < loginFailThreshold; i++ {
		if w := doRemote(r, http.MethodPost, "/api/user/login", `{"username":"target","password":"x"}`, "", ip); w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401", i, w.Code)
		}
	}
	if w := doRemote(r, http.MethodPost, "/api/user/login", `{"username":"target","password":"secret1"}`, "", ip); w.Code != http.StatusTooManyRequests {
		t.Fatalf("locked user login = %d, want 429", w.Code)
	}
	// 同 IP 的不同用户名不被连带锁定（按 IP+用户名 计数）
	if w := doRemote(r, http.MethodPost, "/api/user/login", `{"username":"other","password":"x"}`, "", ip); w.Code != http.StatusUnauthorized {
		t.Fatalf("other username same ip = %d, want 401 (not 429)", w.Code)
	}
}

func TestSecureCookieBehindProxy(t *testing.T) {
	a := newApp(t)
	r := newRouter(a, nil)

	// X-Forwarded-Proto: https → 会话 Cookie 应带 Secure
	req := httptest.NewRequest(http.MethodPost, basePath+"/login", nil)
	req.Header.Set("Authorization", "Bearer "+testAdminToken)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.RemoteAddr = "198.51.100.31:4444"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login via proxy = %d, body=%s", w.Code, w.Body.String())
	}
	var secureSeen bool
	for _, ck := range w.Result().Cookies() {
		if ck.Name == adminSessionCookie {
			secureSeen = ck.Secure
		}
	}
	if !secureSeen {
		t.Fatal("session cookie should be Secure when X-Forwarded-Proto=https")
	}

	// 无该头（明文 HTTP）→ 不应带 Secure
	w2 := doRemote(r, http.MethodPost, basePath+"/login", "", testAdminToken, "198.51.100.32:4444")
	if w2.Code != http.StatusOK {
		t.Fatalf("plain login = %d", w2.Code)
	}
	for _, ck := range w2.Result().Cookies() {
		if ck.Name == adminSessionCookie && ck.Secure {
			t.Fatal("session cookie should not be Secure over plain HTTP")
		}
	}
}
