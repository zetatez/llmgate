package apiv1

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// 登录失败限速参数：同一来源在 window 内失败达 threshold 次后，锁定 lockout 时长。
// 目标不是绝对防爆破（那需要验证码/WAF），而是挡住脚本化的高频口令猜测并降低噪声。
const (
	loginFailThreshold = 8
	loginFailWindow    = 10 * time.Minute
	loginLockout       = 10 * time.Minute
	loginMaxKeys       = 4096 // 限速表上限，超过则清理过期项
)

type loginAttempt struct {
	fails int
	first time.Time
	until time.Time
}

// loginLimiter 内存态登录失败限速器（单进程、并发安全）。
type loginLimiter struct {
	mu  sync.Mutex
	m   map[string]*loginAttempt
	now func() time.Time // 便于测试注入
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{m: map[string]*loginAttempt{}, now: time.Now}
}

// allow 当前来源是否允许尝试登录（处于锁定期则拒绝）。
func (l *loginLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.m[key]
	if a == nil {
		return true
	}
	n := l.now()
	if a.until.After(n) {
		return false
	}
	// 锁定已到期且窗口已过：清理
	if n.Sub(a.first) > loginFailWindow {
		delete(l.m, key)
	}
	return true
}

// fail 记录一次登录失败；达阈值则进入锁定。
func (l *loginLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := l.now()
	a := l.m[key]
	if a == nil || n.Sub(a.first) > loginFailWindow {
		a = &loginAttempt{first: n}
		l.m[key] = a
	}
	a.fails++
	if a.fails >= loginFailThreshold {
		a.until = n.Add(loginLockout)
		a.fails = 0
		a.first = n
	}
	if len(l.m) > loginMaxKeys {
		l.sweepLocked(n)
	}
}

// reset 登录成功后清除该来源的失败计数。
func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, key)
}

func (l *loginLimiter) sweepLocked(n time.Time) {
	for k, a := range l.m {
		if !a.until.After(n) && n.Sub(a.first) > loginFailWindow {
			delete(l.m, k)
		}
	}
}

// authLimiter 进程级共享的登录限速器（管理员端与用户端共用）。
var authLimiter = newLoginLimiter()

// remoteKey 以客户端 IP 作为限速键（防单点爆破）。
func remoteKey(c *gin.Context) string { return c.ClientIP() }

// tooManyLogins 返回标准 429 响应（带 Retry-After）。
func tooManyLogins(c *gin.Context) {
	c.Header("Retry-After", fmt.Sprintf("%d", int(loginLockout/time.Second)))
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "too many attempts, please retry later"})
}

// isSecureRequest 判断连接是否应视为 HTTPS（用于决定 Cookie 是否带 Secure）。
// 直连 TLS，或反代透传 X-Forwarded-Proto=https 时视为安全。
// 仅当该头存在且为 https 时才置 Secure——即使被伪造，后果也只是"Cookie 在明文下不生效"（伤不到服务端）。
func isSecureRequest(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	return strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
}
