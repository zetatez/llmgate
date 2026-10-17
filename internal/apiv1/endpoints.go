package apiv1

import (
	"net"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"llmgate/internal/app"
)

// gatewayEndpoints 返回所有可用的网关 Base URL（当前访问域名 + 本机全部 IP）。
func gatewayEndpoints(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": buildGatewayURLs(a, c)})
	}
}

func buildGatewayURLs(a *app.App, c *gin.Context) []string {
	scheme := "http"
	if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}

	host, port, err := net.SplitHostPort(c.Request.Host)
	if err != nil {
		host = c.Request.Host
		if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
			host = strings.Trim(host, "[]")
		}
		// 请求头没有端口时，回退监听地址中的端口
		if _, p, e := net.SplitHostPort(a.Cfg.HTTPAddr); e == nil {
			port = p
		}
	}

	prefix := strings.TrimRight(a.Cfg.GatewayPrefix, "/")
	path := prefix + "/v1"
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	seen := map[string]bool{}
	hosts := make([]string, 0, 8)
	addHost := func(h string) {
		h = strings.TrimSpace(strings.Trim(h, "[]"))
		if h == "" || seen[h] {
			return
		}
		seen[h] = true
		hosts = append(hosts, h)
	}

	// 当前访问使用的域名优先（不展示 localhost）
	if host != "" && !strings.EqualFold(host, "localhost") && net.ParseIP(host) == nil {
		addHost(host)
	}
	addHost("127.0.0.1")

	// 仅展示 wlan 开头网卡的 IPv4 地址（无线局域网地址）
	if ifaces, err := net.Interfaces(); err == nil {
		ips := make([]string, 0, 4)
		for _, iface := range ifaces {
			if !strings.HasPrefix(strings.ToLower(iface.Name), "wlan") {
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				ipnet, ok := addr.(*net.IPNet)
				if !ok {
					continue
				}
				ip := ipnet.IP.To4()
				if ip == nil || ip.IsUnspecified() || ip.IsLoopback() {
					continue
				}
				ips = append(ips, ip.String())
			}
		}
		sort.Strings(ips)
		for _, ip := range ips {
			addHost(ip)
		}
	}

	out := make([]string, 0, len(hosts))
	for _, h := range hosts {
		hp := h
		if port != "" {
			hp = net.JoinHostPort(h, port)
		}
		out = append(out, scheme+"://"+hp+path)
	}
	return out
}
