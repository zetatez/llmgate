package gateway

import (
	"errors"
	"net/http"
	"testing"

	"llmgate/internal/router"
)

// 网络耗尽 / 熔断可用性 → 503（客户端退避重试）；模型未配置 → 404（不应重试）。
func TestMapRouteError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code int
	}{
		{"no candidate", router.ErrNoCandidate, http.StatusNotFound},
		{"channel busy", router.ErrChannelBusy, http.StatusServiceUnavailable},
		{"network exhausted", router.ErrNetworkExhausted, http.StatusServiceUnavailable},
		{"generic", errors.New("boom"), http.StatusBadGateway},
	}
	for _, c := range cases {
		got, _ := mapRouteError(c.err)
		if got != c.code {
			t.Errorf("%s: got %d want %d", c.name, got, c.code)
		}
	}
}

func TestRetryAfter(t *testing.T) {
	if got := retryAfterFor(http.StatusServiceUnavailable); got != "5" {
		t.Fatalf("503 should carry Retry-After=5, got %q", got)
	}
	if got := retryAfterFor(http.StatusNotFound); got != "" {
		t.Fatalf("404 should carry no Retry-After, got %q", got)
	}
}
